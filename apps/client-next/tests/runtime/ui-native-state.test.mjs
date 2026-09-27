/*
===========================================================================

ui-native-state.test.mjs - tests for button-state.ts, title-status.ts,
ui-glyphs.ts, title.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { isPlaceholderText, loadEnglishCompletions } from "../../../../scripts/build/shared/englishCompletions.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import fc from "fast-check";
import path from "node:path";
import { textDataDir } from "../../../../scripts/build/shared/resourceIo.mjs";
import { readLocalizedTextDataRowsSync } from "../../../../scripts/build/shared/textDataIo.mjs";
import { defined } from "../helpers/defined.mjs";
const { buttonAccess, buttonTextColor } = await import( "../../src/engine/foundation/ui/button-state.ts" );
const { titleStatusKey, titleStatusMessage } = await import( "../../src/engine/foundation/ui/title-status.ts" );
const { titleTextBox } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );
const { createTitleUi } = await import( "../../src/engine/runtime/ui/title/title.ts" );
const assets = "../../.generated/client-public/assets",
	read = async path => JSON.parse( await readFile( assets + path, "utf8" ) );
const atlas = await read( "/fonts/native-ui-font-atlas.json" ),
	catalog = (await read( "/text/textuisystem.en.json" )).entries;

test("all published authored control colors survive input inhibition; true disabling uses native state 3", async () => {
	let controls = 0;
	for ( const file of await readdir( assets + "/cif/layouts" ) ) {
		if ( !file.endsWith( ".json" ) ) continue;
		const layout = await read( "/cif/layouts/" + file );
		for ( const node of layout.sections?.flatMap( section => section.nodes ) ?? [] ) {
			if ( !node.fontColor ) continue;
			const c = node.fontColor, color = [ c.r / 255, c.g / 255, c.b / 255, c.a / 255 ];
			for ( const state of [ "enabled", "inhibited", "enabled", "disabled", "inhibited", "enabled" ] ) {
				assert.deepEqual(
					buttonTextColor( color, state ),
					state === "disabled" ? [ 230 / 255, 230 / 255, 230 / 255, 1 ] : color,
					`${file}/${node.name}/${state}`
				);
			}
			controls++;
		}
	}
	assert.ok( controls > 100, "must enumerate the published control universe" );
	fc.assert(
		fc.property( fc.array( fc.tuple( fc.boolean(), fc.boolean() ), { minLength: 1, maxLength: 50 } ), sequence => {
			const color = [ .2, .7, .4, .8 ];
			for ( const [blocked, unavailable] of sequence ) {
				const state = buttonAccess( blocked, unavailable );
				assert.equal( state, unavailable ? "disabled" : blocked ? "inhibited" : "enabled" );
				assert.deepEqual(
					buttonTextColor( color, state ),
					unavailable ? [ 230 / 255, 230 / 255, 230 / 255, 1 ] : color
				);
			}
		} )
	);
});

test("every native font slot controls wrapped and blank-line pitch", () => {
	for ( const [slot, font] of Object.entries( atlas.fonts ) ) {
		const quads = titleTextBox( atlas, "A\n\nA", [ 0, 0, 200, 200 ], [ 0, 0, 200, 200 ], [ 1, 1, 1, 1 ], {
			fontIndex: Number( slot )
		} );
		assert.equal( quads[1].rect[1] - quads[0].rect[1], 2 * (font.recordHeight + 5), slot );
	}
});

test("native title statuses preserve packed argument order and every simple message branch", () => {
	const resolve = key => catalog[key];
	fc.assert(
		fc.property( fc.integer( { min: 0, max: 65535 } ), fc.integer( { min: 0, max: 65535 } ), ( failed, total ) => {
			const argument = total * 65536 + failed;
			assert.equal(
				titleStatusMessage( 2, argument, resolve ),
				`Password entry has failed ${failed} out of ${total} times.`
			);
		} )
	);
	for ( const status of [ 5, 7, 8, 9, 10 ] ) {
		assert.equal( titleStatusMessage( status, undefined, resolve ), `Failed to connect to server.(C${status})` );
	}
	for ( const status of [ 4, 6, 11, 12, 13, 14, 15, 16 ] ) {
		assert.equal( titleStatusMessage( status, 0, resolve ), catalog[titleStatusKey( status, 0 )] );
	}
	// The branch exists even when the shipped English cell is empty; the
	// English completion layer supplies the message, so the branch must resolve
	// it rather than treat the empty retail cell as an unsupported subtype.
	const stopped = readLocalizedTextDataRowsSync( path.join( textDataDir, "textuisystem.txt" ) ).find( row =>
		row[1] === "UIO_MSG_ERROR_ACCOUNT_STOP"
	);
	assert.equal( stopped?.[8], "" );
	assert.equal(
		titleStatusMessage( 3, 1, resolve ),
		loadEnglishCompletions( "textuisystem.txt" ).UIO_MSG_ERROR_ACCOUNT_STOP?.english
	);
	for ( const subtype of [ 1, 2, 3, 4 ] ) {
		const key = titleStatusKey( 3, subtype );
		assert.ok( key );
		assert.equal( titleStatusMessage( 3, subtype, k => `resolved:${k}` ), `resolved:${key}` );
	}
	for ( const subtype of [ 2, 3, 4 ] ) assert.ok( titleStatusMessage( 3, subtype, resolve ) );
	for ( const subtype of [ 0, 5, 65536 ] ) assert.equal( titleStatusMessage( 3, subtype, resolve ), undefined );
	for ( const status of [ undefined, 0, 1, 17, 255 ] ) {
		assert.equal( titleStatusMessage( status, 0, resolve ), undefined );
	}
	assert.equal(
		titleStatusMessage( 2, undefined, resolve ),
		"Invalid ID or password.",
		"legacy server without counters must not fabricate them"
	);
});

test("title and dock projections preserve every glyph across request and camera inhibition", async () => {
	const buffers = new Map();
	for (
		const path of [
			"/cif/layouts/pstitle.json",
			"/fonts/native-ui-font-atlas.json",
			"/text/textuisystem.en.json",
			"/cif/layouts/pscharacterselect_europe.json",
			"/cif/layouts/pscharactercreate_europe.json",
			"/cif/layouts/pscharactercreatechina.json"
		]
	) buffers.set( "/assets" + path, new TextEncoder().encode( JSON.stringify( await read( path ) ) ).buffer );
	let serial = 0;
	const jobs = new Map(),
		owner = createTitleUi( {
			available: () => 8,
			request: url => {
				jobs.set( ++serial, buffers.get( decodeURIComponent( new URL( url ).pathname ) ) );
				return serial;
			},
			take: id => {
				const buffer = jobs.get( id );
				jobs.delete( id );
				return buffer ? { kind: "bytes", buffer } : null;
			},
			cancel: id => jobs.delete( id )
		}, "http://fixture.invalid" );
	const base = { phase: "login", generation: 1, elapsed: 10, alpha: 1, logoAlpha: 0, error: null },
		input = { hover: null, pressed: null, focus: null, now: 1000, draft: "s", offset: 0, message: "" };
	const render = ( phase, pending ) =>
		owner.render(
			{ ...base, phase, cameraMoving: pending },
			1024,
			768,
			"",
			"",
			[],
			"",
			pending,
			false,
			input,
			[],
			""
		);
	render( "login", false );
	for ( const phase of [ "login", "dock" ] ) {
		const normal = render( phase, false );
		assert.equal( normal.ready, true );
		const glyphs = output => output.quads.filter( q => q.texture === atlas.image );
		for ( const pending of [ true, false, true, false ] ) {
			assert.deepEqual( glyphs( render( phase, pending ) ), glyphs( normal ), phase );
		}
	}
	for ( const alpha of [ 0, .5, 1 ] ) {
		const state = { ...base, phase: "dock-arrival", alpha };
		const output = owner.render(
			state,
			1024,
			768,
			"",
			"",
			[],
			"",
			false,
			false,
			{ ...input, message: "UIO_MSG_ERROR_CITATION" },
			[],
			""
		);
		const overlay = output.quads.findLastIndex( q => q.texture === "" && q.rect.join( "," ) === "0,0,1024,768" );
		if ( alpha < 1 ) {
			assert.ok( overlay >= 0 );
			assert.equal( output.quads[overlay].color[3], (1 - alpha) * 128 / 255 );
			assert.ok(
				!output.quads.slice( overlay + 1 ).some( q => q.texture === atlas.image ),
				"retired title notice cannot paint over dock arrival"
			);
		}
	}
	for ( const [width, height] of [ [ 1024, 768 ], [ 1835, 945 ], [ 2560, 1080 ], [ 800, 1200 ] ] ) {
		for ( const phase of [ "login", "dock", "customize" ] ) {
			const output = owner.render(
				{ ...base, phase, race: 0 },
				width,
				height,
				"",
				"",
				[],
				"",
				false,
				false,
				input,
				[],
				""
			);
			const bars = output.quads.filter( q => /bar_(up|down)/.test( q.texture ) );
			assert.equal( bars.length, 2 );
			assert.ok( bars.every( q => q.rect[0] === 0 && q.rect[2] === width ), "bars cover the entire viewport" );
			assert.ok(
				bars.every( q => !q.texture.includes( "_18" ) ),
				"native Europe branch has no Korean rating badges"
			);
		}
	}
	owner.dispose();
});

const { characterStatus } = await import( "../../src/engine/foundation/ui/character-status.ts" );
const { catalogMessage } = await import( "../../src/engine/foundation/ui/catalog-message.ts" );
test("character status table preserves direct exceptions, suffixes, no-message success and missing text", () => {
	const expected = {
		2: "UIO_MSG_ERROR_SEVER_CONNECT",
		3: "UIO_SMERR_INVALID_CHARGEN_INFO",
		4: "UIO_MSG_ERROR_CHARACTER_SELECTWEAPON",
		5: "UIO_MSG_ERROR_CHARACTER_OVER_3",
		6: "UIO_SMERR_FAILED_TO_CREATE_CHARACTER",
		7: "UIO_MSG_ERROR_SEVER_CONNECT",
		8: "UIO_MSG_ERROR_SEVER_CONNECT",
		9: "UIO_SMERR_CANT_FIND_GAMESERVER",
		10: "UIO_MSG_ERROR_SEVER_CONNECT",
		11: "UIO_MSG_ERROR_SEVER_CONNECT",
		12: "UIO_MSG_ERROR_CHARACTER_NAME_STRING",
		13: "UIO_SMERR_NOT_ALLOWED_CHARNAME",
		14: "UIO_MSG_ERROR_SEVER_CONNECT",
		15: "UIO_SMERR_CANT_ACCESS_PARENT_SERVER",
		16: "UIO_MSG_ERROR_ID",
		17: "UIO_MSG_ERROR_OVERLAP",
		18: "UIO_SMERR_FAILED_TO_CREATE_NEW_USER",
		19: "UIO_MSG_ERROR_SEVER_CONNECT",
		20: "UIO_SMERR_MAX_USER_EXCEEDED",
		21: "UIO_SMERR_FAILED_TO_ENTERLOBBY",
		22: "UIO_MSG_ERROR_SEVER_CONNECT",
		23: "UIO_MSG_ERROR_SEVER_CONNECT",
		24: "UIO_MSG_ERROR_SEVER_CONNECT",
		25: "UIO_MSG_ERROR_CANT_BE_REVIVED"
	};
	assert.equal( characterStatus( 1 ), undefined );
	for ( const [code, key] of Object.entries( expected ) ) {
		const status = characterStatus( Number( code ) ),
			suffix = [ 4, 5, 12, 13, 16, 17, 20 ].includes( Number( code ) ) ? "" : `(S${code})`;
		assert.deepEqual( status, { key, suffix } );
		assert.equal( catalogMessage( status, catalog[status.key] ), (catalog[key] ?? "") + suffix );
	}
	assert.equal( catalogMessage( characterStatus( 25 ), undefined ), "(S25)" );
	assert.equal(
		catalogMessage(
			{ key: "UIO_MSG_ERROR_CHARACTER_OVER_3", suffix: "", args: [ 4 ] },
			catalog.UIO_MSG_ERROR_CHARACTER_OVER_3
		),
		"Maximum of 4 characters can be created."
	);
	assert.equal(
		catalogMessage( characterStatus( 5 ), catalog.UIO_MSG_ERROR_CHARACTER_OVER_3 ),
		"Maximum of %d characters can be created.",
		"only the full-roster caller supplies the limit argument"
	);
});

test("authored ARGB components are byte-valued before renderer admission", async () => {
	const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
	const source = await read( "/cif/layouts/ifparty.json" ), layout = decodeAuthoredLayout( source );
	// Shipped ifparty contains 2255 in the alpha field, not 255.
	assert.equal( layout.GDR_PTY_STATIC_LEVEL_DATA.color[3], (2255 & 255) / 255 );
	for ( const node of Object.values( layout ) ) assert.ok( node.color.every( c => c >= 0 && c <= 1 ) );
});

test("font line_margin is scoped but ignored by the native font tag branch", async () => {
	const { guideTokens } = await import( "../../src/engine/foundation/ui/guide-content.ts" );
	assert.deepEqual( guideTokens( '<font line_margin="9">Camera</font>' ), guideTokens( "Camera" ) );
	assert.deepEqual(
		guideTokens( '<font color="255,1,2,3"><font line_margin="9">Camera</font></font>' ),
		guideTokens( '<font color="255,1,2,3">Camera</font>' )
	);
	assert.throws( () => guideTokens( '<font line_margin="9">Camera' ) );
	assert.throws( () => guideTokens( '<font line_margin="bad">Camera</font>' ) );
});

test("stat buttons use empty retail packets and only acknowledgements spend points", async () => {
	const { createGameplay } = await import(
		"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
	);
	const sent = [],
		game = createGameplay( f => sent.push( f ) ),
		local = { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0, kind: "local-player" };
	game.bootstrap( { character: { level: 1, statPoints: 2 } } );
	game.seed( local );
	const str = game.command( { kind: "stat-increase", stat: "str" }, 0, undefined, local );
	assert.equal( defined( str ).opcode, 0x727a );
	assert.equal( defined( str ).payload.length, 0 );
	assert.equal( defined( defined( game.take() ).progression ).statPoints, 2 );
	game.receive( { opcode: 0xb27a, payload: Uint8Array.of( 1 ) }, 1 );
	assert.equal( defined( defined( game.take() ).progression ).statPoints, 1 );
	const int = game.command( { kind: "stat-increase", stat: "int" }, 2, undefined, local );
	assert.equal( defined( int ).opcode, 0x7552 );
	assert.equal( defined( int ).payload.length, 0 );
	game.receive( { opcode: 0xb552, payload: Uint8Array.of( 2, 1 ) }, 3 );
	assert.equal( defined( defined( game.take() ).progression ).statPoints, 1 );
	game.receive( { opcode: 0xb552, payload: Uint8Array.of( 1 ) }, 4 );
	assert.equal( defined( defined( game.take() ).progression ).statPoints, 0 );
	assert.equal( game.command( { kind: "stat-increase", stat: "str" }, 5, undefined, local ), null );
	assert.throws( () => game.receive( { opcode: 0xb27a, payload: Uint8Array.of( 1, 1 ) }, 6 ) );
	game.dispose();
});
