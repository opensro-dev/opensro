/*
===========================================================================

quest-list.test.mjs - the quest list row model and tracker (#4245)

The later client's quest window rendered in this one: a quest-row-then-
objectives row model, the content view face, the track set (<=3, fold)
and the tracker window placed at the screen's right edge from y 267.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const load = relative => import( sourceFileUrl( path.join( root, relative ) ).href );
const model = await load( "src/engine/foundation/ui/quest-list.ts" );
const { decodeResLayout } = await load( "src/engine/foundation/ui/res-layout.ts" );

const entries = {
	"UIIT_STT_LEVEL_LV": "Lv",
	"UIIT_STT_QUEST_ING": "In progress",
	"UIIT_STT_QUEST_END": "Complete",
	"UIIT_STT_PARTY": "Party",
	"UIIT_STT_GUILDVIEW": "Guild",
	"QGUARDS": "Kill %d guards"
};
const copy = key => entries[key] ?? key;

const quest = ( refId, u08 = 0x31, u09 = 0, completeKind = 1 ) => ({
	refId,
	u08,
	u09,
	targetIds: [ 9000 + refId ],
	contents: [
		{
			description: "QGUARDS",
			objectiveValues: [ 2 ],
			objectiveSentinel: false,
			kind: completeKind
		}
	]
});

const records = {
	1: { level: 20, title: "Alpha" },
	2: { level: 45, title: "Beta" },
	3: { level: 10, title: "Gamma" }
};

test("rows: a quest row then its objective rows", () => {
	const rows = model.questListRows( [ quest( 1 ), quest( 2 ) ], entries );
	assert.equal( rows.length, 4 );
	assert.equal( rows[0].content, -1 );
	assert.equal( rows[0].quest.refId, 1 );
	assert.equal( rows[1].content, 0 );
	assert.match( rows[1].objective.description, /Kill 2 guards/ );
	assert.equal( rows[1].objective.statusKey, "UIIT_STT_QUEST_ING" );
	assert.equal( rows[2].content, -1 );
});

test("counts: low nibble is done, high nibble is total (capped at 15)", () => {
	assert.deepEqual( model.questProgressCounts( quest( 1, 0x31 ) ), [ 1, 3 ] );
	assert.deepEqual( model.questProgressCounts( quest( 1, 0xf5 ) ), [ 5, 15 ] );
});

test("title: level, name and counts", () => {
	const title = model.questListTitle( records[1], quest( 1 ), copy );
	assert.equal( title, "Lv. 20 Alpha (1/3)" );
});

test("content face: blue for a quest far below the player, plain in reach, red above", () => {
	// Player level less quest level: >= 10 blue, >= 0 plain, < 0 red.
	assert.equal( model.questContentFace( 20, 30 ), "blue" );
	assert.equal( model.questContentFace( 21, 30 ), "normal" );
	assert.equal( model.questContentFace( 30, 30 ), "normal" );
	assert.equal( model.questContentFace( 31, 30 ), "red" );
});

test("track: adding stops at three, toggling removes, a full set says full", () => {
	let tracked = [];
	tracked = model.questToggleTracked( tracked, 1 ).tracked;
	tracked = model.questToggleTracked( tracked, 2 ).tracked;
	tracked = model.questToggleTracked( tracked, 3 ).tracked;
	assert.deepEqual( tracked, [ 1, 2, 3 ] );
	const full = model.questToggleTracked( tracked, 4 );
	assert.equal( full.status, "full" );
	assert.deepEqual( full.tracked, [ 1, 2, 3 ] );
	const removed = model.questToggleTracked( tracked, 2 );
	assert.equal( removed.status, "removed" );
	assert.deepEqual( removed.tracked, [ 1, 3 ] );
});

test("tracker entries: title, party/guild prefix, objective lines, fold", () => {
	const quests = [ quest( 1, 0x31, 1 ), quest( 2, 0xf5, 2, 0 ) ];
	const out = model.questTrackerEntries( quests, [ 1, 2 ], new Set( [ 2 ] ), records, entries, copy );
	assert.equal( out.length, 2 );
	assert.equal( out[0].refId, 1 );
	assert.equal( out[0].title, "[Party] Alpha" );
	assert.equal( out[0].lines.length, 1 );
	assert.match( out[0].lines[0].text, /Kill 2 guards/ );
	assert.equal( out[1].title, "[Guild] Beta" );
	assert.deepEqual( out[1].lines, [] );
});

test("placement: right edge from y 267; beside pushes left, above pushes down", () => {
	const [x, y] = model.questTrackerPlacement(
		{ screenWidth: 1280, screenHeight: 800, width: 324, height: 60, beside: [], above: [] }
	);
	assert.equal( x, 956 );
	assert.equal( y, 267 );
	const beside = model.questTrackerPlacement(
		{ screenWidth: 1280, screenHeight: 800, width: 324, height: 60, beside: [ [ 950, 250, 330, 120 ] ], above: [] }
	);
	assert.equal( beside[0], 950 - 324 );
	const above = model.questTrackerPlacement(
		{ screenWidth: 1280, screenHeight: 800, width: 324, height: 60, beside: [], above: [ [ 1000, 240, 200, 40 ] ] }
	);
	assert.equal( above[1], 280 );
	const clamped = model.questTrackerPlacement(
		{ screenWidth: 250, screenHeight: 200, width: 324, height: 60, beside: [], above: [] }
	);
	assert.equal( clamped[0], 0 );
});

test("layout decode: keyed path records refused when structurally damaged", () => {
	const good = {
		format: "res-ui-layout",
		version: 1,
		name: "questlist",
		root: 76,
		controls: [
			{
				key: "76",
				id: 76,
				parent: null,
				children: [ "5" ],
				type: 0,
				className: "CNIFQuest",
				rect: [ 198, 193, 624, 369 ],
				frame: "interface/frame/mframe_wnd_",
				texture: "",
				text: "x",
				color: 4294967295,
				flags: 0,
				rootWord: 0
			},
			{
				key: "5",
				id: 5,
				parent: "76",
				children: [],
				type: 8,
				className: "CNIFQuestSlot",
				rect: [ 214, 236, 576, 32 ],
				frame: "",
				texture: "interface/quest/qst_sub_window.ddj",
				text: "",
				color: 4294967295,
				flags: 0,
				rootWord: 0
			}
		],
		order: [ "76", "5" ]
	};
	const layout = decodeResLayout( good );
	assert.equal( layout.root.rect[2], 624 );
	assert.throws( () => decodeResLayout( { ...good, version: 2 } ) );
	assert.throws( () => decodeResLayout( "nope" ) );
});
