/*
===========================================================================

ui-world-chrome.test.mjs - tests for frame-ring.ts, ui-glyphs.ts, text.ts,
normal-tile.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import fc from "fast-check";
import { defined } from "../helpers/defined.mjs";
const { frameRing, frameParts } = await import( "../../src/engine/foundation/ui/frame-ring.ts" );
const { decodeUiFont, titleText, resolveTextOverlaps } = await import(
	"../../src/engine/foundation/rendering/ui-glyphs.ts"
);
const { expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );
const { createUiText } = await import( "../../src/engine/runtime/ui/text/text.ts" );
const { normalTile } = await import( "../../src/engine/foundation/ui/normal-tile.ts" );
const { partyProposalLayout } = await import( "../../src/engine/foundation/ui/party-proposal.ts" );
const { decodeAuthoredLayout, authoredRect } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
const { createHudResources } = await import( "../../src/engine/runtime/ui/hud/resources.ts" );
const { hotbarSlot } = await import( "../../src/engine/foundation/gameplay/quickslots.ts" );
const atlas = JSON.parse(
	readFileSync( CLIENT_PUBLIC_ROOT + "/assets/fonts/native-ui-font-atlas.json", "utf8" )
);
const layout = JSON.parse(
	readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifsystemwnd.json", "utf8" )
);

test("HUD admission retains native atlas UVs and natural-size controls, rejects malformed input", () => {
	const raw = JSON.parse(
			readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ginterface.json", "utf8" )
		),
		decoded = decodeAuthoredLayout( raw );
	assert.deepEqual( decoded.GDR_PLAYER_MINI_INFO.uv, [ .723633, 0, .930664 - .723633, .136719 ] );
	raw.controlsByName.GDR_PLAYER_MINI_INFO.rect.width = 999;
	assert.equal( decoded.GDR_PLAYER_MINI_INFO.rect[2], 212, "admitted layout owns its snapshot" );
	const bar = decodeAuthoredLayout(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifunderbar.json", "utf8" ) )
	);
	assert.deepEqual( authoredRect( bar.GDR_DECORATE_3, 112, 716 ), [ 789, 676, ...bar.GDR_DECORATE_3.size ] );
	raw.controlsByName.GDR_PLAYER_MINI_INFO.properties.UV_LT.value.x = "bad";
	assert.throws( () => decodeAuthoredLayout( raw ), /number/ );
});

test("native hotbar keeps common slot zero and covers exactly four numbered pages", () => {
	const seen = new Set();
	for ( let page = 0; page < 4; page++ ) {
		assert.equal( hotbarSlot( page, 0 ), 0 );
		for ( let key = 1; key <= 10; key++ ) seen.add( hotbarSlot( page, key ) );
	}
	assert.deepEqual( [ ...seen ], Array.from( { length: 40 }, ( _, i ) => i + 1 ) );
	assert.throws( () => hotbarSlot( 4, 1 ), /index/ );
	assert.throws( () => hotbarSlot( 0, 11 ), /index/ );
});

test("HUD metadata disposal cancels each owned request and cannot publish late results", () => {
	for ( const capacity of [ 4, 16 ] ) {
		const cancelled = [], pending = new Set();
		let serial = 0, takes = 0;
		const hud = createHudResources( {
			available: () => capacity - pending.size,
			request: () => {
				assert.ok( pending.size < capacity );
				pending.add( ++serial );
				return serial;
			},
			take: () => {
				takes++;
				return null;
			},
			cancel: id => {
				assert.ok( pending.delete( id ) );
				cancelled.push( id );
			}
		}, "http://fixture.invalid" );
		hud.step();
		assert.ok( serial > 0 && serial <= capacity );
		const issued = [ ...pending ];
		assert.equal( hud.data(), null );
		hud.dispose();
		hud.step();
		assert.deepEqual( cancelled, issued );
		assert.equal( pending.size, 0 );
		assert.equal( takes, 0 );
		assert.equal( hud.data(), null );
	}
});

test("new published popup resources prepare automatically without a runtime filename list", () => {
	const jobs = new Map();
	let serial = 0;
	const future = "/assets/images/Media_extracted/interface/new-published-control.png";
	const hud = createHudResources( {
		available: () => 4 - jobs.size,
		/*
		================
		request
		================
		*/
		request( url ) {
			const path = decodeURIComponent( new URL( url ).pathname );
			if ( !path.endsWith( ".json" ) ) {
				const bytes = readFileSync( CLIENT_PUBLIC_ROOT + path );
				jobs.set( ++serial, {
					kind: "bytes",
					buffer: bytes.buffer.slice( bytes.byteOffset, bytes.byteOffset + bytes.byteLength )
				} );
				return serial;
			}
			const raw = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + path, "utf8" ) );
			if ( path.endsWith( "/ifmainpopup.json" ) ) {
				raw.resourcesByDdjPath["new-control.ddj"] = { publicPath: future };
			}
			if ( path.endsWith( "/iftw_commonenemy.json" ) ) {
				raw.resourcesByDdjPath["new-target.ddj"] = { publicPath: future.replace( "control", "target" ) };
			}
			const buffer = new TextEncoder().encode( JSON.stringify( raw ) ).buffer;
			jobs.set( ++serial, { kind: "bytes", buffer } );
			return serial;
		},
		/*
		================
		take
		================
		*/
		take( id ) {
			const value = jobs.get( id );
			jobs.delete( id );
			return value;
		},
		cancel: id => jobs.delete( id )
	}, "http://fixture.invalid" );
	try {
		for ( let i = 0; i < 100 && !hud.data(); i++ ) hud.step();
		assert.equal( hud.error(), null );
		assert.ok( defined( hud.data() ).warmPaths.includes( future ) );
		assert.ok( defined( hud.data() ).warmPaths.includes( future.replace( "control", "target" ) ) );
		assert.equal( defined( hud.data() ).warmPaths.filter( path => path === future ).length, 1 );
	} finally {
		hud.dispose();
	}
});

