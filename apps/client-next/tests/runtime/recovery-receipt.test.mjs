/*
===========================================================================

recovery-receipt.test.mjs - recovery counts across inventory publications

Literal success receipts exercise the inventory and gameplay owners. Setup
sequences are regression scenarios, not reconstructions of incident history.
The answer's count is the slot's truth (755E40 never compares it with
what the client held); a repeated answer is applied (its lane restarts,
as 755E40) without ending the session. No test substitutes a
private reducer.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const RECOVERY = { country: 0, abnormal: 0 };
const RECEIPTS = [
	{ slot: 15, flags: 0x08ec, count: 49, hex: "010f3100ec08" },
	{ slot: 25, flags: 0x10ec, count: 49, hex: "01193100ec10" },
	{ slot: 16, flags: 0x10ec, count: 49, hex: "01103100ec10" },
	{ slot: 29, flags: 0x08ec, count: 4, hex: "011d0400ec08" }
];

/*
================
bootstrap
================
*/
function bootstrap( flags, rows ) {
	return {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: 1, typeFlags: flags, nativeFields: { maxStack: 50 } } ],
		equipItems: rows.map( ( [slot, quantity] ) => ({ slot, refObjId: 1, body: [ 1, 0, 0, 0, quantity, 0 ] }) )
	};
}

/*
================
inventory
================
*/
function inventory( flags, rows ) {
	/** @type {import("../../src/engine/contracts/network").WireFrame[]} */
	const sent = [];
	const owner = createInventory( frame => sent.push( frame ) );
	owner.bootstrap( bootstrap( flags, rows ) );
	return { owner, sent };
}

/*
================
quantity
================
*/
function quantity( owner, slot ) {
	return defined( owner.state().inventory.find( row => row.slot === slot ), "inventory slot" ).quantity;
}

/*
================
json
================
*/
function json( value ) {
	return new TextEncoder().encode( JSON.stringify( value ) );
}

for ( const receipt of RECEIPTS ) {
	test(`literal ${receipt.hex} consumes the absolute pickup count while use is pending`, () => {
		const { owner, sent } = inventory( receipt.flags, [ [ receipt.slot, receipt.count ] ] );
		owner.use( receipt.slot, 0 );
		assert.equal( quantity( owner, receipt.slot ), receipt.count, "sending does not spend locally" );
		// Pickup bodies publish the new absolute total, including an occupied slot.
		const pickup = Uint8Array.of( 1, 6, receipt.slot, 1, 0, 0, 0, receipt.count + 1, 0 );
		owner.receive( 0xb06d, pickup, 1 );
		owner.receive( 0xb06d, pickup, 2 );
		assert.equal( quantity( owner, receipt.slot ), receipt.count + 1 );
		assert.equal( owner.state().inventoryPending, true );
		assert.throws( () => owner.use( receipt.slot, 3 ), /unavailable/ );
		assert.equal( sent.length, 1 );
		owner.receive( 0xb5bd, Buffer.from( receipt.hex, "hex" ), 4, RECOVERY );
		assert.equal( quantity( owner, receipt.slot ), receipt.count );
		assert.equal( owner.state().inventoryPending, false );
		const before = owner.state().inventory;
		assert.doesNotThrow( () => owner.receive( 0xb5bd, Buffer.from( receipt.hex, "hex" ), 5, RECOVERY ) );
		assert.deepEqual( owner.state().inventory, before, "a repeated answer restates the same count" );
	});
}

test("shop fill to fifty waits for the purchase echo before literal recovery", () => {
	const { owner, sent } = inventory( 0x10ec, [ [ 25, 45 ] ] );
	owner.openShop( 17, 0 );
	owner.receive(
		11,
		json( {
			version: 1,
			npc: 17,
			name: "Merchant",
			offers: [ { tab: 0, slot: 2, refObjId: 1, name: "Potion", price: "60", maxStack: 50 } ]
		} ),
		1
	);
	owner.trade( true, 2, 5, 0, 2 );
	owner.receive(
		12,
		json( {
			version: 1,
			npc: 17,
			tab: 0,
			slot: 2,
			quantity: 5,
			items: [ { slot: 25, refObjId: 1, typeFlags: 0x10ec, name: "Potion", body: [ 1, 0, 0, 0, 50, 0 ] } ]
		} ),
		3
	);
	assert.equal( quantity( owner, 25 ), 50 );
	assert.equal( owner.state().inventoryPending, true );
	assert.throws( () => owner.use( 25, 4 ), /unavailable/ );
	owner.receive( 0xb06d, Buffer.from( "0108000201190500", "hex" ), 5 );
	assert.equal( owner.state().inventoryPending, false );
	owner.use( 25, 6 );
	owner.receive( 0xb5bd, Buffer.from( "01193100ec10", "hex" ), 7, RECOVERY );
	assert.equal( quantity( owner, 25 ), 49 );
	assert.equal( sent.filter( frame => frame.opcode === 0x75bd ).length, 1 );
});

