/*
===========================================================================

missionPresentationOverlay.test.mjs - overlay monsters publish a recovery period

The client times a knocked-down monster's get-up by its characterdata
recovery period (column 88). The presentation build read only the retail
shards, so the Qin-Shi overlay monsters the flag-on server spawns had none:
"Missing native recovery duration MOB_TQ_TOMBBUGGHOST" on r202.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { readCharacterRows } from "../../build/data/buildMissionPresentationAsset.mjs";

const COL_RECOVERY = 88;
const ROW_WIDTH = 100;

/*
================
characterRow

A characterdata row: service, id, codename, recovery period.
================
*/
function characterRow( service, id, codename, recovery ) {
	const cols = Array( ROW_WIDTH ).fill( "0" );
	cols[0] = String( service );
	cols[1] = String( id );
	cols[2] = codename;
	cols[COL_RECOVERY] = String( recovery );
	return cols;
}

/*
================
textdataWith

A scratch textdata folder holding one characterdata shard, encoded as
the retail files are.
================
*/
function textdataWith( t, rows ) {
	const dir = fs.mkdtempSync( path.join( os.tmpdir(), "mission-presentation-" ) );
	t.after( () => fs.rmSync( dir, { recursive: true, force: true } ) );
	// Retail textdata is UTF-16LE with a BOM and CRLF records.
	const text = "\ufeff" + rows.map( r => r.join( "\t" ) + "\r\n" ).join( "" );
	fs.writeFileSync( path.join( dir, "characterdata_100.txt" ), Buffer.from( text, "utf16le" ) );
	return dir;
}

test("an overlay monster's recovery period is published beside the retail rows", t => {
	const textdata = textdataWith( t, [ characterRow( 1, 1954, "MOB_CH_TIGERWOMAN", 2000 ) ] );
	const { recoveryByCodename, charactersById } = readCharacterRows( textdata, [
		characterRow( 1, 14810, "MOB_TQ_TOMBBUGGHOST", 3000 )
	] );
	assert.deepEqual( recoveryByCodename, { MOB_CH_TIGERWOMAN: 2000, MOB_TQ_TOMBBUGGHOST: 3000 } );
	assert.equal( charactersById.get( "14810" ).codename, "MOB_TQ_TOMBBUGGHOST" );
});

test("an out-of-service overlay row is not published", t => {
	const textdata = textdataWith( t, [] );
	const { recoveryByCodename } = readCharacterRows( textdata, [
		characterRow( 0, 14810, "MOB_TQ_TOMBBUGGHOST", 3000 )
	] );
	assert.deepEqual( recoveryByCodename, {} );
});

test("an overlay row without a valid period refuses the build", t => {
	const textdata = textdataWith( t, [] );
	assert.throws(
		() => readCharacterRows( textdata, [ characterRow( 1, 14810, "MOB_TQ_TOMBBUGGHOST", "x" ) ] ),
		/Invalid native recovery period MOB_TQ_TOMBBUGGHOST/
	);
});
