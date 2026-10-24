/*
===========================================================================

gender-tool.test.mjs - armour gender tool activation, targeting and confirmation

Exercise the production HUD so a confirmed use cannot re-arm the cursor or
lose the selected armour slot before reaching the worker.

Native v1.150 client SHA-256:
f6580bf65213c0c074efe312514c55c73040667e7a9ad1aae9b8595d48ccd3a2.
Instruction-checked: 5501D0 classifies group 13, 562140..5621D9 arms A6
for subtype 8, and 567372..5673F9 opens the shared type-1F confirmation.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

/*
================
genderToolFixture
================
*/
function genderToolFixture() {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	const base = { quantity: 1, plus: 0, durability: 0, variance: "0", magic: [] };
	const tool = { ...base, slot: 13, refObjId: 3787, typeFlags: 0x46ec };
	const armour = { ...base, slot: 14, refObjId: 600, typeFlags: 0x08ac };
	const state = {
		...f.state,
		gameplay: {
			...f.state.gameplay,
			inventory: [ tool, armour ],
			inventorySlotCount: 45,
			quickSlots: [ { slot: 0, kind: 0x46, payload: 0 } ]
		}
	};
	f.ui.step( state, 0 );
	f.ui.event( { kind: "key", code: "KeyI" } );
	let scene, now = 100;
	/*
	================
	settle
	================
	*/
	function settle() {
		for ( const end = now + 500; now <= end; now += 100 ) scene = f.ui.step( state, now ) ?? scene;
		assert.ok( scene );
		return scene;
	}
	settle();
	return { ...f, state, sent, settle };
}

const activations = /** @type {const} */ ([
	{ kind: "right-activate", id: "slot:13" },
	{ kind: "activate", id: "hotbar:0" },
	{ kind: "right-activate", id: "hotbar:0" }
]);
for ( const activation of activations ) {
	test(`gender tool ${activation.kind} on ${activation.id} selects armour before sending use`, () => {
		const f = genderToolFixture();
		try {
			f.ui.event( activation );
			assert.equal( f.ui.cursor(), 0xa6 );
			assert.equal( f.sent.length, 0, "activation waits for an armour target" );
			f.settle();
			f.ui.event( { kind: "activate", id: "slot:14" } );
			assert.equal( f.ui.cursor(), null );
			assert.equal( f.sent.length, 0, "selection waits for confirmation" );
			const scene = f.settle();
			assert.ok( scene.controls.some( row => row.id === "gender-change-confirm" ) );
			assert.ok( !scene.controls.some( row => row.id === "cos-renew-confirm" ) );
			f.ui.event( { kind: "activate", id: "gender-change-confirm" } );
			assert.deepEqual( f.sent, [ {
				kind: "gameplay",
				command: { kind: "item-use", slot: 13, targetSlot: 14 }
			} ] );
			assert.equal( f.ui.cursor(), null, "a confirmed use must not arm the tool again" );
		} finally {
			f.dispose();
		}
	});
}

test("cancelling the gender tool's cursor or confirmation sends no item use", () => {
	const f = genderToolFixture();
	try {
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		assert.equal( f.ui.cursor(), 0xa6 );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.equal( f.ui.cursor(), null );
		f.settle();
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		f.ui.event( { kind: "activate", id: "slot:14" } );
		const scene = f.settle();
		assert.ok( scene.controls.some( row => row.id === "gender-change-cancel" ) );
		f.ui.event( { kind: "activate", id: "gender-change-cancel" } );
		assert.equal( f.ui.cursor(), null );
		assert.equal( f.sent.length, 0 );
	} finally {
		f.dispose();
	}
});
