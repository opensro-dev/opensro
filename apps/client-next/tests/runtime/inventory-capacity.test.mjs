/*
===========================================================================

inventory-capacity.test.mjs - the bag grows when the server announces it

Port-only, not native: with SRO_INSTANT_INVENTORY_EXPANSION on, a quest that
pays bag slots sends the v1.188 0x3092 [1][capacity] at the turn-in, and the
inventory owner adopts the new capacity without a world entry. A shrink, a
chest announce or a malformed payload is refused.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);
const STORAGE_CAPACITY = 0x3092;

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

test("a capacity announce grows the bag at once", () => {
	const owner = fixture();
	assert.equal( owner.receive( STORAGE_CAPACITY, Uint8Array.of( 1, 55 ) ), true );
	assert.equal( owner.state().inventorySlotCount, 55 );
	owner.receive( STORAGE_CAPACITY, Uint8Array.of( 1, 57 ) );
	assert.equal( owner.state().inventorySlotCount, 57 );
});

test("a shrinking, chest or malformed announce is refused and changes nothing", () => {
	for ( const payload of [ [ 1, 44 ], [ 2, 60 ], [ 1 ], [ 1, 55, 0 ] ] ) {
		const owner = fixture();
		assert.throws(
			() => owner.receive( STORAGE_CAPACITY, Uint8Array.from( payload ) ),
			/Invalid inventory capacity/
		);
		assert.equal( owner.state().inventorySlotCount, 45 );
	}
});
