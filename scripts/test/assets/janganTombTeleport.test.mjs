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
import { STUB_FINGERPRINT_KEYS } from "../helpers/fingerprintKeys.mjs";
import { switchServerRoot, withEnv } from "../helpers/shardSwitchFixture.mjs";

const JANGAN_ROW = { env: "SRO_JANGAN_TOMB_TELEPORT", nomadVar: "jangan_tomb_teleport", defaultValue: "off" };

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

// The posture comes from a server root of the test's own: the checkout's
// profile turned the switch on (f55561ce7), which failed a test that read
// it (#4547).
test("an unset switch takes the release profile's value, else the registry default", ( t ) => {
	withEnv( t, { SRO_QINSHI_TOMB: "on", SRO_JANGAN_TOMB_TELEPORT: "" } );
	switchServerRoot( t, JANGAN_ROW, undefined );
	assert.equal( janganTombTeleportEnabled(), false, "no profile entry: the registry default, off" );
});

test("an unset switch follows a profile that turns it on", ( t ) => {
	withEnv( t, { SRO_QINSHI_TOMB: "on", SRO_JANGAN_TOMB_TELEPORT: "" } );
	switchServerRoot( t, JANGAN_ROW, "on" );
	assert.equal( janganTombTeleportEnabled(), true, "the profile turns it on" );
});

test("flipping the switch changes the resource build fingerprint", async ( t ) => {
	withEnv( t, { SRO_QINSHI_TOMB: "on", SRO_JANGAN_TOMB_TELEPORT: "off" } );
	const off = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
	process.env.SRO_JANGAN_TOMB_TELEPORT = "on";
	const on = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
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
