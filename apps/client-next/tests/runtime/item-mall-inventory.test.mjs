/*
===========================================================================

item-mall-inventory.test.mjs - native purchase receipts and inventory isolation

===========================================================================
*/
import test from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
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
catalog
================
*/
function catalog() {
	return {
		version: 1,
		silk: 100,
		giftSilk: 20,
		points: 10,
		tabs: [ { shop: 2, tab: 3, category: "MALL_CONSUME", label: "SN_TAB" } ],
		offers: [ {
			group: 852,
			shop: 2,
			tab: 3,
			slot: 4,
			packageId: 100,
			name: "SN_PACKAGE",
			description: "SN_DESC",
			icon: "item/etc/hp.ddj",
			silk: 30,
			giftSilk: 2,
			currencyMask: 22,
			allowsPoints: true,
			purchaseLimit: 5,
			itemIds: [ 3630 ]
		} ]
	};
}

/*
================
fixture
================
*/
function fixture() {
	const sent = [], owner = createInventory( frame => sent.push( frame ) );
	owner.bootstrap( { inventorySlotCount: 109, equipmentSlotCount: 13 } );
	owner.openMall( 0 );
	owner.receive( 15, json( catalog() ) );
	return { owner, sent, request: { group: 852, shop: 2, tab: 3, slot: 4, packageId: 100, quantity: 2, points: 5 } };
}

/*
================
delivery
================
*/
function delivery() {
	return {
		...catalog(),
		silk: 45,
		giftSilk: 16,
		points: 5,
		items: [ { slot: 13, refObjId: 3630, typeFlags: 0x8ec, name: "Potion", body: [ 46, 14, 0, 0, 20, 0 ] } ]
	};
}

test("mall commits currency and items only after matching native receipt", () => {
	const { owner, sent, request } = fixture();
	owner.purchaseMall( request, 1 );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ 24, 84, 3, 2, 3, 4, 2, 0, 5, 0, 0, 0, 100, 0, 0, 0 ] );
	assert.throws( () => owner.openShop( 17, 2 ), /unavailable/i );
	owner.receive( 15, json( delivery() ) );
	assert.equal( owner.state().inventory.length, 0 );
	assert.equal( owner.state().itemMall?.silk, 100 );
	assert.equal( owner.state().inventoryPending, true );
	owner.receive( 0xb06d, Uint8Array.of( 1, 24, 84, 3, 2, 3, 4, 1, 13, 2, 0 ) );
	assert.equal( owner.state().inventory[0].quantity, 20 );
	assert.equal( owner.state().itemMall?.silk, 45 );
	assert.equal( owner.state().inventoryPending, false );
});

test("mall rejects unsolicited, stale and malformed delivery without publishing staged changes", () => {
	for (
		const kind of [
			"unsolicited",
			"equipment",
			"truncated",
			"wrong-slot",
			"wrong-quantity",
			"rejection-after-stage"
		]
	) {
		const { owner, request } = fixture();
		if ( kind === "unsolicited" ) {
			assert.throws( () => owner.receive( 15, json( delivery() ) ) );
			continue;
		}
		owner.purchaseMall( request, 1 );
		const next = delivery();
		if ( kind === "equipment" ) {
			next.items[0].slot = 0;
			assert.throws( () => owner.receive( 15, json( next ) ) );
			continue;
		}
		owner.receive( 15, json( next ) );
		const receipt = Uint8Array.of( 1, 24, 84, 3, 2, 3, 4, 1, 13, 2, 0 );
		if ( kind === "wrong-slot" ) receipt[8] = 14;
		if ( kind === "wrong-quantity" ) receipt[9] = 3;
		assert.throws( () =>
			owner.receive(
				0xb06d,
				kind === "rejection-after-stage" ?
					Uint8Array.of( 2, 7 ) :
					kind === "truncated" ?
					receipt.subarray( 0, 10 ) :
					receipt
			)
		);
		assert.equal( owner.state().inventory.length, 0 );
		assert.equal( owner.state().itemMall?.silk, 100 );
	}
});

test("mall rejects duplicate clicks, excessive points and late receipts after timeout", () => {
	const { owner, sent, request } = fixture();
	assert.throws( () => owner.purchaseMall( { ...request, points: 61 }, 1 ) );
	assert.throws( () => owner.purchaseMall( { ...request, quantity: 65535 }, 1 ) );
	assert.equal( sent.length, 1 );
	owner.purchaseMall( request, 1 );
	assert.throws( () => owner.purchaseMall( request, 2 ) );
	assert.throws( () => owner.step( 10001 ), /reconnect/ );
	assert.throws( () => owner.receive( 15, json( delivery() ) ) );
	assert.equal( owner.state().inventory.length, 0 );
	owner.clear();
	assert.equal( owner.state().itemMall, undefined );
});

test("mall catalogue rejects numeric overflow, duplicate identities and escaped artwork paths", () => {
	for (
		const mutate of [
			value => value.silk = -1,
			value => value.points = 4294967296,
			value => value.offers.push( { ...value.offers[0] } ),
			value => value.offers[0].icon = "../../secret",
			value => value.offers[0].icon = "%2e%2e/secret.ddj",
			value => value.offers[0].icon = "item/icon.ddj?redirect=secret",
			value => value.offers[0].currencyMask = 16,
			value => value.offers[0].purchaseLimit = 0
		]
	) {
		const { owner } = fixture();
		owner.openMall( 1 );
		const value = catalog();
		mutate( value );
		assert.throws( () => owner.receive( 15, json( value ) ) );
		assert.equal( owner.state().itemMall?.silk, 100 );
	}
});

test("mall refusal retains native banner context after clearing the pending request", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( { inventorySlotCount: 109, equipmentSlotCount: 13 } );
	game.seed( {
		gid: 1,
		refObjId: 1907,
		kind: "local-player",
		name: "Mall tester",
		regionId: 25000,
		x: 0,
		y: 0,
		z: 0,
		heading: 0
	} );
	game.command( { kind: "mall-open" }, 0, undefined );
	game.receive( { opcode: 15, payload: json( catalog() ) }, 1 );
	game.command(
		{
			kind: "mall-buy",
			request: { group: 852, shop: 2, tab: 3, slot: 4, packageId: 100, quantity: 1, points: 0 }
		},
		2,
		undefined
	);
	game.receive( { opcode: 0xb06d, payload: Uint8Array.of( 2, 7 ) }, 3 );
	const result = game.take();
	assert.equal( result?.itemMall?.pending, false );
	assert.equal( result?.itemMall?.silk, 100 );
	assert.equal( result?.notices?.at( -1 )?.banner, true );
	game.dispose();
});
