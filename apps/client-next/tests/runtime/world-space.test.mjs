/*
===========================================================================

world-space.test.mjs - client/server world-space parity and floor rules

Verifies that foundation/world/world-space.ts matches the shared parity
cases in apps/server/internal/game/world/testdata/world-space-cases.json
(worldgeom), native dungeon offset suppression
(SWorld_CalculateRegionOffsetVector, 0x00888140), and dungeon floor
admission (CObj_IsVisibleInSector, 0x00863330).

===========================================================================
*/

import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { minimapPoseKey } = await import( "../../src/engine/foundation/gameplay/minimap-floor-key.ts" );
const { minimapSameFloor } = await import( "../../src/engine/foundation/ui/minimap-floor.ts" );
const {
	displaceRegionXZ,
	DUNGEON_REGION_BIT,
	isDungeonRegion,
	normalizeRegionXZ,
	planarDistance,
	planarDistanceSquared,
	REGION_SIZE,
	regionOffsetXZ,
	regionRelativeXZ,
	sameCoordinateSpace,
	sameDungeonFloor,
	samePlane,
	samePlaneAdjacent,
	sectorX,
	sectorY,
	worldAudioPosition,
	worldDelta,
	writeWorldDelta
} = await import( "../../src/engine/foundation/world/world-space.ts" );

const parityPath = new URL(
	"../../../server/internal/game/world/testdata/world-space-cases.json",
	import.meta.url
);

test("world-space matches shared server worldgeom parity cases", () => {
	const suite = JSON.parse( readFileSync( parityPath, "utf8" ) );
	assert.equal( REGION_SIZE, suite.regionSize );
	assert.equal( DUNGEON_REGION_BIT, suite.dungeonRegionBit );
	assert.ok( Array.isArray( suite.cases ) && suite.cases.length > 0 );

	for ( const tc of suite.cases ) {
		assert.equal( isDungeonRegion( tc.from.regionId ), tc.fromDungeon, `${tc.name}: fromDungeon` );
		assert.equal( isDungeonRegion( tc.to.regionId ), tc.toDungeon, `${tc.name}: toDungeon` );
		if ( tc.fromSector !== null ) {
			assert.equal( sectorX( tc.from.regionId ), tc.fromSector.x, `${tc.name}: fromSector.x` );
			assert.equal( sectorY( tc.from.regionId ), tc.fromSector.y, `${tc.name}: fromSector.y` );
		}
		if ( tc.toSector !== null ) {
			assert.equal( sectorX( tc.to.regionId ), tc.toSector.x, `${tc.name}: toSector.x` );
			assert.equal( sectorY( tc.to.regionId ), tc.toSector.y, `${tc.name}: toSector.y` );
		}
		assert.equal( samePlane( tc.from.regionId, tc.to.regionId ), tc.samePlane, `${tc.name}: samePlane` );
		assert.equal(
			samePlaneAdjacent( tc.from.regionId, tc.to.regionId ),
			tc.samePlaneAdjacent,
			`${tc.name}: samePlaneAdjacent`
		);
		if ( tc.delta !== null ) {
			const [dx, dz] = worldDelta( tc.from, tc.to );
			assert.ok( Math.abs( dx - tc.delta[0] ) <= 1e-9, `${tc.name}: dx=${dx}, want ${tc.delta[0]}` );
			assert.ok( Math.abs( dz - tc.delta[1] ) <= 1e-9, `${tc.name}: dz=${dz}, want ${tc.delta[1]}` );
		}
		if ( tc.distance !== null ) {
			const d = planarDistance( tc.from, tc.to );
			assert.ok( Math.abs( d - tc.distance ) <= 1e-9, `${tc.name}: distance=${d}, want ${tc.distance}` );
		}
		if ( tc.distanceSquared !== null ) {
			const d2 = planarDistanceSquared( tc.from, tc.to );
			assert.ok(
				Math.abs( d2 - tc.distanceSquared ) <= 1e-9,
				`${tc.name}: distanceSquared=${d2}, want ${tc.distanceSquared}`
			);
		}
	}
});

test("sectorY masks the dungeon bit and worldDelta suppresses sector offsets on any dungeon endpoint", () => {
	assert.equal( sectorX( 0x8001 ), 1 );
	assert.equal( sectorY( 0x8001 ), 0 );
	assert.equal( sectorY( 0xeb4f ), 0x6b );

	// SWorld_CalculateRegionOffsetVector (0x00888140): when either region has
	// bit 0x8000 set, the region offset vector is (0, 0, 0).
	assert.deepEqual(
		worldDelta(
			{ regionId: 0x8001, x: 100, z: 200 },
			{ regionId: 0x8002, x: 130, z: 240 }
		),
		[ 30, 40 ]
	);
	assert.deepEqual(
		worldDelta(
			{ regionId: 0x8001, x: 100, z: 200 },
			{ regionId: 0x64a8, x: 130, z: 240 }
		),
		[ 30, 40 ]
	);
	assert.equal(
		planarDistance(
			{ regionId: 0x8001, x: 100, z: 200 },
			{ regionId: 0x8002, x: 130, z: 240 }
		),
		50
	);
	assert.equal(
		planarDistanceSquared(
			{ regionId: 0x8001, x: 100, z: 200 },
			{ regionId: 0x8002, x: 130, z: 240 }
		),
		2500
	);
});

