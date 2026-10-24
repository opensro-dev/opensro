/*
===========================================================================

exchange-click-shortcuts.test.mjs - Ctrl/right/double-click stages into an exchange

Port-only, not native (owner decision 2026-10-24): with the Experimental
"Exchange click shortcuts" row on (the default), Ctrl+click, right-click
and double-click on a bag item stage it into the open exchange through the
same path as the native drag-and-drop: a single item goes on the table at
once, a stack first asks how much in the divide box's twin, and a partial
offer moves the rest to the bag's first free slot before the put. Row off,
a closed exchange or a locked offer leaves every gesture's old meaning.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

// An etc stack (TypeID 3/3/1/1, so 0x6c in the split mask) and a single.
const STACK = {
	slot: 20,
	refObjId: 421,
	typeFlags: 0x8ec,
	quantity: 10,
	name: "Bolt",
	plus: 0,
	durability: 0,
	variance: "0",
	magic: []
};
const SINGLE = {
	slot: 21,
	refObjId: 422,
	typeFlags: 0x8ec,
	quantity: 1,
	name: "Arrow",
	plus: 0,
	durability: 0,
	variance: "0",
	magic: []
};

/*
================
commands
================
*/
function commands( sent ) {
	return sent.filter( m => m.kind === "gameplay" ).map( m => m.command );
}

/*
================
exchangeFixture

The bag and the exchange open together, as they do in the world: the
exchange forces the Inventory panel (69FBB0).
================
*/
function exchangeFixture( t, { open = true, locked = false, items = [ STACK, SINGLE ] } = {} ) {
	const sent = [];
	const f = uiFixture( m => sent.push( m ) );
	t.after( () => f.dispose() );
	Object.assign( f.state.gameplay, {
		inventory: items,
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		inventoryPending: false,
		exchange: {
			open,
			partner: 2,
			own: [],
			theirs: [],
			ownGold: 0,
			theirGold: 0,
			ownLocked: locked,
			theirLocked: false,
			approved: false,
			requesting: false
		}
	} );
	let now = 1000, last = null;
	const step = () => {
		for ( let i = 0; i < 20; i++ ) last = f.ui.step( f.state, now += 50 ) ?? last;
		return defined( last );
	};
	step();
	f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
	step();
	return { f, sent, step };
}

test("Ctrl+click stages a single bag item at once", t => {
	const { f, sent, step } = exchangeFixture( t );
	f.ui.event( { kind: "activate", id: "slot:" + SINGLE.slot, ctrl: true } );
	step();
	assert.deepEqual( commands( sent ), [ { kind: "exchange-put", slot: SINGLE.slot } ] );
});

test("Ctrl+click on a stack asks its count; a partial confirm moves the rest off first", t => {
	const { f, sent, step } = exchangeFixture( t );
	f.ui.event( { kind: "activate", id: "slot:" + STACK.slot, ctrl: true } );
	const scene = step();
	assert.deepEqual( commands( sent ), [] );
	assert.equal(
		defined( scene.controls.find( c => c.id === "exchange-count" ) ).value,
		String( STACK.quantity )
	);
	f.ui.event( {
		kind: "edit",
		id: "exchange-count",
		value: "4",
		start: 1,
		end: 1,
		composing: false
	} );
	step();
	// The prompt swallows other gestures while it is up.
	f.ui.event( { kind: "activate", id: "slot:" + SINGLE.slot, ctrl: true } );
	step();
	assert.deepEqual( commands( sent ), [] );
	f.ui.event( { kind: "activate", id: "exchange-count-confirm" } );
	step();
	assert.deepEqual( commands( sent ), [
		{ kind: "inventory-move", source: STACK.slot, destination: 13, quantity: 6 },
		{ kind: "exchange-put", slot: STACK.slot }
	] );
});

test("an over-large count clamps to the stack and Enter puts the whole stack", t => {
	const { f, sent, step } = exchangeFixture( t );
	f.ui.event( { kind: "activate", id: "slot:" + STACK.slot, ctrl: true } );
	step();
	f.ui.event( {
		kind: "edit",
		id: "exchange-count",
		value: "99",
		start: 2,
		end: 2,
		composing: false
	} );
	step();
	f.ui.event( { kind: "key", code: "Enter" } );
	step();
	assert.deepEqual( commands( sent ), [ { kind: "exchange-put", slot: STACK.slot } ] );
});

test("Escape cancels the count box and stages nothing", t => {
	const { f, sent, step } = exchangeFixture( t );
	f.ui.event( { kind: "activate", id: "slot:" + STACK.slot, ctrl: true } );
	step();
	f.ui.event( { kind: "key", code: "Escape" } );
	const scene = step();
	assert.deepEqual( commands( sent ), [] );
	assert.equal( scene.controls.some( c => c.id === "exchange-count" ), false );
});

