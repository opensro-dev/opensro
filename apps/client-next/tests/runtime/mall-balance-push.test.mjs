/*
===========================================================================

mall-balance-push.test.mjs - the beta's earned-silk balance push

A MALL_BALANCE_CONTROL frame (server action/betasilk.go) updates an open
mall's balance in place, does nothing before a catalogue has arrived, and
rejects anything but exactly silk, giftSilk and points.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createMall } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/inventory/mall/mall.ts" ).href
);
const { MALL_BALANCE_CONTROL, isWorldControl } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/commerce-controls.ts" ).href
);
const { createItemMall } = await import( "../../src/engine/runtime/ui/hud/item-mall.ts" );

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
		offers: [ 0, 1 ].map( slot => ({
			group: 852,
			shop: 2,
			tab: 0,
			slot,
			packageId: 20000 + slot,
			name: "Package " + slot,
			description: "Description",
			icon: "item/example.ddj",
			silk: 20,
			giftSilk: 0,
			currencyMask: 22,
			allowsPoints: true,
			purchaseLimit: 5,
			itemIds: [ 100 + slot ]
		}) )
	};
}

test("a balance push updates an open mall and routes as a world control", () => {
	assert.equal( isWorldControl( MALL_BALANCE_CONTROL ), true );
	const mall = createMall();
	// Before any catalogue there is no mall to update.
	mall.balance( json( { silk: 350, giftSilk: 0, points: 0 } ) );
	mall.open( 0 );
	mall.projection( json( { version: 1, silk: 300, giftSilk: 0, points: 0, tabs: [], offers: [] } ) );
	const opened = mall.state();
	assert.ok( opened );
	assert.equal( opened.silk, 300 );
	mall.balance( json( { silk: 350, giftSilk: 2, points: 1 } ) );
	const pushed = mall.state();
	assert.ok( pushed );
	assert.deepEqual( [ pushed.silk, pushed.giftSilk, pushed.points ], [ 350, 2, 1 ] );
	assert.notStrictEqual( pushed, opened );
	assert.equal( pushed.revision, opened.revision );
});

test("balance pushes preserve purchase and point confirmations", () => {
	const worker = createMall(), hud = createItemMall();
	worker.open( 0 );
	worker.projection( json( catalogue() ) );
	const initial = worker.state();
	assert.ok( initial );
	hud.observe( initial );
	hud.choose( initial.offers[0] );
	hud.showPoints();
	worker.balance( json( { silk: 350, giftSilk: 0, points: 10 } ) );
	const pushed = worker.state();
	assert.ok( pushed );
	hud.observe( pushed );
	assert.equal( hud.read( pushed ).selected?.packageId, initial.offers[0].packageId );
	assert.equal( hud.read( pushed ).pointDialog, true );
	assert.ok( hud.purchase( pushed ) );
});

test("a balance snapshot before worker admission cannot acknowledge a basket purchase", () => {
	for ( const accepted of [ true, false ] ) {
		const worker = createMall(), hud = createItemMall();
		const catalog = catalogue();
		worker.open( 0 );
		worker.projection( json( catalog ) );
		const initial = worker.state();
		assert.ok( initial );
		hud.open();
		hud.observe( initial );
		hud.browse( 7 );
		for ( const offer of initial.offers ) hud.reserve( offer );
		hud.askBatch( "basket", initial );
		hud.confirmQuestion( initial );
		const first = hud.takeNextPurchase( initial );
		assert.ok( first );
		assert.equal( first.slot, 0 );

		// This snapshot can already be in flight when the HUD posts the command.
		worker.balance( json( { silk: 350, giftSilk: 0, points: 10 } ) );
		const pushed = worker.state();
		assert.ok( pushed );
		assert.equal( pushed.pending, false );
		hud.observe( pushed );
		assert.equal( hud.read( pushed ).count, 2 );
		assert.equal( hud.takeNextPurchase( pushed ), null );
		worker.purchase( first, 1 );
		const pending = worker.state();
		assert.ok( pending );
		hud.observe( pending );
		assert.equal( hud.takeNextPurchase( pending ), null );
		assert.equal( hud.read( pending ).count, 2 );
		if ( accepted ) {
			worker.projection( json( { ...catalog, silk: 330, items: [ { slot: 13 } ] } ) );
			const receipt = new Uint8Array( [ 1, 0x18, 0x54, 3, 2, 0, 0, 1, 13, 1, 0 ] );
			worker.acknowledge( receipt, [ 13 ] );
		} else {
			assert.equal( worker.reject( new Uint8Array( [ 2, 1 ] ) ), true );
		}
		const completed = worker.state();
		assert.ok( completed );
		hud.observe( completed );
		assert.equal( hud.read( completed ).count, accepted ? 1 : 2 );
		const second = hud.takeNextPurchase( completed );
		if ( accepted ) {
			assert.ok( second );
			assert.equal( second.slot, 1 );
			worker.purchase( second, 2 );
		} else {
			assert.equal( second, null );
		}
		assert.equal( hud.takeNextPurchase( completed ), null );
	}
});

test("a malformed balance push is refused", () => {
	const mall = createMall();
	for (
		const bad of [
			{ silk: 1, giftSilk: 0 },
			{ silk: -1, giftSilk: 0, points: 0 },
			{ silk: 1.5, giftSilk: 0, points: 0 },
			{ silk: 1, giftSilk: 0, points: 0, extra: 1 }
		]
	) {
		assert.throws( () => mall.balance( json( bad ) ), /Invalid mall balance/ );
	}
});
