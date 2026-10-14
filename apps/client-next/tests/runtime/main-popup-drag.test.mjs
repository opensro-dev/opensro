/*
===========================================================================

main-popup-drag.test.mjs - the bag moves by its title bar beside every window

The main popup shows the bag beside the warehouse and the magic option
window too. Its drag once answered a hand-copied panel list without those
two, so the bag stayed wherever it opened (INVENTORY_POPUP_PANELS).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const DRAG = "main-popup-drag";

/*
================
dragRect

The bag's title bar after f.ui has drawn its current panel.
================
*/
function dragRect( f, at ) {
	let output = f.ui.step( f.state, at );
	for ( let i = 1; i < 20; i++ ) output = f.ui.step( f.state, at + i ) ?? output;
	const control = output?.controls.find( c => c.id === DRAG );
	assert.ok( control, "the bag's title bar is drawn" );
	return control.rect;
}

/*
================
assertDrags

Dragging the title bar moves the bag by the drag.
================
*/
function assertDrags( f, at ) {
	const before = dragRect( f, at );
	f.ui.event( { kind: "drag", id: DRAG, dx: -60, dy: -40 } );
	const after = dragRect( f, at + 100 );
	assert.deepEqual( [ after[0] - before[0], after[1] - before[1] ], [ -60, -40 ], "the bag follows the drag" );
}

test("the bag drags beside the warehouse", () => {
	const f = uiFixture();
	try {
		f.ui.step( f.state, 0 );
		// A storage room appearing opens the warehouse, as a ticket does; the
		// UI compares against the previous view, so this is a new state object.
		const storage = { npc: 17, phase: "open", capacity: 150, gold: "0", items: [] };
		f.state = { ...f.state, gameplay: { ...f.state.gameplay, storage } };
		assertDrags( f, 10 );
	} finally {
		f.dispose();
	}
});

test("the bag drags beside the magic option window", () => {
	const f = uiFixture();
	try {
		f.ui.step( f.state, 0 );
		f.state.gameplay.magicOption = { visible: true, item: null, parts: [], phase: "idle" };
		assertDrags( f, 10 );
	} finally {
		f.dispose();
	}
});

test("the bag on its own still drags", () => {
	const f = uiFixture();
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "key", code: "KeyI" } );
		assertDrags( f, 10 );
	} finally {
		f.dispose();
	}
});
