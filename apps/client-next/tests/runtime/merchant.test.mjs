/*
===========================================================================

merchant.test.mjs - tests for merchant.ts, merchant-branches.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fc from "fast-check";

const { merchantSelection, merchantQuote, merchantPage, merchantCommand } = await import(
	sourceFileUrl( "src/engine/foundation/ui/merchant.ts" ).href
);
const offer = ( tab, slot, price = "60" ) => ({ tab, slot, refObjId: 100 + slot, name: "Item", price, maxStack: 50 });
const inventory = [ {
	slot: 13,
	refObjId: 102,
	name: "Item",
	quantity: 4,
	plus: 0,
	durability: 0,
	variance: "0",
	magic: [],
	typeFlags: 0x8ec
} ];
const fixture = () => ({
	npc: 17,
	name: "Merchant",
	tabs: [ { index: 0, labelSymbol: "A" }, { index: 1, labelSymbol: "B" }, { index: 2, labelSymbol: "Empty" } ],
	offers: [ offer( 0, 2 ), offer( 0, 31 ), offer( 1, 0 ) ],
	saleQuotes: [ { slot: 13, refObjId: 102, quantity: 4, price: "20" } ],
	buyback: [ { id: 7, index: 0, refObjId: 102, quantity: 4, plus: 0, name: "Item", price: "80" }, {
		id: 8,
		index: 1,
		refObjId: 103,
		quantity: 1,
		plus: 0,
		name: "Other",
		price: "15"
	} ]
});
test("merchant preserves authored sparse slots and empty tabs through repeated paging", () => {
	const shop = fixture();
	assert.deepEqual( merchantPage( shop, 0, 0 ).tabs, [ 0, 1, 2 ] );
	assert.equal( merchantPage( shop, 0, 1 ).slots[1].item.slot, 31 );
	assert.equal( merchantPage( shop, 0, 0 ).slots[0], undefined );
	assert.equal( merchantPage( shop, 2, 5 ).pages, 1 );
	assert.ok( merchantPage( shop, 2, 5 ).slots.every( s => !s ) );
	fc.assert(
		fc.property(
			fc.array( fc.tuple( fc.integer( { min: 0, max: 3 } ), fc.integer( { min: -20, max: 20 } ) ), {
				maxLength: 100
			} ),
			events => {
				let page = 0;
				for ( const [tab, delta] of events ) {
					const p = merchantPage( shop, tab, page + delta );
					assert.ok( p.page >= 0 && p.page < p.pages );
					for ( const [index, row] of p.slots.entries() ) {
						if ( row ) {
							assert.equal( row.item.slot, p.page * 30 + index );
							assert.equal( row.item.tab, p.tab );
						}
					}
					page = p.page;
				}
			}
		),
		{ seed: 706, numRuns: 200 }
	);
});
test("merchant quantity and decimal gold checks preserve precision and zero-price offers", () => {
	const shop = fixture(), selection = merchantSelection( "buy", 0, shop, inventory );
	for ( const quantity of [ "0", "51", "65536", "-1", "1.5", "1e1", "NaN", "" ] ) {
		assert.equal( merchantQuote( selection, shop, inventory, quantity, "99999" ).valid, false, quantity );
	}
	assert.equal( merchantQuote( selection, shop, inventory, "50", "3000" ).valid, true );
	assert.equal( merchantQuote( selection, shop, inventory, "50", "2999" ).valid, false );
	shop.offers[0].price = "9007199254740993";
	const precise = merchantSelection( "buy", 0, shop, inventory );
	assert.equal( merchantQuote( precise, shop, inventory, "1", "9007199254740993" ).total, 9007199254740993n );
	shop.offers[0].price = "0";
	assert.equal(
		merchantQuote( merchantSelection( "buy", 0, shop, inventory ), shop, inventory, "1", "0" ).valid,
		true
	);
});
test("sale requires the authoritative quote and cannot confirm a replaced inventory item", () => {
	const shop = fixture(), selection = merchantSelection( "sell", 13, shop, inventory );
	assert.equal(
		merchantQuote( selection, shop, inventory, "3", "0" ).valid,
		false,
		"opening a modal must not reuse an earlier quote"
	);
	shop.saleQuotes = [ ...shop.saleQuotes ];
	assert.equal( merchantQuote( selection, shop, inventory, "3", "0" ).total, 60n );
	assert.equal( merchantQuote( selection, shop, inventory, "5", "0" ).valid, false );
	assert.equal( merchantQuote( selection, { ...shop, saleQuotes: [] }, inventory, "1", "0" ).valid, false );
	for (
		const item of [ { ...inventory[0], quantity: 3 }, { ...inventory[0], refObjId: 103 }, {
			...inventory[0],
			magic: [ "123" ]
		} ]
	) assert.equal( merchantQuote( selection, shop, [ item ], "1", "0" ), null );
});
test("buyback identity survives ordinal shifts but rejects eviction, repricing and merchant replacement", () => {
	const shop = fixture(), selection = merchantSelection( "buyback", 1, shop, inventory );
	shop.buyback.shift();
	shop.buyback[0].index = 0;
	const q = merchantQuote( selection, shop, inventory, "999", "100" );
	assert.equal( q.valid, true );
	assert.equal( q.quantity, 1 );
	assert.equal( q.total, 15n );
	assert.equal( merchantQuote( selection, { ...shop, npc: 18 }, inventory, "1", "100" ), null );
	shop.buyback[0].price = "16";
	assert.equal( merchantQuote( selection, shop, inventory, "1", "100" ), null );
	shop.buyback = [];
	assert.equal( merchantQuote( selection, shop, inventory, "1", "100" ), null );
});

const { merchantBranches } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/merchant-branches.ts" ).href
);
test("merchant branches preserve every authored tab without the four-tab truncation", () => {
	const groups = [ {
			Tabs: Array.from(
				{ length: 12 },
				( _, i ) => ({
					GroupID: 850 + Math.floor( i / 3 ),
					GroupLabelSymbol: "SN_GROUP_" + Math.floor( i / 3 )
				})
			)
		} ],
		branches = merchantBranches( groups );
	assert.equal( branches.length, 4 );
	assert.deepEqual( branches.flatMap( b => b.tabs ), Array.from( { length: 12 }, ( _, i ) => i ) );
	const shop = {
		npc: 1,
		name: "Armor",
		tabs: Array.from( { length: 12 }, ( _, i ) => ({ index: i, labelSymbol: "Tab" }) ),
		offers: Array.from( { length: 12 }, ( _, i ) => offer( i, 0 ) )
	};
	for ( const branch of branches ) {
		const p = merchantPage( shop, branch.tabs[0], 0, branch.tabs );
		assert.deepEqual( p.tabs, branch.tabs );
		assert.equal( p.slots[0].item.tab, branch.tabs[0] );
	}
	assert.throws(
		() =>
			merchantBranches( [ {
				Tabs: Array.from( { length: 5 }, () => ({ GroupID: 1, GroupLabelSymbol: "A" }) )
			} ] ),
		/branch tabs/
	);
});

/*
================
cargoQuoteAndContainerIdentity
================
*/
test("cargo quotes use whole-quantity rounding and bind owner and transport", () => {
	const item = { ...inventory[0], slot: 0, quantity: 3, typeFlags: 0xc6c, label: "Victim" };
	const quote = {
		cosGid: 42,
		slot: 0,
		refObjId: item.refObjId,
		quantity: 3,
		price: "75",
		totals: [ "75", "151", "227" ]
	};
	const shop = { ...fixture(), cosGid: 42, saleQuotes: [ quote ] };
	const selection = merchantSelection( "sell", 0, shop, [ item ] );
	assert.ok( selection );
	assert.equal( merchantQuote( selection, shop, [ item ], "2", "0" ).valid, false );
	const refreshed = { ...shop, saleQuotes: [ { ...quote } ] };
	assert.equal( merchantQuote( selection, refreshed, [ item ], "2", "0" ).total, 151n );
	assert.equal( merchantQuote( selection, refreshed, [ item ], "3", "0" ).total, 227n );
	assert.equal( merchantQuote( selection, { ...refreshed, cosGid: 43 }, [ item ], "2", "0" ), null );
	assert.equal( merchantQuote( selection, refreshed, [ { ...item, label: "Other" } ], "2", "0" ), null );
	assert.deepEqual( merchantCommand( selection, 2 ), { kind: "cos-shop-sell", gid: 42, slot: 0, quantity: 2 } );
	const purchase = merchantSelection( "buy", 0, shop, [ item ] );
	assert.deepEqual( merchantCommand( purchase, 3 ), { kind: "cos-shop-buy", gid: 42, slot: 2, tab: 0, quantity: 3 } );
});
