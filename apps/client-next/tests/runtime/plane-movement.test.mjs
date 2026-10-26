/*
===========================================================================

plane-movement.test.mjs - the movement doors ask the plane owner (#1793 E)

Parity for the slice-E migrations: projectileSpace, interpolateMovement,
admitPose and pickDestination used to test the dungeon region bit
themselves. Each test re-states the former body with the raw bit test
((regionId & 0x8000) !== 0) and compares it with the migrated door over
outdoor, cave and boundary regions, so an arm swapped in plane.ts or in a
door fails here.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const load = async entry => {
	return import( sourceFileUrl( entry ).href );
};
const { projectileSpace } = await load( "src/engine/foundation/animation/projectile-time.ts" );
const { interpolateMovement } = await load( "src/engine/foundation/gameplay/native-movement.ts" );
const { admitPose } = await load( "src/engine/foundation/gameplay/movement-wire.ts" );
const { pickDestination } = await load( "src/engine/foundation/rendering/pick-destination.ts" );
const { REGION_SIZE, normalizeRegionXZ, sameCoordinateSpace, sectorX, sectorY } = await load(
	"src/engine/foundation/world/world-space.ts"
);

// The former predicate: the dungeon bit the doors tested before #1793.
const wasDungeon = regionId => (regionId & 0x8000) !== 0;

// Outdoors (bit clear), caves (bit set), and both boundaries of the bit.
const REGIONS = [ 0x0001, 0x62a8, 0x7fff, 0x8000, 0x8001, 0x8004, 0xffff ];

test("projectileSpace: two regions share a projection as the bit test did", () => {
	const former = ( start, end ) => !wasDungeon( start ) && !wasDungeon( end ) || start === end;
	for ( const start of REGIONS ) {
		for ( const end of REGIONS ) {
			assert.equal( projectileSpace( start, end ), former( start, end ), `${start} -> ${end}` );
		}
	}
});

test("interpolateMovement: the cave arm lerps locally, the outdoor arm in sector space", () => {
	const former = ( from, to, t ) => {
		if ( !sameCoordinateSpace( from.regionId, to.regionId ) ) {
			return from;
		}
		if ( wasDungeon( from.regionId ) ) {
			return {
				...to,
				x: from.x + (to.x - from.x) * t,
				y: from.y + (to.y - from.y) * t,
				z: from.z + (to.z - from.z) * t
			};
		}
		const wx = from.x + sectorX( from.regionId ) * REGION_SIZE,
			wz = from.z + sectorY( from.regionId ) * REGION_SIZE;
		const x = wx + (to.x + sectorX( to.regionId ) * REGION_SIZE - wx) * t,
			z = wz + (to.z + sectorY( to.regionId ) * REGION_SIZE - wz) * t;
		return {
			regionId: Math.floor( x / REGION_SIZE ) | (Math.floor( z / REGION_SIZE ) << 8),
			x: x % REGION_SIZE,
			y: from.y + (to.y - from.y) * t,
			z: z % REGION_SIZE,
			angle: to.angle
		};
	};
	const pose = ( regionId, x, y, z ) => ({ regionId, x, y, z, angle: 7 });
	const cases = [
		[ pose( 0x8001, -100, 5, 200 ), pose( 0x8001, 300, 25, -400 ) ],
		[ pose( 0x62a8, 100, 5, 200 ), pose( 0x62a9, 300, 25, 400 ) ],
		[ pose( 0x62a8, 100, 5, 200 ), pose( 0x8001, 300, 25, 400 ) ],
		[ pose( 0x8004, 0, 0, 0 ), pose( 0x8004, -32768, -10, 32767 ) ]
	];
	for ( const [from, to] of cases ) {
		for ( const t of [ 0, 0.25, 0.5, 1 ] ) {
			assert.deepEqual( interpolateMovement( from, to, t ), former( from, to, t ) );
		}
	}
});

test("admitPose: a cave pose admits the signed plane, a field pose its cell", () => {
	const admits = ( former, pose ) => {
		try {
			former( pose );
			return true;
		} catch {
			return false;
		}
	};
	const former = pose => {
		const p = pose;
		if (
			!p || !Number.isInteger( p.regionId ) || p.regionId <= 0 || p.regionId > 65535 ||
			![ p.x, p.y, p.z, p.angle ].every( Number.isFinite ) || (wasDungeon( p.regionId ) ?
				p.x < -32768 || p.x > 32767 || p.z < -32768 || p.z > 32767 :
				p.x < 0 || p.x >= REGION_SIZE || p.z < 0 || p.z >= REGION_SIZE) ||
			p.y < -32768 || p.y > 32767 || p.angle < 0 || p.angle > 65535
		) {
			throw new Error( "Invalid movement pose" );
		}
		return Object.freeze( { ...p } );
	};
	const pose = ( regionId, x, z ) => ({ regionId, x, y: 0, z, angle: 0 });
	const cases = [
		pose( 0x8001, -32768, 32767 ),
		pose( 0x8001, 32768, 0 ),
		pose( 0x8001, -1, -1 ),
		pose( 0x62a8, 0, 0 ),
		pose( 0x62a8, 1919.5, 1919.5 ),
		pose( 0x62a8, 1920, 0 ),
		pose( 0x62a8, -1, 0 ),
		pose( 0x8000, 0, 0 ),
		pose( 0x7fff, 0, 0 ),
		pose( 0xffff, 1, 1 )
	];
	for ( const candidate of cases ) {
		assert.equal(
			admits( admitPose, candidate ),
			admits( former, candidate ),
			`${candidate.regionId} ${candidate.x},${candidate.z}`
		);
		if ( admits( admitPose, candidate ) ) {
			assert.deepEqual( admitPose( candidate ), former( candidate ) );
		}
	}
});

test("pickDestination: an outdoor hit normalizes, a cave hit stays local", () => {
	const former = ( ray, depth, originRegion ) => {
		if ( !Number.isFinite( depth ) || depth < 0 || depth > 1 || !originRegion ) return null;
		const rawX = ray.start[0] + ray.delta[0] * depth,
			y = ray.start[1] + ray.delta[1] * depth,
			rawZ = ray.start[2] + ray.delta[2] * depth;
		if ( ![ rawX, y, rawZ ].every( Number.isFinite ) ) return null;
		if ( !wasDungeon( originRegion ) ) {
			const normalized = normalizeRegionXZ( originRegion, rawX, rawZ );
			const rx = sectorX( originRegion ) + Math.floor( rawX / REGION_SIZE ),
				rz = sectorY( originRegion ) + Math.floor( rawZ / REGION_SIZE );
			if ( rx < 0 || rx > 255 || rz < 0 || rz >= 128 ) return null;
			return { regionId: normalized.regionId, x: normalized.x, y, z: normalized.z };
		}
		return { regionId: originRegion, x: rawX, y, z: rawZ };
	};
	const ray = { start: [ 100, 50, 100 ], delta: [ 5000, -10, -300 ] };
	for ( const originRegion of REGIONS ) {
		for ( const depth of [ 0, 0.5, 1 ] ) {
			assert.deepEqual( pickDestination( ray, depth, originRegion ), former( ray, depth, originRegion ) );
		}
	}
});
