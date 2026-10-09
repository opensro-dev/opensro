/*
===========================================================================

drop-hold.test.mjs - tests for undroppedLoot in actor-presentation.ts

A monster's drops wait while its death waits for the killing hit, and only
those drops: other loot and every character keep presenting.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { undroppedLoot } = await import( "../../src/engine/runtime/characters/actor-presentation.ts" );

/*
================
entity

One presented entity at the origin; drop is its ground item, if any.
================
*/
function entity( gid, kind, drop ) {
	/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
	const state = { gid, refObjId: 1, kind, name: "", regionId: 1, x: 0, y: 0, z: 0, heading: 0 };
	return drop ? { ...state, groundItem: { typeFlags: 0x2ec, goldAmount: 1, tint: 0, ...drop } } : state;
}

const monster = entity( 7, "monster" ),
	own = entity( 901, "ground-item", { dropperGid: 7 } ),
	other = entity( 902, "ground-item", { dropperGid: 8 } ),
	loose = entity( 903, "ground-item", {} );

test("a held death holds its own drops and nothing else", () => {
	const selected = [ monster, own, other, loose ];
	assert.deepEqual( undroppedLoot( selected, new Set( [ 7 ] ) ).map( e => e.gid ), [ 7, 902, 903 ] );
});

test("the drops land once the killing hit has played", () => {
	const selected = [ monster, own, other, loose ];
	assert.equal( undroppedLoot( selected, new Set() ), selected );
	assert.deepEqual( undroppedLoot( selected, new Set( [ 8 ] ) ).map( e => e.gid ), [ 7, 901, 903 ] );
});
