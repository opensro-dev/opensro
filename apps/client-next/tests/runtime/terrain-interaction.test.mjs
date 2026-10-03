/*
===========================================================================

terrain-interaction.test.mjs - tests for terrain-interaction.ts,
ground-pick.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { pickTerrainCells, selectionDecalGeometry, terrainCellKey } = await import(
	"../../src/engine/foundation/rendering/terrain-interaction.ts"
);
const { pickNavigationGround } = await import( "../../src/engine/foundation/navigation/ground-pick.ts" );
const cell = ( height = 0, water ) => ({
	cell: [ 0, 0 ],
	heights: Float32Array.from( { length: 289 }, () => height ),
	water
});
const cells = ( value ) => new Map( [ [ terrainCellKey( 0, 0 ), value ] ] ),
	ray = { start: [ 10, 100, 10 ], delta: [ 0, -200, 0 ] };
test("ground picks the nearest raw terrain or special-water surface and rejects misses", () => {
	assert.equal( pickTerrainCells( cells( cell( 20 ) ), ray ), .4 );
	assert.equal( pickTerrainCells( cells( cell( 20, { type: 1, waveType: 1, height: 50 } ) ), ray ), .25 );
	assert.equal( pickTerrainCells( cells( cell( 20, { type: 1, waveType: 0, height: 50 } ) ), ray ), .4 );
	assert.equal(
		pickTerrainCells( cells( cell( 60, { type: 1, waveType: 1, height: 50 } ) ), ray ),
		.2,
		"land above ice must not pick through to the lower plane"
	);
	assert.equal(
		pickTerrainCells( cells( cell( 60, { type: 0, waveType: 1, height: 80 } ) ), ray ),
		.2,
		"ordinary water does not contribute a ground plane"
	);
	assert.equal( pickTerrainCells( cells( cell( 20 ) ), { ...ray, start: [ -1, 100, 10 ] } ), null );
});

test("oblique ice clicks retain the visible terrain intersection instead of walking farther", () => {
	const oblique = { start: [ 20, 100, 20 ], delta: [ 100, -100, 0 ] },
		map = cells( cell( 60, { type: 1, waveType: 3, height: 50 } ) );
	const depth = pickTerrainCells( map, oblique );
	assert.equal( depth, .4 );
	assert.deepEqual( pickNavigationGround( [], 0x587d, { originRegion: 0x587d, ray: oblique, terrainDepth: depth } ), {
		regionId: 0x587d,
		x: 60,
		y: 60,
		z: 20
	} );
	// A segment that ends on the land never reaches the lower water plane:
	// broad-phase bounds must include both surfaces as well.
	assert.equal( pickTerrainCells( map, { start: [ 20, 100, 20 ], delta: [ 100, -40, 0 ] } ), 1 );
});
test("navigation is picked ahead of terrain with region-relative placement and a bounded ray", () => {
	const mesh = {
		vertices: Float32Array.of( 0, 0, 0, 0, 0, 20, 20, 0, 20, 20, 0, 0 ),
		cells: Uint16Array.of( 0, 1, 2, 0, 2, 3 ),
		edges: new Uint32Array(),
		bounds: [ 0, 0, 0, 20, 0, 20 ],
		passThrough: false
	};
	const object = { x: 0, y: 40, z: 0, yaw: 0, mesh }, query = { originRegion: 257, ray, terrainDepth: .5 };
	assert.equal( defined( pickNavigationGround( [ object ], 257, query ) ).y, 40 );
	assert.equal( defined( pickNavigationGround( [ { ...object, x: -1920 } ], 258, query ) ).y, 40 );
	assert.equal( defined( pickNavigationGround( [], 257, query ) ).y, 0 );
	assert.equal( pickNavigationGround( [], 257, { ...query, terrainDepth: null } ), null );
	assert.equal( pickNavigationGround( [ object ], 0x8001, { ...query, originRegion: 0x8002 } ), null );
	assert.throws(
		() => pickNavigationGround( [], 257, { ...query, ray: { ...ray, delta: [ 0, -1001, 0 ] } } ),
		/Invalid ground ray/
	);
});
test("selection stays on raw MAPM heights, covers nine tiles and adds only the native special-water fan", () => {
	const data = selectionDecalGeometry( cells( cell( 3 ) ), [ 30, 80, 30 ] );
	assert.equal( defined( data ).positions.length, 9 * 4 * 3 );
	assert.equal( defined( data ).indices.length, 9 * 6 );
	for ( let i = 1; i < defined( data ).positions.length; i += 3 ) assert.equal( defined( data ).positions[i], 3 );
	assert.equal( defined( defined( data ).material ).decal, true );
	assert.equal( defined( defined( data ).material ).unlit, true );
	const wet = selectionDecalGeometry( cells( cell( 3, { type: 1, waveType: 1, height: 100 } ) ), [ 30, 80, 30 ] );
	assert.equal( defined( wet ).positions.length, defined( data ).positions.length + 12 );
	assert.equal( defined( wet ).positions.at( -11 ), Math.fround( 80 + Math.fround( .1 ) ) );
	assert.deepEqual( [ ...defined( defined( wet ).uvs ).slice( -8 ) ], [ 0, 0, 1, 0, 1, 1, 0, 1 ] );
	assert.equal( selectionDecalGeometry( new Map(), [ 30, 80, 30 ] ), null );
});
