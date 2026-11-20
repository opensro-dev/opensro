/*
===========================================================================

quest-info.test.mjs - the level-up quest notice (#2310)

The pure walk (0x7e5fa0): a quest qualifies when its level byte equals
the gained level, its country byte is 3 (both) or the player's country,
and it is not already held; past level 40 the country check is skipped
(0x7e603a). The owner mirrors the native lifecycle: icon on a gain with
matches, window on click, notice retired on close.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const load = relative => import( sourceFileUrl( path.join( root, relative ) ).href );
const {
	questInfoEntries,
	questInfoWindowHeight,
	questInfoRows,
	questInfoDividers,
	questInfoIconFrame,
	QUEST_INFO_MAX_ENTRIES
} = await load(
	"src/engine/foundation/ui/quest-info.ts"
);
const { decodeQuestPresentation } = await load( "src/engine/foundation/ui/quest-presentation.ts" );
const { createQuestInfo } = await load( "src/engine/runtime/ui/hud/quest-info.ts" );
const { uiFixture, fontAtlas } = await import( "../helpers/ui-fixture.mjs" );
const { defined } = await import( "../helpers/defined.mjs" );
const { CLIENT_PUBLIC_ROOT } = await import( "../../../../scripts/lib/generatedRoot.mjs" );

const record = ( level, country, title = "Quest" ) => ({
	level,
	title,
	notify: "NPC " + title,
	condition: "Do " + title,
	country
});
const catalog = {
	101: record( 10, 0, "China ten" ),
	102: record( 10, 1, "Europe ten" ),
	103: record( 10, 3, "Both ten" ),
	104: record( 11, 3, "Both eleven" ),
	105: record( 45, 1, "Europe forty-five" )
};

test("the walk admits only the gained level, the player's country or both, and unheld quests", () => {
	assert.deepEqual(
		questInfoEntries( catalog, 10, 0, new Set() ).map( e => e.id ),
		[ 101, 103 ]
	);
	assert.deepEqual(
		questInfoEntries( catalog, 10, 1, new Set() ).map( e => e.id ),
		[ 102, 103 ]
	);
	assert.deepEqual(
		questInfoEntries( catalog, 10, 0, new Set( [ 101 ] ) ).map( e => e.id ),
		[ 103 ]
	);
	assert.deepEqual( questInfoEntries( catalog, 12, 0, new Set() ), [] );
	const [entry] = questInfoEntries( catalog, 10, 0, new Set() );
	assert.equal( entry.title, "China ten" );
	assert.equal( entry.notify, "NPC China ten" );
	assert.equal( entry.condition, "Do China ten" );
});

test("past level 40 the walk skips the country check (0x7e603a)", () => {
	assert.deepEqual(
		questInfoEntries( catalog, 45, 0, new Set() ).map( e => e.id ),
		[ 105 ]
	);
	assert.deepEqual(
		questInfoEntries( catalog, 40, 0, new Set() ).map( e => e.id ),
		[]
	);
});

test("the walk stops at the native out-list capacity", () => {
	const wide = {};
	for ( let id = 1; id <= QUEST_INFO_MAX_ENTRIES + 5; id++ ) wide[id] = record( 7, 3, "Q" + id );
	assert.equal( questInfoEntries( wide, 7, 0, new Set() ).length, QUEST_INFO_MAX_ENTRIES );
});

test("the window takes 62D260's three heights: one row, two rows, the list", () => {
	assert.equal( questInfoWindowHeight( 1 ), 0x8e );
	assert.equal( questInfoWindowHeight( 2 ), 0xf1 );
	assert.equal( questInfoWindowHeight( 3 ), 0x154 );
	assert.equal( questInfoWindowHeight( 9 ), 0x154 );
});

test("up to three rows show, 0x63 apart once there are two, with a divider between each", () => {
	assert.deepEqual( questInfoRows( 1 ), { visible: 1, pitch: 0x60 } );
	assert.deepEqual( questInfoRows( 2 ), { visible: 2, pitch: 0x63 } );
	assert.deepEqual( questInfoRows( 7 ), { visible: 3, pitch: 0x63 } );
	assert.deepEqual( questInfoDividers( 1 ), [] );
	assert.deepEqual( questInfoDividers( 2 ), [ [ 0x28, 0x85, 0xeb, 3 ] ] );
	assert.deepEqual( questInfoDividers( 9 ), [ [ 0x28, 0x85, 0xeb, 3 ], [ 0x28, 0xe8, 0xeb, 3 ] ] );
	// Every divider fits inside its window, under the scroll manager's rows.
	for ( const count of [ 2, 3 ] ) {
		for ( const [, y, , h] of questInfoDividers( count ) ) assert.ok( y + h <= questInfoWindowHeight( count ) );
	}
});

test("the icon swaps frames every 500 ms", () => {
	assert.equal( questInfoIconFrame( 0 ), 0 );
	assert.equal( questInfoIconFrame( 499 ), 0 );
	assert.equal( questInfoIconFrame( 500 ), 1 );
	assert.equal( questInfoIconFrame( 999 ), 1 );
	assert.equal( questInfoIconFrame( 1000 ), 0 );
});

test("the presentation decode resolves the notify, condition and country columns", () => {
	const raw = {
		version: 5,
		rows: [ "7\t10\tSN_Q\tSN_R\tSN_B\tSN_NN_NPC\tSN_NC_DO" ],
		giveupWarnBytes: [ 0 ],
		progressClearBytes: [ 0 ],
		countryBytes: [ 3 ],
		textEntries: {
			SN_Q: "Name",
			SN_R: "Reward",
			SN_B: "Body",
			SN_NN_NPC: "The NPC",
			SN_NC_DO: "The condition"
		}
	};
	const { records } = decodeQuestPresentation( raw );
	assert.equal( records[7].notify, "The NPC" );
	assert.equal( records[7].condition, "The condition" );
	assert.equal( records[7].country, 3 );
	assert.throws( () => decodeQuestPresentation( { ...raw, countryBytes: [] } ), /Invalid quest presentation/ );
});

test("the icon shows on a gain with matches, the window replaces it, and closing retires the notice", () => {
	const info = createQuestInfo();
	// World entry sets the baseline: no icon at the entered level.
	info.observe( 9, 0, [], catalog, 0 );
	assert.equal( info.iconVisible(), false );
	// A gain to a level that opens quests shows the icon.
	info.observe( 10, 0, [], catalog, 16 );
	assert.equal( info.iconVisible(), true );
	assert.equal( info.iconPath().endsWith( "qstinfoic_1.png" ), true );
	// The blink phase flips with the clock while the icon shows.
	info.observe( 10, 0, [], catalog, 500 );
	assert.equal( info.iconPath().endsWith( "qstinfoic_2.png" ), true );
	// Click: window open, icon hidden, rows kept.
	info.openWindow();
	assert.equal( info.windowOpen(), true );
	assert.equal( info.iconVisible(), false );
	assert.deepEqual( info.entries().map( e => e.id ), [ 101, 103 ] );
	// Close: the notice is retired until the next gain.
	info.closeWindow();
	assert.equal( info.windowOpen(), false );
	assert.equal( info.iconVisible(), false );
	// The next gain re-arms the notice.
	info.observe( 11, 0, [], catalog, 1000 );
	assert.equal( info.iconVisible(), true );
	assert.deepEqual( info.entries().map( e => e.id ), [ 104 ] );
});

test("a gain with no new quests shows nothing, and a level drop never shows the icon", () => {
	const info = createQuestInfo();
	info.observe( 10, 0, [], catalog, 0 );
	info.observe( 12, 0, [], catalog, 16 );
	assert.equal( info.iconVisible(), false );
	info.observe( 10, 0, [], catalog, 32 );
	assert.equal( info.iconVisible(), false );
});

test("a gain before the catalog loads settles on the first observe that brings it", () => {
	const info = createQuestInfo();
	info.observe( 9, 0, [], catalog, 0 );
	info.observe( 10, 0, [], undefined, 16 );
	assert.equal( info.iconVisible(), false );
	info.observe( 10, 0, [], catalog, 32 );
	assert.equal( info.iconVisible(), true );
	assert.deepEqual( info.entries().map( e => e.id ), [ 101, 103 ] );
});

test("held quests are left out of the notice", () => {
	const info = createQuestInfo();
	info.observe( 9, 0, [], catalog, 0 );
	info.observe( 10, 0, [ 101, 103 ], catalog, 16 );
	assert.equal( info.iconVisible(), false );
});

test("reset forgets the baseline so re-entry starts clean", () => {
	const info = createQuestInfo();
	info.observe( 9, 0, [], catalog, 0 );
	info.observe( 10, 0, [], catalog, 16 );
	assert.equal( info.iconVisible(), true );
	info.reset();
	assert.equal( info.iconVisible(), false );
	info.observe( 10, 0, [], catalog, 32 );
	assert.equal( info.iconVisible(), false );
});

/*
================
armedQuestFixture

A UI fixture whose served quest table carries one extra level-11 quest
with the given texts, stepped from level 10 to 11 until the gain arms
the quest icon. Returns the fixture, its step and the armed scene.
================
*/
function armedQuestFixture( texts = {} ) {
	const served = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/data/questData.json", "utf-8" ) );
	served.rows.push( "999001\t11\tSN_Q999\tSN_R999\tSN_B999\tSN_NN999\tSN_NC999" );
	served.countryBytes.push( 3 );
	served.giveupWarnBytes.push( 0 );
	served.progressClearBytes.push( 0 );
	Object.assign( served.textEntries, {
		SN_Q999: "Name",
		SN_R999: "Reward",
		SN_B999: "Body",
		SN_NN999: "The NPC",
		SN_NC999: "The condition",
		...texts
	} );
	const f = uiFixture(
		() => {},
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		{},
		{ "/assets/data/questData.json": new TextEncoder().encode( JSON.stringify( served ) ) }
	);
	f.state.entities = f.state.entities.map( e => ({ ...e, countryByte9c: 0 }) );
	const step = ( level, now ) => {
		f.state.gameplay = { ...f.state.gameplay, progression: { level, masteries: [] } };
		return f.ui.step( f.state, now );
	};
	for ( let i = 0; i < 80; i++ ) step( 10, 1000 + i * 100 );
	let scene;
	for ( let i = 0; i < 40 && !scene; i++ ) {
		step( 11, 10000 + i * 100 );
		const candidate = f.scenes.at( -1 );
		if ( candidate?.quads.some( q => q.texture?.includes( "qstinfoic_" ) ) ) scene = candidate;
	}
	return { f, step, scene };
}