test("normal-tile projection covers its interior exactly without scaling source texels", () => {
	fc.assert(
		fc.property( fc.integer( { min: 1, max: 800 } ), fc.integer( { min: 1, max: 600 } ), ( w, h ) => {
			const quads = normalTile( [ 17, 23, w, h ], "tile", [ 128, 128 ], [ 0, 0, 1000, 1000 ] );
			assert.equal( quads.reduce( ( area, q ) => area + q.rect[2] * q.rect[3], 0 ), w * h );
			for ( const q of quads ) {
				assert.equal( q.uv[2] * 128, q.rect[2] );
				assert.equal( q.uv[3] * 128, q.rect[3] );
				assert.ok( q.rect[0] + q.rect[2] <= 17 + w && q.rect[1] + q.rect[3] <= 23 + h );
			}
		} ),
		{ seed: 150686, numRuns: 150 }
	);
	assert.deepEqual(
		normalTile( [ 0, 0, 148, 112 ], "tile", [ 128, 128 ], [ 0, 0, 200, 200 ] ).map( q => [ q.rect, q.uv ] ),
		[
			[ [ 0, 0, 128, 112 ], [ 0, 0, 1, .875 ] ],
			[ [ 128, 0, 20, 112 ], [ 0, 0, .15625, .875 ] ]
		]
	);
	assert.deepEqual( normalTile( [ 0, 0, 1, 1 ], "tile", undefined, [ 0, 0, 1, 1 ] ), [] );
	assert.throws( () => normalTile( [ 0, 0, 1, 1 ], "tile", [ 0, 128 ], [ 0, 0, 1, 1 ] ), /extent/ );
});

