/*
===========================================================================

commerce-tooltip.test.mjs - transaction prices remain attached to offers

Exercise package, buyback and replacement catalog contexts without deriving
gold from an item's reference fields or losing precision through Number.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
const { commerceTooltip } = await import( "../../src/engine/foundation/ui/commerce-tooltip.ts" );

// Independent translated labels catch accidental raw-symbol output.
const copy = { price: "Price", gold: "Gold", honor: "Honor point", point: " point(s)" };

/*
================
fixture

Only transaction metadata is relevant; item details are a separate owner.
================
*/
function fixture() {
	return {
		localGid: 1,
		inventory: [],
		vitals: [],
		target: 2,
		shop: {
			npc: 2,
			name: "Merchant",
			offers: [ { tab: 0, slot: 0, refObjId: 10, name: "Package", maxStack: 5, price: "9007199254740993" } ],
			buyback: [ { index: 0, id: 3, refObjId: 10, name: "Returned stack", quantity: 50, plus: 0, price: "250" } ]
		}
	};
}

test("package price is exact and occurs once", () => {
	const game = fixture();
	const rows = commerceTooltip( "shop-offer:0", game, copy );
	assert.deepEqual( rows.filter( row => row.value.trim() ), [
		{ value: "Price : 9007199254740993 Gold", color: 0xffffffff }
	] );
});

test("buyback uses the whole-entry quote without multiplying its stack", () => {
	assert.equal( commerceTooltip( "shop-buyback:0", fixture(), copy ).at( -1 )?.value, "Price : 250 Gold" );
});

test("new offer price replaces the previous context, including free offers", () => {
	const game = fixture();
	assert.match( commerceTooltip( "shop-offer:0", game, copy ).at( -1 )?.value ?? "", /9007199254740993/ );
	game.shop = { ...game.shop, offers: [ { ...game.shop.offers[0], price: "0" } ] };
	assert.equal( commerceTooltip( "shop-offer:0", game, copy ).at( -1 )?.value, "Price : 0 Gold" );
});

test("unrelated, missing and failed contexts do not invent prices", () => {
	const game = fixture();
	for ( const id of [ "slot:13", "hotbar:0", "shop-offer:99", "shop-buyback:99" ] ) {
		assert.deepEqual( commerceTooltip( id, game, copy ), [] );
	}
	assert.deepEqual( commerceTooltip( "shop-offer:0", { ...game, shop: undefined }, copy ), [] );
	assert.deepEqual(
		commerceTooltip( "shop-offer:0", { ...game, shop: { ...game.shop, error: "Closed" } }, copy ),
		[]
	);
});

test("an honor package prints its points in the native honor row", () => {
	const game = fixture();
	game.shop = { ...game.shop, offers: [ { ...game.shop.offers[0], price: "1500", currency: 32 } ] };
	assert.equal( commerceTooltip( "shop-offer:0", game, copy ).at( -1 )?.value, "Honor point : 1500 point(s)" );
});
