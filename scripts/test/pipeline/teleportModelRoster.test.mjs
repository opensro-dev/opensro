/*
===========================================================================

teleportModelRoster.test.mjs - which teleport buildings bake their own model

A gate whose AssocFileObj names a model is drawn by the native client
(CITeleportGate 8762E0); a gate naming xxx keeps its map architecture.
Fixture tables only, in an isolated directory.

===========================================================================
*/

import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { resolveNpcModel } from "../../build/char/buildNpcModelAssets.mjs";
import { ASSOC_FILE_OBJ_COLUMN, loadTeleportModelRows } from "../../build/char/teleportModelRoster.mjs";

// The UTF-16LE byte-order mark the native tables open with.
const BYTE_ORDER_MARK = 0xfeff;

/*
================
building

One teleportbuilding row: 58 columns, its model in AssocFileObj.
================
*/
function building( service, identity, codename, model ) {
	const row = Array( 58 ).fill( "0" );
	row[0] = service;
	row[1] = identity;
	row[2] = codename;
	row[48] = "100";
	for ( let at = ASSOC_FILE_OBJ_COLUMN; at < ASSOC_FILE_OBJ_COLUMN + 5; at++ ) row[at] = "xxx";
	row[ASSOC_FILE_OBJ_COLUMN] = model;
	return row;
}

/*
================
fixtureDir
================
*/
async function fixtureDir( t, rows ) {
	const dir = await fs.mkdtemp( path.join( os.tmpdir(), "sro-teleport-models-" ) );
	t.after( () => fs.rm( dir, { recursive: true, force: true } ) );
	const text = String.fromCharCode( BYTE_ORDER_MARK ) + rows.map( row => row.join( "\t" ) ).join( "\r\n" ) + "\r\n";
	await fs.writeFile( path.join( dir, "teleportbuilding.txt" ), Buffer.from( text, "utf16le" ) );
	return dir;
}

test("only in-service gates naming a model bake one", async ( t ) => {
	const dir = await fixtureDir( t, [
		building( "1", "2094", "STORE_CH_GATE", "xxx" ),
		building( "1", "19076", "INS_QUEST_TELEPORT", "quest\\ins_quest_teleport.bsr" ),
		building( "0", "19077", "STORE_RETIRED_GATE", "quest\\teleport01.bsr" ),
		building( "1", "40790", "STORE_AR_GATE_M", "Bldg\\Arabia\\Bagh_City\\bagh_city_portal.bsr" )
	] );
	const rows = loadTeleportModelRows( dir );
	assert.deepEqual( rows.map( row => row[2] ), [ "INS_QUEST_TELEPORT", "STORE_AR_GATE_M" ] );
});

test("a gate's row resolves its model like a character's", async ( t ) => {
	const dir = await fixtureDir( t, [
		building( "1", "40790", "STORE_AR_GATE_M", "Bldg\\Arabia\\Bagh_City\\bagh_city_portal.bsr" )
	] );
	const rows = new Map( loadTeleportModelRows( dir ).map( row => [ row[2], row ] ) );
	const model = resolveNpcModel( "STORE_AR_GATE_M", rows );
	assert.equal( model.refObjId, 40790 );
	assert.equal( model.bsrPath, "res/bldg/arabia/bagh_city/bagh_city_portal.bsr" );
	assert.equal( model.scalePercent, 100 );
});