test("sameDungeonFloor implements native CObj_IsVisibleInSector (0x00863330)", () => {
	const outdoorA = { regionId: 0x64a8, x: 100, y: 0, z: 200 };
	const outdoorB = { regionId: 0x64a9, x: 300, y: 0, z: 400 };
	assert.equal( sameDungeonFloor( outdoorA, outdoorB ), true );

	const localDungeon = { regionId: 0x8001, x: 10, y: 0, z: 20 };
	const sameFloorTarget = { regionId: 0x8001, x: 50, y: 0, z: 60 };
	const otherFloorTarget = { regionId: 0x8001, x: 50, y: -120, z: 60 };
	const otherDungeonTarget = { regionId: 0x8002, x: 50, y: 0, z: 60 };

	assert.equal( sameDungeonFloor( localDungeon, sameFloorTarget ), false );
	assert.equal( sameDungeonFloor( localDungeon, outdoorB, { navigationFloor: 1 } ), false );
	assert.equal( sameDungeonFloor( localDungeon, otherDungeonTarget, { navigationFloor: 1 } ), false );

	const floors = {
		[minimapPoseKey( sameFloorTarget )]: 2,
		[minimapPoseKey( otherFloorTarget )]: 3
	};
	assert.equal(
		sameDungeonFloor( localDungeon, sameFloorTarget, { navigationFloor: 2, minimapFloors: floors } ),
		true
	);
	assert.equal(
		sameDungeonFloor( localDungeon, otherFloorTarget, { navigationFloor: 2, minimapFloors: floors } ),
		false
	);
	assert.equal(
		minimapSameFloor( localDungeon, sameFloorTarget, { navigationFloor: 2, minimapFloors: floors } ),
		true
	);
	assert.equal(
		minimapSameFloor( localDungeon, otherFloorTarget, { navigationFloor: 2, minimapFloors: floors } ),
		false
	);
});

test("regionOffsetXZ, regionRelativeXZ, normalizeRegionXZ, and worldAudioPosition preserve outdoor and dungeon contracts", () => {
	assert.deepEqual( regionOffsetXZ( 0x64a8, 0x65a9 ), [ 1920, 1920 ] );
	assert.deepEqual( regionOffsetXZ( 0x8001, 0x8002 ), [ 0, 0 ] );
	assert.deepEqual( regionOffsetXZ( 0x64a8, 0x8001 ), [ 0, 0 ] );

	assert.deepEqual(
		regionRelativeXZ( 0x64a8, { regionId: 0x65a9, x: 50, z: 75 } ),
		[ 1970, 1995 ]
	);
	assert.deepEqual(
		regionRelativeXZ( 0x8001, { regionId: 0x8001, x: -2400, z: 3100 } ),
		[ -2400, 3100 ]
	);

	assert.deepEqual( normalizeRegionXZ( 0x64a8, 1950, -20 ), {
		regionId: 0x63a9,
		x: 30,
		z: 1900
	} );
	assert.deepEqual( normalizeRegionXZ( 0x8001, 1950, -20 ), {
		regionId: 0x8001,
		x: 1950,
		z: -20
	} );

	assert.deepEqual(
		worldAudioPosition( { regionId: 0x64a8, x: 100, y: 25, z: 200 } ),
		[ 0xa8 * 1920 + 100, 25, 0x64 * 1920 + 200 ]
	);

	assert.equal( sameCoordinateSpace( 0x64a8, 0x65a9 ), true );
	assert.equal( sameCoordinateSpace( 0x8001, 0x8001 ), true );
	assert.equal( sameCoordinateSpace( 0x8001, 0x8002 ), false );
	assert.equal( sameCoordinateSpace( 0x64a8, 0x8001 ), false );

	assert.deepEqual(
		displaceRegionXZ( { regionId: 0x64a8, x: 1900, z: 10 }, 50, -30 ),
		{ regionId: 0x63a9, x: 30, z: 1900 }
	);
	assert.deepEqual(
		displaceRegionXZ( { regionId: 0x8001, x: 1900, z: 10 }, 50, -30 ),
		{ regionId: 0x8001, x: 1950, z: -20 }
	);
});

test("a delta inside one region is the plain difference added to zero, outdoors and in a dungeon", () => {
	// pose-presentation.ts writeWorldVector takes this for two poses of one
	// region without asking: the same bits as the general path, a
	// difference of negative zero included.
	let seed = 11;
	const random = () => (seed = (seed * 1664525 + 1013904223) >>> 0) / 2 ** 32;
	const coordinate = () => {
		const kind = random();
		if ( kind < .1 ) return 0;
		if ( kind < .2 ) return -0;
		if ( kind < .6 ) return Math.fround( random() * REGION_SIZE );
		return (random() - .5) * 4 * REGION_SIZE;
	};
	/** @type {[number, number]} */
	const out = [ NaN, NaN ];
	for ( let i = 0; i < 200000; i++ ) {
		const regionId = (random() < .25 ? DUNGEON_REGION_BIT : 0) | Math.floor( random() * 0x8000 );
		const from = { regionId, x: coordinate(), z: coordinate() },
			to = { regionId, x: coordinate(), z: coordinate() };
		assert.equal( sameCoordinateSpace( to.regionId, from.regionId ), true );
		writeWorldDelta( from, to, out );
		assert.ok( Object.is( out[0], 0 + (to.x - from.x) ), `x ${regionId} ${from.x} ${to.x}` );
		assert.ok( Object.is( out[1], 0 + (to.z - from.z) ), `z ${regionId} ${from.z} ${to.z}` );
	}
	// The zero matters: the general path writes the offset, then adds.
	writeWorldDelta( { regionId: 0x64a8, x: 0, z: 0 }, { regionId: 0x64a8, x: -0, z: -0 }, out );
	assert.ok( Object.is( out[0], 0 ) && Object.is( out[1], 0 ) );
});
