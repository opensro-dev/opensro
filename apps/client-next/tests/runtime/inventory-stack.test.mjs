/*
===========================================================================

inventory-stack.test.mjs - native bag merge receipts and BUG-060 regression

Exercises the inventory owner through wire replies, including batched moves
and the equipment boundary. Expected counts follow client 756A60.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);
const ARROW = 1;
const ARROW_FLAGS = 0xa6c;
const MAX_STACK = 250;
const MOVE_REPLY = 0xb06d;

/*
================
fixture
================
*/
function fixture( rows ) {
	const owner = createInventory( () => {} );
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ ARROW, 2 ].map( refObjId => ({
			refObjId,
			typeFlags: ARROW_FLAGS,
			nativeFields: { maxStack: MAX_STACK }
		}) ),
		equipItems: rows.map( ( [slot, quantity, refObjId = ARROW] ) => ({
			slot,
			refObjId,
			body: [ refObjId, 0, 0, 0, quantity & 255, quantity >>> 8 ]
		}) )
	} );
	return owner;
}

/*
================
counts
================
*/
function counts( owner ) {
	return owner.state().inventory.map( row => [ row.slot, row.quantity, row.refObjId ] );
}

/*
================
move
================
*/
function move( owner, source, destination, quantity ) {
	owner.move( source, destination, quantity );
	owner.receive( MOVE_REPLY, Uint8Array.of( 1, 0, source, destination, quantity & 255, quantity >>> 8, 0 ) );
	assert.equal( owner.state().inventoryPending, false );
}

test("BUG-060: repeated arrow merges preserve all 3330 arrows in fourteen capped slots", () => {
	const owner = fixture( Array.from( { length: 14 }, ( _, i ) => [ 13 + i, i === 0 ? 80 : MAX_STACK ] ) );
	for ( let source = 14; source <= 26; source++ ) move( owner, source, 13, MAX_STACK );
	assert.deepEqual(
		counts( owner ),
		Array.from( { length: 14 }, ( _, i ) => [
			13 + i,
			i === 1 ? 80 : MAX_STACK,
			ARROW
		] )
	);
	assert.equal( owner.state().inventory.reduce( ( sum, row ) => sum + row.quantity, 0 ), 3330 );
});

/** @type {Array<[string, number, number, number[][]]>} */
const mergeCases = [
	[ "merge below cap", 40, 80, [ [ 14, 120, ARROW ] ] ],
	[ "merge exactly to cap", 170, 80, [ [ 14, 250, ARROW ] ] ],
	[ "overflow leaves source remainder", 200, 80, [ [ 13, 30, ARROW ], [ 14, 250, ARROW ] ] ],
	[ "full destination swaps counts", 80, 250, [ [ 13, 250, ARROW ], [ 14, 80, ARROW ] ] ],
	[ "two full stacks stay full", 250, 250, [ [ 13, 250, ARROW ], [ 14, 250, ARROW ] ] ]
];
for ( const [name, source, destination, expected] of mergeCases ) {
	test(`native ${name} ignores echoed split quantity`, () => {
		const owner = fixture( [ [ 13, source ], [ 14, destination ] ] );
		move( owner, 13, 14, 1 );
		assert.deepEqual( counts( owner ), expected );
		assert.deepEqual( owner.takeBindingMoves(), [ {
			source: 13,
			destination: 14,
			sourceRemains: expected.length === 2,
			destinationMoves: false
		} ] );
	});
}

test("empty bag slots split the requested count and whole moves remove the source", () => {
	const owner = fixture( [ [ 13, 80 ] ] );
	move( owner, 13, 14, 30 );
	assert.deepEqual( counts( owner ), [ [ 13, 50, ARROW ], [ 14, 30, ARROW ] ] );
	move( owner, 13, 15, 50 );
	assert.deepEqual( counts( owner ), [ [ 14, 30, ARROW ], [ 15, 50, ARROW ] ] );
});

test("different references swap whole stacks and quickslot identities", () => {
	const owner = fixture( [ [ 13, 80 ], [ 14, 120, 2 ] ] );
	move( owner, 13, 14, 1 );
	assert.deepEqual( counts( owner ), [ [ 13, 120, 2 ], [ 14, 80, ARROW ] ] );
	assert.deepEqual( owner.takeBindingMoves(), [ {
		source: 13,
		destination: 14,
		sourceRemains: false,
		destinationMoves: true
	} ] );
});

test("quiver equipment swaps whole stacks without applying bag caps or split quantities", () => {
	const owner = fixture( [ [ 7, 80 ], [ 13, 250 ] ] );
	move( owner, 13, 7, 1 );
	assert.deepEqual( counts( owner ), [ [ 7, 250, ARROW ], [ 13, 80, ARROW ] ] );
	move( owner, 7, 14, 1 );
	assert.deepEqual( counts( owner ), [ [ 13, 80, ARROW ], [ 14, 250, ARROW ] ] );
});

test("submoves use the preceding capped result and malformed batches do not commit", () => {
	const owner = fixture( [ [ 13, 200 ], [ 14, 80 ], [ 15, 240 ] ] );
	const before = counts( owner );
	assert.throws( () => owner.receive( MOVE_REPLY, Uint8Array.of( 1, 0, 13, 14, 1, 0, 1, 0, 13, 16, 31, 0 ) ) );
	assert.deepEqual( counts( owner ), before );
	assert.deepEqual( owner.takeBindingMoves(), [] );
	owner.receive( MOVE_REPLY, Uint8Array.of( 1, 0, 13, 14, 1, 0, 1, 0, 13, 15, 1, 0 ) );
	assert.deepEqual( counts( owner ), [ [ 13, 20, ARROW ], [ 14, 250, ARROW ], [ 15, 250, ARROW ] ] );
});

test("absolute ammunition updates leave bag stacks intact and zero removes only the quiver", () => {
	const owner = fixture( [ [ 7, 80 ], [ 13, 250 ] ] );
	owner.receive( 0x3752, Uint8Array.of( 79, 0 ) );
	assert.deepEqual( counts( owner ), [ [ 7, 79, ARROW ], [ 13, 250, ARROW ] ] );
	owner.receive( 0x3752, Uint8Array.of( 0, 0 ) );
	assert.deepEqual( counts( owner ), [ [ 13, 250, ARROW ] ] );
	move( owner, 13, 7, 250 );
	owner.receive( 0x3752, Uint8Array.of( 249, 0 ) );
	assert.deepEqual( counts( owner ), [ [ 7, 249, ARROW ] ] );
});

test("missing stack references cannot commit an uncapped merge", () => {
	const owner = createInventory( () => {} );
	owner.bootstrap( {
		refItemSnapshot: [ { refObjId: ARROW, typeFlags: ARROW_FLAGS } ],
		equipItems: [ 13, 14 ].map( slot => ({ slot, refObjId: ARROW, body: [ ARROW, 0, 0, 0, 80, 0 ] }) )
	} );
	const before = counts( owner );
	assert.throws( () => owner.receive( MOVE_REPLY, Uint8Array.of( 1, 0, 13, 14, 80, 0, 0 ) ), /stack limit/ );
	assert.deepEqual( counts( owner ), before );
	assert.deepEqual( owner.takeBindingMoves(), [] );
});
