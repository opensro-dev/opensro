/*
===========================================================================

storage-contents.test.mjs - an open storage shows what it holds, loaded or not

BUG report thread 1559029656391393347: "I put items yet I can't see them",
the items vanishing and coming back on hover. A window waited for every
image it drew before it published, replaying its earlier contents until
then; a deposited item whose icon was still loading (or absent) kept the
storage on its contents before the deposit.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

// The deposited item's icon, held back to stand for a slow or absent image.
const HELD_ICON = "/ch_sword_01_a.png";

/*
================
row
================
*/
function row( slot, refObjId, name, icon ) {
	return { slot, refObjId, quantity: 1, name, icon, typeFlags: 0x6c };
}

test("a deposited item shows in storage while its icon still loads", t => {
	let held = true;
	const f = uiFixture( undefined, path => held && path.endsWith( HELD_ICON ) );
	t.after( () => f.dispose() );
	let now = 0;
	// A property, not a local: the closure assigns it.
	const seen = {
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */ presentation: null
	};
	const step = () => {
		for ( let i = 0; i < 16; i++ ) seen.presentation = f.ui.step( f.state, now += 100 ) ?? seen.presentation;
	};
	const shown = () =>
		(seen.presentation?.controls ?? []).filter( c => c.id.startsWith( "storage-slot:" ) && c.label ).map( c =>
			c.label
		);
	step();
	f.state.gameplay = {
		...f.state.gameplay,
		storage: {
			npc: 17,
			phase: "open",
			capacity: 150,
			gold: "0",
			items: [ row( 0, 1, "Potion", "item/etc/hp_potion_01.ddj" ) ]
		}
	};
	f.ui.event( { kind: "activate", id: "open-window:Storage" } );
	step();
	assert.deepEqual( shown(), [ "Potion" ] );
	f.state.gameplay = {
		...f.state.gameplay,
		storage: {
			...f.state.gameplay.storage,
			items: [
				row( 0, 1, "Potion", "item/etc/hp_potion_01.ddj" ),
				row( 1, 2, "Sword", "item/china/weapon/ch_sword_01_a.ddj" )
			]
		}
	};
	step();
	assert.deepEqual( shown(), [ "Potion", "Sword" ], "the deposit shows before its icon arrives" );
	assert.ok(
		!seen.presentation?.controls.find( c => c.label === "Sword" )?.disabled,
		"the new item is live, not a replayed snapshot"
	);
	held = false;
	step();
	assert.deepEqual( shown(), [ "Potion", "Sword" ] );
});
