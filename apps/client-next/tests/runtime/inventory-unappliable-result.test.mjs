/*
===========================================================================

inventory-unappliable-result.test.mjs - a grant answer no arm can apply
resyncs the bag instead of evaporating (B-11)

A 0xB06D success whose type no inventory arm applies means the server
granted or moved something this client cannot account for; the rendered
bag would drift from the server's until pickups refuse as full against
visibly free slots. The inventory declares itself desynchronized (the
timeout lane's state) so the next bootstrap rebuilds it. Two benign
exceptions stay unclaimed, neither moving the bag: a late exchange
table answer after the window closed, and a late warehouse gold answer
whose amount lived only in the pending that already settled it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);

/*
================
fixture

An empty 45-slot bag (13 sockets, 32 slots), as a fresh character enters.
================
*/
function fixture() {
	const owner = createInventory( () => {} );
	owner.bootstrap( { inventorySlotCount: 45, equipmentSlotCount: 13, refItemSnapshot: [], equipItems: [] } );
	return owner;
}

test("an unappliable success result resyncs instead of evaporating", () => {
	const owner = fixture();
	// Type 0x02 is a success result no arm applies; whatever it granted,
	// this client cannot account for it.
	assert.equal( owner.receive( 0xb06d, Uint8Array.of( 1, 2, 20, 0 ) ), true );
	// Sends now refuse with the resync instruction instead of quietly
	// drifting away from the server's bag.
	assert.throws( () => owner.move( 13, 14, 1, 0 ), /reconnect to resynchronize/ );
	// The next bootstrap rebuilds the bag and clears the state.
	owner.bootstrap( { inventorySlotCount: 45, equipmentSlotCount: 13, refItemSnapshot: [], equipItems: [] } );
	assert.throws( () => owner.move( 13, 14, 1, 0 ), /Inventory command unavailable/ );
});

test("a success byte with no result type changes nothing and resyncs nothing", () => {
	const owner = fixture();
	assert.equal( owner.receive( 0xb06d, Uint8Array.of( 1 ) ), false );
	// No resync was declared: a send fails only the ordinary way.
	assert.throws( () => owner.move( 13, 14, 1, 0 ), /Inventory command unavailable/ );
});

test("a late exchange table answer after the window closed stays unclaimed", () => {
	const owner = fixture();
	for ( const type of [ 4, 5, 0x0d ] ) {
		assert.equal( owner.receive( 0xb06d, Uint8Array.of( 1, type, 0, 0, 0, 0 ) ), false );
	}
	// No resync was declared: a send fails only the ordinary way.
	assert.throws( () => owner.move( 13, 14, 1, 0 ), /Inventory command unavailable/ );
});

test("a claimed warehouse gold answer settles against its pending", () => {
	const owner = fixture();
	/** @type {import("../../src/engine/foundation/gameplay/storage-room.ts").StorageRoom} */
	const room = { npc: 7, phase: "open", capacity: 45, gold: "1000", items: [] };
	// STORAGE_GOLD_DEPOSIT (0x0C): storageMove arms the pending the answer
	// settles against; the moved amount is the pending's, not the frame's.
	owner.storageMove( room, { type: 0x0c, source: 0, destination: 0, quantity: 0, gold: 250 }, 0, new Map() );
	const answer = new Uint8Array( 6 );
	answer[0] = 1;
	answer[1] = 0x0c;
	new DataView( answer.buffer ).setUint32( 2, 250, true );
	assert.equal( owner.storageSettle( room, answer, new Map() )?.gold, "1250" );
	// The guild warehouse carries the same deposit on wire type 0x20.
	const guildOwner = fixture();
	/** @type {import("../../src/engine/foundation/gameplay/storage-room.ts").StorageRoom} */
	const guildRoom = { npc: 7, guild: true, phase: "open", capacity: 45, gold: "500", items: [] };
	guildOwner.storageMove(
		guildRoom,
		{ type: 0x0c, source: 0, destination: 0, quantity: 0, gold: 100 },
		0,
		new Map()
	);
	answer[1] = 0x20;
	new DataView( answer.buffer ).setUint32( 2, 100, true );
	assert.equal( guildOwner.storageSettle( guildRoom, answer, new Map() )?.gold, "600" );
});

test("a late warehouse gold answer with no pending stays unclaimed", () => {
	const owner = fixture();
	// Personal withdraw/deposit (0x0B/0x0C) and the guild wire pair
	// (0x20/0x21): with no pending the amount exists nowhere, nothing can
	// be applied and the bag never moves, so no resync is declared.
	for ( const type of [ 0x0b, 0x0c, 0x20, 0x21 ] ) {
		assert.equal( owner.receive( 0xb06d, Uint8Array.of( 1, type, 0, 0, 0, 0 ) ), false );
	}
	// No resync was declared: a send fails only the ordinary way.
	assert.throws( () => owner.move( 13, 14, 1, 0 ), /Inventory command unavailable/ );
});

test("the pet-bag pickup and the quest gold grant apply without resync", () => {
	const owner = createInventory( () => {} );
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: 5, typeFlags: 0x86c, nativeFields: { maxStack: 50 } } ],
		equipItems: [ { refObjId: 5, slot: 20, body: itemBody( 5, 10 ) } ]
	} );
	// The pet-bag pickup into the player bag (type 0x1C) applies.
	assert.equal( owner.receive( 0xb06d, Uint8Array.of( 1, 0x1c, 20, ...itemBody( 5, 30 ) ) ), true );
	assert.equal( owner.state().inventory.find( row => row.slot === 20 )?.quantity, 30 );
	// A quest gold heap at slot 254 is an amount, not a bag row (#2256).
	const bag = owner.state().inventory;
	assert.equal( owner.receive( 0xb06d, Uint8Array.of( 1, 14, 254, 200, 3, 0, 0 ) ), true );
	assert.deepEqual( owner.state().inventory, bag );
	assert.throws( () => owner.receive( 0xb06d, Uint8Array.of( 1, 14, 254, 200, 3 ) ), /quest gold grant/ );
	// Nothing declared a desync: a send fails only the ordinary way.
	assert.throws( () => owner.move( 13, 14, 1, 0 ), /Inventory command unavailable/ );
});

/*
================
itemBody

A stacked item body: reference, then the u16 quantity.
================
*/
function itemBody( ref, quantity ) {
	const p = Buffer.alloc( 6 );
	p.writeUInt32LE( ref );
	p.writeUInt16LE( quantity, 4 );
	return [ ...p ];
}
