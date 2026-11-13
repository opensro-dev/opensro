/*
===========================================================================

uniqueNests.test.mjs - the world map's unique nest publication (port-only)

Synthetic textdata rows in the retail column layout; no game data is read.
The rule under test is the server's: a served CICMonster reference whose
rarity & 15 is 3, at every npcpos anchor it has.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

import { buildUniqueNestsAsset, uniqueNests, uniqueReferences } from "../../build/data/buildUniqueNestsAsset.mjs";

// The character bit and TID1 1, TID2 2, TID3 1 pack to the monster word
// 0x0C6; TID3 5 is a fortress structure (0x2C6).
const MONSTER_TIDS = { 8: 1, 9: 1, 10: 2, 11: 1 };
const STRUCTURE_TIDS = { 8: 1, 9: 1, 10: 2, 11: 5 };

/*
================
characterRow

A characterdata row with the columns the unique rule reads set.
================
*/
function characterRow( id, values ) {
	const cols = Array.from( { length: 113 }, () => "0" );
	cols[0] = "1";
	cols[1] = String( id );
	cols[2] = "MOB_" + id;
	for ( const [column, value] of Object.entries( values ) ) cols[Number( column )] = String( value );
	return cols;
}

const CHARACTER_ROWS = [
	characterRow( 10, { ...MONSTER_TIDS, 15: 3 } ),
	characterRow( 11, { ...MONSTER_TIDS, 15: 3 } ),
	// The server refuses a rarity above 0x0F, low nibble or not.
	characterRow( 18, { ...MONSTER_TIDS, 15: 0x13 } ),
	characterRow( 12, { ...MONSTER_TIDS, 15: 0 } ),
	characterRow( 13, { ...MONSTER_TIDS, 15: 3, 0: 0 } ),
	characterRow( 14, { ...STRUCTURE_TIDS, 15: 3 } ),
	// An NPC (TID1 1, TID2 2, TID3 2) is not a monster.
	characterRow( 15, { 8: 1, 9: 1, 10: 2, 11: 2, 15: 3 } ),
	// Without the character bit the word is 0x0C4, not a monster.
	characterRow( 17, { ...MONSTER_TIDS, 8: 0, 15: 3 } ),
	characterRow( 16, { ...MONSTER_TIDS, 15: 3 } ).slice( 0, 40 )
];

test("a unique is a served monster reference of rarity 3", () => {
	assert.deepEqual( [ ...uniqueReferences( CHARACTER_ROWS ) ].sort(), [ 10, 11 ] );
});

test("every npcpos anchor of a unique is a nest, in table order", () => {
	const rows = [
		[ "10", "25000", "100.5", "3", "200.25" ],
		[ "12", "25000", "1", "2", "3" ],
		[ "10", "25257", "659.7", "0", "981.1" ],
		[ "11", "-32761", "960", "0", "960" ],
		[ "10", "25000", "x", "0", "1" ],
		[ "10", "25000" ]
	];
	assert.deepEqual( uniqueNests( rows, uniqueReferences( CHARACTER_ROWS ) ), [
		{ refObjId: 10, regionId: 25000, x: 100.5, z: 200.25 },
		{ refObjId: 10, regionId: 25257, x: 659.7, z: 981.1 },
		{ refObjId: 11, regionId: 0x8007, x: 960, z: 960 }
	] );
});

test("the asset is published under the data root with its format", async () => {
	const root = mkdtempSync( path.join( tmpdir(), "unique-nests-" ) );
	try {
		const result = await buildUniqueNestsAsset( {
			publicRoot: root,
			sources: { characterRows: CHARACTER_ROWS, npcposRows: [ [ "10", "25000", "1", "2", "3" ] ] }
		} );
		assert.equal( result.nests, 1 );
		assert.deepEqual(
			JSON.parse( readFileSync( path.join( root, "assets", "data", "uniqueNests.json" ), "utf8" ) ),
			{
				format: "sro-unique-nests",
				version: 1,
				nests: [ { refObjId: 10, regionId: 25000, x: 1, z: 3 } ]
			}
		);
	} finally {
		rmSync( root, { recursive: true, force: true } );
	}
});
