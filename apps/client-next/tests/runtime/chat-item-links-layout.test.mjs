/*
===========================================================================

chat-item-links-layout.test.mjs - the chat log and input draw item links

Port-only, not native (SRO_CHAT_ITEM_LINKS). A linked line shows the
item's label where its marker sits; with the Experimental row on the label
is a hover region whose tooltip is the linked item, underlined while
hovered and linked across a wrap. The chat input draws the label too and
keeps its focus on Alt presses.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";

const { chatLayout } = await import( "../../src/engine/foundation/ui/chat-layout.ts" );
const { tooltipItems } = await import( "../../src/engine/foundation/ui/tooltip-target.ts" );
const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
const { CHAT_LINK_MARKER } = await import( "../../src/engine/foundation/gameplay/chat-item-links.ts" );

const M = CHAT_LINK_MARKER;
// A presented bag row: only the fields the chat log reads.
const BLADE =
	/** @type {any} */ ({ slot: 6, refObjId: 11459, typeFlags: 0, quantity: 1, plus: 7, name: "Blade", magic: [] });
const layout = decodeAuthoredLayout(
	JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifchatviewer.json", "utf8" ) )
);
const LINE = { channel: 4, name: "Berk", text: "wts " + M + " ok", outgoing: false, sequence: 5, links: [ BLADE ] };

/*
================
layoutWith

A chat layout over LINE whose text drawing records every string it is asked
to draw.
================
*/
function layoutWith( overrides ) {
	const drawn = [];
	const out = chatLayout( {
		layout,
		width: 1024,
		height: 768,
		rows: 2,
		tab: 0,
		input: "",
		lines: [ LINE ],
		welcome: "",
		copy: key => key,
		size: () => [ 16, 16 ],
		text: value => {
			drawn.push( value );
			return [];
		},
		hover: null,
		pressed: null,
		measure: value => value.length * 7,
		chatItemLinks: true,
		...overrides
	} );
	return { ...out, drawn };
}

test("a linked line shows the label in place of the marker", () => {
	const { controls, drawn } = layoutWith( {} );
	assert.ok( controls.some( c => c.id.startsWith( "chat-line:" ) && c.label.includes( "wts <Blade +7> ok" ) ) );
	assert.ok( !drawn.some( value => value.includes( M ) ) );
});

test("the label is a hover region with the bag tooltip", () => {
	const on = layoutWith( {} );
	const row = on.controls.find( c => c.id.startsWith( "chat-line:" ) && c.label.includes( "<Blade +7>" ) );
	const link = on.controls.find( c => c.id === "chat-item:5:0" );
	assert.ok( row && link );
	assert.equal( link.kind, "region" );
	assert.deepEqual( link.rect, [ row.rect[0] + row.label.indexOf( "<Blade +7>" ) * 7, row.rect[1], 10 * 7, 14 ] );
	const game = /** @type {any} */ ({ inventory: [], chat: { lines: [ LINE ] } });
	assert.deepEqual( tooltipItems( "chat-item:5:0", game, 0 ), [ BLADE ] );
	assert.deepEqual( tooltipItems( "chat-item:5:0:1", game, 0 ), [ BLADE ] );
	assert.deepEqual( tooltipItems( "chat-item:5:1", game, 0 ), [] );
	const plain = on.quads.filter( q => q.texture === "" ).length;
	const hovered = layoutWith( { hover: "chat-item:5:0" } );
	assert.equal( hovered.quads.filter( q => q.texture === "" ).length, plain + 1 );
});

test("with the row off the label is plain", () => {
	const off = layoutWith( { chatItemLinks: false } );
	assert.ok( !off.controls.some( c => c.id.startsWith( "chat-item:" ) ) );
	assert.ok( off.controls.some( c => c.id.startsWith( "chat-line:" ) && c.label.includes( "<Blade +7>" ) ) );
});

test("a label wrapped across two rows links both parts", () => {
	const long = { ...LINE, text: "x".repeat( 18 ) + " " + M, links: [ { ...BLADE, name: "Heaven's Cloud Spear" } ] };
	const on = layoutWith( { lines: [ long ] } );
	const parts = on.controls.filter( c => c.id.startsWith( "chat-item:5:0" ) ).map( c => c.id );
	assert.ok( parts.includes( "chat-item:5:0" ) && parts.includes( "chat-item:5:0:1" ), JSON.stringify( parts ) );
});

test("a missing item shows <?> and no region", () => {
	const on = layoutWith( { lines: [ { ...LINE, links: [ undefined ] } ] } );
	assert.ok( on.controls.some( c => c.id.startsWith( "chat-line:" ) && c.label.includes( "wts <?> ok" ) ) );
	assert.ok( !on.controls.some( c => c.id.startsWith( "chat-item:" ) ) );
});

test("the input draws the label and keeps its focus on Alt presses", () => {
	const { controls, drawn } = layoutWith( { input: "a" + M, inputLabels: [ "<A>" ] } );
	const input = defined( controls.find( c => c.id === "chat-text" ) );
	assert.equal( input.value, "a" + M );
	assert.equal( input.keepFocusOnAltPress, true );
	assert.ok( drawn.includes( "a<A>" ) );
});

test("the overhead bubble shows the label, never the marker", async () => {
	const { createSpeech } = await import( "../../src/engine/runtime/ui/hud/speech.ts" );
	const speech = createSpeech(), players = [ { gid: 2, name: "Berk" } ];
	speech.step( [], players, 0 );
	const said = { ...LINE, channel: 1, gid: 2, sequence: 6 };
	assert.equal( speech.step( [ said ], players, 1 ).get( 2 )?.text, "wts <Blade +7> ok" );
});
