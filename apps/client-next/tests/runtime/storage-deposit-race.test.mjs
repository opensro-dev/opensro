/*
===========================================================================

storage-deposit-race.test.mjs - a warehouse move answered after its window
closed

Drives the shipped gameplay dispatcher: the move is admitted while the room
is open, the window closes before the 0xB06D echo, and the echo must still
settle the bag and the session's room copy (+0x7BC), or the item shows in
neither place until a reload relists the warehouse.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const SELECT_RESULT = 0xb45a;
const NPC_INTERACTION = 0xb338;
const STORAGE_GOLD = 0x3126;
const STORAGE_LIST = 0x321a;
const ITEM_MOVE_RESULT = 0xb06d;
const NPC_GID = 7;
const POTION = 1;
const BAG_SLOT = 13;

/*
================
warehouseFixture

A player with one bag potion beside a selected warehouse NPC whose room is
listed empty and open.
================
*/
function warehouseFixture() {
	const sent = [];
	const game = createGameplay( frame => sent.push( frame ) );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: POTION, typeFlags: 0xec } ],
		equipItems: [ { slot: BAG_SLOT, refObjId: POTION, body: [ 1, 0, 0, 0, 10, 0 ] } ]
	} );
	const entity = {
		refObjId: 1,
		gid: NPC_GID,
		name: "Warehouse",
		kind: "npc",
		regionId: 1,
		x: 0,
		y: 0,
		z: 0,
		heading: 0
	};
	game.seed( { ...entity, gid: 1, kind: "player" } );
	game.command( { kind: "select", gid: NPC_GID }, 0, entity );
	const grant = Buffer.alloc( 11 );
	grant[0] = 1;
	grant.writeUInt32LE( NPC_GID, 1 );
	grant.writeUInt32LE( 4, 6 );
	game.receive( { opcode: SELECT_RESULT, payload: grant }, 1 );
	game.command( { kind: "storage-open", gid: NPC_GID }, 2, undefined );
	game.receive( { opcode: STORAGE_GOLD, payload: new Uint8Array( 8 ) }, 3 );
	game.receive( { opcode: STORAGE_LIST, payload: Uint8Array.of( 150, 0 ) }, 4 );
	game.receive( { opcode: NPC_INTERACTION, payload: Uint8Array.of( 1, 4, 0, 0, 0 ) }, 5 );
	return { game, sent };
}

/*
================
snapshot
================
*/
function snapshot( game ) {
	return defined( game.take(), "gameplay snapshot" );
}

test("a deposit answered after the window closed lands in the room copy and leaves the bag", () => {
	const { game } = warehouseFixture();
	assert.equal( snapshot( game ).storage?.phase, "open" );
	game.command( {
		kind: "storage-move",
		move: { type: 2, source: BAG_SLOT, destination: 0, quantity: 0, gold: 0 }
	}, 6, undefined );
	game.command( { kind: "storage-close" }, 7, undefined );
	game.receive( { opcode: ITEM_MOVE_RESULT, payload: Uint8Array.of( 1, 2, BAG_SLOT, 0 ) }, 8 );
	const closed = snapshot( game );
	assert.ok(
		!closed.inventory.some( row => row.slot === BAG_SLOT ),
		"the deposited item stayed in the bag"
	);
	game.command( { kind: "storage-open", gid: NPC_GID }, 9, undefined );
	game.receive( { opcode: NPC_INTERACTION, payload: Uint8Array.of( 1, 4, 0, 0, 0 ) }, 10 );
	const reopened = defined( snapshot( game ).storage, "reopened room" );
	assert.deepEqual(
		reopened.items.map( row => [ row.slot, row.refObjId ] ),
		[ [ 0, POTION ] ],
		"the deposited item is missing from the reopened warehouse"
	);
});