for ( const full of [ false, true ] ) {
	test(`${full ? "full destination swaps" : "overflow merge retains"} counts before literal recovery`, () => {
		const { owner } = inventory( 0x08ec, [ [ 29, full ? 50 : 10 ], [ 15, full ? 5 : 45 ] ] );
		// Full case moves the five-stack onto fifty; overflow moves ten onto 45.
		const source = full ? 15 : 29, destination = full ? 29 : 15;
		owner.move( source, destination, 1, 0 );
		owner.receive( 0xb06d, Buffer.from( full ? "01000f1d010000" : "01001d0f010000", "hex" ), 1 );
		assert.equal( quantity( owner, 15 ), 50 );
		assert.equal( quantity( owner, 29 ), 5 );
		owner.use( 29, 2 );
		owner.receive( 0xb5bd, Buffer.from( "011d0400ec08", "hex" ), 3, RECOVERY );
		assert.equal( quantity( owner, 29 ), 4 );
		assert.equal( quantity( owner, 15 ), 50 );
	});
}

/*
================
automatic

Use the same bootstrap, vital packet and local actor inputs as auto-potion's
gameplay harness; both manual and timer attempts enter the real owner.
================
*/
function automatic() {
	/** @type {import("../../src/engine/contracts/network").WireFrame[]} */
	const sent = [];
	const game = createGameplay( frame => sent.push( frame ) );
	/** @type {import("../../src/engine/contracts/world").EntityState} */
	const local = {
		gid: 1,
		refObjId: 1,
		kind: "player",
		name: "Recovery fixture",
		countryByte9c: 0,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	game.bootstrap( {
		...bootstrap( 0x08ec, [ [ 15, 50 ] ] ),
		character: {
			hp: 40,
			mp: 100,
			maxHp: 100,
			maxMp: 100,
			autoPotion: { hp: 0xb211, mp: 0x3212, cure: 0x13, timing: 0x8a },
			quickSlots: [ { slot: 1, kind: 0x46, payload: 2 } ]
		}
	} );
	game.seed( local );
	return { game, sent, local };
}

for ( const manualFirst of [ false, true ] ) {
	test(`${manualFirst ? "manual" : "automatic"} pending use serializes competing potion attempts`, () => {
		const { game, sent, local } = automatic();
		try {
			if ( manualFirst ) game.command( { kind: "item-use", slot: 15 }, 0, undefined );
			game.receive( { opcode: 0x33a6, payload: Buffer.from( "0100000000000128000000", "hex" ) }, 0 );
			game.step( 0, local );
			assert.equal( sent.length, 1 );
			assert.equal( sent[0].opcode, 0x75bd );
			assert.deepEqual( [ ...sent[0].payload ], [ 15, 0xec, 8 ] );
			assert.throws( () => game.command( { kind: "item-use", slot: 15 }, 1, undefined ), /Inventory is busy/ );
			game.step( 1000, local );
			assert.equal( sent.length, 1 );
			assert.equal(
				defined( game.take(), "pending gameplay" ).inventory.find( row => row.slot === 15 )?.quantity,
				50
			);
			game.receive( { opcode: 0xb5bd, payload: Buffer.from( "010f3100ec08", "hex" ) }, 1001 );
			const state = defined( game.take(), "recovery gameplay" );
			assert.equal( state.inventory.find( row => row.slot === 15 )?.quantity, 49 );
			assert.equal( state.inventoryPending, false );
			assert.equal(
				game.command( { kind: "item-use", slot: 15 }, 1002, undefined ),
				null,
				"receipt starts the shared cooldown"
			);
			assert.equal( sent.length, 1 );
		} finally {
			game.dispose();
		}
	});
}
