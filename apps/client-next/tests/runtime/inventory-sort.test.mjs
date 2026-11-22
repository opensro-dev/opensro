/*
===========================================================================

inventory-sort.test.mjs - the bag sort's order, merges and run lifetime

The planner is driven to the end against planContainerMove, the client's
mirror of the server's bag move (merge, split or swap), so every test
checks what the server would leave: no item lost, partial stacks merged,
the owner's category order, and a sort that ends.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	compareInventorySort,
	inventorySortCategory,
	nextInventorySortMove,
	SORT_ACCESSORY,
	SORT_ALCHEMY,
	SORT_ARMOR,
	SORT_CONSUMABLE,
	SORT_HP_POTION,
	SORT_MATERIAL,
	SORT_MP_POTION,
	SORT_OTHER,
	SORT_PET,
	SORT_QUEST_EVENT,
	SORT_SCROLL,
	SORT_WEAPON
} = await import( "../../src/engine/foundation/gameplay/inventory-sort.ts" );
const { planContainerMove } = await import( "../../src/engine/foundation/gameplay/container-transfer.ts" );
const { createInventorySortRun } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory-sort-run.ts"
);

const BAG = { first: 13, end: 13 + 32 };

/*
================
typeFlags

An item type word: TID1 3, then TID2, TID3, TID4.
================
*/
function typeFlags( tid2, tid3, tid4 ) {
	return (3 << 2) | (tid2 << 5) | (tid3 << 7) | (tid4 << 11);
}

/*
================
item
================
*/
/**
 * @param {number} slot
 * @param {number} refObjId
 * @param {readonly number[]} type
 * @param {{ quantity?: number; plus?: number; codename?: string; fields?: Record<string, number>; }} [options]
 */
function item( slot, refObjId, type, { quantity = 1, plus = 0, codename = undefined, fields = {} } = {} ) {
	return {
		slot,
		refObjId,
		typeFlags: typeFlags( type[0] ?? 0, type[1] ?? 0, type[2] ?? 0 ),
		quantity,
		plus,
		durability: 0,
		variance: "0",
		magic: [],
		codename,
		tooltip: { fields }
	};
}

const HP = [ 3, 1, 1 ], MP = [ 3, 1, 2 ], RETURN = [ 3, 3, 1 ], ELIXIR = [ 3, 10, 1 ], ELEMENT = [ 3, 11, 5 ];
const SWORD = [ 1, 6, 2 ], SHIELD = [ 1, 4, 1 ], NECKLACE = [ 1, 5, 2 ], RING = [ 1, 5, 3 ];

/*
================
sortToEnd

Runs the planner against the client's move mirror until it has nothing
left to do; returns the final bag and the moves it took.
================
*/
function sortToEnd( rows, bag = BAG ) {
	const caps = new Map( rows.map( row => [ row.refObjId, row.tooltip.fields.maxStack ?? 1 ] ) );
	let bagRows = rows, moves = 0;
	for ( ;; ) {
		const move = nextInventorySortMove( bagRows, bag );
		if ( !move ) return { rows: bagRows, moves };
		assert.ok( moves < 4 * (bag.end - bag.first), "the sort ends" );
		bagRows = planContainerMove( bagRows, move, caps, "inventory" );
		moves++;
	}
}

/*
================
totals

Count of every reference in a bag: a sort keeps it exactly.
================
*/
function totals( rows ) {
	const out = new Map();
	for ( const row of rows ) out.set( row.refObjId, (out.get( row.refObjId ) ?? 0) + row.quantity );
	return out;
}

