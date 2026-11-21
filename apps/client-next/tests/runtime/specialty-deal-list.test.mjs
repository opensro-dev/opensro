/*
===========================================================================

specialty-deal-list.test.mjs - the trade window's scale list stays visible

The open Trade scale list is a popup: it is published after the window's
Confirm button, so nothing draws over it (a player saw Confirm cover rows 2
and 3). Port-only, not native (owner 2026-11-21: the default, no row): a
list that would leave the window opens upward from its box; the original
opens it downward.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

// A trader, the transport the goods load into, and a trade goods type word
// (specialty-deal.ts isTradeGoods: TID 3/3/8).
const TRADER = 18;
const TRANSPORT = 7;
const TRADE_GOODS_TYPE = 0x46c;

/*
================
openScaleList

The deal box for one trade good with its scale list open; the published
controls.
================
*/
function openScaleList() {
	const f = uiFixture();
	let now = 0;
	/** @type {any} */
	let seen = null;
	const step = () => {
		for ( let i = 0; i < 16; i++ ) seen = f.ui.step( f.state, now += 100 ) ?? seen;
	};
	f.state.entities.push( { ...f.state.entities[0], gid: TRADER, refObjId: 2011, kind: "npc", name: "Trader" } );
	f.state.gameplay.cosRecords = [
		{ gid: TRANSPORT, refObjId: 100, band: 4, hp: 100, mp: 0, status: 0, dead: false, inventory: [] }
	];
	Object.assign( f.state.gameplay, {
		target: TRADER,
		progression: { level: 40, gold: "10000000" },
		shop: {
			npc: TRADER,
			name: "Trader",
			cosGid: TRANSPORT,
			offers: [ {
				tab: 0,
				slot: 0,
				refObjId: 2001,
				name: "Trade goods",
				price: "100",
				maxStack: 10,
				items: [ { refObjId: 2001, typeFlags: TRADE_GOODS_TYPE, quantity: 10, name: "Trade goods" } ]
			} ]
		}
	} );
	step();
	f.ui.event( { kind: "activate", id: "open-window:Shop" } );
	step();
	f.ui.event( { kind: "activate", id: "shop-offer:0" } );
	step();
	f.ui.event( { kind: "activate", id: "specialty-deal-scale" } );
	step();
	const controls = seen?.controls ?? [];
	f.dispose();
	return controls;
}

test("the open scale list is published after Confirm, so nothing covers it", () => {
	const ids = openScaleList().map( control => control.id );
	const ok = ids.indexOf( "specialty-deal-ok" ), first = ids.indexOf( "specialty-deal-row:0" );
	assert.ok( ok >= 0 && first > ok, `rows ${first} after Confirm ${ok}` );
});

test("a list that would leave the window opens upward", () => {
	const at = controls => ({
		box: controls.find( control => control.id === "specialty-deal-scale" ).rect,
		row: controls.find( control => control.id === "specialty-deal-row:0" ).rect
	});
	const on = at( openScaleList() );
	assert.ok( on.row[1] < on.box[1], "the list sits above its box" );
});
