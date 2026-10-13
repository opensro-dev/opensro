/*
===========================================================================

storage-contents.test.mjs - an open storage shows what it holds, loaded or not

BUG report thread 1559029656391393347: "I put items yet I can't see them",
the items vanishing and coming back on hover. A window waited for every
image it drew before it published, replaying its earlier contents until
then; a deposited item whose icon was still loading (or absent) kept the
storage on its contents before the deposit. Report BR-261013-1135-AD39:
hovering a stored item showed no information at all.

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

test("hovering a stored item shows its tooltip, as hovering it in the bag does", t => {
	const f = uiFixture();
	t.after( () => f.dispose() );
	let now = 0;
	const step = () => {
		for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, now += 100 );
	};
	// The tooltip title may use the plain or the emphasized face.
	const shown = name => [ 0, 2 ].some( style => f.hasText( name, "0", style ) );
	step();
	const stored = { ...row( 0, 1, "Stored Stack", "item/etc/hp_potion_01.ddj" ), quantity: 10, magic: [] };
	f.state.gameplay = {
		...f.state.gameplay,
		// The fixture types its bag as empty; this case needs one row.
		inventory: /** @type {any} */ ([ { ...stored, slot: 14, name: "Bag Stack" } ]),
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		inventoryPending: false,
		storage: { npc: 17, phase: "open", capacity: 150, gold: "0", items: [ stored ] }
	};
	f.ui.event( { kind: "activate", id: "open-window:Storage" } );
	step();
	f.ui.event( { kind: "hover", id: "storage-slot:0" } );
	step();
	assert.ok( shown( "Stored Stack" ), "the stored item's tooltip" );
	f.ui.event( { kind: "hover", id: "slot:14" } );
	step();
	assert.ok( shown( "Bag Stack" ), "the bag item's tooltip" );
	assert.equal( shown( "Stored Stack" ), false, "only the hovered item's" );
});