test("categories follow the owner's order, codename first for quest and event items", () => {
	assert.equal( inventorySortCategory( item( 13, 1, HP ) ), SORT_HP_POTION );
	assert.equal( inventorySortCategory( item( 13, 1, MP ) ), SORT_MP_POTION );
	assert.equal( inventorySortCategory( item( 13, 1, [ 3, 1, 3 ] ) ), SORT_CONSUMABLE );
	assert.equal( inventorySortCategory( item( 13, 1, [ 3, 2, 6 ] ) ), SORT_CONSUMABLE );
	assert.equal( inventorySortCategory( item( 13, 1, RETURN ) ), SORT_SCROLL );
	assert.equal( inventorySortCategory( item( 13, 1, SWORD ) ), SORT_WEAPON );
	assert.equal( inventorySortCategory( item( 13, 1, SHIELD ) ), SORT_WEAPON );
	assert.equal( inventorySortCategory( item( 13, 1, [ 1, 3, 1 ] ) ), SORT_ARMOR );
	assert.equal( inventorySortCategory( item( 13, 1, NECKLACE ) ), SORT_ACCESSORY );
	assert.equal( inventorySortCategory( item( 13, 1, ELIXIR ) ), SORT_ALCHEMY );
	assert.equal( inventorySortCategory( item( 13, 1, [ 3, 11, 1 ] ) ), SORT_ALCHEMY );
	assert.equal( inventorySortCategory( item( 13, 1, ELEMENT ) ), SORT_MATERIAL );
	assert.equal( inventorySortCategory( item( 13, 1, [ 2, 1, 1 ] ) ), SORT_PET );
	assert.equal( inventorySortCategory( item( 13, 1, [ 3, 3, 2 ], { codename: "ITEM_COS_T_LIZARD" } ) ), SORT_PET );
	assert.equal(
		inventorySortCategory( item( 13, 1, [ 3, 1, 8 ], { codename: "ITEM_QNO_EU_IVY_3_07" } ) ),
		SORT_QUEST_EVENT
	);
	assert.equal(
		inventorySortCategory( item( 13, 1, [ 3, 1, 3 ], { codename: "ITEM_ETC_E051003_GARLIC" } ) ),
		SORT_QUEST_EVENT
	);
	assert.equal( inventorySortCategory( item( 13, 1, [ 3, 8, 1 ] ) ), SORT_OTHER );
});

test("equipment sorts by degree, type, part, seal, level, then plus", () => {
	const order = ( a, b ) => compareInventorySort( a, b );
	// Degree 8 (class 22) before degree 1 (class 1).
	const high = item( 13, 1, SWORD, { fields: { itemClass: 22 } } ),
		low = item( 14, 2, SWORD, { fields: { itemClass: 1 } } );
	assert.ok( order( high, low ) < 0 );
	// Within a degree: weapons before shields; necklaces before rings.
	assert.ok( order( item( 13, 1, SWORD ), item( 14, 2, SHIELD ) ) < 0 );
	assert.ok( order( item( 13, 1, NECKLACE ), item( 14, 2, RING ) ) < 0 );
	// Head (TID4 1) before chest (3) before shoulders (2).
	const head = item( 13, 1, [ 1, 3, 1 ] ), chest = item( 14, 2, [ 1, 3, 3 ] ), shoulders = item( 15, 3, [ 1, 3, 2 ] );
	assert.deepEqual( [ shoulders, chest, head ].sort( order ), [ head, chest, shoulders ] );
	// Same degree: Sun (class 3) > Moon (2) > Star (1) > normal.
	const sealed = cls => item( 13, 10 + cls, SWORD, { fields: { itemClass: cls, rarity: 2 } } );
	const sun = sealed( 3 ), moon = sealed( 2 ), star = sealed( 1 );
	const normal = item( 13, 20, SWORD, { fields: { itemClass: 3, rarity: 0 } } );
	assert.deepEqual( [ normal, star, moon, sun ].sort( order ), [ sun, moon, star, normal ] );
	// Then plus, highest first.
	assert.ok( order( item( 13, 1, SWORD, { plus: 7 } ), item( 14, 1, SWORD, { plus: 3 } ) ) < 0 );
});