/*
================
quest icon paint order

Native creates the guide as its own window and brings it to front
(69CCA0), so the minimap plate under its spot (w-0x57, 0xB3) never
covers it. The port draws the icon with the frame's topmost group:
in a rendered frame its quad comes after the minimap plate's, and no
panel quad paints after it (glyph quads of a tooltip may).
================
*/
test("the armed quest icon paints in the topmost group, after the minimap plate", () => {
	const { scene } = armedQuestFixture();
	assert.ok( scene, "the level gain never armed the quest icon" );
	const quads = scene.quads,
		iconIndex = quads.findIndex( q => q.texture?.includes( "qstinfoic_" ) ),
		plateIndex = quads.findIndex( q => q.texture?.endsWith( "/minimap/mm_window.png" ) );
	assert.ok( plateIndex >= 0, "the minimap plate is not in the frame" );
	assert.ok( iconIndex > plateIndex, "the quest icon paints under the minimap plate" );
	const covered = quads.slice( iconIndex + 1 ).find( q => q.texture && q.texture !== fontAtlas.image );
	assert.ok( !covered, "a panel quad paints after the quest icon: " + covered?.texture );
});

/*
================
quest-chain text clip

Port-only (owner decision on #2806, on by default): the native statics
draw one unbounded line (62E240), sized for the Korean names, so the
English NPC names ran past the window's right border. The title, NPC
and condition are now cut at their control's edge: no glyph ink of the
open window reaches past its right border.
================
*/
test("a long quest-chain title, NPC and condition stay inside the window (#2806)", () => {
	const long = "Yeosun, shopgirl of haberdashery in Donwhang, far past the window border";
	const { f, step, scene } = armedQuestFixture( { SN_Q999: long, SN_NN999: long, SN_NC999: long } );
	try {
		assert.ok( scene, "the level gain never armed the quest icon" );
		f.ui.event( { kind: "activate", id: "quest-info-icon" } );
		let product, drawn;
		for ( let i = 0; i < 4; i++ ) {
			product = step( 11, 20000 + i * 100 ) ?? product;
			drawn = f.scenes.at( -1 );
		}
		const plate = defined( product?.controls.find( c => c.id === "quest-info-drag" ), "quest info window" ).rect;
		const [left, top, width, height] = plate;
		const glyphs = defined( drawn ).quads.filter( q =>
			q.texture === fontAtlas.image && q.rect[0] >= left && q.rect[0] < left + width && q.rect[1] >= top &&
			q.rect[1] < top + height
		);
		assert.ok( glyphs.length > 0, "the open window drew no text" );
		for ( const quad of glyphs ) {
			const inkRight = Math.min( quad.rect[0] + quad.rect[2], quad.clip[0] + quad.clip[2] );
			assert.ok(
				inkRight <= left + width,
				`text ink reaches x=${inkRight}, past the window edge ${left + width}`
			);
		}
	} finally {
		f.dispose();
	}
});

