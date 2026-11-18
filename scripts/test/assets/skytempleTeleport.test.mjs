/*
===========================================================================

skytempleTeleport.test.mjs - the Sky Temple fields leave the bake while closed

Port-only, not native: the server keeps the Sky Temple field gates closed,
with every link to them, while SRO_SKYTEMPLE is off
(monster.SkytempleHidesGate). The bake reads the switch the same way, so
the Jangan gate's menu offers the fields exactly when the shard admits
them.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { computeResourceBuildFingerprint } from "../../build/shared/resourceBuildFingerprint.mjs";
import { closedSkytempleIds, skytempleEnabled } from "../../build/shared/skytemple.mjs";

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

// Jangan's gate, a town gate, and two Sky Temple fields, as teleportdata rows.
const ROWS = [
	[ "1", "1", "GATE_CH" ],
	[ "1", "2", "GATE_WC" ],
	[ "1", "349", "GATE_OTHER_SKYTEMPLE_A" ],
	[ "1", "352", "GATE_OTHER_SKYTEMPLE_D" ]
];

test("skytempleEnabled mirrors the server words", ( t ) => {
	withEnv( t, { SRO_SKYTEMPLE: "off" } );
	for ( const word of [ "1", "on", "true", " ON " ] ) {
		process.env.SRO_SKYTEMPLE = word;
		assert.equal( skytempleEnabled(), true, word );
	}
	for ( const word of [ "0", "off", "false", "yes" ] ) {
		process.env.SRO_SKYTEMPLE = word;
		assert.equal( skytempleEnabled(), false, word );
	}
});

test("an unset switch takes the registry default, closed", ( t ) => {
	withEnv( t, { SRO_SKYTEMPLE: "" } );
	assert.equal( skytempleEnabled(), false );
});

test("closed, only the Sky Temple field gates leave the bake; open, none do", () => {
	assert.deepEqual( [ ...closedSkytempleIds( ROWS, false ) ].sort(), [ 349, 352 ] );
	assert.equal( closedSkytempleIds( ROWS, true ).size, 0 );
});

test("flipping the switch changes the resource build fingerprint", async ( t ) => {
	withEnv( t, { SRO_SKYTEMPLE: "off" } );
	const off = await computeResourceBuildFingerprint();
	process.env.SRO_SKYTEMPLE = "on";
	const on = await computeResourceBuildFingerprint();
	assert.notEqual( on.hash, off.hash );
});
