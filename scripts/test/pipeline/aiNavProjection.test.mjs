/*
===========================================================================

aiNavProjection.test.mjs - the server projection's dungeon route tables

Every AINavData_<region>.DAT in the navmesh folder is packed, bytes intact,
into world-authority/movement/dungeon/ainav-data.json; other files are
ignored, and a folder without any (or no folder) writes nothing.
Synthetic bytes; no game data is read.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { buildAINavProjection } from "../../build/server/aiNavProjection.mjs";

const PROJECTED = path.join( "world-authority", "movement", "dungeon", "ainav-data.json" );

/*
================
withNavmesh

A temporary navmesh folder holding the given files, and a bundle root
beside it.
================
*/
async function withNavmesh( files, check ) {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "ainav-projection-" ) );
	try {
		const navmesh = path.join( root, "navmesh" );
		if ( files ) {
			fs.mkdirSync( navmesh );
			for ( const [name, bytes] of Object.entries( files ) ) {
				fs.writeFileSync( path.join( navmesh, name ), bytes );
			}
		}
		await check( path.join( root, "bundle" ), navmesh );
	} finally {
		fs.rmSync( root, { recursive: true, force: true } );
	}
}

test("no navmesh folder, or one without route tables, adds no file", async () => {
	for ( const files of [ null, { "objectstring.ifo": Buffer.from( "x" ) } ] ) {
		await withNavmesh( files, async ( bundle, navmesh ) => {
			await buildAINavProjection( bundle, navmesh );
			assert.equal( fs.existsSync( path.join( bundle, PROJECTED ) ), false );
		} );
	}
});

test("each route table is packed by region, bytes intact", async () => {
	const files = {
		"AINavData_32777.DAT": Buffer.from( [ 1, 2, 3, 0, 255 ] ),
		"AINavData_32769.DAT": Buffer.from( [ 9, 8, 7 ] ),
		"objectstring.ifo": Buffer.from( "ignored" )
	};
	await withNavmesh( files, async ( bundle, navmesh ) => {
		await buildAINavProjection( bundle, navmesh );
		const projected = JSON.parse( fs.readFileSync( path.join( bundle, PROJECTED ), "utf8" ) );
		assert.equal( projected.format, "sro-server-dungeon-ainav" );
		assert.equal( projected.version, 1 );
		assert.deepEqual( projected.files.map( ( file ) => file.regionId ), [ 32769, 32777 ] );
		for ( const file of projected.files ) {
			const name = `AINavData_${file.regionId}.DAT`;
			assert.equal( file.sourcePath, `navmesh/${name}` );
			assert.equal( file.byteLength, files[name].length );
			assert.deepEqual( Buffer.from( file.rawBase64, "base64" ), files[name] );
		}
	} );
});