/*
================
quest info frame

CIFQuestInfo is a main frame on mframe_wnd_ (ginterface GDR_QUESTINFO):
its ring is the window's edges and its plate the background. The port
drew only ifquestinfo.txt, whose one node is the scroll manager, so the
slot plates floated over the world with no edge or background. The open
window now draws the mframe_wnd_ ring around its whole rect, and a
divider tile under each shown row but the last.
================
*/
test("the open quest info window draws its main frame and the row dividers", () => {
	const { f, step, scene } = armedQuestFixture();
	try {
		assert.ok( scene, "the level gain never armed the quest icon" );
		f.ui.event( { kind: "activate", id: "quest-info-icon" } );
		let product, drawn;
		for ( let i = 0; i < 4; i++ ) {
			product = step( 11, 20000 + i * 100 ) ?? product;
			drawn = f.scenes.at( -1 );
		}
		const [left, top, width, height] = defined(
			product?.controls.find( c => c.id === "quest-info-drag" ),
			"quest info window"
		).rect;
		const ring = defined( drawn ).quads.filter( q => q.texture?.includes( "/frame/mframe_wnd_" ) );
		assert.ok( ring.length > 0, "the open window drew no main frame" );
		const ringLeft = Math.min( ...ring.map( q => q.rect[0] ) ),
			ringTop = Math.min( ...ring.map( q => q.rect[1] ) ),
			ringRight = Math.max( ...ring.map( q => q.rect[0] + q.rect[2] ) ),
			ringBottom = Math.max( ...ring.map( q => q.rect[1] + q.rect[3] ) );
		assert.deepEqual( [ ringLeft, ringTop, ringRight, ringBottom ], [ left, top, left + width, top + height ] );
		const rows = Math.round( (height - 0x8e) / 0x63 ) + 1,
			dividers = drawn.quads.filter( q => q.texture?.endsWith( "/bg_tile/com_bg_tile_a.png" ) );
		assert.equal( new Set( dividers.map( q => q.rect[1] ) ).size, rows - 1 );
	} finally {
		f.dispose();
	}
});

