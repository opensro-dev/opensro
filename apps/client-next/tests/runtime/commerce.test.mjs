/*
===========================================================================

commerce.test.mjs - tests for commerce.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { saleResult, soldInventory, shopCatalog, buybackEntries } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/commerce.ts" ).href
);
test("native COS sale retains COS identity and opaque context without touching another inventory", () => {
	const p = Uint8Array.of( 1, 20, 42, 0, 0, 0, 2, 5, 0, 17, 0, 0, 0, 7 );
	assert.deepEqual( saleResult( 0xb06d, p ), { cosGid: 42, slot: 2, quantity: 5, context: 17, tag: 7 } );
	const inventory = [ { slot: 2, quantity: 10 }, { slot: 3, quantity: 6 } ], other = [ { slot: 2, quantity: 40 } ];
	assert.deepEqual( soldInventory( inventory, 2, 5 ), [ { slot: 2, quantity: 5 }, { slot: 3, quantity: 6 } ] );
	assert.equal( inventory[0].quantity, 10 );
	assert.equal( other[0].quantity, 40 );
	assert.throws( () => soldInventory( inventory, 2, 11 ) );
	assert.throws( () => soldInventory( inventory, 4, 1 ) );
	for ( let n = 2; n < p.length; n++ ) assert.throws( () => saleResult( 0xb06d, p.slice( 0, n ) ) );
	assert.throws( () => saleResult( 0xb06d, Uint8Array.of( ...p, 0 ) ) );
	const zero = p.slice();
	zero[2] = 0;
	assert.throws( () => saleResult( 0xb06d, zero ) );
	assert.equal( saleResult( 0xb06d, Uint8Array.of( 2, 1 ) ), null );
});

test("tax-adjusted gold prices retain signed-64 bounds without Number conversion", () => {
	const offer = { tab: 0, slot: 0, refObjId: 1, name: "item", maxStack: 1, price: "0" };
	const entry = { index: 0, id: 1, refObjId: 1, name: "item", quantity: 1, price: "0", plus: 0 };
	const encode = price =>
		new TextEncoder().encode(
			JSON.stringify( { version: 1, npc: 1, name: "shop", offers: [ { ...offer, price } ] } )
		);
	for ( const price of [ "0", "4294967296", "9007199254740993", "9223372036854775807" ] ) {
		assert.equal( shopCatalog( encode( price ) ).offers[0].price, price );
		assert.equal( buybackEntries( [ { ...entry, price } ] )[0].price, price );
	}
	for ( const price of [ "-1", "1.1", "9223372036854775808", 9007199254740992, "1e5" ] ) {
		assert.throws( () => shopCatalog( encode( price ) ) );
		assert.throws( () => buybackEntries( [ { ...entry, price } ] ) );
	}
});
test("the repurchase tab shows the newest sale in slot 1 and the oldest in slot 5 (5B6440)", async () => {
	const { restoreSlotEntry } = await import( sourceFileUrl( "src/engine/foundation/gameplay/commerce.ts" ).href );
	const ledger = count => Array.from( { length: count }, ( _, index ) => ({ id: 100 + index, index }) );
	const full = ledger( 5 );
	assert.deepEqual( [ 0, 1, 2, 3, 4 ].map( slot => restoreSlotEntry( full, slot )?.id ), [
		104,
		103,
		102,
		101,
		100
	] );
	// A sixth sale evicts the oldest (68F480); the server reindexes, and it lands in slot 1.
	const after = [ ...full.slice( 1 ), { id: 105 } ].map( ( row, index ) => ({ ...row, index }) );
	assert.deepEqual( [ 0, 1, 2, 3, 4 ].map( slot => restoreSlotEntry( after, slot )?.id ), [
		105,
		104,
		103,
		102,
		101
	] );
	const two = ledger( 2 );
	assert.deepEqual( [ 0, 1, 2 ].map( slot => restoreSlotEntry( two, slot )?.id ), [ 101, 100, undefined ] );
});

/*
================
cargoQuoteValidation
================
*/
test("cargo quotations preserve container identity and reject incomplete total tables", () => {
	const quote = { cosGid: 42, slot: 0, refObjId: 2151, quantity: 3, price: "75", totals: [ "75", "151", "227" ] };
	const decode = quotes =>
		shopCatalog(
			new TextEncoder().encode(
				JSON.stringify( { version: 1, npc: 17, name: "Trader", offers: [], cosGid: 42, saleQuotes: quotes } )
			)
		);
	assert.equal( decode( [ quote ] ).saleQuotes[0].totals[1], "151" );
	assert.equal( decode( [ quote, { ...quote, cosGid: 43 } ] ).saleQuotes.length, 2 );
	assert.throws( () => decode( [ quote, quote ] ) );
	for ( const totals of [ [ "75" ], [ "75", "151", "-1" ], [ "75", "151", 227 ], null ] ) {
		assert.throws( () => decode( [ { ...quote, totals } ] ) );
	}
});

