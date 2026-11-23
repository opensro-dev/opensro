/*
===========================================================================

populationCoverage.test.mjs - the server projection refuses an
incoherent population

#3683: a bundle assembled from the Alexandria variant's textdata and a
native-only region catalog loaded every Kings Valley and Storm Desert
nest, then never spawned one: the movement authority has no surface for
those regions, so the spawn is retried forever, silently. The bundle
builder refuses that assembly and names the missing regions. Synthetic
fixtures; no game data is read.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { assertPopulationMovementCoherence } from "../../build/server/buildServerGameDataBundle.mjs";

/*
================
withBundle

A temporary bundle root holding only the given population rows.
================
*/
async function withBundle( rows, check ) {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "population-coverage-" ) );
	try {
		const bundle = path.join( root, "bundle" );
		if ( rows ) {
			fs.mkdirSync( path.join( bundle, "textdata" ), { recursive: true } );
			fs.writeFileSync(
				path.join( bundle, "textdata", "cap110-monsters.txt" ),
				JSON.stringify( { Version: 1, Rows: rows } )
			);
		}
		await check( bundle );
	} finally {
		fs.rmSync( root, { recursive: true, force: true } );
	}
}

test("a native bundle without the population file is left alone", async () => {
	await withBundle( null, async ( bundle ) => {
		await assertPopulationMovementCoherence( bundle, {} );
	} );
});

test("a population fully covered by the movement authority passes", async () => {
	const rows = [ { RegionID: 19001 }, { RegionID: 21569 }, { RegionID: 19001 } ];
	await withBundle( rows, async ( bundle ) => {
		await assertPopulationMovementCoherence( bundle, { "0x4a39": [], "0x5441": [] } );
	} );
});

test("a population region the movement authority lacks is refused and named", async () => {
	const rows = [ { RegionID: 19001 }, { RegionID: 21569 } ];
	await withBundle( rows, async ( bundle ) => {
		await assert.rejects( assertPopulationMovementCoherence( bundle, { "0x4a39": [] } ), /0x5441/ );
	} );
});
