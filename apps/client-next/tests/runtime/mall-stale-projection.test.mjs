/*
===========================================================================

mall-stale-projection.test.mjs - a mall answer for a scene already left

A projection answers one outstanding request, but the transport's
resume ring can re-deliver an answer after the scene reset at the next
bootstrap retired that request (live failure group #2864). The answer
is stale, not corrupt: the mall sets it aside and the session lives.
The server's entering refusal (ops #1455) is the same lane: with a
catalogue request outstanding it retires the request with its reason
shown, and it never retires a purchase.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createMall } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/inventory/mall/mall.ts" ).href
);

/*
================
json
================
*/
function json( value ) {
	return new TextEncoder().encode( JSON.stringify( value ) );
}

/*
================
catalogue
================
*/
function catalogue() {
	return {
		version: 1,
		silk: 300,
		giftSilk: 0,
		points: 10,
		tabs: [ { shop: 2, tab: 0, category: "MALL_CONSUME", label: "Consumables" } ],
		offers: [ {
			group: 852,
			shop: 2,
			tab: 0,
			slot: 0,
			packageId: 20000,
			name: "Package 0",
			description: "Description",
			icon: "item/example.ddj",
			silk: 20,
			giftSilk: 0,
			currencyMask: 22,
			allowsPoints: true,
			purchaseLimit: 5,
			itemIds: [ 100 ]
		} ]
	};
}

/*
================
refusal

The entering refusal exactly as the server marshals it (register.go):
balances and an error, no tabs or offers.
================
*/
function refusal() {
	return json( {
		version: 1,
		silk: 0,
		giftSilk: 0,
		points: 0,
		tabs: null,
		offers: null,
		error: "mall is unavailable while entering"
	} );
}

const PURCHASE = { group: 852, shop: 2, tab: 0, slot: 0, packageId: 20000, quantity: 1, points: 0 };
const RECEIPT = new Uint8Array( [ 1, 0x18, 0x54, 3, 2, 0, 0, 1, 13, 1, 0 ] );

test("a projection for a scene already left is set aside, never fatal", () => {
	const mall = createMall();
	mall.open( 0 );
	// The scene resets while the catalogue answer is in flight.
	mall.reset();
	assert.equal( mall.projection( json( catalogue() ) ), undefined );
	assert.equal( mall.state(), undefined );
	// The refusal gets the same treatment: no request, no session end.
	assert.equal( mall.projection( refusal() ), undefined );
	// The next scene's own request still works end to end.
	mall.open( 1000 );
	mall.projection( json( catalogue() ) );
	assert.equal( mall.state()?.silk, 300 );
});

test("a re-delivered catalogue is dropped once it has been applied", () => {
	const mall = createMall();
	mall.open( 0 );
	mall.projection( json( catalogue() ) );
	const applied = mall.state();
	assert.ok( applied );
	// The resume ring delivers the same answer again.
	assert.equal( mall.projection( json( catalogue() ) ), undefined );
	assert.equal( mall.state(), applied );
});

test("a re-delivered purchase projection keeps the staged delivery", () => {
	const mall = createMall();
	mall.open( 0 );
	mall.projection( json( catalogue() ) );
	mall.purchase( PURCHASE, 10 );
	const delivery = json( { ...catalogue(), silk: 280, items: [ { slot: 13 } ] } );
	assert.ok( Array.isArray( mall.projection( delivery ) ) );
	// The same projection again while the receipt is still owed: the
	// staged delivery stands and the receipt still completes it.
	assert.equal( mall.projection( delivery ), undefined );
	mall.acknowledge( RECEIPT, [ 13 ] );
	assert.equal( mall.state()?.silk, 280 );
	assert.equal( mall.pending(), false );
});

test("the entering refusal retires a catalogue request with its reason", () => {
	const mall = createMall();
	mall.open( 0 );
	assert.equal( mall.projection( refusal() ), undefined );
	const state = mall.state();
	assert.ok( state );
	assert.equal( state.error, "mall is unavailable while entering" );
	assert.equal( state.pending, false );
	assert.equal( mall.pending(), false );
	// Retired means the mall can be opened again.
	mall.open( 1000 );
	mall.projection( json( catalogue() ) );
	assert.equal( mall.state()?.error, undefined );
});

test("the entering refusal never retires a purchase", () => {
	const mall = createMall();
	mall.open( 0 );
	mall.projection( json( catalogue() ) );
	mall.purchase( PURCHASE, 10 );
	assert.equal( mall.projection( refusal() ), undefined );
	assert.equal( mall.pending(), true );
	const delivery = json( { ...catalogue(), silk: 280, items: [ { slot: 13 } ] } );
	assert.ok( Array.isArray( mall.projection( delivery ) ) );
	mall.acknowledge( RECEIPT, [ 13 ] );
	assert.equal( mall.state()?.silk, 280 );
});