test("a sort merges partial stacks, orders the bag from its first slot and keeps every item", () => {
	const rows = [
		item( 13, 900, ELEMENT, { quantity: 10, fields: { maxStack: 50 } } ),
		item( 15, 100, MP, { quantity: 30, fields: { maxStack: 50 } } ),
		item( 17, 200, HP, { quantity: 40, fields: { maxStack: 50 } } ),
		item( 20, 300, SWORD, { fields: { itemClass: 4 } } ),
		item( 22, 200, HP, { quantity: 25, fields: { maxStack: 50 } } ),
		item( 30, 100, MP, { quantity: 30, fields: { maxStack: 50 } } ),
		item( 40, 400, RETURN, { quantity: 3, fields: { maxStack: 10 } } )
	];
	const { rows: sorted, moves } = sortToEnd( rows );
	assert.deepEqual( totals( sorted ), totals( rows ) );
	assert.deepEqual(
		sorted.map( row => [ row.slot, row.refObjId, row.quantity ] ),
		[
			[ 13, 200, 50 ],
			[ 14, 200, 15 ],
			[ 15, 100, 50 ],
			[ 16, 100, 10 ],
			[ 17, 400, 3 ],
			[ 18, 300, 1 ],
			[ 19, 900, 10 ]
		]
	);
	assert.ok( moves <= 2 * rows.length );
	assert.equal( nextInventorySortMove( sorted, BAG ), null, "a sorted bag needs no move" );
});

test("two full stacks of one item are never dropped on each other", () => {
	const rows = [
		item( 13, 200, HP, { quantity: 50, fields: { maxStack: 50 } } ),
		item( 14, 900, ELEMENT, { quantity: 5, fields: { maxStack: 50 } } ),
		item( 15, 200, HP, { quantity: 50, fields: { maxStack: 50 } } )
	];
	const move = nextInventorySortMove( rows, BAG );
	assert.deepEqual( move, { source: 15, destination: 14, quantity: 50 } );
});

test("equipment sockets are never read or moved", () => {
	const rows = [ item( 6, 300, SWORD ), item( 20, 200, HP, { quantity: 5, fields: { maxStack: 50 } } ) ];
	assert.deepEqual( nextInventorySortMove( rows, BAG ), { source: 20, destination: 13, quantity: 5 } );
	assert.equal( nextInventorySortMove( [ item( 6, 300, SWORD ) ], BAG ), null );
});

test("a run sends one move at a time and ends when the bag is sorted", () => {
	const run = createInventorySortRun();
	let rows = [ item( 20, 200, HP, { quantity: 5, fields: { maxStack: 50 } } ) ];
	const frame = ( over = {} ) => ({ items: rows, bag: BAG, pending: false, error: null, locked: false, ...over });
	assert.deepEqual( run.step( frame() ), { move: null, ended: false }, "an idle run sends nothing" );
	run.start( "bag" );
	assert.deepEqual( run.step( frame( { pending: true } ) ).move, null, "a request in flight waits" );
	const first = run.step( frame() );
	assert.deepEqual( first.move, { source: 20, destination: 13, quantity: 5 } );
	rows = [ { ...rows[0], slot: 13 } ];
	assert.deepEqual( run.step( frame() ), { move: null, ended: true } );
	assert.equal( run.active(), false );
});

test("a refused move, a locking window or a cancel ends the run", () => {
	const rows = [
		item( 20, 200, HP, { quantity: 5, fields: { maxStack: 50 } } ),
		item( 25, 100, MP, { quantity: 5, fields: { maxStack: 50 } } )
	];
	const frame = ( over = {} ) => ({ items: rows, bag: BAG, pending: false, error: null, locked: false, ...over });
	const run = createInventorySortRun();
	// An error left from before the sort does not end it; one after its move does.
	run.start( "bag" );
	assert.ok( run.step( frame( { error: "Inventory rejected: 3" } ) ).move );
	assert.deepEqual( run.step( frame( { error: "Inventory rejected: 3" } ) ), { move: null, ended: true } );
	run.start( "bag" );
	assert.deepEqual( run.step( frame( { locked: true } ) ), { move: null, ended: true } );
	run.start( "bag" );
	assert.equal( run.cancel(), true );
	assert.equal( run.active(), false );
});

