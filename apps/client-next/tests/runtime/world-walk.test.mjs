/*
===========================================================================

world-walk.test.mjs - tests for the selection walk's typed data

object-fades.ts, walk-table.ts and instance-bounds.ts let the world
renderer's walk skip work. Each skip claims to give exactly what the full
work would; these tests check those claims against the full work.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const src = path => import( pathToFileURL( path ).href );
const { createObjectFades } = await src( "src/engine/runtime/renderer/world/object-fades.ts" );
const { advanceObjectFade } = await src( "src/engine/foundation/rendering/object-visibility.ts" );
const { instanceGroupSphere } = await src( "src/engine/foundation/rendering/instance-bounds.ts" );
const { compileWalkTable, residentSlots, fadeKeeps, FADE_KEEPS_IN, FADE_KEEPS_OUT } = await src(
	"src/engine/runtime/renderer/world/walk-table.ts"
);
const {
	viewProjection,
	prepareViewFrustum,
	visibleFrustumBox,
	visibleFrustumSphere
} = await src( "src/engine/foundation/rendering/world-math.ts" );

/*
================
random

A small deterministic generator, so failures reproduce.
================
*/
function random( seed ) {
	let state = seed >>> 0;
	return () => {
		state = (state * 1664525 + 1013904223) >>> 0;
		return state / 2 ** 32;
	};
}

/*
================
placements

count rotated, scaled placements around centre, as instance matrices.
================
*/
function placements( next, count, centre, spread ) {
	const instances = new Float32Array( count * 16 );
	for ( let i = 0; i < count; i++ ) {
		const yaw = next() * Math.PI * 2, scale = .5 + next() * 2, at = i * 16;
		instances.set( [
			Math.cos( yaw ) * scale,
			0,
			-Math.sin( yaw ) * scale,
			0,
			0,
			scale,
			0,
			0,
			Math.sin( yaw ) * scale,
			0,
			Math.cos( yaw ) * scale,
			0,
			centre[0] + (next() - .5) * spread,
			centre[1] + (next() - .5) * 20,
			centre[2] + (next() - .5) * spread,
			1
		], at );
	}
	return instances;
}

test("a fade row advances exactly as advanceObjectFade does, and compaction keeps every kept row", () => {
	const fades = createObjectFades(), next = random( 7 );
	const ids = Array.from( { length: 40 }, ( _, i ) => "p" + i ), mirror = new Map();
	for ( const id of ids ) mirror.set( id, { state: 0, alpha: 0, lastFrame: 0 } );
	for ( let frame = 1; frame < 200; frame++ ) {
		for ( const id of ids ) {
			if ( next() < .3 ) continue;
			const row = fades.row( id ), distance = next() * 900, dt = next() / 30;
			fades.visit( row, frame );
			fades.advance( row, distance, 20, 400, dt, frame );
			advanceObjectFade( mirror.get( id ), distance, 20, 400, dt, frame, mirror.get( id ) );
		}
	}
	const check = () => {
		for ( const id of ids ) {
			const row = fades.find( id ), expected = mirror.get( id );
			if ( row === undefined ) continue;
			assert.equal( fades.state( row ), expected.state, id );
			assert.equal( fades.lastFrame( row ), expected.lastFrame, id );
			assert.equal( fades.published( row ), Math.max( 0, Math.min( 255, Math.trunc( expected.alpha ) ) ), id );
		}
	};
	check();
	const kept = new Set( ids.filter( ( _, i ) => i % 3 ) );
	fades.keepOnly( kept );
	assert.equal( fades.size(), kept.size );
	assert.equal( fades.find( "p0" ), undefined );
	check();
	// The visited rows were remapped with the rows: the stamp reaches every
	// kept row (each was visited) and nothing else.
	fades.stampActive( 999 );
	for ( const id of kept ) assert.equal( fades.lastFrame( fades.find( id ) ), 999, id );
	fades.clear();
	assert.equal( fades.size(), 0 );
});

