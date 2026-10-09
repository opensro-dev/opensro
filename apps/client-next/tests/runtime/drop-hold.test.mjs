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

const monster = { gid: 7, kind: "monster" },
	own = { gid: 901, kind: "item", groundItem: { typeFlags: 0x2ec, goldAmount: 1, tint: 0, dropperGid: 7 } },
	other = { gid: 902, kind: "item", groundItem: { typeFlags: 0x2ec, goldAmount: 1, tint: 0, dropperGid: 8 } },
	loose = { gid: 903, kind: "item", groundItem: { typeFlags: 0x2ec, goldAmount: 1, tint: 0 } };

test("a held death holds its own drops and nothing else", () => {
	const selected = [ monster, own, other, loose ];
	assert.deepEqual( undroppedLoot( selected, new Set( [ 7 ] ) ).map( e => e.gid ), [ 7, 902, 903 ] );
});

test("the drops land once the killing hit has played", () => {
	const selected = [ monster, own, other, loose ];
	assert.equal( undroppedLoot( selected, new Set() ), selected );
	assert.deepEqual( undroppedLoot( selected, new Set( [ 8 ] ) ).map( e => e.gid ), [ 7, 901, 903 ] );
});
