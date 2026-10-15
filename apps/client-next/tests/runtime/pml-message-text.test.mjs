/*
===========================================================================

pml-message-text.test.mjs - SML2 message and label text renders as markup

A78F: the job league's confirm box drew its native SML2 text as raw
characters ("<sml2><center>Join job league?<br>..."), running past both
sides of the box. CIFSimpleMessageBox_SetTextAndResize (52DB20) renders any
text Text_HasSml2Markup (843100) admits as markup: centred lines and breaks.
Every affected string, with the shipped text and font, must draw no "<"
and stay inside a 300-pixel box (52E720's message width).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
const { guideTokens, guideContent, hasSml2Markup, groupedNumber } = await import(
	"../../src/engine/foundation/ui/guide-content.ts"
);
const { tooltipDescription } = await import( "../../src/engine/foundation/ui/tooltip-description.ts" );
const { decodeUiFont } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );

// 52E720's message control width.
const BOX_WIDTH = 300;

test("SML2 is recognised as 843100 does", () => {
	assert.equal( hasSml2Markup( "<sml2>Join?</sml2>" ), true );
	assert.equal( hasSml2Markup( "<sml2></sml2>" ), false, "13 characters is the bare wrapper" );
	assert.equal( hasSml2Markup( "Join job league?" ), false );
	assert.equal( hasSml2Markup( "<sml2>no closing tag here" ), false );
	assert.equal( groupedNumber( 40 * 50000 ), "2,000,000" );
	assert.equal( groupedNumber( 999 ), "999" );
});

test("center, left and right scope whole lines and must balance", () => {
	const glyph = { x: 0, y: 0, width: 4, height: 10, originX: 0, originY: 10, advanceX: 4 };
	const font = { recordHeight: 10, ascent: 10, descent: 0, glyphs: { 63: glyph } };
	const atlas = { image: "/assets/fonts/test.png", atlasWidth: 16, atlasHeight: 16, fonts: { 0: font, "0:2": font } };
	const tokens = guideTokens( "<sml2><center>AB<br>A</center>L<right>R</right></sml2>" );
	assert.deepEqual( tokens.filter( t => t.kind === "align" ).map( t => t.align ), [
		"center",
		"left",
		"right",
		"left"
	] );
	const result = guideContent(
		atlas,
		tokens,
		[ 0, 0, 100, 100 ],
		[ 0, 0, 100, 100 ],
		[ 1, 1, 1, 1 ],
		() => undefined
	);
	// AB is 8 wide: centred at 46. A is 4 wide: centred at 48. L stays at 0.
	// R is right-aligned at 96.
	assert.deepEqual( result.quads.map( q => q.rect[0] ), [ 46, 48, 0, 96 ] );
	assert.throws( () => guideTokens( "<center>A</left>" ) );
	assert.throws( () => guideTokens( "<center>A" ) );
	// Tooltip rows ignore alignment instead of reading it as text.
	assert.deepEqual( tooltipDescription( "<sml2><center>A</center></sml2>" ).map( row => row.value ), [ "A" ] );
});

const atlas = decodeUiFont(
	JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/fonts/native-ui-font-atlas.json", "utf-8" ) )
);
const strings = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/text/textuisystem.en.json", "utf-8" ) );
const table = strings.entries ?? strings;

/*
================
filled

A shipped string with its %s arguments filled the way the box fills them.
================
*/
function filled( key, args = [] ) {
	const value = table[key];
	assert.equal( typeof value, "string", key );
	return args.reduce( ( text, arg ) => text.replace( "%s", arg ), value );
}

test("every affected shipped string draws no '<' and stays inside its box", () => {
	const lt = atlas.fonts["0"].glyphs["60"];
	const ltUv = lt && [ lt.x / atlas.atlasWidth, lt.y / atlas.atlasHeight ].join( "," );
	const cases = [
		filled( "UIIT_STT_JOBGUILD_JOIN_WINDOW" ),
		filled( "UIIT_STT_JOBGUILD_WITHD_WINDOW" ),
		filled( "UIIT_STT_ALIAS_CREATE_WINDOW", [ "Player", "Hawk" ] ),
		filled( "UIIT_STT_ALIAS_MODIFY_WINDOW", [ "Hawk", "Falcon", groupedNumber( 40 * 50000 ) ] ),
		filled( "UIIT_STT_SILKMALL_MAIN_INTRO" ),
		filled( "UIIT_STT_SILKMALL_MAIN_ZZIM_SUB_TITLE_ZZIM" ),
		filled( "UIIT_STT_SILKMALL_MAIN_PREMIUM" ),
		filled( "UIIT_STT_ALCHEMYBOX_REINFORCE_TEXT" ),
		filled( "UIIT_STT_ALCHEMYBOX_MATERIAL_PROCESSING_TEXT" ),
		filled( "UIIT_STT_ALCHEMYBOX_REINFORCE_ATTR_TEXT" )
	];
	for ( const value of cases ) {
		assert.ok( hasSml2Markup( value ), value );
		assert.ok( !value.includes( "%s" ), value );
		const out = guideContent( atlas, guideTokens( value ), [ 0, 0, BOX_WIDTH, 400 ], [ 0, 0, BOX_WIDTH, 400 ], [
			1,
			1,
			1,
			1
		], () => undefined );
		assert.ok( out.quads.length > 0, value );
		for ( const q of out.quads ) {
			assert.ok( q.rect[0] >= 0 && q.rect[0] + q.rect[2] <= BOX_WIDTH + 0.5, `${value}: glyph at ${q.rect[0]}` );
			if ( ltUv ) assert.notEqual( q.uv.slice( 0, 2 ).join( "," ), ltUv, `${value} drew '<'` );
		}
	}
});

test("the join confirm centres its three lines", () => {
	const value = filled( "UIIT_STT_JOBGUILD_JOIN_WINDOW" );
	const out = guideContent( atlas, guideTokens( value ), [ 0, 0, BOX_WIDTH, 400 ], [ 0, 0, BOX_WIDTH, 400 ], [
		1,
		1,
		1,
		1
	], () => undefined );
	const rows = new Map();
	for ( const q of out.quads ) {
		const row = rows.get( q.rect[1] ) ?? { left: Infinity, right: -Infinity };
		row.left = Math.min( row.left, q.rect[0] );
		row.right = Math.max( row.right, q.rect[0] + q.rect[2] );
		rows.set( q.rect[1], row );
	}
	assert.ok( rows.size >= 3, "Join job league? / the reset warning / the gold line" );
	const first = [ ...rows.values() ][0];
	assert.ok( Math.abs( first.left - (BOX_WIDTH - first.right) ) <= 2, "the first line is centred" );
});

test("the job league's confirm box draws its words, not its tags", async () => {
	const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );
	const f = uiFixture();
	try {
		f.ui.step( f.state, 1000 );
		// A job NPC's menu (5DA1B0 case 0x1E opens the join question).
		f.state.gameplay = { ...f.state.gameplay, npcConversation: { gid: 77, phase: "menu" } };
		f.ui.step( f.state, 1050 );
		f.ui.event( { kind: "activate", id: "npc-job-join:3" } );
		for ( let i = 0; i < 30; i++ ) f.ui.step( f.state, 1100 + i * 50 );
		assert.ok( f.hasText( "Join job league?" ), "the question's words are drawn" );
		assert.ok( !f.hasText( "<sml2>" ) && !f.hasText( "<center>" ), "no raw tag reaches the screen" );
	} finally {
		f.dispose();
	}
});
