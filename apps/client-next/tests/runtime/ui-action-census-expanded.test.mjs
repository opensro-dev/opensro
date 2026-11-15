/*
===========================================================================

ui-action-census-expanded.test.mjs - the census on an expanded build

A native build must match the committed census exactly. An expanded build
(the Alexandria variant, Arabia's map guide rows) publishes more controls,
so its check requires every committed scene, control and row to survive
unchanged and lets added ones pass (tools/census-expanded.mjs).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { lostNativeRow } from "../../tools/census-expanded.mjs";
import { defined } from "../helpers/defined.mjs";

// The rows below are written in canonical key order already.
const asIs = value => value;

const NATIVE = {
	scenes: {
		Map: {
			controls: [ "map-close", "map-guide" ],
			focus: null,
			panel: "Map",
			rows: {
				"map-close": { panel: null },
				"map-guide": null,
				"map-side-tab": { added: [ "map-close" ], hash: "2ef4", panel: "Map", removed: [] }
			}
		}
	}
};

/*
================
lost

lostNativeRow over the native table with one change applied to the built
Map scene.
================
*/
function lost( change ) {
	const map = structuredClone( NATIVE.scenes.Map );
	change( map );
	return lostNativeRow( NATIVE, { scenes: { Map: map } }, asIs );
}

/*
================
failure

The fact lost reports for a change that must fail: present, and a string.
================
*/
function failure( change ) {
	return defined( lost( change ), "the lost native fact" );
}

test("added controls and rows pass; every native one survives", () => {
	assert.equal(
		lost( map => {
			map.controls.push( "map-guide-main:22" );
			map.rows["map-guide-main:22"] = { panel: "Map" };
			map.rows["map-side-tab"].added.push( "map-guide-main:22" );
			map.rows["map-side-tab"].hash = "a0d6";
		} ),
		undefined
	);
});

test("a lost control, a lost row, a changed row or a moved scene fails", () => {
	assert.match( failure( map => map.controls.splice( 0, 1 ) ), /lost control map-close/ );
	assert.match( failure( map => delete map.rows["map-guide"] ), /lost row map-guide/ );
	assert.match( failure( map => map.rows["map-close"] = { panel: "Map" } ), /row map-close changed/ );
	assert.match( failure( map => map.panel = "Option" ), /opens differently/ );
	assert.match( failure( map => map.rows["map-side-tab"].added = [] ), /row map-side-tab changed/ );
	assert.match( failure( map => map.rows["map-side-tab"].panel = null ), /row map-side-tab changed/ );
	// A native control the action now closes, or newly opens, is a native change.
	assert.match( failure( map => map.rows["map-side-tab"].removed.push( "map-guide" ) ), /row map-side-tab changed/ );
	assert.match( failure( map => map.rows["map-side-tab"].added.push( "map-guide" ) ), /row map-side-tab changed/ );
	assert.match( defined( lostNativeRow( NATIVE, { scenes: {} }, asIs ), "the lost scene" ), /scene Map is gone/ );
});
