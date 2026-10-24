/*
===========================================================================

inventory-unappliable-result.test.mjs - a grant answer no arm can apply
resyncs the bag instead of evaporating (B-11)

A 0xB06D success whose type no inventory arm applies means the server
granted or moved something this client cannot account for; the rendered
bag would drift from the server's until pickups refuse as full against
visibly free slots. The inventory declares itself desynchronized (the
timeout lane's state) so the next bootstrap rebuilds it. A late
exchange table answer after the window closed is the one benign
exception: it moves nothing in the bag and stays unclaimed.

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
	// Type 0x1C is a success result no arm applies; whatever it granted,
	// this client cannot account for it.
	assert.equal( owner.receive( 0xb06d, Uint8Array.of( 1, 0x1c, 20, 0 ) ), true );
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
