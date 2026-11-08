/*
===========================================================================

experimental-list.test.mjs - the Experimental window's sectioned list

Port-only, not native. A tab's list is each section's header, then its rows
two per line, left to right. It scrolls line by line: a view never starts
on a header, because a view starting on a header paints exactly as one
starting on its first line with that header pinned, and such a step would
move nothing on screen.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { experimentalListItems, experimentalListRange, experimentalListView } = await import(
	"../../src/engine/foundation/ui/experimental-list.ts"
);

// Sections A (5 rows: 3 lines) and B (9 rows: 5 lines):
// 2 x 40 + 8 x 46 = 448 > 362.
const ITEMS = experimentalListItems( [
	{ title: "A", rows: [ "a1", "a2", "a3", "a4", "a5" ] },
	{ title: "B", rows: [ "b1", "b2", "b3", "b4", "b5", "b6", "b7", "b8", "b9" ] }
] );

/*
================
name
================
*/
function name( item ) {
	return item.kind === "head" ? "#" + item.title : item.rows.join( "+" );
}

test("items are each section's header then its rows two per line", () => {
	assert.deepEqual( ITEMS.map( name ), [
		"#A",
		"a1+a2",
		"a3+a4",
		"a5",
		"#B",
		"b1+b2",
		"b3+b4",
		"b5+b6",
		"b7+b8",
		"b9"
	] );
});

test("the range counts the line steps until the rest fits under its pinned header", () => {
	// From a5: the pinned A, a5, #B and five lines = 40 + 46 + 40 + 230 = 356.
	// From a3+a4 one more line makes 402. Steps: a1+a2 (0), a3+a4 (1), a5 (2).
	assert.equal( experimentalListRange( ITEMS ), 2 );
	assert.equal( experimentalListRange( ITEMS.slice( 0, 4 ) ), 0 );
	assert.equal( experimentalListRange( [] ), 0 );
});

test("the top view pins the first section and stacks what fits", () => {
	const view = experimentalListView( ITEMS, 0 );
	assert.equal( view.pinned, "A" );
	assert.deepEqual( view.slots.map( slot => [ name( slot.item ), slot.top ] ), [
		[ "a1+a2", 40 ],
		[ "a3+a4", 86 ],
		[ "a5", 132 ],
		[ "#B", 178 ],
		[ "b1+b2", 218 ],
		[ "b3+b4", 264 ],
		[ "b5+b6", 310 ]
	] );
});

test("every step moves the first painted line", () => {
	const firsts = [ 0, 1, 2 ].map( step => name( experimentalListView( ITEMS, step ).slots[0].item ) );
	assert.deepEqual( firsts, [ "a1+a2", "a3+a4", "a5" ] );
	// The last step reaches the end of the list.
	const last = experimentalListView( ITEMS, 2 ).slots.at( -1 );
	assert.ok( last );
	assert.equal( name( last.item ), "b9" );
});

test("the step is truncated and clamped", () => {
	assert.deepEqual( experimentalListView( ITEMS, 99 ), experimentalListView( ITEMS, 2 ) );
	assert.deepEqual( experimentalListView( ITEMS, -3 ), experimentalListView( ITEMS, 0 ) );
	assert.deepEqual( experimentalListView( ITEMS, 1.7 ), experimentalListView( ITEMS, 1 ) );
});
