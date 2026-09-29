/*
===========================================================================

englishCorrections.test.mjs - shared catalog and server English correction

Exercise real projection boundaries, including the preserved language cells,
record framing and format placeholders. Stale corrections must fail closed.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { completedEnglish, completeEnglishTextProjection } from "../../build/shared/englishCompletions.mjs";

const CASES = [
	[ "textdataname.txt", "SN_NPC_CH_SOLDIER_EA2", "Solder Sangnam [Teleport]", "Soldier Sangnam [Teleport]" ],
	[ "textdataname.txt", "SN_ZONE_25031_2", "Karakoram South Sock", "Karakoram South Dock" ],
	[ "textdataname.txt", "SN_ITEM_QNO_CH_EUROPE_3_02", "Blood Devil 's leaf", "Blood Devil's leaf" ],
	[
		"textquest.txt",
		"SN_CON_QSP_ALL_POTION_3_01",
		"Collect  Purification Seed (%d)",
		"Collect Purification Seed (%d)"
	],
	[
		"textquest.txt",
		"SN_CON_QSP_ALL_POTION_4",
		"Collect  Purification Fruit (%d)",
		"Collect Purification Fruit (%d)"
	]
];

test("reviewed English reaches catalog and server without changing other cells or framing", () => {
	for ( const [file, key, before, after] of CASES ) {
		const columns = [ "1", key, "Original", "", "", "", "", "", before, "Other language" ];
		assert.equal( completedEnglish( file, columns ), after );
		for ( const encoding of [ "utf8", "utf16le" ] ) {
			const prefix = encoding === "utf16le" ? "\ufeff" : "";
			const source = Buffer.from( prefix + columns.join( "\t" ) + "\r\n", encoding );
			const expected = [ ...columns ];
			expected[8] = after;
			const result = completeEnglishTextProjection( file, source );
			assert.deepEqual( result, Buffer.from( prefix + expected.join( "\t" ) + "\r\n", encoding ) );
			assert.deepEqual( completeEnglishTextProjection( file, result ), result );
		}
	}
});

test("corrections reject stale wording and never rewrite unrelated symbols", () => {
	const columns = [ "1", "SN_NPC_CH_SOLDIER_EA2", "Original", "", "", "", "", "", "New wording" ];
	assert.throws( () => completedEnglish( "textdataname.txt", columns ), /Stale English correction/ );
	columns[1] = "UNRELATED";
	columns[8] = "Solder Sangnam [Teleport]";
	assert.equal( completedEnglish( "textdataname.txt", columns ), columns[8] );
});
