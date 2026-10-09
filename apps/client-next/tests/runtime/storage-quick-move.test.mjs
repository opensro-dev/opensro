/*
===========================================================================

storage-quick-move.test.mjs - one-click moves across an open warehouse

storageQuickMove backs both the native right-click and the port's Ctrl+click
(owner decision 2026-10-10): a bag item deposits into the room's first free
slot, a room item withdraws into the bag's first free slot.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { storageQuickMove } = await import( sourceFileUrl( "src/engine/foundation/ui/storage-panel.ts" ).href );
const { STORAGE_MOVE_DEPOSIT, STORAGE_MOVE_WITHDRAW } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/storage-room.ts" ).href
);

/*
================
game

A bag holding slots 13 and 14 (of 17) and a 4-slot room holding slots 0
and 2.
================
*/
function game( overrides = {} ) {
	return {
		storage: {
			npc: 1,
			phase: "open",
			capacity: 4,
			gold: "0",
			items: [ { slot: 0 }, { slot: 2 } ]
		},
		inventory: [ { slot: 13 }, { slot: 14 } ],
		inventoryPending: false,
		equipmentSlotCount: 13,
		inventorySlotCount: 17,
		...overrides
	};
}

test("a bag item deposits into the room's first free slot", () => {
	assert.deepEqual( storageQuickMove( "slot:14", game() ), {
		type: STORAGE_MOVE_DEPOSIT,
		source: 14,
		destination: 1,
		quantity: 0,
		gold: 0
	} );
});

test("a room item withdraws into the bag's first free slot after the equipment", () => {
	assert.deepEqual( storageQuickMove( "storage-slot:2", game() ), {
		type: STORAGE_MOVE_WITHDRAW,
		source: 2,
		destination: 15,
		quantity: 0,
		gold: 0
	} );
});

test("nothing moves from an empty slot, into a full side, or while the room is not open", () => {
	assert.equal( storageQuickMove( "slot:16", game() ), null );
	assert.equal( storageQuickMove( "storage-slot:1", game() ), null );
	const fullRoom = game();
	assert.equal(
		storageQuickMove( "slot:13", {
			...fullRoom,
			storage: { ...fullRoom.storage, capacity: 2, items: [ { slot: 0 }, { slot: 1 } ] }
		} ),
		null
	);
	assert.equal(
		storageQuickMove( "storage-slot:0", game( { inventorySlotCount: 15 } ) ),
		null
	);
	assert.equal( storageQuickMove( "slot:13", game( { inventoryPending: true } ) ), null );
	const listing = game();
	assert.equal(
		storageQuickMove( "slot:13", { ...listing, storage: { ...listing.storage, phase: "listing" } } ),
		null
	);
	assert.equal( storageQuickMove( "slot:13", game( { storage: null } ) ), null );
	assert.equal( storageQuickMove( "hotbar:1", game() ), null );
});
