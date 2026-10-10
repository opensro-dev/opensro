/*
===========================================================================

storage-quick-move.test.mjs - one-click moves across an open warehouse

storageQuickMove backs both the native right-click and the port's Ctrl+click
(owner decision 2026-10-10): a bag item deposits into the first free slot of
the open room page (567290 / 5B0BA0), a room item withdraws into the bag's
first free slot.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { storageQuickMove } = await import( sourceFileUrl( "src/engine/foundation/ui/storage-panel.ts" ).href );
const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);
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
	assert.deepEqual( storageQuickMove( "slot:14", game(), { page: 0, autoStack: false } ), {
		type: STORAGE_MOVE_DEPOSIT,
		source: 14,
		destination: 1,
		quantity: 0,
		gold: 0
	} );
});

test("a room item withdraws into the bag's first free slot after the equipment", () => {
	assert.deepEqual( storageQuickMove( "storage-slot:2", game(), { page: 0, autoStack: false } ), {
		type: STORAGE_MOVE_WITHDRAW,
		source: 2,
		destination: 15,
		quantity: 0,
		gold: 0
	} );
});

test("nothing moves from an empty slot, into a full side, or while the room is not open", () => {
	assert.equal( storageQuickMove( "slot:16", game(), { page: 0, autoStack: false } ), null );
	assert.equal( storageQuickMove( "storage-slot:1", game(), { page: 0, autoStack: false } ), null );
	const fullRoom = game();
	assert.equal(
		storageQuickMove( "slot:13", {
			...fullRoom,
			storage: { ...fullRoom.storage, capacity: 2, items: [ { slot: 0 }, { slot: 1 } ] }
		}, { page: 0, autoStack: false } ),
		null
	);
	assert.equal(
		storageQuickMove( "storage-slot:0", game( { inventorySlotCount: 15 } ), { page: 0, autoStack: false } ),
		null
	);
	assert.equal(
		storageQuickMove( "slot:13", game( { inventoryPending: true } ), { page: 0, autoStack: false } ),
		null
	);
	const listing = game();
	assert.equal(
		storageQuickMove( "slot:13", { ...listing, storage: { ...listing.storage, phase: "listing" } }, {
			page: 0,
			autoStack: false
		} ),
		null
	);
	assert.equal( storageQuickMove( "slot:13", game( { storage: null } ), { page: 0, autoStack: false } ), null );
	assert.equal( storageQuickMove( "hotbar:1", game(), { page: 0, autoStack: false } ), null );
});

test("a bag item deposits into the open page first, then any page when that page is full", () => {
	// Two 30-slot pages: page 0 has room at slot 5, page 1 at slot 33.
	const items = Array.from( { length: 60 }, ( _, slot ) => ({ slot }) ).filter( row =>
		row.slot !== 5 && row.slot !== 33
	);
	const twoPages = game( { storage: { ...game().storage, capacity: 60, items } } );
	assert.equal( storageQuickMove( "slot:13", twoPages, { page: 1, autoStack: false } )?.destination, 33 );
	assert.equal( storageQuickMove( "slot:13", twoPages, { page: 0, autoStack: false } )?.destination, 5 );
	// Native would send slot 0 here and swap that item out; the port uses
	// the first free slot of any page instead (port-only).
	const fullSecond = game( { storage: { ...game().storage, capacity: 60, items: items.concat( { slot: 33 } ) } } );
	assert.equal( storageQuickMove( "slot:13", fullSecond, { page: 1, autoStack: false } )?.destination, 5 );
});

// A stackable potion class (typeFlags & 0x7E === 0x6C) with a 50 cap.
const POTION = 0x6c | 0x80;

/*
================
potion
================
*/
function potion( slot, quantity, refObjId = 3630 ) {
	return {
		slot,
		refObjId,
		typeFlags: POTION,
		quantity,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		tooltip: { fields: { maxStack: 50 } }
	};
}

test("auto-stack deposits onto a matching stack with room, the open page first", () => {
	const room = [ potion( 2, 45 ), potion( 31, 10 ), potion( 40, 10 ) ];
	const state = game( {
		inventory: [ potion( 13, 20 ) ],
		storage: { ...game().storage, capacity: 60, items: room }
	} );
	const on = page => storageQuickMove( "slot:13", state, { page, autoStack: true } )?.destination;
	// Slot 2 has no room for 20 more (45 + 20 > 50); page 1 holds slot 31.
	assert.equal( on( 1 ), 31 );
	// Page 0 has no stack with room, so any page: slot 31.
	assert.equal( on( 0 ), 31 );
	// Off is the native empty-slot rule exactly.
	assert.equal( storageQuickMove( "slot:13", state, { page: 1, autoStack: false } )?.destination, 30 );
	// A different item never stacks.
	const other = game( {
		inventory: [ potion( 13, 20, 3631 ) ],
		storage: { ...game().storage, capacity: 60, items: room }
	} );
	assert.equal( storageQuickMove( "slot:13", other, { page: 1, autoStack: true } )?.destination, 30 );
});

test("auto-stack withdraws onto a matching bag stack, else the first free slot", () => {
	const state = game( {
		inventory: [ potion( 13, 10 ), potion( 15, 40 ) ],
		storage: { ...game().storage, items: [ potion( 1, 10 ) ] }
	} );
	assert.equal( storageQuickMove( "storage-slot:1", state, { page: 0, autoStack: true } )?.destination, 13 );
	assert.equal( storageQuickMove( "storage-slot:1", state, { page: 0, autoStack: false } )?.destination, 14 );
	const full = game( {
		inventory: [ potion( 13, 45 ) ],
		storage: { ...game().storage, items: [ potion( 1, 10 ) ] }
	} );
	assert.equal( storageQuickMove( "storage-slot:1", full, { page: 0, autoStack: true } )?.destination, 14 );
});

test("the server's auto-stack rule reaches the HUD snapshot and clears on a native reentry", () => {
	const game = createGameplay( () => {} );
	const pose = {
		gid: 7,
		refObjId: 1907,
		kind: "player",
		name: "Tester",
		regionId: 0x6b4f,
		x: 60,
		y: 10,
		z: 100,
		heading: 0
	};
	game.bootstrap( { simulationProtocolVersion: 1, storageAutoStack: true } );
	game.seed( pose );
	assert.equal( game.take()?.storageAutoStack, true );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( pose );
	assert.equal( game.take()?.storageAutoStack, undefined );
	game.dispose();
});
