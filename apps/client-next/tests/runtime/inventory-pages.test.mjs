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

import { defined } from "../helpers/defined.mjs";

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

/*
================
bag cell hit areas

BUG-092: the hit areas (hitRect) cover the whole grid between the first and
the last cell, gaps included, with no point in two areas, and each area
holds its drawn cell. So a drop anywhere on the grid lands in exactly one
cell, the nearer one across a gap.
================
*/
test("every point between the bag cells belongs to exactly one cell's hit area", () => {
	const { slots } = inventorySlots( 0, 0, 45, 13, 0 );
	const inside = ( r, x, y ) => x >= r[0] && y >= r[1] && x < r[0] + r[2] && y < r[1] + r[3];
	for ( const cell of slots ) {
		const [x, y, w, h] = cell.rect, hit = cell.hitRect;
		assert.ok(
			hit[0] <= x && hit[1] <= y && hit[0] + hit[2] >= x + w && hit[1] + hit[3] >= y + h,
			"hit area holds cell " + cell.slot
		);
	}
	const first = defined( slots[0] ).hitRect, last = defined( slots.at( -1 ) ).hitRect;
	for ( let y = first[1]; y < last[1] + last[3]; y++ ) {
		for ( let x = first[0]; x < last[0] + last[2]; x++ ) {
			const owners = slots.filter( cell => inside( cell.hitRect, x, y ) );
			assert.equal( owners.length, 1, `point ${x},${y} is in ${owners.length} hit areas` );
		}
	}
	// Two pixels right of slot 13's cell (in the gap) is slot 13's; three is slot 14's.
	const [x0, y0, w0] = defined( slots[0] ).rect;
	assert.equal( defined( slots.find( cell => inside( cell.hitRect, x0 + w0 + 1, y0 + 4 ) ) ).slot, 13 );
	assert.equal( defined( slots.find( cell => inside( cell.hitRect, x0 + w0 + 2, y0 + 4 ) ) ).slot, 14 );
});
