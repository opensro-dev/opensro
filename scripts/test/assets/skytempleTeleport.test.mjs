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
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { computeResourceBuildFingerprint } from "../../build/shared/resourceBuildFingerprint.mjs";
import { STUB_FINGERPRINT_KEYS } from "../helpers/fingerprintKeys.mjs";
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

/*
================
fixtureServerRoot

A server root whose profile and registry row for the switch are the
test's, set as SRO_SERVER_SOURCE_ROOT for the test, so the checkout's own
shard-settings.json (which turns the fields on) does not decide it.
================
*/
function fixtureServerRoot( t, profileValue ) {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "sro-skytemple-" ) );
	const previous = process.env.SRO_SERVER_SOURCE_ROOT;
	t.after( () => {
		if ( previous === undefined ) delete process.env.SRO_SERVER_SOURCE_ROOT;
		else process.env.SRO_SERVER_SOURCE_ROOT = previous;
		fs.rmSync( root, { recursive: true, force: true } );
	} );
	fs.mkdirSync( path.join( root, "config" ), { recursive: true } );
	fs.mkdirSync( path.join( root, "internal", "config", "shardsettings" ), { recursive: true } );
	const profile = profileValue === undefined ? {} : { SRO_SKYTEMPLE: profileValue };
	fs.writeFileSync( path.join( root, "config", "shard-settings.json" ), JSON.stringify( profile ) );
	fs.writeFileSync(
		path.join( root, "internal", "config", "shardsettings", "shardsettings.go" ),
		`var Settings = []Setting{
	{Env: "SRO_SKYTEMPLE", NomadVar: "skytemple", Default: "off", Catalogue: true},
}
`
	);
	process.env.SRO_SERVER_SOURCE_ROOT = root;
}

test("an unset switch takes the release profile's value, else the registry default", ( t ) => {
	withEnv( t, { SRO_SKYTEMPLE: "" } );
	fixtureServerRoot( t, undefined );
	assert.equal( skytempleEnabled(), false, "no profile entry: the registry default, closed" );
});

test("an unset switch follows a profile that opens the fields", ( t ) => {
	withEnv( t, { SRO_SKYTEMPLE: "" } );
	fixtureServerRoot( t, "on" );
	assert.equal( skytempleEnabled(), true );
});

test("closed, only the Sky Temple field gates leave the bake; open, none do", () => {
	assert.deepEqual( [ ...closedSkytempleIds( ROWS, false ) ].sort(), [ 349, 352 ] );
	assert.equal( closedSkytempleIds( ROWS, true ).size, 0 );
});

test("flipping the switch changes the resource build fingerprint", async ( t ) => {
	withEnv( t, { SRO_SKYTEMPLE: "off" } );
	const off = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
	process.env.SRO_SKYTEMPLE = "on";
	const on = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
	assert.notEqual( on.hash, off.hash );
});