test("a partial offer with no free bag slot stages nothing", t => {
	const items = Array.from( { length: 32 }, ( _, i ) => ({ ...SINGLE, slot: 13 + i, refObjId: 500 + i }) );
	items[STACK.slot - 13] = STACK;
	const { f, sent, step } = exchangeFixture( t, { items } );
	f.ui.event( { kind: "activate", id: "slot:" + STACK.slot, ctrl: true } );
	step();
	f.ui.event( {
		kind: "edit",
		id: "exchange-count",
		value: "4",
		start: 1,
		end: 1,
		composing: false
	} );
	step();
	f.ui.event( { kind: "activate", id: "exchange-count-confirm" } );
	step();
	assert.deepEqual( commands( sent ), [] );
});

test("right-click and double-click stage like a drop does", t => {
	const { f, sent, step } = exchangeFixture( t );
	f.ui.event( { kind: "right-activate", id: "slot:" + SINGLE.slot } );
	step();
	assert.deepEqual( commands( sent ), [ { kind: "exchange-put", slot: SINGLE.slot } ] );
	f.ui.event( { kind: "double-activate", id: "slot:" + SINGLE.slot } );
	step();
	assert.deepEqual( commands( sent ), [
		{ kind: "exchange-put", slot: SINGLE.slot },
		{ kind: "exchange-put", slot: SINGLE.slot }
	] );
	f.ui.event( { kind: "right-activate", id: "slot:" + STACK.slot } );
	step();
	assert.equal( commands( sent ).length, 2, "the stack asked its count instead of staging" );
	f.ui.event( { kind: "activate", id: "exchange-count-confirm" } );
	step();
	assert.deepEqual( commands( sent ).at( -1 ), { kind: "exchange-put", slot: STACK.slot } );
});

test("a drop on the table takes the same path, count box included", t => {
	const { f, sent, step } = exchangeFixture( t );
	const target = defined( step().controls.find( c => c.id === "exchange-my:0" ) ),
		x = target.rect[0] + 5,
		y = target.rect[1] + 5;
	f.ui.event( { kind: "drag", id: "slot:" + SINGLE.slot, dx: 5, dy: 5 } );
	f.ui.event( { kind: "drag-end", id: "slot:" + SINGLE.slot, x, y } );
	step();
	assert.deepEqual( commands( sent ), [ { kind: "exchange-put", slot: SINGLE.slot } ] );
	f.ui.event( { kind: "drag", id: "slot:" + STACK.slot, dx: 5, dy: 5 } );
	f.ui.event( { kind: "drag-end", id: "slot:" + STACK.slot, x, y } );
	const scene = step();
	assert.equal( commands( sent ).length, 1, "the dropped stack asked its count instead of staging" );
	assert.ok( scene.controls.some( c => c.id === "exchange-count" ) );
	f.ui.event( { kind: "activate", id: "exchange-count-cancel" } );
	step();
	assert.equal( commands( sent ).length, 1 );
});

test("with the row off the gestures keep their old meanings", t => {
	const { f, sent, step } = exchangeFixture( t );
	f.ui.event( /** @type {any} */ ({ kind: "experimental-preferences", value: { exchangeClickShortcuts: false } }) );
	step();
	f.ui.event( { kind: "activate", id: "slot:" + SINGLE.slot, ctrl: true } );
	step();
	assert.deepEqual( commands( sent ), [], "Ctrl+click staged with the row off" );
	f.ui.event( { kind: "right-activate", id: "slot:" + SINGLE.slot } );
	step();
	assert.deepEqual( commands( sent ), [ { kind: "item-use", slot: SINGLE.slot } ] );
	sent.length = 0;
	f.ui.event( { kind: "double-activate", id: "slot:" + SINGLE.slot } );
	step();
	assert.deepEqual( commands( sent ), [ { kind: "item-use", slot: SINGLE.slot } ] );
});

test("outside an exchange the gestures do nothing new", t => {
	const { f, sent, step } = exchangeFixture( t, { open: false } );
	f.ui.event( { kind: "activate", id: "slot:" + SINGLE.slot, ctrl: true } );
	step();
	assert.deepEqual( commands( sent ), [] );
	f.ui.event( { kind: "right-activate", id: "slot:" + SINGLE.slot } );
	step();
	assert.deepEqual( commands( sent ), [ { kind: "item-use", slot: SINGLE.slot } ] );
	sent.length = 0;
	f.ui.event( { kind: "double-activate", id: "slot:" + STACK.slot } );
	step();
	assert.deepEqual( commands( sent ), [ { kind: "item-use", slot: STACK.slot } ] );
});

test("a locked offer stages nothing and the gestures keep their old meanings", t => {
	const { f, sent, step } = exchangeFixture( t, { locked: true } );
	f.ui.event( { kind: "activate", id: "slot:" + SINGLE.slot, ctrl: true } );
	step();
	assert.deepEqual( commands( sent ), [] );
	f.ui.event( { kind: "right-activate", id: "slot:" + SINGLE.slot } );
	step();
	assert.deepEqual( commands( sent ), [ { kind: "item-use", slot: SINGLE.slot } ] );
});