test("a rejected group sphere rejects every placement's own frustum test", () => {
	const next = random( 11 ), bounds = [ -6, -1, -4, 5, 9, 7 ];
	let rejected = 0;
	for ( let trial = 0; trial < 60; trial++ ) {
		const centre = [ next() * 4000, next() * 100, next() * 4000 ];
		const instances = placements( next, 12, centre, 300 );
		for ( const box of [ bounds, undefined ] ) {
			const sphere = instanceGroupSphere( instances, box, 9 );
			for ( let view = 0; view < 40; view++ ) {
				const eye = [ centre[0] + (next() - .5) * 2000, 30 + next() * 300, centre[2] + (next() - .5) * 2000 ];
				const target = [ eye[0] + (next() - .5) * 100, eye[1] - 20, eye[2] + (next() - .5) * 100 ];
				const frustum = prepareViewFrustum(
					viewProjection( { eye, target, fov: Math.PI / 3, near: 1, far: 3500 }, 16 / 9 )
				);
				if ( visibleFrustumSphere( frustum, sphere[0], sphere[1], sphere[2], sphere[3] ) ) continue;
				rejected++;
				for ( let i = 0; i < instances.length; i += 16 ) {
					const shown = box ?
						visibleFrustumBox( frustum, box, instances, i ) :
						visibleFrustumSphere( frustum, instances[i + 12], instances[i + 13], instances[i + 14], 9 );
					assert.equal( shown, false, `trial ${trial} view ${view} placement ${i / 16}` );
				}
			}
		}
	}
	assert.ok( rejected > 500, `only ${rejected} rejections exercised` );
});

test("fadeKeeps flags imply advanceObjectFade changes only the frame stamp", () => {
	const next = random( 5 ), seen = { [FADE_KEEPS_IN]: 0, [FADE_KEEPS_OUT]: 0 };
	for ( let trial = 0; trial < 40; trial++ ) {
		const centre = [ next() * 4000, 0, next() * 4000 ], instances = placements( next, 10, centre, 200 );
		const visibility = Array.from( { length: 10 }, () => ({
			id: String( next() ),
			radius: 5 + next() * 40,
			range: 600 + next() * 800,
			sceneryRange: next() < .3,
			cells: [],
			cellRadius: 0
		}) );
		const group = {
			material: {},
			geometry: { instances, positions: new Float32Array( 3 ), indices: new Uint32Array( 3 ) },
			instanceRadius: 10,
			visibility
		};
		const table = compileWalkTable( [ group ], new WeakMap() ), sceneryRange = 1500;
		for ( let view = 0; view < 60; view++ ) {
			// Near and far views, so both steady states are exercised.
			const reach = view % 2 ? 300 : 8000;
			const eye = [ centre[0] + (next() - .5) * reach, next() * 200, centre[2] + (next() - .5) * reach ];
			const keeps = fadeKeeps( table, 0, eye, sceneryRange );
			for ( const [flag, state] of [ [ FADE_KEEPS_IN, 2 ], [ FADE_KEEPS_OUT, 0 ] ] ) {
				if ( !(keeps & flag) ) continue;
				seen[flag]++;
				for ( let slot = 0; slot < 10; slot++ ) {
					const at = slot * 16, d = visibility[slot];
					const dx = Math.fround( eye[0] - instances[at + 12] ),
						dy = Math.fround( eye[1] - instances[at + 13] ),
						dz = Math.fround( eye[2] - instances[at + 14] );
					const distance = Math.fround( Math.sqrt( Math.fround( dy * dy + dx * dx + dz * dz ) ) );
					const before = { state, alpha: state === 2 ? 255 : 0, lastFrame: 9 };
					const after = advanceObjectFade(
						before,
						distance,
						d.radius,
						d.sceneryRange ? sceneryRange : d.range,
						.016,
						10
					);
					assert.deepEqual( after, { ...before, lastFrame: 10 }, `trial ${trial} view ${view} slot ${slot}` );
				}
			}
		}
	}
	assert.ok( seen[FADE_KEEPS_IN] > 50 && seen[FADE_KEEPS_OUT] > 50, JSON.stringify( seen ) );
});

test("resident slots follow the target cell and keep undescribed slots", () => {
	const instances = new Float32Array( 4 * 16 );
	const visibility = [
		{ id: "a", radius: 1, range: 1, cells: [ [ 0, 0 ] ], cellRadius: 1 },
		{ id: "b", radius: 1, range: 1, cells: [ [ 5, 5 ], [ 9, 9 ] ], cellRadius: 2 },
		{ id: "c", radius: 1, range: 1, cells: [ [ 3, 0 ] ], cellRadius: 0 }
	];
	const group = {
		material: {},
		geometry: { instances, positions: new Float32Array( 3 ), indices: new Uint32Array( 3 ) },
		instanceRadius: 1,
		visibility
	};
	const table = compileWalkTable( [ group ], new WeakMap() );
	const at = ( x, z ) => [ ...table.resident.subarray( 0, residentSlots( table, 0, x, z ) ) ];
	// Slot 3 has no descriptor: always resident.
	assert.deepEqual( at( 0, 0 ), [ 0, 3 ] );
	assert.deepEqual( at( 3, 0 ), [ 2, 3 ] );
	assert.deepEqual( at( 7, 7 ), [ 1, 3 ] );
	assert.deepEqual( at( 1, 1 ), [ 0, 3 ] );
});
