/*
===========================================================================

inventory-journal.test.mjs - the bug report's record of bag slot changes

Port-only diagnostics (foundation/gameplay/inventory-journal.ts): each
0xB06D, 0xB5BD and 0x3645 answer leaves the slots it changed and whether
each still holds an item, with no reference, name or count.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { journalInventoryFrame, inventorySignatures, INVENTORY_JOURNAL_ROWS, INVENTORY_JOURNAL_FIELDS } = await import(
	"../../src/engine/foundation/gameplay/inventory-journal.ts"
);

/*
================
rows

A flattened journal as rows of INVENTORY_JOURNAL_FIELDS.
================
*/
function rows( journal ) {
	const out = [];
	for ( let i = 0; i < journal.length; i += INVENTORY_JOURNAL_FIELDS ) {
		out.push( journal.slice( i, i + INVENTORY_JOURNAL_FIELDS ) );
	}
	return out;
}

test("a move records both slots, a refusal records none, and the journal keeps the newest rows", () => {
	const potion = { slot: 13, refObjId: 1, quantity: 10, plus: 0 };
	const before = inventorySignatures( [ potion ] ), after = inventorySignatures( [ { ...potion, slot: 14 } ] );
	const moved = journalInventoryFrame(
		[],
		{ atMs: 5, opcode: 0xb06d, payload: Uint8Array.of( 1, 0, 13, 14 ) },
		before,
		after
	);
	assert.deepEqual( rows( moved ), [ [ 5, 0xb06d, 1, 0, 13, 0 ], [ 5, 0xb06d, 1, 0, 14, 1 ] ] );
	const refused = journalInventoryFrame(
		moved,
		{ atMs: 6, opcode: 0xb06d, payload: Uint8Array.of( 2, 0x10 ) },
		after,
		after
	);
	assert.deepEqual( rows( refused ).at( -1 ), [ 6, 0xb06d, 2, 0x10, -1, -1 ] );
	let journal = [];
	for ( let i = 0; i < INVENTORY_JOURNAL_ROWS + 5; i++ ) {
		journal = journalInventoryFrame(
			journal,
			{ atMs: i, opcode: 0x3645, payload: Uint8Array.of( 13, 8 ) },
			after,
			after
		);
	}
	assert.equal( journal.length, INVENTORY_JOURNAL_ROWS * INVENTORY_JOURNAL_FIELDS );
	assert.equal( journal[0], 5, "the oldest rows are dropped first" );
});

test("a count change on a slot that stays occupied is a change, recorded as present", () => {
	const before = inventorySignatures( [ { slot: 13, refObjId: 1, quantity: 10, plus: 0 } ] );
	const after = inventorySignatures( [ { slot: 13, refObjId: 1, quantity: 9, plus: 0 } ] );
	const journal = journalInventoryFrame(
		[],
		{ atMs: 1, opcode: 0x3645, payload: Uint8Array.of( 13, 8 ) },
		before,
		after
	);
	assert.deepEqual( rows( journal ), [ [ 1, 0x3645, 13, 8, 13, 1 ] ] );
});

test("an item use keeps only the slots it emptied, so auto-potion cannot flood the journal", () => {
	const full = inventorySignatures( [ { slot: 13, refObjId: 1, quantity: 10, plus: 0 } ] );
	const spent = inventorySignatures( [ { slot: 13, refObjId: 1, quantity: 9, plus: 0 } ] );
	const use = { atMs: 1, opcode: 0xb5bd, payload: Uint8Array.of( 1, 13 ) };
	assert.deepEqual( journalInventoryFrame( [], use, full, spent ), [], "a spent count records nothing" );
	const emptied = journalInventoryFrame( [], use, spent, new Map() );
	assert.deepEqual( rows( emptied ), [ [ 1, 0xb5bd, 1, 13, 13, 0 ] ], "the last potion's slot is recorded" );
	const refused = journalInventoryFrame( [], { ...use, payload: Uint8Array.of( 2, 0x10 ) }, full, full );
	assert.deepEqual( rows( refused ), [ [ 1, 0xb5bd, 2, 0x10, -1, -1 ] ], "a refused use stays visible" );
});

test("live inventory answers reach the published journal with slots and codes only", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( {
		inventorySlotCount: 77,
		equipmentSlotCount: 13,
		character: { hp: 100, mp: 100, maxHp: 100, maxMp: 100 },
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x8ec, nativeFields: { maxStack: 50 } } ],
		equipItems: [ { slot: 13, refObjId: 1, body: [ 1, 0, 0, 0, 20, 0 ] } ]
	} );
	const local = {
		gid: 1,
		refObjId: 1,
		kind: "player",
		name: "Journal",
		countryByte9c: 0,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	game.seed( local );
	game.step( 0, local );
	game.receive( { opcode: 0xb06d, payload: Uint8Array.of( 1, 0, 13, 20, 20, 0, 0 ) }, 7 );
	const state = game.take();
	assert.deepEqual( state?.inventory.map( row => row.slot ), [ 20 ] );
	const moved = [ [ 7, 0xb06d, 1, 0, 13, 0 ], [ 7, 0xb06d, 1, 0, 20, 1 ] ];
	assert.deepEqual( rows( state?.inventoryJournal ?? [] ), moved );
	// A character switch resets the session: the next report starts empty.
	game.reset();
	assert.deepEqual( game.take()?.inventoryJournal ?? [], [] );
	game.dispose();
});
