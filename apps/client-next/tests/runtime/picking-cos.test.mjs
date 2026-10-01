/*
===========================================================================

picking-cos.test.mjs - tests for picking.ts, characters.ts, cos-record.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { pickRay, pickGeometry, geometryPickBounds, palettePickBounds, rayIntersectsBounds } = await import(
	"../../src/engine/foundation/rendering/picking.ts"
);
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const { decodeCosRecord } = await import( "../../src/engine/foundation/gameplay/cos-record.ts" );
const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
const geometry = () => ({
	positions: Float32Array.of( -.2, -.2, .5, .2, -.2, .5, 0, .2, .5 ),
	indices: Uint32Array.of( 0, 1, 2 ),
	transform: identity()
});
test("bounds preserve exact hits under reflection, nonuniform scale and blended skinning", () => {
	const g = geometry(), bounds = geometryPickBounds( g.positions );
	for ( const scale of [ -3, -1, .5, 2 ] ) {
		for ( const translation of [ -.4, 0, .4 ] ) {
			const matrix = identity();
			matrix[0] = scale;
			matrix[5] = 2;
			matrix[12] = translation;
			for ( let x = 0; x <= 1; x += .05 ) {
				const ray = pickRay( identity(), x, .5 ), hit = pickGeometry( ray, g, matrix );
				if ( hit !== null ) assert.equal( rayIntersectsBounds( ray, bounds, matrix ), true );
			}
		}
	}
	g.joints = Uint32Array.from( { length: 12 }, ( _, i ) => i % 4 === 1 ? 1 : 0 );
	g.weights = Float32Array.from( { length: 12 }, ( _, i ) => i % 4 < 2 ? .5 : 0 );
	const a = identity(), b = identity();
	a[12] = -.6;
	b[12] = .6;
	const palette = Float32Array.of( ...a, ...b ), posed = palettePickBounds( bounds, palette );
	const ray = pickRay( identity(), .5, .5 );
	assert.equal( pickGeometry( ray, g, identity(), palette ), .5 );
	assert.equal( rayIntersectsBounds( ray, posed, identity() ), true );
	assert.equal( rayIntersectsBounds( { start: [ 5, 5, 0 ], delta: [ 0, 0, 1 ] }, posed, identity() ), false );
	assert.equal( rayIntersectsBounds( ray, bounds, identity(), .4 ), false );
});
test("click ray uses WebGPU depth, actual triangles, and submitted skin/instance matrices", () => {
	const ray = pickRay( identity(), .5, .5 ), g = geometry();
	assert.equal( pickGeometry( ray, g, identity() ), .5 );
	assert.equal( pickGeometry( pickRay( identity(), .9, .5 ), g, identity() ), null );
	const transform = identity();
	transform[12] = .8;
	assert.equal( pickGeometry( ray, g, transform ), null );
	g.joints = new Uint32Array( 12 );
	g.weights = Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 );
	const bones = identity();
	bones[12] = -.8;
	assert.equal( pickGeometry( ray, g, transform, bones ), .5 );
	transform[14] = 1;
	assert.equal( pickGeometry( ray, g, transform, bones ), null );
	assert.equal( pickRay( new Float32Array( 16 ), .5, .5 ), null );
	assert.equal( pickRay( identity(), NaN, .5 ), null );
});
test("renderer picking selects nearest submitted entity and invalidates on removal and loss", () => {
	const c = createCharacters();
	c.model( "m", {
		nodes: [],
		clips: [],
		images: [],
		primitives: [ {
			name: "p",
			node: 0,
			joints: [],
			inverseBind: new Float32Array(),
			image: -1,
			geometry: geometry()
		} ]
	}, [] );
	const actor = ( gid, z ) => ({
		gid,
		model: "m",
		pose: { regionId: 257, x: 0, y: 0, z, yaw: 0 },
		clip: "",
		time: 0,
		loop: false,
		scale: 1
	});
	const gpu = { upload: () => ({}), updateInstances: draw => draw, updateBones() {}, release() {} };
	c.actors( [ actor( 2, .2 ), actor( 3, 0 ) ] );
	c.prepare( gpu, { upload: () => ({}), release() {} }, 257 );
	const ray = pickRay( identity(), .5, .5 );
	assert.equal( defined( c.pick( [ ray ], 0 ) ).gid, 3 );
	assert.equal( defined( c.pick( [ ray ], 3 ) ).gid, 2 );
	c.actors( [ actor( 2, .2 ), { ...actor( 3, 0 ), blindable: true } ] );
	c.prepare( gpu, { upload: () => ({}), release() {} }, 257 );
	assert.equal(
		defined( c.pick( [ ray ], 0, true ) ).gid,
		2,
		"held V skips the front monster and picks the still-visible item/NPC behind"
	);
	assert.equal( defined( c.pick( [ ray ], 0, false ) ).gid, 3, "release restores the front target" );
	c.actors( [] );
	assert.equal( c.pick( [ ray ], 0 ), null );
	c.invalidate();
	assert.equal( c.pick( [ ray ], 0 ), null );
	c.dispose( gpu, null );
});
function record( band, status = 0 ) {
	const parts = [],
		u8 = n => parts.push( Buffer.from( [ n ] ) ),
		u16 = n => {
			const p = Buffer.alloc( 2 );
			p.writeUInt16LE( n );
			parts.push( p );
		},
		u32 = n => {
			const p = Buffer.alloc( 4 );
			p.writeUInt32LE( n );
			parts.push( p );
		};
	for ( const n of [ 7, 100 + band, 1000, 500 ] ) u32( n );
	if ( band === 3 ) {
		u32( 0xffffffff );
		u32( 123 );
		u8( 40 );
		u16( 9999 );
	}
	if ( band === 3 || band === 4 ) {
		u32( 88 );
		u16( 3 );
		parts.push( Buffer.from( "pet" ) );
	}
	u8( status );
	if ( status ) u8( 0 );
	if ( band !== 1 && band !== 5 ) u32( 1 );
	if ( band === 3 || band === 4 ) u8( 14 );
	return Buffer.concat( parts );
}
test("native COS bands retain conditional fields and reject every truncated prefix atomically", () => {
	const refs = new Map( [ 1, 2, 3, 4, 5 ].map( band => [ 100 + band, (band << 11) | 0x1c6 ] ) );
	for ( const band of [ 1, 2, 3, 4, 5 ] ) {
		for ( const status of [ 0, 1 ] ) {
			const bytes = record( band, status ), row = decodeCosRecord( bytes, refs );
			assert.equal( defined( row ).band, band );
			assert.equal( defined( row ).dead, band !== 1 && band !== 5 );
			if ( band === 3 ) assert.deepEqual( defined( row ).experience, [ 0xffffffff, 123 ] );
			if ( band === 3 || band === 4 ) {
				assert.equal( defined( row ).name, "pet" );
				assert.equal( defined( row ).commandMode, 88 );
				assert.equal( defined( row ).inventorySlot, 14 );
			}
			for ( let n = 0; n < bytes.length; n++ ) {
				assert.throws( () => decodeCosRecord( bytes.subarray( 0, n ), refs ) );
			}
			assert.throws( () => decodeCosRecord( Buffer.concat( [ bytes, Buffer.from( [ 0 ] ) ] ), refs ) );
		}
	}
	assert.equal( decodeCosRecord( record( 2 ), new Map() ), null );
	const inventory = record( 2, 1 );
	inventory[17] = 1;
	assert.throws( () => decodeCosRecord( inventory, refs ), /Truncated|COS inventory slot/ );
});

test("dock selection uses the aggregate box in roster order, including empty silhouette and alpha regions", () => {
	const c = createCharacters(),
		g = geometry(),
		gpu = { upload: () => ({}), updateInstances: draw => draw, updateBones() {}, release() {} };
	c.model( "m", {
		nodes: [],
		clips: [],
		images: [],
		primitives: [ { name: "p", node: 0, joints: [], inverseBind: new Float32Array(), image: -1, geometry: g } ]
	}, [] );
	const actor = gid => ({
		gid,
		model: "m",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
		clip: "",
		time: 0,
		loop: false,
		scale: 1
	});
	c.actors( [ actor( 2 ), actor( 3 ) ] );
	c.prepare( gpu, { upload: () => ({}), release() {} }, 257 );
	const near = { start: [ .18, .18, 0 ], delta: [ 0, 0, 1 ] },
		outside = { start: [ .21, .18, 0 ], delta: [ 0, 0, 1 ] };
	assert.equal(
		defined( c.pick( [ near ], 0 ) ).gid,
		2,
		"retail world picking accepts the model box outside triangle silhouettes"
	);
	// A box-only winner yields to a confirmed rival: here a drop sharing the spot.
	c.actors( [ actor( 2 ), { ...actor( 4 ), groundItem: true } ] );
	c.prepare( gpu, { upload: () => ({}), release() {} }, 257 );
	assert.equal( defined( c.pick( [ near ], 0 ) ).gid, 4, "an empty character box does not hide a drop" );
	c.actors( [ actor( 2 ), actor( 3 ) ] );
	c.prepare( gpu, { upload: () => ({}), release() {} }, 257 );
	assert.equal( c.pickFrontend( near, [ 3, 2 ] ), 3 );
	assert.equal( c.pickFrontend( near, [ 2, 3 ] ), 2 );
	assert.equal( c.pickFrontend( outside, [ 2, 3 ] ), null );
	c.actors( [ actor( 3 ) ] );
	assert.equal( c.pickFrontend( near, [ 2, 3 ] ), 3 );
	c.invalidate();
	assert.equal( c.pickFrontend( near, [ 3 ] ), null );
	c.dispose( gpu, null );
});