test("party popup uses native runtime positions and natural option artwork with client insets", () => {
	const png = readFileSync(
		CLIENT_PUBLIC_ROOT + "/assets/images/Media_extracted/interface/messagebox/msgbox_blackbox.png"
	);
	const size = [ png.readUInt32BE( 16 ), png.readUInt32BE( 20 ) ], p = partyProposalLayout( 1200, 900, size );
	assert.deepEqual( p.frame, [ 450, 362, 300, 176 ] );
	assert.deepEqual( p.background, [ 466, 402, 268, 120 ] );
	assert.deepEqual( p.accept, [ 522, 501, 76, 24 ] );
	assert.deepEqual( p.refuse, [ 602, 501, 76, 24 ] );
	assert.deepEqual( p.options[0].image, [ 494, 455, ...size ] );
	assert.deepEqual( p.options[0].text, [ 497, 462, size[0] - 6, size[1] - 13 ] );
	const authored = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifmessagebox.json", "utf8" ) )
		.sections.find( s => s.name === "MsgBoxINIF" ).nodes;
	assert.equal( p.name[2], authored.find( n => n.id === 1 ).rect.width );
	assert.equal( p.question[2], authored.find( n => n.id === 2 ).rect.width );
	assert.deepEqual( partyProposalLayout( 1200, 900, undefined ).options, [] );
	const resized = partyProposalLayout( 900, 600, size );
	assert.equal( resized.accept[0] - resized.frame[0], 72 );
	assert.equal( resized.accept[1] - resized.frame[1], 139 );
});

test("frame remainders preserve native texel scale and leave the interior undrawn at every size", () => {
	const prefix = "/assets/images/Media_extracted/interface/frame/mframe_wnd_";
	// Independent natural extents published with the authored layout.
	const metadata = Object.values( layout ).find( v =>
		v && typeof v === "object" && !Array.isArray( v ) &&
		Object.values( v ).some( s => s?.sourcePath === "interface/frame/mframe_wnd_mid_up.ddj" )
	);
	assert.ok( metadata, "missing authored sprite catalogue" );
	const sizes = new Map( Object.values( metadata ).map( s => [ s.publicPath, [ s.width, s.height ] ] ) );
	fc.assert(
		fc.property( fc.integer( { min: 80, max: 1200 } ), fc.integer( { min: 116, max: 900 } ), ( w, h ) => {
			const quads = frameRing(
				[ 7, 11, w, h ],
				prefix,
				frameParts().map( part => sizes.get( prefix + part + ".png" ) ),
				[ 0, 0, 1600, 1200 ]
			);
			assert.equal( quads.reduce( ( sum, q ) => sum + q.rect[2] * q.rect[3], 0 ), w * h - (w - 80) * (h - 116) );
			for ( const q of quads ) {
				const size = sizes.get( q.texture );
				assert.equal( q.rect[2], q.uv[2] * defined( size )[0] );
				assert.equal( q.rect[3], q.uv[3] * defined( size )[1] );
				assert.ok(
					q.rect[0] >= 7 && q.rect[1] >= 11 && q.rect[0] + q.rect[2] <= 7 + w &&
						q.rect[1] + q.rect[3] <= 11 + h
				);
				assert.deepEqual( q.color, [ 1, 1, 1, 1 ] );
			}
		} ),
		{ seed: 1506740, numRuns: 150 }
	);
	assert.deepEqual( frameRing( [ 0, 0, 380, 420 ], prefix, [], [ 0, 0, 1200, 900 ] ), [] );
	assert.throws(
		() => frameRing( [ 0, 0, 380, 420 ], prefix, frameParts().map( () => [ 0, 128 ] ), [ 0, 0, 1200, 900 ] ),
		/extent/
	);
	assert.equal( frameParts().length, 8 );
});

