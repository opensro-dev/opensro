/*
===========================================================================

optionalTeleportProjection.test.mjs - the server projection's reverse return map table

Port-only, not native. An imported VSRO optional-teleport table joins the
server textdata unchanged; a game root without the import projects
nothing, and an import of another format is refused. Synthetic files; no
game data is read.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { buildOptionalTeleportProjection } from "../../build/server/buildServerGameDataBundle.mjs";

/*
================
withBundle

A temporary bundle root and an import path beside it.
================
*/
async function withBundle( document, check ) {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "optional-teleport-" ) );
	try {
		const source = path.join( root, "vsro-optional-teleport.json" );
		if ( document ) fs.writeFileSync( source, JSON.stringify( document ) );
		await check( path.join( root, "bundle" ), source );
	} finally {
		fs.rmSync( root, { recursive: true, force: true } );
	}
}

test("a game root without the import adds no file to the projection", async () => {
	await withBundle( null, async ( bundle, source ) => {
		await buildOptionalTeleportProjection( bundle, source );
		assert.equal( fs.existsSync( bundle ), false );
	} );
});

test("an import joins the textdata byte for byte", async () => {
	const document = {
		format: "opensro-vsro-optional-teleport-v1",
		rows: [ { id: 11, name: "Jangan", regionId: 25000 } ]
	};
	await withBundle( document, async ( bundle, source ) => {
		await buildOptionalTeleportProjection( bundle, source );
		const projected = fs.readFileSync( path.join( bundle, "textdata", "vsro-optional-teleport.json" ) );
		assert.deepEqual( projected, fs.readFileSync( source ) );
	} );
});

test("an import of another format or with no rows is refused", async () => {
	for (
		const document of [ { format: "other", rows: [ {} ] }, {
			format: "opensro-vsro-optional-teleport-v1",
			rows: []
		} ]
	) {
		await withBundle( document, async ( bundle, source ) => {
			await assert.rejects( buildOptionalTeleportProjection( bundle, source ), /optional-teleport/ );
		} );
	}
});
