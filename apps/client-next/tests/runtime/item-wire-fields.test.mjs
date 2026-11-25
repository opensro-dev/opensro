/*
===========================================================================

item-wire-fields.test.mjs - the server's copy matches the tuple (#4448)

ITEM_WIRE_FIELDS is the one owner of the item-field read set. The
server's itemwirefields.json is generated from it; this test and the
Go test beside the JSON both fail when the copies drift.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const { ITEM_WIRE_FIELDS } = await import( "../../src/engine/contracts/item-wire-fields.ts" );
const contract = JSON.parse(
	readFileSync( new URL( "../../../server/internal/game/enterworld/itemwirefields.json", import.meta.url ), "utf8" )
);

/*
================
tuple matches the generated server copy
================
*/
test("item wire fields match the server copy", () => {
	assert.equal( new Set( ITEM_WIRE_FIELDS ).size, ITEM_WIRE_FIELDS.length, "tuple lists a name twice" );
	assert.deepEqual( [ ...ITEM_WIRE_FIELDS ].sort(), [ ...contract ].sort() );
});
