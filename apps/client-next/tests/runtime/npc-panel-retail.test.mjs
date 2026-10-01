/*
===========================================================================

npc-panel-retail.test.mjs - tests for npc-talk.ts, npc-panel.ts, npc.ts,
authored-layout.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { npcChoiceColor, npcTalkLayout, npcBranchLabel } = await import( "../../src/engine/foundation/ui/npc-talk.ts" );
const { createNpcPanel } = await import( "../../src/engine/runtime/ui/hud/npc-panel.ts" );
const { createNpcConversation } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/npc/npc.ts"
);
const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
const { textBoxParagraphs } = await import( "../../src/engine/foundation/ui/text-lines.ts" );
const layout = decodeAuthoredLayout(
	JSON.parse( readFileSync( "../../.generated/client-public/assets/cif/layouts/if_npctalk.json", "utf8" ) )
);
const atlas = JSON.parse(
	readFileSync( "../../.generated/client-public/assets/fonts/native-ui-font-atlas.json", "utf8" )
);
const measure = text =>
	Array.from( text, c => (atlas.fonts[0].glyphs[c.codePointAt( 0 )] ?? atlas.fonts[0].glyphs[63]).advanceX ).reduce(
		( a, b ) => a + b,
		0
	);

test("CIFTextBox newline grammar distinguishes escaped breaks from source continuations", () => {
	assert.deepEqual( textBoxParagraphs( "first\\nsecond\nthird\\\nfourth" ), [ "first", "second", "thirdfourth" ] );
	assert.deepEqual( textBoxParagraphs( "\\n\n" ), [ "", "", "" ] );
	assert.deepEqual( textBoxParagraphs( "C:\\quest\\end\\" ), [ "C:\\quest\\end\\" ] );
});

test("NPC colors use native signed level boundaries, missing-record color and SN prefix", () => {
	const lookup = code => code === "QNO_EXAMPLE" ? 10 : undefined;
	for (
		const [level, color] of [ [ 9, [ 1, 74 / 255, 74 / 255, 1 ] ], [ 10, [ 239 / 255, 218 / 255, 164 / 255, 1 ] ], [
			19,
			[ 239 / 255, 218 / 255, 164 / 255, 1 ]
		], [ 20, [ 101 / 255, 175 / 255, 162 / 255, 1 ] ] ]
	) assert.deepEqual( npcChoiceColor( "SN_QNO_EXAMPLE", level, lookup ), color );
	assert.deepEqual( npcChoiceColor( "QNO_EXAMPLE", 0, lookup ), npcChoiceColor( "SN_MISSING", 0, lookup ) );
});

test("every accepted dialogue resets scrolling even when identical replies coalesce between HUD frames", () => {
	const npc = createNpcConversation( () => {} ), panel = createNpcPanel();
	npc.select( 7 );
	npc.talk( 0 );
	const prompt = Buffer.from( "SN_PROMPT" ), payload = Buffer.concat( [ Buffer.of( 3, prompt.length, 0 ), prompt ] );
	npc.receive( { opcode: 0x3773, payload } );
	panel.observe( npc.state() );
	panel.geometry( { range: 30, travel: 272, bounds: [ 23, 23, 302, 343 ] } );
	panel.event( { kind: "scroll", x: 30, y: 30, delta: 1 } );
	assert.equal( panel.top(), 3 );
	npc.choose( 2, 1 );
	npc.receive( { opcode: 0x3773, payload } );
	assert.equal( panel.observe( npc.state() ), true );
	assert.equal( panel.top(), 0 );
	panel.geometry( { range: 30, travel: 272, bounds: [ 23, 23, 302, 343 ] } );
	panel.event( { kind: "activate", id: "npc-scroll-down" } );
	npc.receive( { opcode: 0x3773, payload } );
	assert.equal( panel.observe( npc.state() ), false, "unsolicited duplicate is not an accepted reply" );
	assert.equal( panel.top(), 1 );
	npc.choose( 2, 2 );
	panel.observe( npc.state() );
	assert.equal( panel.top(), 1, "waiting preserves the current page" );
	npc.step( 10002 );
	panel.observe( npc.state() );
	assert.equal( panel.top(), 1 );
	npc.receive( { opcode: 0x3773, payload } );
	panel.observe( npc.state() );
	assert.equal( panel.top(), 0, "late accepted reply also resets" );
});

test("NPC long choice keeps one action across bitmap-wrapped lines and authored scroll endpoints", () => {
	const rows = [],
		state = {
			phase: "ready",
			gid: 7,
			dialogueRevision: 1,
			dialogue: { kind: 4, prompt: "prompt", options: [ { choice: 5, symbol: "SN_QNO_EXAMPLE" } ] }
		};
	const draw = ( text, rect, clip, color ) => {
		rows.push( { text, rect, clip, color } );
		return [];
	};
	const choice = "A deliberately long quest choice ".repeat( 45 ), color = [ 1, 74 / 255, 74 / 255, 1 ];
	const render = ( top, hover = null, pressed = null ) =>
		npcTalkLayout( {
			state,
			layout,
			origin: [ 100, 200 ],
			copy: key => key === "prompt" ? "Prompt" : choice,
			measure,
			draw,
			size: () => [ 16, 16 ],
			hover,
			pressed,
			top,
			choiceColor: () => color
		} );
	const first = render( 0 );
	assert.deepEqual( first.bounds, [ 123, 223, 302, 343 ] );
	assert.equal( first.travel, 320 );
	assert.ok( first.range > 0 );
	assert.equal( first.controls.filter( c => c.id === "npc-choice:5" ).length, 1 );
	assert.deepEqual( defined( first.controls.find( c => c.id === "npc-scroll-up" ) ).rect, [ 436, 211, 16, 16 ] );
	assert.deepEqual( defined( first.controls.find( c => c.id === "npc-scroll-down" ) ).rect, [ 436, 563, 16, 16 ] );
	for ( const row of rows ) assert.ok( measure( row.text ) <= 302, "all visible lines fit the authored width" );
	assert.deepEqual( rows.find( row => row.text.startsWith( "1. " ) ).color, color );
	assert.deepEqual( defined( first.controls.find( c => c.id === "npc-scroll-thumb" ) ).rect, [ 436, 227, 16, 16 ] );
	rows.length = 0;
	const last = render( first.range, null, "npc-choice:5" );
	assert.deepEqual( defined( last.controls.find( c => c.id === "npc-scroll-thumb" ) ).rect, [ 436, 547, 16, 16 ] );
	assert.ok(
		rows.some( row => row.color[0] === 1 && row.color[1] === 138 / 255 && row.color[2] === 0 ),
		"pressed wrapped choice uses native focus color"
	);
	assert.equal( last.controls.filter( c => c.id === "npc-choice:5" ).length, 1 );
});

test("npcBranchLabel disambiguates duplicate shop group labels for multi-cultural merchants", () => {
	const branches = [
		{ id: 868, labelSymbol: "SN_STORE_SMITH_GROUP1", tabs: [ 0, 1, 2 ] },
		{ id: 869, labelSymbol: "SN_STORE_SMITH_EU_GROUP1", tabs: [ 3, 4, 5 ] }
	];
	const unpatchedCopy = () => "Purchase/sell/repair weapon";
	assert.equal( npcBranchLabel( branches[0], branches, unpatchedCopy ), "Purchase/sell/repair weapon (Chinese)" );
	assert.equal( npcBranchLabel( branches[1], branches, unpatchedCopy ), "Purchase/sell/repair weapon (European)" );

	const patchedCopy = sym =>
		sym === "SN_STORE_SMITH_GROUP1" ?
			"Purchase/sell/repair Chinese weapon" :
			"Purchase/sell/repair European weapon";
	assert.equal( npcBranchLabel( branches[0], branches, patchedCopy ), "Purchase/sell/repair Chinese weapon" );
	assert.equal( npcBranchLabel( branches[1], branches, patchedCopy ), "Purchase/sell/repair European weapon" );
});
