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
const { commerceTooltip, mallTooltipRows } = await import( "../../src/engine/foundation/ui/commerce-tooltip.ts" );

// Independent translated labels catch accidental raw-symbol output.
const mallCopy = { rollOfCloth: "ROLL", silk: "SILK", giftSilk: "GIFT" };

/**
 * @param {object} [fields]
 * @returns {any}
 */
function mallOffer( fields = {} ) {
	return { currencyMask: 0, silk: 0, giftSilk: 0, ...fields };
}

// Independent translated labels catch accidental raw-symbol output.
const copy = {
	price: "Price",
	gold: "Gold",
	honor: "Honor point",
	point: " point(s)",
	guildPoint: "Guild point (GP)",
	guildPointUnit: "GP"
};

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

/*
================
packages priced twice

Issue #593: an honor or guild store good costs gold and honor, or gold and
guild points. 559AF6 walks the payment indexes 0..5 and prints one line for
each the package has: gold (0), guild points (3), honor (5).
================
*/
test("a package priced in two currencies prints every price row in payment order", () => {
	const game = fixture(), base = game.shop.offers[0];
	game.shop = {
		...game.shop,
		offers: [
			{
				...base,
				price: "2700000",
				currency: 1,
				prices: [ { currency: 1, amount: "2700000" }, { currency: 32, amount: "1000" } ]
			},
			{
				...base,
				slot: 1,
				price: "1000000",
				currency: 1,
				prices: [ { currency: 1, amount: "1000000" }, { currency: 8, amount: "20000" } ]
			}
		]
	};
	assert.deepEqual(
		commerceTooltip( "shop-offer:0", game, copy ).filter( row => row.value.trim() ).map( row => row.value ),
		[
			"Price : 2700000 Gold",
			"Honor point : 1000 point(s)"
		]
	);
	assert.deepEqual(
		commerceTooltip( "shop-offer:1", game, copy ).filter( row => row.value.trim() ).map( row => row.value ),
		[
			"Price : 1000000 Gold",
			"Guild point (GP) : 20000GP"
		]
	);
});

/*
================
mallTooltipRows

The item-mall leg of 5592B0 (pane 0xf): the package price object's
charged-silk and received-silk rows print where their currency bits
are authored; the object's other native rows cannot occur in the
mall catalogue (mask bits 2/4/16 only).
================
*/

test("charged silk prints under the cloth caption", () => {
	assert.deepEqual( mallTooltipRows( mallOffer( { currencyMask: 2, silk: 100 } ), mallCopy ), [
		{ value: " ", color: 0 },
		{ value: "ROLL : 100SILK", color: 0xffffffff }
	] );
});

test("received gift silk prints second, in native order", () => {
	assert.deepEqual(
		mallTooltipRows( mallOffer( { currencyMask: 6, silk: 100, giftSilk: 5 } ), mallCopy ).map( row => row.value ),
		[ " ", "ROLL : 100SILK", "ROLL : 5GIFT" ]
	);
});

test("gift silk alone prints without a charged row", () => {
	assert.deepEqual(
		mallTooltipRows( mallOffer( { currencyMask: 4, giftSilk: 5 } ), mallCopy ).map( row => row.value ),
		[ " ", "ROLL : 5GIFT" ]
	);
});

test("an authored bit prints at zero; points author no price row", () => {
	assert.deepEqual(
		mallTooltipRows( mallOffer( { currencyMask: 2, silk: 0 } ), mallCopy ).map( row => row.value ),
		[ " ", "ROLL : 0SILK" ]
	);
	assert.deepEqual( mallTooltipRows( mallOffer( { currencyMask: 16 } ), mallCopy ), [] );
	assert.deepEqual( mallTooltipRows( mallOffer(), mallCopy ), [] );
});