/*
================
shop price lists

Issue #593: a package's price rows, two or more known currencies, each
once, ascending by payment bit, the first repeating the offer's own price.
================
*/
test("a package's price list is admitted only in payment order and matching its first row", () => {
	const base = { tab: 0, slot: 0, refObjId: 1, name: "item", maxStack: 1, price: "100000", currency: 1 };
	const encode = offer =>
		new TextEncoder().encode(
			JSON.stringify( { version: 1, npc: 1, name: "shop", offers: [ { ...base, ...offer } ] } )
		);
	const rows = [ { currency: 1, amount: "100000" }, { currency: 8, amount: "1000" } ];
	assert.deepEqual( shopCatalog( encode( { prices: rows } ) ).offers[0].prices, rows );
	assert.deepEqual(
		shopCatalog( encode( { prices: [ rows[0], { currency: 32, amount: "10" } ] } ) ).offers[0].prices[1],
		{
			currency: 32,
			amount: "10"
		}
	);
	for (
		const prices of [
			[ rows[0] ], // a single row is the offer's own price
			[ rows[1], rows[0] ], // out of payment order
			[ rows[0], rows[0] ], // a currency twice
			[ rows[0], { currency: 2, amount: "5" } ], // silk is the Item Mall's
			[ { currency: 1, amount: "99" }, rows[1] ], // first row differs from price
			[ rows[0], { currency: 8, amount: "-1" } ],
			"100000"
		]
	) assert.throws( () => shopCatalog( encode( { prices } ) ), Error, JSON.stringify( prices ) );
});

/*
================
Devil's Spirit token offers

Port-only (SRO_NASRUN, #3456 step 3b): a shop may price an offer in the
token (64) and send the buyer's balance; any other unknown payment is
still refused.
================
*/
test("a token offer and the shop's token balance are admitted, other payments are not", () => {
	const base = { tab: 0, slot: 0, refObjId: 1, name: "Spirit", maxStack: 1, price: "60", currency: 64 };
	const encode = ( offer, shop = {} ) =>
		new TextEncoder().encode(
			JSON.stringify( { version: 1, npc: 1, name: "shop", offers: [ { ...base, ...offer } ], ...shop } )
		);
	const shop = shopCatalog( encode( {}, { nasrunTokens: 130 } ) );
	assert.equal( shop.offers[0].currency, 64 );
	assert.equal( shop.nasrunTokens, 130 );
	assert.throws( () => shopCatalog( encode( { currency: 128 } ) ), /Invalid shop currency/ );
	for ( const nasrunTokens of [ -1, 1.5, "130" ] ) {
		assert.throws( () => shopCatalog( encode( {}, { nasrunTokens } ) ), Error, String( nasrunTokens ) );
	}
});

/*
================
Token offers travel apart from the native stock

The server sends token goods in tokenOffers, which a client built before
them never reads; this client checks them and joins them to the stock.
A gold row there, or a token row that is not an object, is refused.
================
*/
test("token offers arrive in tokenOffers and join the stock", () => {
	const gold = { tab: 0, slot: 0, refObjId: 1, name: "Potion", maxStack: 50, price: "60", currency: 1 };
	const token = { tab: 0, slot: 1, refObjId: 2, name: "Spirit", maxStack: 1, price: "60", currency: 64 };
	const encode = ( tokenOffers ) =>
		new TextEncoder().encode(
			JSON.stringify( { version: 1, npc: 1, name: "shop", offers: [ gold ], tokenOffers, nasrunTokens: 7 } )
		);
	const shop = shopCatalog( encode( [ token ] ) );
	assert.deepEqual( shop.offers.map( ( o ) => o.currency ), [ 1, 64 ] );
	assert.equal( shop.tokenOffers, undefined );
	assert.equal( shop.nasrunTokens, 7 );
	for ( const bad of [ [ gold ], [ null ], "x" ] ) {
		assert.throws( () => shopCatalog( encode( bad ) ), /Invalid token offer/, JSON.stringify( bad ) );
	}
});