/*
================
quest info close reachable

A player could not close the window (BR-261118-0922-7648): a control
stacks by its order in the published list (the platform's z-index), and
the window's drag surface, pushed after the frame, covered the close
button. The close control must stack above every control under it, and
activating it closes the window.
================
*/
test("the quest info close button sits above the window surface and closes it", () => {
	const { f, step, scene } = armedQuestFixture();
	try {
		assert.ok( scene, "the level gain never armed the quest icon" );
		f.ui.event( { kind: "activate", id: "quest-info-icon" } );
		let product;
		for ( let i = 0; i < 4; i++ ) product = step( 11, 20000 + i * 100 ) ?? product;
		const controls = defined( product, "no publication" ).controls,
			closeIndex = controls.findIndex( c => c.id === "quest-info-close" ),
			close = defined( controls[closeIndex], "quest info close" ),
			[cx, cy] = [ close.rect[0] + close.rect[2] / 2, close.rect[1] + close.rect[3] / 2 ],
			inside = r => cx >= r[0] && cx < r[0] + r[2] && cy >= r[1] && cy < r[1] + r[3],
			above = controls.slice( closeIndex + 1 ).filter( c => inside( c.rect ) ).map( c => c.id );
		assert.deepEqual( above, [], "controls cover the close button's centre" );
		f.ui.event( { kind: "activate", id: "quest-info-close" } );
		let after;
		for ( let i = 0; i < 4; i++ ) after = step( 11, 21000 + i * 100 ) ?? after;
		assert.ok( !defined( after, "no publication after close" ).controls.some( c => c.id === "quest-info-drag" ) );
	} finally {
		f.dispose();
	}
});

