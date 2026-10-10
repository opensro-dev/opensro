/*
===========================================================================

alchemy-ctrl-click.test.mjs - Ctrl+click puts a bag item into the alchemy window

Port-only, not native (owner decision 2026-10-10): with the alchemy window
open, Ctrl+click on a bag item places it as double-clicking it does.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

// A sword (TypeID 3/1/6/2): equipment, so the reinforce window takes it.
const SWORD = { slot: 13, refObjId: 3633, typeFlags: 0x132c, quantity: 1, name: "Sword" };

/*
================
alchemyFixture
================
*/
function alchemyFixture( t ) {
	const f = uiFixture( () => {} );
	t.after( () => f.dispose() );
	Object.assign( f.state.gameplay, { inventory: [ SWORD ], inventorySlotCount: 45, equipmentSlotCount: 13 } );
	let now = 1000, last = null;
	const step = () => {
		for ( let i = 0; i < 20; i++ ) last = f.ui.step( f.state, now += 50 ) ?? last;
		return defined( last );
	};
	step();
	f.ui.event( { kind: "activate", id: "open-window:Alchemy" } );
	step();
	return { f, step };
}

/*
================
placed
================
*/
function placed( presentation ) {
	return presentation.controls.some( c => c.id === "alchemy-slot:" + SWORD.slot );
}

test("Ctrl+click on a bag item places it in the open alchemy window, as double-click does", t => {
	const ctrl = alchemyFixture( t );
	assert.ok( !placed( ctrl.step() ), "the window started with the sword placed" );
	ctrl.f.ui.event( { kind: "activate", id: "slot:" + SWORD.slot, ctrl: true } );
	assert.ok( placed( ctrl.step() ), "Ctrl+click did not place the sword" );

	const double = alchemyFixture( t );
	double.f.ui.event( { kind: "double-activate", id: "slot:" + SWORD.slot } );
	assert.ok( placed( double.step() ), "the double-click path no longer places it" );
});
