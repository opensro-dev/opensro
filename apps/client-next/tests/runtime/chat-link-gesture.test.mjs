/*
===========================================================================

chat-link-gesture.test.mjs - Alt+click links an item into the chat line

Port-only, not native (SRO_CHAT_ITEM_LINKS). With the chat input focused,
Alt+click on a bag item puts its link in the line and the send carries it;
with the input not focused Alt+click keeps its native bag to pet move,
Shift+click keeps the native stack split, and the server switch off
inserts nothing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const { CHAT_LINK_MARKER } = await import( "../../src/engine/foundation/gameplay/chat-item-links.ts" );

/*
================
linkFixture

A world UI with a stack in bag slot 13, a summoned pet with a bag, and the
server's link limit (absent is the switch off).
================
*/
function linkFixture( chatItemLinks ) {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	const game = {
		...f.state.gameplay,
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		inventoryPending: false,
		inventory: [ { slot: 13, refObjId: 8, typeFlags: 0x6c, quantity: 30, name: "Potion", plus: 0, magic: [] } ],
		cosRecords: [ {
			gid: 7,
			refObjId: 102,
			band: 4,
			name: "Pet",
			hp: 100,
			dead: false,
			status: 4,
			inventory: [ { slot: 0, refObjId: 8, quantity: 40 } ]
		} ],
		...(chatItemLinks ? { chatItemLinks } : {})
	};
	const state = { ...f.state, gameplay: game };
	let now = 0;
	const step = () => f.ui.step( state, ++now );
	const event = e => {
		f.ui.event( e );
		return step();
	};
	step();
	return { f, sent, event };
}

/*
================
chatValue
================
*/
function chatValue( semantics ) {
	return semantics?.controls.find( c => c.id === "chat-text" )?.value;
}

test("Alt+click a bag item with the input focused links it and the send carries it", () => {
	const { f, sent, event } = linkFixture( 1 );
	try {
		event( { kind: "activate", id: "open-window:Inventory" } );
		event( { kind: "focus", id: "chat-text" } );
		event( { kind: "edit", id: "chat-text", value: "wts ", start: 4, end: 4, composing: false } );
		const shown = event( { kind: "activate", id: "slot:13", alt: true } );
		assert.equal( chatValue( shown ), "wts " + CHAT_LINK_MARKER );
		event( { kind: "activate", id: "chat-send" } );
		const line = sent.findLast( c => c.kind === "chat" );
		assert.equal( line.text, "wts " + CHAT_LINK_MARKER );
		assert.deepEqual( line.links, [ { source: 0, slot: 13 } ] );
	} finally {
		f.dispose();
	}
});

test("Alt+click with the input not focused still moves the item to the pet", () => {
	const { f, sent, event } = linkFixture( 1 );
	try {
		event( { kind: "activate", id: "open-window:Inventory" } );
		event( { kind: "activate", id: "cos-bag" } );
		const shown = event( { kind: "activate", id: "slot:13", alt: true } );
		assert.deepEqual( sent.at( -1 ), { kind: "cos-transfer", gid: 7, toCos: true, source: 13 } );
		assert.equal( chatValue( shown ) ?? "", "" );
	} finally {
		f.dispose();
	}
});

test("Shift+click still opens the split with the input focused", () => {
	const { f, event } = linkFixture( 1 );
	try {
		event( { kind: "activate", id: "open-window:Inventory" } );
		event( { kind: "focus", id: "chat-text" } );
		const shown = event( { kind: "activate", id: "slot:13", shift: true } );
		assert.ok( shown.controls.some( c => c.id === "split-amount" ) );
		assert.equal( chatValue( shown ) ?? "", "" );
	} finally {
		f.dispose();
	}
});

test("the server switch off inserts nothing", () => {
	const { f, event } = linkFixture( undefined );
	try {
		event( { kind: "activate", id: "open-window:Inventory" } );
		event( { kind: "focus", id: "chat-text" } );
		const shown = event( { kind: "activate", id: "slot:13", alt: true } );
		assert.equal( chatValue( shown ) ?? "", "" );
	} finally {
		f.dispose();
	}
});
