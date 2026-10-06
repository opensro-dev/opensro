/*
===========================================================================

quest-guide.test.mjs - tests for quest-guide.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { isPlaceholderText, loadEnglishCompletions } from "../../../../scripts/build/shared/englishCompletions.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { questGuideRecords } from "../../../../scripts/build/shared/questGuideRecords.mjs";
import { completeGuideTitles, GUIDE_TITLE_COMPLETIONS } from "../../../../scripts/build/shared/textResources.mjs";
import { readLocalizedTextDataRowsSync } from "../../../../scripts/build/shared/textDataIo.mjs";
import { textDataDir } from "../../../../scripts/build/shared/resourceIo.mjs";
import path from "node:path";

const { decodeQuestGuide, questGuideRows } = await import(
	sourceFileUrl( "src/engine/foundation/ui/quest-guide.ts" ).href
);
const q = ( id, level = 1, symbol = "QNO_" + id, prerequisites = [], contentKey = "body" + id ) => ({
	id,
	level,
	symbol,
	prerequisites,
	contentKey
});
function catalog( records ) {
	return {
		records,
		articles: [
			{ id: 100000, parent: records.length, depth: 0, title: "Region", tokens: [], contentKey: "region" },
			...records.map( ( r, i ) => ({
				id: 100001 + i,
				parent: 100000,
				depth: 1,
				title: "Quest " + r.id,
				tokens: [ { kind: "text", value: "Authored " + r.id, color: null, strong: false } ],
				contentKey: r.contentKey
			}) )
		]
	};
}
const children = rows => rows.filter( r => r.depth === 1 );

test("raw guide projection matches native capture; published captions apply explicit localization corrections", async () => {
	const native = JSON.parse( await readFile( "tests/fixtures/quest-guide-retail.json", "utf8" ) );
	const data = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/data/questData.json", "utf8" ) );
	assert.equal( native.binarySha256, "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a" );
	assert.deepEqual( data.guideRecords, native.records );
	// This oracle comes from ReadProcessMemory of the hash-bound original client.
	// Never overwrite it with hashes of our generated/localized product output.
	const raw = {};
	const rawSource = {};
	for ( const name of [ "textquest.txt", "texthelp.txt" ] ) {
		for ( const fields of readLocalizedTextDataRowsSync( path.join( textDataDir, name ) ) ) {
			const key = fields[1];
			if ( fields[0] !== "1" || !(key in native.guideTextHashes) || key in raw ) {
				continue;
			}
			raw[key] = (fields[8] ?? "").replaceAll( "\\n", "\n" );
			rawSource[key] = name;
		}
	}
	assert.deepEqual(
		Object.fromEntries(
			Object.entries( raw ).map( (
				[key, value]
			) => [ key, createHash( "sha256" ).update( value ).digest( "hex" ) ] )
		),
		native.guideTextHashes
	);
	// Published captions: the retail text, then the English completion layer
	// where retail left the cell as a placeholder, then the guide titles.
	const localized = Object.fromEntries(
		Object.entries( raw ).map( ( [key, value] ) => {
			const completion = isPlaceholderText( value ) ?
				loadEnglishCompletions( rawSource[key] )[key]?.english :
				undefined;
			return [ key, completion === undefined ? value : completion.replaceAll( "\\n", "\n" ) ];
		} )
	);
	assert.deepEqual( data.guideTextEntries, completeGuideTitles( localized ) );
});

test("guide title completions fill only untranslated cells and never shadow shipped English", () => {
	assert.equal( Object.keys( GUIDE_TITLE_COMPLETIONS ).length, 25 );
	const entries = {
		SRO_GGW_MENU_QCH_MSG: "0",
		SRO_GGW_MENU_QCONS_MSG: "",
		SRO_GGW_MENU_GDG_MSG: "그래드 시스템",
		REAL_KEY: "Real English",
		OTHER: "0"
	};
	completeGuideTitles( entries );
	assert.equal( entries.SRO_GGW_MENU_QCH_MSG, "Jangan" );
	assert.equal( entries.SRO_GGW_MENU_QCONS_MSG, "Constantinople" );
	assert.equal( entries.SRO_GGW_MENU_GDG_MSG, "Guild system" );
	assert.equal( entries.REAL_KEY, "Real English" );
	assert.equal( entries.OTHER, "0" );
});
test("quest dictionary groups carry corrected English region titles, never 0 or blank", async () => {
	const load = async p => JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/" + p, "utf8" ) );
	const [guide, quests] = await Promise.all( [
		load( "data/event-guide-catalog.json" ),
		load( "data/questData.json" )
	] );
	const c = decodeQuestGuide( guide, quests, quests.guideTextEntries );
	const groups = c.articles.filter( a => a.depth === 0 ).map( a => a.title );
	assert.deepEqual( [ ...groups ].sort(), [
		"Asia Minor",
		"Central Asia",
		"Constantinople",
		"Donwhang",
		"Donwhang Stone Cave",
		"East Europe",
		"Hotan",
		"Jangan",
		"Special",
		"Taklamakan"
	] );
	for ( const title of groups ) assert.match( title, /[A-Za-z]/ );
});
test("publication reverses successor edges, keeps service/level gates and first record identity", () => {
	const row = (
		id,
		symbol,
		level = 1,
		service = "1"
	) => [
		service,
		String( id ),
		symbol,
		String( level ),
		"debug",
		"name",
		"reward",
		"xxx",
		"body" + id,
		"npc",
		"condition"
	];
	const records = questGuideRecords( [
		row( 2, "B" ),
		row( 1, "A" ),
		row( 3, "C" ),
		row( 1, "duplicate" ),
		row( 4, "off", 1, "0" ),
		row( 5, "future", 91 )
	], [ [ "A", "", 0, "B,C" ], [ "B", "", 0, "C" ], [ "C", "", 0, "xxx" ] ] );
	assert.deepEqual( records.map( r => [ r.id, r.symbol, r.prerequisites ] ), [ [ 1, "A", [] ], [ 2, "B", [ 1 ] ], [
		3,
		"C",
		[ 1, 2 ]
	] ] );
});
test("unaccepted quests are included through level + 10; QSP and five special trades require actual level", () => {
	const trades = [
		"QNO_TRADE_CH_SPECIAL2_1",
		"QNO_TRADE_WC_SPECIAL2_1",
		"QNO_TRADE_TK_SPECIAL_1",
		"QNO_TRADE_RM_SPECIAL_1",
		"QNO_TRADE_AM_SPECIAL_1"
	];
	const c = catalog( [
		q( 1, 11 ),
		q( 2, 12 ),
		q( 3, 2, "QSP_EXAMPLE" ),
		...trades.map( ( s, i ) => q( 4 + i, 2, s ) )
	] );
	assert.deepEqual( children( questGuideRows( c, 1, [], [] ) ).map( r => r.title ), [ "Quest 1" ] );
	assert.equal( children( questGuideRows( c, 2, [], [] ) ).length, 8 );
});
test("active quests append beyond the level collection but still obey prerequisites", () => {
	const c = catalog( [ q( 1, 40 ), q( 2, 40, "QNO_2", [ 3 ] ), q( 3 ) ] );
	assert.deepEqual( children( questGuideRows( c, 1, [ 1, 2 ], [] ) ).map( r => r.title ), [ "Quest 1", "Quest 3" ] );
	assert.equal( children( questGuideRows( c, 1, [ 1, 2 ], [ 3 ] ) ).length, 3 );
});
test("all prerequisites are required except the third inventory expansion any-branch", () => {
	const c = catalog( [
		q( 1 ),
		q( 2 ),
		q( 3, 60, "QNO_NORMAL", [ 1, 2 ] ),
		q( 4, 60, "QSP_KT_EXINVENTORY_3", [ 1, 2 ] )
	] );
	assert.equal( children( questGuideRows( c, 60, [], [] ) ).length, 2 );
	assert.deepEqual( children( questGuideRows( c, 60, [], [ 1 ] ) ).map( r => r.title ), [
		"Quest 1",
		"Quest 2",
		"Quest 4"
	] );
	assert.equal( children( questGuideRows( c, 60, [], [ 1, 2 ] ) ).length, 4 );
});
test("completed color overrides recommended/current/low colors including level-zero tutorial", () => {
	const c = catalog( [ q( 1, 0 ), q( 2, 1 ), q( 3, 2 ) ] ), rows = children( questGuideRows( c, 1, [], [ 2 ] ) );
	assert.deepEqual( rows.map( r => r.color ), [ [ 1, 217 / 255, 83 / 255, 1 ], [ .6, .6, .6, 1 ], [
		253 / 255,
		59 / 255,
		59 / 255,
		1
	] ] );
	assert.deepEqual( children( questGuideRows( c, 4, [], [] ) )[0].color, [ 1, 1, 1, 1 ] );
});
test("matching uses content symbols, keeps first collected record, and retains empty categories", () => {
	const c = catalog( [ q( 1, 50, "QNO_1", [], "shared" ), q( 2, 1, "QNO_2", [], "shared" ) ] );
	const rows = children( questGuideRows( c, 1, [ 1 ], [ 1 ] ) );
	assert.equal( rows.length, 2 );
	assert.deepEqual( rows[0].color, [ 1, 217 / 255, 83 / 255, 1 ] );
	assert.deepEqual( questGuideRows( c, 246, [], [] ).map( r => r.depth ), [ 0 ], "native uint8 level + 10 wraps" );
});
test("retail catalog admits every authored quest article with full body and correct dependency direction", async () => {
	const load = async p => JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/" + p, "utf8" ) );
	const [guide, quests, help] = await Promise.all( [
		load( "data/event-guide-catalog.json" ),
		load( "data/questData.json" ),
		load( "text/texthelp.en.json" )
	] );
	const c = decodeQuestGuide( guide, quests, help.entries );
	assert.equal( c.articles.length, 200 );
	assert.equal( c.articles.filter( a => a.depth === 1 ).length, 190 );
	const tutorial = c.records.find( r => r.symbol === "QTUTORIAL_CH" ),
		smith = c.records.find( r => r.symbol === "QNO_CH_SMITH_1" );
	assert.ok( smith.prerequisites.includes( tutorial.id ) );
	assert.ok( !tutorial.prerequisites.includes( smith.id ) );
	const rows = questGuideRows( c, 1, [], [] ), article = rows.find( r => r.id === 100001 );
	assert.ok(
		c.articles.find( a => a.id === 100003 ).tokens.some( t =>
			t.kind === "text" && t.value.includes( "Find the shoes" )
		),
		"embedded LF must not truncate English article"
	);
	assert.equal(
		c.articles.find( a => a.id === 104000 ).title,
		"Donwhang Stone Cave",
		"untranslated captions complete from attested region names, never translator notes"
	);
	assert.ok( article.tokens.some( t => t.kind === "text" && t.value.includes( "General Sonhyeon" ) ) );
	assert.ok( !rows.some( r => r.id === 100002 ), "unaccepted successor is hidden until tutorial completion" );
	assert.ok( questGuideRows( c, 1, [], [ tutorial.id ] ).some( r => r.id === 100002 ) );
	assert.throws( () => decodeQuestGuide( guide, { ...quests, guideRecords: undefined }, help.entries ), /metadata/ );
	const missing = { ...quests, guideTextEntries: { ...quests.guideTextEntries } };
	delete missing.guideTextEntries.SN_PAYCON_QTUTORIAL_CH;
	assert.throws( () => decodeQuestGuide( guide, missing, help.entries ), /article text/ );
});
