/*
===========================================================================

exchange-approve.test.mjs - the exchange confirm button's caption (#2276, #3829)

0xB095 [1] accepting the own lock (75B450) runs CIFExchange_SetConfirmMode
(6B21B0) with 1, which swaps the confirm button's caption to
UIIT_CTL_ACKNOWLEDGMENT ("Approve") at 6B21F6 and leaves the button
disabled until the partner locks. The approve answer 0xB34A [1] (75B4B0)
only disables it again; the caption stays.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

/*
================
exchangeFixture

The exchange open against an empty table, ready to read its controls.
================
*/
function exchangeFixture( t ) {
	const f = uiFixture();
	t.after( () => f.dispose() );
	Object.assign( f.state.gameplay, {
		inventory: [],
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		inventoryPending: false,
		exchange: {
			open: true,
			partner: 2,
			own: [],
			theirs: [],
			ownGold: 0,
			theirGold: 0,
			ownLocked: false,
			theirLocked: false,
			approved: false,
			requesting: false
		}
	} );
	let now = 1000, last = null;
	/*
	================
	step
	================
	*/
	const step = () => {
		for ( let i = 0; i < 20; i++ ) last = f.ui.step( f.state, now += 50 ) ?? last;
		return defined( last );
	};
	step();
	f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
	step();
	/*
	================
	confirm
	================
	*/
	const confirm = () => defined( step().controls.find( c => c.id === "exchange-confirm" ) );
	return { f, confirm };
}

test("the confirm button reads Approve from the own lock on", t => {
	const { f, confirm } = exchangeFixture( t );
	assert.equal( confirm().label, "Confirm" );
	assert.equal( confirm().disabled, false );
	f.state.gameplay.exchange.ownLocked = true;
	assert.equal( confirm().label, "Approve", "0xB095 [1] turns confirm mode on" );
	assert.equal( confirm().disabled, true, "until the partner locks" );
	f.state.gameplay.exchange.theirLocked = true;
	assert.equal( confirm().label, "Approve" );
	assert.equal( confirm().disabled, false, "both locked: Approve is pressable" );
	f.state.gameplay.exchange.approved = true;
	assert.equal( confirm().label, "Approve" );
	assert.equal( confirm().disabled, true );
});
