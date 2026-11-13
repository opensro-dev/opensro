/*
===========================================================================

world-layers.test.mjs - seasonal decoration follows the event groups

A placement tagged with a world layer is resident only while that layer's
event group is on; untagged placements never change.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const src = path => import( pathToFileURL( path ).href );
const { compileWalkTable, residentSlots, setWalkLayers } = await src(
	"src/engine/runtime/renderer/world/walk-table.ts"
);
const { activeWorldLayers, sameWorldLayers } = await src(
	"src/engine/foundation/world/world-layers.ts"
);

/*
================
layeredGroup

Three slots in one cell: an ordinary one, a Halloween (5) one and a
Christmas (7) one.
================
*/
function layeredGroup() {
	const cell = { radius: 1, range: 1, cells: [ [ 0, 0 ] ], cellRadius: 1 };
	return {
		material: {},
		geometry: {
			instances: new Float32Array( 3 * 16 ),
			positions: new Float32Array( 3 ),
			indices: new Uint32Array( 3 )
		},
		instanceRadius: 1,
		visibility: [ { id: "plain", ...cell }, { id: "pumpkin", ...cell, layer: 5 }, {
			id: "tree",
			...cell,
			layer: 7
		} ]
	};
}

test("an event group's bit 0 draws its layer", () => {
	assert.equal( activeWorldLayers( undefined ).size, 0 );
	assert.equal( activeWorldLayers( { 5: 0 } ).size, 0 );
	assert.deepEqual( [ ...activeWorldLayers( { 1: 1, 5: 1, 7: 2 } ) ].sort(), [ 1, 5 ] );
	const groups = { 5: 1 };
	assert.ok(
		sameWorldLayers( activeWorldLayers( groups ), activeWorldLayers( groups ) ),
		"one record, one set of layers"
	);
	assert.ok( sameWorldLayers( new Set( [ 5 ] ), activeWorldLayers( { 5: 1 } ) ) );
	assert.equal( activeWorldLayers( undefined ).size, 0, "the event ending clears the layers" );
	assert.ok( !sameWorldLayers( new Set( [ 5 ] ), new Set( [ 7 ] ) ) );
});

test("a layer slot is resident only while its layer is drawn", () => {
	const table = compileWalkTable( [ layeredGroup() ], new WeakMap() );
	const resident = () => [ ...table.resident.subarray( 0, residentSlots( table, 0, 0, 0 ) ) ];
	assert.deepEqual( resident(), [ 0 ], "no event: only the ordinary slot" );
	setWalkLayers( table, activeWorldLayers( { 5: 1 } ) );
	assert.deepEqual( resident(), [ 0, 1 ], "Halloween on: the pumpkin joins without a cell change" );
	setWalkLayers( table, activeWorldLayers( { 5: 1, 7: 1 } ) );
	assert.deepEqual( resident(), [ 0, 1, 2 ] );
	setWalkLayers( table, activeWorldLayers( { 5: 0, 7: 0 } ) );
	assert.deepEqual( resident(), [ 0 ], "both off: the decoration leaves" );
});

test("a table compiled during an event starts with its layers", () => {
	const table = compileWalkTable( [ layeredGroup() ], new WeakMap(), new Set( [ 7 ] ) );
	assert.deepEqual( [ ...table.resident.subarray( 0, residentSlots( table, 0, 0, 0 ) ) ], [ 0, 2 ] );
});
