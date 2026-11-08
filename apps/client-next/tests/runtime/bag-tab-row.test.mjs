/*
===========================================================================

bag-tab-row.test.mjs - the bag's page tabs: native up to two, fitted at three

Up to two tabs the row is the native one (59DF10 for the inventory,
644170 for the gacha). The third tab (SRO_INVENTORY_PAGES, port-only)
keeps the 60x24 art and tightens the stride so the row stays inside the
176 px bag pane; the selected tab paints last and takes the clicks it
covers.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	bagTabRow,
	inventorySlots,
	BAG_PANE_WIDTH,
	GACHA_BAG_CELL_SHIFT,
	GACHA_BAG_TABS,
	INVENTORY_BAG_TABS
} = await import( "../../src/engine/foundation/ui/inventory-layout.ts" );
const { topmostControlAt } = await import( "../../src/engine/foundation/ui/hit-test.ts" );

const PANE_X = 100;
const PANE_Y = 200;

/*
================
byPage
================
*/
function byPage( row ) {
	return [ ...row ].sort( ( a, b ) => a.page - b.page ).map( tab => tab.rect );
}

test("two tabs are the native rows, pixel for pixel", () => {
	// 59DF10: (4 + 0x3e*i, -0x17), 0x3c x 0x18 from the bag pane.
	assert.deepEqual( byPage( bagTabRow( PANE_X, PANE_Y, INVENTORY_BAG_TABS, 2, 0 ) ), [
		[ 104, 177, 60, 24 ],
		[ 166, 177, 60, 24 ]
	] );
	// 644170: (0x1db + 0x3e*i, 0x37) against the gacha bag frame at (475, 77).
	assert.deepEqual( byPage( bagTabRow( 475, 77, GACHA_BAG_TABS, 2, 0 ) ), [
		[ 475, 55, 60, 24 ],
		[ 537, 55, 60, 24 ]
	] );
	assert.deepEqual( byPage( bagTabRow( PANE_X, PANE_Y, INVENTORY_BAG_TABS, 1, 0 ) ), [ [ 104, 177, 60, 24 ] ] );
});

test("three tabs keep the 60x24 art and end inside the bag pane", () => {
	for ( const origin of [ INVENTORY_BAG_TABS, GACHA_BAG_TABS ] ) {
		const rects = byPage( bagTabRow( PANE_X, PANE_Y, origin, 3, 0 ) );
		assert.ok( rects.every( r => r[2] === 60 && r[3] === 24 ), "the art is never scaled" );
		assert.equal( rects[0][0], PANE_X + origin.x, "the first tab stays at the native start" );
		assert.ok(
			rects[2][0] + rects[2][2] <= PANE_X + BAG_PANE_WIDTH - origin.x,
			`the row ends ${origin.x} px inside the pane (${rects[2][0] + rects[2][2]})`
		);
		assert.equal( rects[1][0] - rects[0][0], rects[2][0] - rects[1][0], "an even stride" );
	}
});

test("the selected tab paints last and takes the clicks in its overlap", () => {
	for ( const selected of [ 0, 1, 2 ] ) {
		const row = bagTabRow( PANE_X, PANE_Y, INVENTORY_BAG_TABS, 3, selected );
		assert.equal( row.at( -1 ).page, selected );
		const controls = row.map( tab => ({ id: "inventory-page:" + tab.page, rect: tab.rect }) );
		const own = byPage( row )[selected];
		// The selected tab's left and right edges both lie in an overlap.
		for ( const x of [ own[0] + 1, own[0] + own[2] - 2 ] ) {
			assert.equal( topmostControlAt( controls, x, own[1] + 12 )?.id, "inventory-page:" + selected );
		}
	}
});

test("the gacha bag's cells sit one pixel in from the inventory's (644C20)", () => {
	const gacha = inventorySlots( 475 + GACHA_BAG_CELL_SHIFT, 77 + GACHA_BAG_CELL_SHIFT, 45, 13, 0 );
	// 644C20: cells at (0x1ee + 36 col, 0x5b + 36 row), 32x32.
	assert.deepEqual( gacha.slots[0].rect, [ 494, 91, 32, 32 ] );
	assert.deepEqual( gacha.slots[5].rect, [ 530, 127, 32, 32 ] );
});
