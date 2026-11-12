/*
===========================================================================

shop.test.mjs - tests for inventory.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createInventory } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts" ).href
);
const json = o => new TextEncoder().encode( JSON.stringify( o ) );
const catalog = {
	version: 1,
	npc: 17,
	name: "Merchant",
	offers: [ { tab: 0, slot: 2, refObjId: 3630, name: "Potion", price: "60", maxStack: 50 } ]
};
function fixture() {
	const sent = [], sounds = [], inv = createInventory( f => sent.push( f ), () => {}, cue => sounds.push( cue ) );
	inv.bootstrap( { inventorySlotCount: 109, equipmentSlotCount: 13 } );
	inv.openShop( 17, 0 );
	inv.receive( 11, json( catalog ) );
	return { inv, sent, sounds };
}
test("shop artwork follows reference replacement and does not rebuild on unchanged snapshots", () => {
	const { inv } = fixture();
	const before = inv.state().shop;
	assert.equal( before, inv.state().shop );
	inv.references( [ { refObjId: 3630, typeFlags: 0x8ec, name: "Potion", icon: "item/etc/hp_potion_01.ddj" } ] );
	const after = inv.state().shop;
	assert.notEqual( before, after );
	assert.equal( after.offers[0].icon, "item/etc/hp_potion_01.ddj" );
	assert.equal( after, inv.state().shop );
	inv.references( [ { refObjId: 3630, typeFlags: 0x8ec, name: "Potion", icon: "item/etc/hp_potion_02.ddj" } ] );
	assert.equal( inv.state().shop.offers[0].icon, "item/etc/hp_potion_02.ddj" );
	inv.bootstrap( { inventorySlotCount: 109, equipmentSlotCount: 13 } );
	assert.equal( inv.state().shop, undefined );
});
function trader( rows, offers ) {
	const inv = createInventory( () => {}, () => {}, () => {} );
	inv.bootstrap( { inventorySlotCount: 109, equipmentSlotCount: 13, refItemSnapshot: rows } );
	inv.openShop( 23, 0 );
	inv.receive( 11, json( { version: 1, npc: 23, name: "Trader", offers } ) );
	return inv;
}
const TRADE_ROW = { refObjId: 7001, typeFlags: 0x46c };
const TRADE_OFFER = { tab: 0, slot: 0, refObjId: 7001, name: "Trade goods", price: "100", maxStack: 40 };
test("the sell-end close takes a specialty-goods shop and leaves any other shop open", () => {
	// The potion catalogue is no specialty page (a good with no reference
	// row never marks it either): the close leaves this shop alone.
	const plain = fixture();
	assert.equal( plain.inv.state().shop.specialty, false );
	assert.equal( plain.inv.closeSpecialtyShop(), false );
	assert.ok( plain.inv.state().shop );

	// Trade goods throughout: the open page is the specialty-goods page
	// and the close takes the shop, exactly once.
	const specialty = trader( [ TRADE_ROW ], [ TRADE_OFFER ] );
	assert.equal( specialty.state().shop.specialty, true );
	assert.equal( specialty.closeSpecialtyShop(), true );
	assert.equal( specialty.state().shop, undefined );
	assert.equal( specialty.closeSpecialtyShop(), false );

	// A package classifies by its first content, as natively.
	const bundle = trader( [ TRADE_ROW ], [ {
		tab: 0,
		slot: 0,
		refObjId: 8001,
		name: "Bundle",
		price: "100",
		maxStack: 1,
		contents: [ { refObjId: 7001, name: "Trade goods", quantity: 10, plus: 0 } ]
	} ] );
	assert.equal( bundle.state().shop.specialty, true );

	// One ordinary good among trade goods: not a specialty page.
	const mixed = trader( [ TRADE_ROW, { refObjId: 3630, typeFlags: 0x8ec } ], [
		TRADE_OFFER,
		{ tab: 0, slot: 2, refObjId: 3630, name: "Potion", price: "60", maxStack: 50 }
	] );
	assert.equal( mixed.state().shop.specialty, false );
	assert.equal( mixed.closeSpecialtyShop(), false );
	assert.ok( mixed.state().shop );
});
function purchase( quantity = 3 ) {
	return {
		version: 1,
		npc: 17,
		tab: 0,
		slot: 2,
		quantity,
		items: [ { slot: 13, refObjId: 3630, typeFlags: 0x8ec, name: "Potion", body: [ 46, 14, 0, 0, quantity, 0 ] } ]
	};
}
test("shop purchase admits missing item reference and waits for matching native receipt", () => {
	const { inv, sent } = fixture();
	inv.trade( true, 2, 3, 0, 1 );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ 8, 0, 2, 3, 0, 17, 0, 0, 0 ] );
	assert.equal( inv.state().inventory.length, 0 );
	inv.receive( 12, json( purchase() ) );
	assert.equal( inv.state().inventory[0].quantity, 3 );
	assert.equal( inv.state().inventoryPending, true );
	inv.receive( 0xb06d, Uint8Array.of( 1, 8, 0, 2, 1, 13, 3, 0 ) );
	assert.equal( inv.state().inventoryPending, false );
	inv.trade( false, 13, 2, 0, 2 );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ 9, 13, 2, 0, 17, 0, 0, 0 ] );
	inv.receive( 0xb06d, Uint8Array.of( 1, 9, 13, 2, 0, 17, 0, 0, 0, 0 ) );
	assert.equal( inv.state().inventory[0].quantity, 1 );
	assert.equal( inv.state().inventoryPending, false );
});
test("shop snapshot rejects stale, duplicate and malformed data atomically", () => {
	for (
		const change of [
			p => p.npc = 18,
			p => p.quantity = 2,
			p => p.items.push( p.items[0] ),
			p => p.items[0].body.push( 1 ),
			p => p.items[0].body[0] = 256
		]
	) {
		const { inv } = fixture();
		inv.trade( true, 2, 3, 0, 1 );
		const p = purchase();
		change( p );
		assert.throws( () => inv.receive( 12, json( p ) ) );
		assert.equal( inv.state().inventory.length, 0 );
		assert.equal( inv.state().inventoryPending, true );
	}
});
test("shop price admission, pending barrier, rejection and reconnect reset", () => {
	const { inv } = fixture();
	assert.throws( () => inv.trade( true, 3, 1, 0, 0 ) );
	inv.trade( true, 2, 1, 0, 1 );
	assert.throws( () => inv.trade( true, 2, 1, 0, 2 ) );
	inv.receive( 0xb06d, Uint8Array.of( 2, 1 ) );
	assert.equal( inv.state().inventoryPending, false );
	inv.trade( true, 2, 1, 0, 3 );
	assert.throws( () => inv.step( 10003 ), /timed out/ );
	assert.throws( () => inv.trade( true, 2, 1, 0, 10004 ) );
	inv.clear();
	assert.equal( inv.state().shop, undefined );
	inv.openShop( 17, 0 );
	assert.throws( () => inv.receive( 11, json( { ...catalog, offers: [ { ...catalog.offers[0], price: "-1" } ] } ) ) );
	assert.equal( inv.state().shop, undefined );
});

test("purchase updates preserve unrelated inventory and catalog read timeouts can retry", () => {
	const sent = [], inv = createInventory( f => sent.push( f ) );
	inv.bootstrap( {
		inventorySlotCount: 109,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: 99, typeFlags: 0x6c } ],
		equipItems: [ { slot: 20, refObjId: 99, body: [ 99, 0, 0, 0, 9, 0 ] } ]
	} );
	inv.openShop( 17, 0 );
	assert.equal( inv.step( 10000 ), true );
	assert.equal( inv.state().inventoryPending, false );
	inv.openShop( 17, 10001 );
	inv.receive( 11, json( catalog ) );
	inv.trade( true, 2, 3, 0, 10002 );
	inv.receive( 12, json( purchase() ) );
	assert.equal( inv.state().inventory.find( i => i.slot === 20 ).quantity, 9 );
	assert.equal( inv.state().inventory.find( i => i.slot === 13 ).quantity, 3 );
});

const entry = { index: 0, id: 7, refObjId: 3630, name: "Potion", quantity: 3, price: "45", plus: 0 };
test("COS commerce keeps item ownership separate and waits for its native receipt", () => {
	const { inv, sent } = fixture();
	let record = {
		gid: 7,
		refObjId: 102,
		band: 4,
		hp: 100,
		mp: 50,
		status: 4,
		dead: false,
		commandMode: 0xc7,
		inventory: []
	};
	inv.cosTrade( record, true, 2, 3, 0, 1 );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ 19, 7, 0, 0, 0, 0, 2, 3, 0, 17, 0, 0, 0 ] );
	const p = { ...purchase(), cosGid: 7, items: purchase().items.map( row => ({ ...row, slot: 0 }) ) };
	assert.throws( () => inv.cosShopSnapshot( record, json( { ...p, cosGid: 8 } ) ) );
	record = inv.cosShopSnapshot( record, json( p ) );
	assert.equal( record.inventory[0].quantity, 3 );
	assert.equal( inv.state().inventory.length, 0 );
	assert.equal( inv.state().inventoryPending, true );
	assert.throws( () => inv.cosPurchase( Uint8Array.of( 1, 19, 7, 0, 0, 0, 0, 2, 1, 1, 3, 0 ) ) );
	const receipt = Uint8Array.of( 1, 19, 7, 0, 0, 0, 0, 2, 1, 0, 3, 0 );
	inv.cosPurchase( receipt );
	assert.equal( inv.state().inventoryPending, false );
	assert.throws( () => inv.cosPurchase( receipt ) );
	inv.cosTrade( record, false, 0, 1, 0, 2 );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ 20, 7, 0, 0, 0, 0, 1, 0, 17, 0, 0, 0 ] );
	assert.throws( () => inv.cosSold( 8, 0, 1, 17 ) );
	assert.equal( inv.state().inventoryPending, true );
	inv.cosSold( 7, 0, 1, 17 );
	assert.equal( inv.state().inventoryPending, false );
});
test("buyback binds merchant and immutable entry and atomically restores item", () => {
	const { inv, sent, sounds } = fixture();
	inv.receive( 13, json( { version: 1, npc: 17, id: 0, entries: [ entry ], items: [] } ) );
	inv.buyback( 7, 1 );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ 17, 0, 0, 0, 0 ] );
	assert.throws( () => inv.trade( true, 2, 1, 0, 2 ) );
	const response = { version: 1, npc: 17, id: 7, entries: [], items: purchase().items };
	assert.throws( () => inv.receive( 13, json( { ...response, id: 8 } ) ) );
	assert.equal( inv.state().inventory.length, 0 );
	assert.throws( () => inv.receive( 13, json( { ...response, entries: [ entry, entry ] } ) ) );
	assert.equal( inv.state().inventory.length, 0 );
	inv.receive( 13, json( response ) );
	assert.deepEqual( sounds, [] );
	assert.equal( inv.state().inventoryPending, true );
	inv.receive( 0xb06d, Uint8Array.of( 1, 0x22, 13, 0, 3, 0 ) );
	inv.receive( 0xb7e7, Uint8Array.of( 1 ) );
	assert.equal( inv.state().inventory[0].quantity, 3 );
	assert.equal( inv.state().inventoryPending, false );
	assert.deepEqual( inv.state().shop.buyback, [] );
	assert.throws( () => inv.buyback( 7, 2 ) );
	assert.throws( () => inv.receive( 13, json( response ) ) );
	assert.deepEqual( sounds, [ { handle: "SND_EQUIP", typeFlags: 0x8ec } ] );
	assert.throws( () => inv.receive( 0xb06d, Uint8Array.of( 1, 0x22, 13, 0, 3, 0 ) ) );
	assert.equal( sounds.length, 1 );
});
test("buyback refusal preserves inventory; timeout requires reconnect", () => {
	const { inv } = fixture();
	inv.receive( 13, json( { version: 1, npc: 17, id: 0, entries: [ entry ], items: [] } ) );
	inv.buyback( 7, 1 );
	inv.receive(
		13,
		json( { version: 1, npc: 17, id: 7, entries: [ entry ], items: [], error: "Insufficient gold" } )
	);
	assert.equal( inv.state().inventory.length, 0 );
	assert.equal( inv.state().error, "Insufficient gold" );
	inv.buyback( 7, 2 );
	assert.throws( () => inv.step( 10002 ), /timed out/ );
	assert.throws( () => inv.buyback( 7, 10003 ) );
});

test("native buyback acknowledgement cannot release an unmatched restore", () => {
	const { inv, sent } = fixture();
	inv.receive( 13, json( { version: 1, npc: 17, id: 0, entries: [ { ...entry, index: 3 } ], items: [] } ) );
	inv.buyback( 7, 1 );
	assert.equal( sent.at( -1 ).opcode, 0x77e7 );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ 17, 0, 0, 0, 3 ] );
	inv.receive( 0xb7e7, Uint8Array.of( 1 ) );
	assert.equal( inv.state().inventoryPending, true );
	assert.throws( () => inv.receive( 0xb06d, Uint8Array.of( 1, 0x22, 13, 3, 3, 0 ) ) );
	inv.receive( 13, json( { version: 1, npc: 17, id: 7, entries: [], items: purchase().items } ) );
	assert.throws( () => inv.receive( 0xb7e7, Uint8Array.of( 2, 1 ) ) );
	for ( const bytes of [ [ 1, 0x22, 13, 2, 3, 0 ], [ 1, 0x22, 14, 3, 3, 0 ], [ 1, 0x22, 13, 3, 2, 0 ] ] ) {
		assert.throws( () => inv.receive( 0xb06d, Uint8Array.from( bytes ) ) );
	}
	inv.receive( 0xb06d, Uint8Array.of( 1, 0x22, 13, 3, 3, 0 ) );
	assert.equal( inv.state().inventoryPending, false );
});

test("shop completion identity advances only for matching replies and timeout, not cached snapshots", () => {
	const inv = createInventory( () => {} );
	assert.equal( inv.state().shopCompletionRevision, 0 );
	inv.openShop( 17, 0 );
	inv.receive( 11, json( { ...catalog, npc: 18 } ) );
	assert.equal( inv.state().shopCompletionRevision, 0 );
	inv.receive( 11, json( { ...catalog, error: "Too far", offers: [] } ) );
	assert.equal( inv.state().shopCompletionRevision, 1 );
	inv.state();
	assert.equal( inv.state().shopCompletionRevision, 1 );
	inv.openShop( 17, 1 );
	inv.step( 10001 );
	assert.equal( inv.state().shopCompletionRevision, 2 );
	assert.equal( inv.state().shop, undefined );
});