test("world text uses the same published mask/advance contract for caret and glyph placement", () => {
	const font = decodeUiFont( atlas ), jobs = new Map();
	let next = 0;
	const owner = createUiText( {
		available: () => 1,
		/*
		================
		request
		================
		*/
		request() {
			jobs.set( ++next, { kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( atlas ) ).buffer } );
			return next;
		},
		take: id => {
			const result = jobs.get( id );
			jobs.delete( id );
			return result;
		},
		cancel: id => jobs.delete( id )
	}, "http://fixture.invalid" );
	assert.deepEqual( owner.quads( "Loading", [ 0, 0, 100, 24 ], [ 0, 0, 100, 24 ], [ 1, 1, 1, 1 ] ), [] );
	// What a label publishes: its quads through overlap resolution, with text
	// runs expanded into their glyphs (text-run.ts).
	const published = quads => expandTextRuns( resolveTextOverlaps( quads ) );
	owner.step();
	assert.equal( owner.step(), true );
	for ( const value of [ "Inventory", "Guild notice", "A B", "", "日本語" ] ) {
		const expected = Array.from(
			value,
			c => font.fonts["0"].glyphs[String( c.codePointAt( 0 ) )] ?? font.fonts["0"].glyphs["63"]
		).reduce( ( n, g ) => n + g.advanceX, 0 );
		assert.equal( owner.run( value ).width, expected );
		assert.deepEqual(
			published( owner.quads( value, [ 10, 20, 200, 24 ], [ 0, 0, 500, 500 ], [ 1, 1, 1, 1 ] ) ),
			published( titleText( font, value, [ 10, 20, 200, 24 ], [ 0, 0, 500, 500 ], [ 1, 1, 1, 1 ] ) )
		);
	}
	// The layout memo: a repeat is equal, each caller owns its array, and the
	// shared quads cannot be edited.
	const first = owner.quads( "Inventory", [ 10, 20, 200, 24 ], [ 0, 0, 500, 500 ], [ 1, 1, 1, 1 ] );
	first.push( { marker: true } );
	const again = owner.quads( "Inventory", [ 10, 20, 200, 24 ], [ 0, 0, 500, 500 ], [ 1, 1, 1, 1 ] );
	assert.notEqual( again, first );
	assert.deepEqual(
		published( again ),
		published( titleText( font, "Inventory", [ 10, 20, 200, 24 ], [ 0, 0, 500, 500 ], [ 1, 1, 1, 1 ] ) )
	);
	assert.equal( again.length, 1, "a string travels as one run quad" );
	assert.ok( again.every( quad => Object.isFrozen( quad ) ) );
	assert.notDeepEqual(
		owner.quads( "Inventory", [ 11, 20, 200, 24 ], [ 0, 0, 500, 500 ], [ 1, 1, 1, 1 ] ),
		again
	);
	for ( let i = 0; i < 2300; i++ ) {
		const value = "Glyph " + i;
		const expected = Array.from(
			value,
			c => font.fonts["0"].glyphs[String( c.codePointAt( 0 ) )] ?? font.fonts["0"].glyphs["63"]
		).reduce( ( n, g ) => n + g.advanceX, 0 );
		assert.equal( owner.run( value ).width, expected );
		assert.equal( owner.run( value ).width, expected );
	}
	owner.dispose();
	assert.equal( owner.path(), null );
	assert.equal( owner.step(), false );
	assert.equal( owner.run( "Inventory" ).width, 0 );
});

test("font admission rejects malformed atlas bounds and disposed loading cannot publish a late result", () => {
	const invalid = structuredClone( atlas );
	invalid.fonts["0"].glyphs["63"].x = invalid.atlasWidth;
	assert.throws( () => decodeUiFont( invalid ), /outside atlas/ );
	const cancelled = [];
	let takes = 0;
	const owner = createUiText( {
		available: () => 1,
		request: () => 7,
		take: () => {
			takes++;
			return null;
		},
		cancel: id => cancelled.push( id )
	}, "http://fixture.invalid" );
	owner.step();
	owner.dispose();
	owner.step();
	assert.deepEqual( cancelled, [ 7 ] );
	assert.equal( takes, 0 );
});

const { comboBoxChrome } = await import( "../../src/engine/foundation/ui/combo-box.ts" );
test("native combo children reserve the arrow and draw their authored border inside the list", () => {
	const size = p => p.includes( "arrow" ) ? [ 20, 20 ] : p.includes( "side" ) ? [ 1, 4 ] : [ 2, 3 ];
	const c = comboBoxChrome( [ 100, 80, 156, 20 ], size, [ 0, 0, 800, 600 ] );
	assert.deepEqual( c.textRect, [ 105, 80, 126, 20 ] );
	assert.deepEqual( c.quads[0].rect, [ 100, 80, 136, 20 ] );
	assert.deepEqual( c.quads[0].color, [ 0, 0, 0, 1 ] );
	assert.deepEqual( c.quads[1].rect, [ 100, 80, 2, 3 ] );
	assert.deepEqual( c.quads[2].rect, [ 234, 80, 2, 3 ] );
	assert.deepEqual( c.quads[5].rect, [ 100, 83, 1, 14 ] );
	assert.deepEqual( defined( c.quads.at( -1 ) ).rect, [ 236, 80, 20, 20 ] );
	assert.ok( c.paths.some( p => p.endsWith( "_focus.png" ) ) );
	assert.ok( c.paths.some( p => p.endsWith( "_press.png" ) ) );
});

