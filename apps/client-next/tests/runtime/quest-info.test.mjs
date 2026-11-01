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
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const load = relative => import( sourceFileUrl( path.join( root, relative ) ).href );
const { questInfoEntries, questInfoWindowHeight, questInfoIconFrame, QUEST_INFO_MAX_ENTRIES } = await load(
	"src/engine/foundation/ui/quest-info.ts"
);
const { decodeQuestPresentation } = await load( "src/engine/foundation/ui/quest-presentation.ts" );
const { createQuestInfo } = await load( "src/engine/runtime/ui/hud/quest-info.ts" );

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

test("the window takes the short height for one row and the list height above it", () => {
	assert.equal( questInfoWindowHeight( 1 ), 0x8e );
	assert.equal( questInfoWindowHeight( 2 ), 0x154 );
	assert.equal( questInfoWindowHeight( 9 ), 0x154 );
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