test("a run names the container it sorts until it ends, and a new run replaces it", () => {
	const run = createInventorySortRun();
	assert.equal( run.container(), undefined );
	run.start( "cos", 77 );
	assert.equal( run.container(), "cos" );
	assert.equal( run.gid(), 77 );
	run.start( "storage" );
	assert.equal( run.container(), "storage" );
	// A storage room's slots start at 0: the planner sorts that range alone.
	const rows = [
		item( 3, 200, HP, { quantity: 5, fields: { maxStack: 50 } } ),
		item( 0, 100, MP, { quantity: 5, fields: { maxStack: 50 } } )
	];
	const step = run.step( { items: rows, bag: { first: 0, end: 150 }, pending: false, error: null, locked: false } );
	assert.deepEqual( step.move && [ step.move.source, step.move.destination ], [ 3, 0 ] );
	// A closed warehouse (no target) ends the run.
	assert.deepEqual( run.step( { items: [], bag: { first: 0, end: 0 }, pending: false, error: null, locked: true } ), {
		move: null,
		ended: true
	} );
	assert.equal( run.container(), undefined );
});

test("a run that finds a layout it already left stops", () => {
	const rows = [
		item( 20, 200, HP, { quantity: 5, fields: { maxStack: 50 } } ),
		item( 25, 100, MP, { quantity: 5, fields: { maxStack: 50 } } )
	];
	const frame = { items: rows, bag: BAG, pending: false, error: null, locked: false };
	const run = createInventorySortRun();
	run.start( "bag" );
	assert.ok( run.step( frame ).move );
	// The server answered but the bag is unchanged (it ignored the move).
	assert.deepEqual( run.step( frame ), { move: null, ended: true } );
});

test("random bags always end sorted, merged and whole", () => {
	// A fixed linear congruential generator keeps the bags reproducible.
	let seed = 12345;
	const next = n => (seed = (seed * 1103515245 + 12345) % 2147483648) % n;
	/** @type {readonly [number, readonly number[], number][]} */
	const kinds = [
		[ 200, HP, 50 ],
		[ 100, MP, 50 ],
		[ 400, RETURN, 10 ],
		[ 900, ELEMENT, 50 ],
		[ 300, SWORD, 1 ],
		[ 301, SHIELD, 1 ],
		[ 302, NECKLACE, 1 ],
		[ 500, ELIXIR, 20 ]
	];
	for ( let round = 0; round < 200; round++ ) {
		const free = Array.from( { length: BAG.end - BAG.first }, ( _, i ) => BAG.first + i );
		const rows = Array.from( { length: next( 28 ) }, () => {
			const [ref, type, cap] = kinds[next( kinds.length )];
			const slot = free.splice( next( free.length ), 1 )[0];
			return item( slot, ref, type, {
				quantity: cap === 1 ? 1 : 1 + next( cap ),
				plus: cap === 1 ? next( 4 ) : 0,
				// Reference fields belong to the reference, as in the game data.
				fields: { maxStack: cap, itemClass: 1 + (ref % 6) }
			} );
		} );
		const { rows: sorted, moves } = sortToEnd( rows );
		assert.deepEqual( totals( sorted ), totals( rows ) );
		assert.ok( moves <= 2 * rows.length + 2 );
		assert.deepEqual( sorted.map( row => row.slot ), sorted.map( ( _, i ) => BAG.first + i ), "no gap" );
		for ( let i = 1; i < sorted.length; i++ ) {
			assert.ok( compareInventorySort( sorted[i - 1], sorted[i] ) <= 0, "ordered" );
		}
		const partial = sorted.filter( row => row.quantity < row.tooltip.fields.maxStack );
		assert.equal( new Set( partial.map( row => row.refObjId ) ).size, partial.length, "one partial per item" );
	}
});
