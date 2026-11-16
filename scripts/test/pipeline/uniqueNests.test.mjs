/*
===========================================================================

uniqueNests.test.mjs - the world map's unique nest publication (port-only)

The rule is the server's alone (sro-evidence unique-nests, covered by its
Go tests); the build only checks the exported table's shape and writes it.
No game data is read and no Go is run: the table is handed in.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

import { buildUniqueNestsAsset, checkUniqueNestTable } from "../../build/data/buildUniqueNestsAsset.mjs";

const TABLE = {
	format: "sro-unique-nests",
	version: 1,
	nests: [
		{ refObjId: 10, regionId: 25000, x: 100.5, z: 200.25 },
		{ refObjId: 10, regionId: 0x8007, x: 960, z: 960 }
	]
};

test("the server's table passes through unchanged, nests in its order", () => {
	assert.deepEqual( checkUniqueNestTable( TABLE ), TABLE );
});

test("a table outside the client's contract, or naming no nest, is refused whole", () => {
	for (
		const table of [
			null,
			{ ...TABLE, format: "other" },
			{ ...TABLE, version: 2 },
			{ ...TABLE, nests: {} },
			{ ...TABLE, nests: [] },
			{ ...TABLE, nests: [ { ...TABLE.nests[0], refObjId: 0 } ] },
			{ ...TABLE, nests: [ { ...TABLE.nests[0], regionId: 0x10000 } ] },
			{ ...TABLE, nests: [ { ...TABLE.nests[0], x: null } ] },
			{ ...TABLE, nests: [ null ] }
		]
	) {
		assert.throws( () => checkUniqueNestTable( table ), /unique-nests/ );
	}
});

test("the asset is published under the data root with its format", async () => {
	const root = mkdtempSync( path.join( tmpdir(), "unique-nests-" ) );
	try {
		const result = await buildUniqueNestsAsset( { publicRoot: root, table: TABLE } );
		assert.equal( result.nests, 2 );
		assert.deepEqual(
			JSON.parse( readFileSync( path.join( root, "assets", "data", "uniqueNests.json" ), "utf8" ) ),
			TABLE
		);
	} finally {
		rmSync( root, { recursive: true, force: true } );
	}
});