/*
================
quest condition wrap

Port-only (Experimental "Fit window labels", on by default): a link
condition wider than the 200-pixel condition field (x 66 in the 280
plate) read "Available Once. Link (2nd bett" cut at the field's edge.
It now wraps onto two lines inside the field's columns, so every word
shows and no ink passes the field's right edge.
================
*/
test("a long link condition wraps onto two lines inside its field", () => {
	const condition = "Available Once. Link (2nd betting (Mamoje))";
	const { f, step, scene } = armedQuestFixture( { SN_NC999: condition, SN_Q999: "Q", SN_NN999: "N" } );
	try {
		assert.ok( scene, "the level gain never armed the quest icon" );
		f.ui.event( { kind: "activate", id: "quest-info-icon" } );
		let product, drawn;
		for ( let i = 0; i < 4; i++ ) {
			product = step( 11, 20000 + i * 100 ) ?? product;
			drawn = f.scenes.at( -1 );
		}
		const [left, top] = defined( product?.controls.find( c => c.id === "quest-info-drag" ), "quest info window" )
			.rect;
		// The fixture's player is country 0, so the walk shows quest 56 and
		// the fixture row (country 3, both): the fixture is the second and
		// last slot. Its condition field: scroll manager x 9 + field x 66,
		// 200 wide, in the slot's lower band.
		const fieldLeft = left + 9 + 66, fieldRight = fieldLeft + 200;
		const inField = defined( drawn ).quads.filter( q =>
			q.texture === fontAtlas.image && q.rect[0] >= fieldLeft && q.rect[0] < fieldRight && q.rect[1] > top
		);
		const lastY = Math.max( ...inField.map( q => q.rect[1] ) ),
			shown = inField.filter( q => q.rect[1] > lastY - 20 );
		// Every character is drawn but the one space the break consumes, the
		// text stands taller than one 13-pixel line, and no ink passes the
		// field's right edge (the old cut drew one clipped line).
		assert.equal( shown.length, condition.length - 1 );
		const span = Math.max( ...shown.map( q => q.rect[1] + q.rect[3] ) ) -
			Math.min( ...shown.map( q => q.rect[1] ) );
		assert.ok( span > 13, `the condition stands ${span} pixels tall: one line` );
		for ( const q of shown ) {
			assert.ok(
				Math.min( q.rect[0] + q.rect[2], q.clip[0] + q.clip[2] ) <= fieldRight,
				"condition ink past its field"
			);
			assert.ok( q.rect[0] + q.rect[2] <= q.clip[0] + q.clip[2], "a condition glyph is cut" );
		}
	} finally {
		f.dispose();
	}
});
