/*
===========================================================================

inventory-binding-branches.test.mjs - native remapping and expanded bag bounds

Exercises live gameplay receipts, persisted bindings and shared cargo moves.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { quickSlotDrop, quickSlotPacket } = await import( "../../src/engine/foundation/gameplay/quickslots.ts" );
const { planContainerMove, planWholeTransfer } = await import(
	"../../src/engine/foundation/gameplay/container-transfer.ts"
);

/*
================
binding
================
*/
function binding( slot, item ) {
	return { slot, kind: 0x46, payload: item - 13 };
}

/*
================
fixture
================
*/
function fixture( items, typeFlags = 0x8ec, maxStack = 50 ) {
	const sent = [], game = createGameplay( frame => sent.push( frame ) );
	game.bootstrap( {
		inventorySlotCount: 77,
		equipmentSlotCount: 13,
		character: { hp: 100, mp: 100, maxHp: 100, maxMp: 100, quickSlots: [ binding( 1, 13 ), binding( 2, 14 ) ] },
		refItemSnapshot: [ { refObjId: 1, typeFlags, nativeFields: { maxStack } } ],
		equipItems: items.map( ( [slot, quantity, plus = 0] ) => ({
			slot,
			refObjId: 1,
			body: [ 1, 0, 0, 0, quantity, 0, ...(typeFlags === 0xdec ? [ plus ] : []) ]
		}) )
	} );
	const local = {
		gid: 1,
		refObjId: 1,
		kind: "player",
		name: "Inventory audit",
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
	return { game, sent };
}

/*
================
move
================
*/
function move( game, source, destination, quantity ) {
	game.receive( { opcode: 0xb06d, payload: Uint8Array.of( 1, 0, source, destination, quantity, 0, 0 ) }, 1 );
	return game.take();
}

test("a split remaps the source hotbar binding to the split stack and saves it", () => {
	const { game, sent } = fixture( [ [ 13, 50 ] ] );
	const state = move( game, 13, 15, 10 );
	assert.deepEqual( state?.inventory.map( row => [ row.slot, row.quantity ] ), [ [ 13, 40 ], [ 15, 10 ] ] );
	assert.deepEqual( state?.quickSlots.find( row => row.slot === 1 ), binding( 1, 15 ) );
	assert.ok( sent.some( frame => frame.opcode === 0x7541 && frame.payload[1] === 1 && frame.payload[3] === 2 ) );
	game.dispose();
});

for ( const destinationCount of [ 20, 50 ] ) {
	test(`occupied stack (${destinationCount}) keeps destination bindings and moves source bindings`, () => {
		const { game } = fixture( [ [ 13, 40 ], [ 14, destinationCount ] ] );
		const state = move( game, 13, 14, 1 );
		assert.deepEqual( state?.quickSlots, [ binding( 1, 14 ), binding( 2, 14 ) ] );
		game.dispose();
	});
}

test("native destination-plus flag takes the other remap branch for enhanced stackables", () => {
	// Magic stones at their native cap of 1: 757652 adds the destination's
	// plus (its assimilation value) to the count and moves both bindings.
	const { game } = fixture( [ [ 13, 1, 0 ], [ 14, 1, 1 ] ], 0xdec, 1 );
	const state = move( game, 13, 14, 1 );
	assert.deepEqual( state?.quickSlots, [ binding( 1, 14 ), binding( 2, 13 ) ] );
	game.dispose();
});

test("stacked stones merge only with an equal assimilation value (port-only, #583)", () => {
	const equal = fixture( [ [ 13, 10, 90 ], [ 14, 20, 90 ] ], 0xdec );
	let state = move( equal.game, 13, 14, 10 );
	assert.deepEqual( state?.inventory.map( row => [ row.slot, row.quantity, row.plus ] ), [ [ 14, 30, 90 ] ] );
	// A merged stack keeps the destination's bindings, as any stackable does.
	assert.deepEqual( state?.quickSlots, [ binding( 1, 14 ), binding( 2, 14 ) ] );
	equal.game.dispose();
	const different = fixture( [ [ 13, 10, 40 ], [ 14, 20, 90 ] ], 0xdec );
	state = move( different.game, 13, 14, 10 );
	assert.deepEqual( state?.inventory.map( row => [ row.slot, row.quantity, row.plus ] ), [ [ 13, 20, 90 ], [
		14,
		10,
		40
	] ] );
	assert.deepEqual( state?.quickSlots, [ binding( 1, 14 ), binding( 2, 13 ) ] );
	different.game.dispose();
});

test("stone identity is shared by bag, warehouse and COS merge planning", () => {
	const stone = ( slot, plus, quantity ) => ({
		slot,
		plus,
		quantity,
		refObjId: 1,
		typeFlags: 0xdec,
		durability: 0,
		variance: "0",
		magic: []
	});
	const a = stone( 13, 40, 10 ), b = stone( 14, 90, 20 ), caps = new Map( [ [ 1, 50 ] ] );
	const swap = planContainerMove( [ a, b ], { source: 13, destination: 14, quantity: 10 }, caps, "storage" );
	assert.deepEqual( swap, [ { ...b, slot: 13 }, { ...a, slot: 14 } ] );
	const across = planWholeTransfer( [ a ], [ { ...b, slot: 0 } ], 13, 0, caps );
	assert.deepEqual( across, { from: [ { ...b, slot: 13 } ], to: [ { ...a, slot: 0 } ] } );
	const merge = planContainerMove(
		[ a, { ...b, plus: 40 } ],
		{ source: 13, destination: 14, quantity: 10 },
		caps,
		"COS"
	);
	assert.deepEqual( merge.map( row => [ row.slot, row.quantity, row.plus ] ), [ [ 14, 30, 40 ] ] );
});

test("expanded bag moves, drag bindings, persistence and depletion use the published capacity", () => {
	const { game, sent } = fixture( [ [ 13, 1 ], [ 76, 50 ] ] );
	const moved = move( game, 13, 58, 1 );
	assert.deepEqual( moved?.quickSlots.find( row => row.slot === 1 ), binding( 1, 58 ) );
	assert.ok( sent.some( frame => frame.opcode === 0x7541 && frame.payload[3] === 45 ) );
	assert.ok( moved );
	assert.deepEqual( quickSlotDrop( "slot:76", 50, moved ), [ binding( 50, 76 ) ] );
	assert.deepEqual( quickSlotDrop( "slot:76", 50, { ...moved, inventorySlotCount: 76 } ), [] );
	assert.equal( quickSlotPacket( binding( 50, 76 ) ).payload[3], 63 );
	game.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 1, 58, 0, 0, 0xec, 8 ) }, 2 );
	assert.deepEqual( game.take()?.quickSlots?.find( row => row.slot === 1 ), binding( 1, 76 ) );
	game.dispose();
});

/*
================
cargo
================
*/
function cargo( slot, label, quantity ) {
	return { slot, label, quantity, refObjId: 1, typeFlags: 0x46c, plus: 0, durability: 0, variance: "0", magic: [] };
}

test("cargo owner identity is shared by bag, warehouse and COS merge planning", () => {
	const a = cargo( 13, "Alice", 30 ), b = cargo( 14, "Bob", 40 ), caps = new Map( [ [ 1, 50 ] ] );
	const swap = planContainerMove( [ a, b ], { source: 13, destination: 14, quantity: 1 }, caps, "bag" );
	assert.deepEqual( swap, [ { ...b, slot: 13 }, { ...a, slot: 14 } ] );
	const across = planWholeTransfer( [ a ], [ { ...b, slot: 0 } ], 13, 0, caps );
	assert.deepEqual( across, { from: [ { ...b, slot: 13 } ], to: [ { ...a, slot: 0 } ] } );
	const merge = planContainerMove(
		[ a, { ...b, label: "Alice" } ],
		{ source: 13, destination: 14, quantity: 1 },
		caps,
		"bag"
	);
	assert.deepEqual( merge.map( row => row.quantity ), [ 20, 50 ] );
});
