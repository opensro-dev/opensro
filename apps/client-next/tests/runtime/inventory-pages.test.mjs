/*
===========================================================================

inventory-pages.test.mjs - the inventory grid at both capacity sizes

The entry block's capacity byte is the only input: the grid pages by 32
behind the 13 equipment sockets, so the native 45..61 range shows one to
two pages and the port-only 2.5-page base (93) shows a half-enabled third
page that the expansion quests fill to the full three pages (109). No
enabled control may name a wire slot past the capacity.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { inventorySlots } = await import( "../../src/engine/foundation/ui/inventory-layout.ts" );

const EQUIPMENT = 13;

/*
================
enabledSlots

The wire slots one page enables at a total capacity.
================
*/
function enabledSlots( total, page ) {
	const layout = inventorySlots( 0, 0, total, EQUIPMENT, page );

	return {
		pages: layout.pages,
		enabled: layout.slots.filter( ( slot ) => slot.enabled ).map( ( slot ) => slot.slot )
	};
}

test("the native sizes page as the v1.150 client does", () => {
	assert.equal( enabledSlots( 45, 0 ).pages, 1 );
	assert.equal( enabledSlots( 45, 0 ).enabled.length, 32 );
	assert.equal( enabledSlots( 61, 1 ).pages, 2 );
	assert.deepEqual( enabledSlots( 61, 1 ).enabled, Array.from( { length: 16 }, ( _, i ) => 45 + i ) );
});

test("the 2.5-page base shows a half-enabled third page", () => {
	const third = enabledSlots( 93, 2 );

	assert.equal( third.pages, 3 );
	assert.deepEqual( third.enabled, Array.from( { length: 16 }, ( _, i ) => 77 + i ) );
	assert.equal( enabledSlots( 93, 9 ).pages, 3 );
});

test("the three-page maximum enables every control", () => {
	const third = enabledSlots( 109, 2 );

	assert.equal( third.pages, 3 );
	assert.deepEqual( third.enabled, Array.from( { length: 32 }, ( _, i ) => 77 + i ) );
});

test("no page enables a wire slot past its capacity", () => {
	for ( const total of [ 45, 61, 77, 93, 109 ] ) {
		for ( let page = 0; page < 4; page++ ) {
			for ( const slot of enabledSlots( total, page ).enabled ) {
				assert.ok( slot < total, `capacity ${total} page ${page} enables slot ${slot}` );
			}
		}
	}
});
