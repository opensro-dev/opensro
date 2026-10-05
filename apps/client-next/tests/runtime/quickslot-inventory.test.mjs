/*
===========================================================================

quickslot-inventory.test.mjs - tests for quickslot-inventory.ts, gameplay.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { reconcileQuickslotInventory: repair } = await import(
	"../../src/engine/foundation/gameplay/quickslot-inventory.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const facts = { country: 0, progression: { level: 20, masteries: [] }, maxHp: 1000, maxMp: 2000 };
/*
================
potion
================
*/
const potion = ( slot, id, amount, category = 2, extra = {} ) => ({
	slot,
	refObjId: id,
	typeFlags: 0xec | (category << 11),
	quantity: 50,
	tooltip: { fields: { country: 3, [category === 1 ? "itemParam1_29c" : "itemParam3_2a4"]: amount, ...extra } }
});
/*
================
binding
================
*/
const binding = ( slot, item ) => ({ slot, kind: item < 13 ? 0x47 : 0x46, payload: item < 13 ? item : item - 13 });

test("depleted potion picks strongest usable same category, then deterministic same-reference stacks", () => {
	const old = potion( 13, 9, 100 ), same = potion( 14, 9, 100 ), big = potion( 15, 2, 500 );
	const invalid = potion( 16, 99, 9999, 2, { reqLevelType1: 1, requiredLevel: 21 } );
	const hp = potion( 17, 88, 9999, 1 ), foreign = potion( 18, 90, 9999, 2, { country: 1 } );
	const rows = [ binding( 10, 13 ), binding( 50, 13 ) ];
	assert.deepEqual( repair( rows, [ old ], [ same, big, invalid, hp, foreign ], [], facts, true ), [
		binding( 10, 15 ),
		binding( 50, 15 )
	] );
	assert.deepEqual( repair( rows, [ old ], [ potion( 22, 9, 100 ), same ], [], facts, true ), [
		binding( 10, 14 ),
		binding( 50, 14 )
	] );
	assert.deepEqual( repair( rows, [ old ], [ potion( 14, 8, 50 ) ], [], facts, true ), [
		binding( 10, 14 ),
		binding( 50, 14 )
	] );
	assert.deepEqual(
		repair( rows, [ old ], [], [], facts, true ),
		rows.map( r => ({ slot: r.slot, kind: 0, payload: 0 }) )
	);
	assert.deepEqual(
		repair( rows, [ old ], [ { ...old, quantity: 1 }, big ], [], facts, true ),
		rows,
		"do not override an explicitly selected nonempty stack"
	);
	assert.deepEqual(
		repair( rows, [ old ], [ same ], [], facts, false ),
		rows.map( r => ({ slot: r.slot, kind: 0, payload: 0 }) ),
		"sale/drop is not consumption refill"
	);
});

test("percentage amounts and missing metadata never use reference ID as potency", () => {
	const old = potion( 13, 999, 100 ), percent = potion( 14, 1, 0, 2, { itemParam4_2a8: 25 } );
	assert.deepEqual( repair( [ binding( 9, 13 ) ], [ old ], [ potion( 15, 9000, 400 ), percent ], [], facts, true ), [
		binding( 9, 14 )
	] );
	const unknown = { ...potion( 16, 9001, 99999 ), tooltip: undefined };
	assert.deepEqual( repair( [ binding( 9, 13 ) ], [ old ], [ unknown ], [], facts, true ), [ {
		slot: 9,
		kind: 0,
		payload: 0
	} ] );
});

test("swap, merge, split and bag/equipment moves preserve each reference", () => {
	const a = potion( 13, 1, 100 ),
		b = { ...potion( 14, 2, 0 ), typeFlags: 0x6c },
		rows = [ binding( 9, 13 ), binding( 10, 14 ), binding( 50, 13 ) ];
	const swap = { source: 13, destination: 14, sourceRemains: false, destinationMoves: true };
	assert.deepEqual( repair( rows, [ a, b ], [ { ...a, slot: 14 }, { ...b, slot: 13 } ], [ swap ], facts ), [
		binding( 9, 14 ),
		binding( 10, 13 ),
		binding( 50, 14 )
	] );
	const move = { source: 13, destination: 14, sourceRemains: false, destinationMoves: false };
	assert.deepEqual( repair( [ binding( 9, 13 ) ], [ a ], [ { ...a, slot: 14 } ], [ move ], facts ), [
		binding( 9, 14 )
	] );
	assert.deepEqual(
		repair( [ binding( 9, 13 ) ], [ a ], [ { ...a, quantity: 25 }, { ...a, slot: 14, quantity: 25 } ], [ {
			...move,
			sourceRemains: true
		} ], facts ),
		[ binding( 9, 13 ) ]
	);
	assert.deepEqual(
		repair( [ binding( 9, 13 ) ], [ a ], [ { ...a, slot: 7 } ], [ { ...move, destination: 7 } ], facts ),
		[ binding( 9, 7 ) ]
	);
});

