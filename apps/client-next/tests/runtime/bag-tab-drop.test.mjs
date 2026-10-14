/*
===========================================================================

bag-tab-drop.test.mjs - where an item dropped on a bag tab lands

foundation/ui/bag-tab-drop.ts: the target page's first free enabled slot,
nothing when that page is full or already holds the dragged item.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { bagTabDropSlot } = await import( "../../src/engine/foundation/ui/bag-tab-drop.ts" );

test("the first free slot of the target page, never the dragged item's own page", () => {
	const rows = slots => slots.map( slot => ({ slot }) );
	// 59 slots: page 0 is 13..44, page 1 is 45..58.
	assert.equal( bagTabDropSlot( rows( [ 13, 14, 46 ] ), 59, 13, 0, 46 ), 15 );
	assert.equal( bagTabDropSlot( rows( [ 13 ] ), 59, 13, 1, 13 ), 45 );
	assert.equal( bagTabDropSlot( rows( [ 46 ] ), 59, 13, 1, 46 ), null, "its own page" );
	const full = rows( Array.from( { length: 32 }, ( _, i ) => 13 + i ) );
	assert.equal( bagTabDropSlot( [ ...full, { slot: 50 } ], 59, 13, 0, 50 ), null, "a full page" );
	assert.equal( bagTabDropSlot( rows( [ 13 ] ), 45, 13, 1, 13 ), null, "no second page" );
});