const { barChrome } = await import( "../../src/engine/foundation/ui/bar.ts" );
const { matchingSlots } = await import( "../../src/engine/foundation/ui/matching-slots.ts" );
test("empty and partially filled party/academy pages retain every authored slot", () => {
	for (
		const [name, type] of [ [ "ifpartymatch", "CIFPartyMatchSlot" ], [ "ifmentormatch", "CIFMentorMatchSlot" ] ]
	) {
		const layout = decodeAuthoredLayout(
			JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/" + name + ".json", "utf8" ) )
		);
		const empty = matchingSlots( layout, type, [], 100, 200 ),
			filled = matchingSlots( layout, type, [ { id: 7 } ], 100, 200 );
		assert.equal( empty.length, 12 );
		assert.deepEqual( empty[0].rect, [ 128, 344, 715, 24 ] );
		assert.deepEqual( defined( empty.at( -1 ) ).rect, [ 128, 597, 715, 24 ] );
		assert.deepEqual( filled.map( s => s.rect ), empty.map( s => s.rect ) );
		assert.equal( filled.filter( s => s.row ).length, 1 );
		assert.throws( () => matchingSlots( layout, type, Array( 13 ).fill( { id: 7 } ), 0, 0 ), /authored slots/ );
	}
});
test("Academy and matching title bars cover their middle without stretching the tail texels", () => {
	const out = barChrome( [ 10, 20, 326, 24 ], "/bar_", p => p.endsWith( "mid.png" ) ? [ 24, 24 ] : [ 12, 24 ], [
		0,
		0,
		800,
		600
	] );
	assert.equal( out.quads.reduce( ( sum, q ) => sum + q.rect[2], 0 ), 326 );
	assert.ok( out.quads.some( q => q.texture === "/bar_mid.png" ) );
	assert.equal( defined( out.quads.at( -1 ) ).uv[2], 14 / 24 );
});
test("Skills selects Create and MainSkillWnd without importing Withdrawal overrides", () => {
	const raw = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifskill.json", "utf8" ) );
	const main = decodeAuthoredLayout( raw, [ "Create", "MainSkillWnd" ] );
	assert.deepEqual( main.GDR_SKILL_BG.rect, [ 27, 12, 319, 17 ] );
	assert.ok( main.GDR_SKILL_BOTTOM_BOX.texture.endsWith( "/skill/skl_wnd_box.png" ) );
	assert.deepEqual( authoredRect( main.GDR_SKILL_BOTTOM_BOX, 0, 0 ), [ 0, 299, 364, 36 ] );
	assert.throws( () => decodeAuthoredLayout( raw, [ "absent" ] ), /Missing authored section/ );
});

const { decodeQuestPresentation } = await import( "../../src/engine/foundation/ui/quest-presentation.ts" );
test("quest presentation uses the quest catalog, reward symbols and byte-joined warning variant", () => {
	const raw = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/data/questData.json", "utf8" ) ),
		catalog = decodeQuestPresentation( raw ),
		row = raw.rows.find( r => r.startsWith( "3\t" ) ).split( "\t" );
	assert.equal( catalog.records[3].title, raw.textEntries[row[2]] );
	assert.equal( catalog.records[3].rewardBody, raw.textEntries[row[4]] );
	assert.ok( catalog.records[3].rewardBody.length > 0 );
	assert.throws( () => decodeQuestPresentation( { ...raw, giveupWarnBytes: [] } ), /Invalid quest presentation/ );
});