const local = { gid: 1, countryByte9c: 0, regionId: 257, x: 0, y: 0, z: 0, heading: 0, appearanceState: [ 1, 0, 0 ] };
/*
================
fixture
================
*/
function fixture() {
	return {
		inventorySlotCount: 58,
		equipmentSlotCount: 13,
		character: { hp: 100, mp: 100, maxHp: 100, maxMp: 1000, quickSlots: [ binding( 9, 13 ), binding( 10, 14 ) ] },
		refItemSnapshot: [
			{ refObjId: 1, typeFlags: 0x8ec, nativeFields: { maxStack: 50, itemParam1_29c: 100 } },
			{ refObjId: 2, typeFlags: 0x10ec, nativeFields: { maxStack: 50, itemParam3_2a4: 100 } },
			{ refObjId: 3, typeFlags: 0x10ec, nativeFields: { maxStack: 50, itemParam3_2a4: 500 } },
			{ refObjId: 4, typeFlags: 0x6c, nativeFields: { maxStack: 1 } }
		],
		equipItems: [ [ 13, 1, 50 ], [ 14, 2, 1 ], [ 15, 2, 50 ], [ 16, 3, 50 ], [ 17, 4, 1 ] ].map( (
			[slot, id, n]
		) => ({ slot, refObjId: id, body: [ id, 0, 0, 0, n, 0 ] }) )
	};
}
/*
================
move
================
*/
const move = ( source, dest, count = 0 ) => ({
	opcode: 0xb06d,
	payload: Uint8Array.of( 1, 0, source, dest, count, 0, 0 )
});
/*
================
consume
================
*/
const consume = ( slot, count ) => ({ opcode: 0xb5bd, payload: Uint8Array.of( 1, slot, count, 0, 0xec, 0x10 ) });

test("authoritative swap and exhaustion repair display/activation bindings and persist 7541", () => {
	const sent = [], g = createGameplay( f => sent.push( f ) );
	g.bootstrap( fixture() );
	g.seed( local );
	g.step( 0, local );
	g.receive( move( 13, 17 ), 1 );
	assert.deepEqual( defined( g.take() ).quickSlots, [ binding( 9, 17 ), binding( 10, 14 ) ] );
	g.receive( consume( 14, 0 ), 2 );
	const state = g.take();
	assert.deepEqual( defined( state ).quickSlots, [ binding( 9, 17 ), binding( 10, 16 ) ] );
	assert.equal(
		defined( defined( state ).inventory.find( i => i.slot === 16 ) ).quantity,
		50,
		"refill binds; does not consume"
	);
	assert.deepEqual(
		sent.filter( f => f.opcode === 0x7541 ).map(
			f => [ f.payload[1], f.payload[2], new DataView( f.payload.buffer ).getUint32( 3, true ) ]
		),
		[ [ 9, 0x46, 4 ], [ 10, 0x46, 3 ] ]
	);
	g.dispose();
});

test("malformed multi-move and rejection publish no repair; failed save retries latest binding", () => {
	const sent = [];
	let fail = false;
	const g = createGameplay( f => {
		if ( fail ) throw Error( "closed" );
		sent.push( f );
	} );
	g.bootstrap( fixture() );
	g.seed( local );
	g.step( 0, local );
	const bad = Uint8Array.of( 1, 0, 13, 17, 0, 0, 1, 1, 17, 18, 0, 0 );
	assert.throws( () => g.receive( { opcode: 0xb06d, payload: bad }, 1 ), /submove/ );
	assert.deepEqual( defined( g.take() ).quickSlots, fixture().character.quickSlots );
	assert.equal( sent.length, 0 );
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 1 ) }, 2 );
	assert.equal( sent.length, 0 );
	fail = true;
	g.receive( move( 13, 17 ), 3 );
	assert.deepEqual( defined( g.take() ).quickSlots, [ binding( 9, 17 ), binding( 10, 14 ) ] );
	g.receive( move( 17, 18, 50 ), 4 );
	assert.deepEqual( defined( g.take() ).quickSlots, [ binding( 9, 18 ), binding( 10, 14 ) ] );
	fail = false;
	g.step( 5, local );
	assert.equal( sent.length, 1 );
	assert.equal( new DataView( sent[0].payload.buffer ).getUint32( 3, true ), 5 );
	g.dispose();
});
