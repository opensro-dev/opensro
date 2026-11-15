/*
===========================================================================

janganTombTeleport.test.mjs - the Jangan gate's tomb floor links in the bake

Port-only, not native: the server appends the Jangan gate's four Qin-Shi
Tomb floor links behind SRO_JANGAN_TOMB_TELEPORT, and only while
SRO_QINSHI_TOMB is on. The bake must read the switch the same way, so
the client publishes the floor rows exactly when the shard admits them.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { janganTombTeleportEnabled, loadJanganTombTeleportLinkRows } from "../../build/shared/janganTombTeleport.mjs";
import { computeResourceBuildFingerprint } from "../../build/shared/resourceBuildFingerprint.mjs";

/*
================
withEnv

Set the switches for one test and restore the caller's values after.
================
*/
function withEnv( t, values ) {
	for ( const [name, value] of Object.entries( values ) ) {
		const saved = process.env[name];
		t.after( () => {
			if ( saved === undefined ) delete process.env[name];
			else process.env[name] = saved;
		} );
		process.env[name] = value;
	}
}

test("janganTombTeleportEnabled mirrors the server words and needs the tomb", ( t ) => {
	withEnv( t, { SRO_QINSHI_TOMB: "on", SRO_JANGAN_TOMB_TELEPORT: "off" } );
	for ( const word of [ "1", "on", "true", "yes", " ON " ] ) {
		process.env.SRO_JANGAN_TOMB_TELEPORT = word;
		assert.equal( janganTombTeleportEnabled(), true, word );
	}
	for ( const word of [ "0", "off", "false", "no" ] ) {
		process.env.SRO_JANGAN_TOMB_TELEPORT = word;
		assert.equal( janganTombTeleportEnabled(), false, word );
	}
	process.env.SRO_JANGAN_TOMB_TELEPORT = "on";
	process.env.SRO_QINSHI_TOMB = "off";
	assert.equal( janganTombTeleportEnabled(), false, "the tomb closed keeps the gate native" );
});

test("an unset switch takes the registry default, off", ( t ) => {
	withEnv( t, { SRO_QINSHI_TOMB: "on", SRO_JANGAN_TOMB_TELEPORT: "" } );
	assert.equal( janganTombTeleportEnabled(), false );
});

test("flipping the switch changes the resource build fingerprint", async ( t ) => {
	withEnv( t, { SRO_QINSHI_TOMB: "on", SRO_JANGAN_TOMB_TELEPORT: "off" } );
	const off = await computeResourceBuildFingerprint();
	process.env.SRO_JANGAN_TOMB_TELEPORT = "on";
	const on = await computeResourceBuildFingerprint();
	assert.notEqual( on.hash, off.hash );
});

test("the overlay links the Jangan gate to the floor arrival rows, floor 4 by corner", () => {
	const rows = loadJanganTombTeleportLinkRows().rows;
	assert.deepEqual( rows.map( ( cols ) => [ cols[1], cols[2], cols[3] ].map( Number ) ), [
		[ 1, 50, 40000 ],
		[ 1, 53, 40000 ],
		[ 1, 120, 40000 ],
		[ 1, 129, 40000 ],
		[ 1, 126, 40000 ],
		[ 1, 128, 40000 ],
		[ 1, 127, 40000 ]
	] );
	for ( const cols of rows ) {
		assert.equal( cols.length, 21 );
	}
});
