/*
===========================================================================

muhanOverlay.test.mjs - the Muhan teleport overlay bake gates

Port-only, not native (#3314): the v1.150 textdata has no Muhan rows;
the server appends its committed teleport overlays behind SRO_MUHAN
(monster.MuhanEnabled, default off). The bake must publish the arena
manager hub, the eight gates and their 16 links exactly when the shard
admits them, and a flag-off bake is the retail table alone. An unset
flag takes the shard's deployed posture (profile, else registry
default), so a default build bakes what the default shard spawns.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { buildKnobLines, computeResourceBuildFingerprint } from "../../build/shared/resourceBuildFingerprint.mjs";
import { loadMuhanTeleportDataRows, loadMuhanTeleportLinkRows, muhanEnabled } from "../../build/shared/muhan.mjs";

// The server source root this process started with (fixtureServerRoot
// points it elsewhere for one test).
const ORIGINAL_SERVER_ROOT = process.env.SRO_SERVER_SOURCE_ROOT;

/*
================
withMuhanFlag

Set SRO_MUHAN for one test and restore the caller's value after.
================
*/
function withMuhanFlag( t, value ) {
	const saved = process.env.SRO_MUHAN;
	t.after( () => {
		if ( saved === undefined ) delete process.env.SRO_MUHAN;
		else process.env.SRO_MUHAN = saved;
	} );
	if ( value === undefined ) delete process.env.SRO_MUHAN;
	else process.env.SRO_MUHAN = value;
}

/*
================
fixtureServerRoot

A server source tree holding only what the posture reads: the release
profile (profileValue undefined leaves the key out) and a registry row
with the given default. Pointed at through SRO_SERVER_SOURCE_ROOT for
one test.
================
*/
function fixtureServerRoot( t, profileValue, registryDefault ) {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "sro-muhan-posture-" ) );
	// Restore the value the process started with, whatever order several
	// fixtures' hooks run in.
	t.after( () => {
		if ( ORIGINAL_SERVER_ROOT === undefined ) delete process.env.SRO_SERVER_SOURCE_ROOT;
		else process.env.SRO_SERVER_SOURCE_ROOT = ORIGINAL_SERVER_ROOT;
		fs.rmSync( root, { recursive: true, force: true } );
	} );
	fs.mkdirSync( path.join( root, "config" ), { recursive: true } );
	fs.mkdirSync( path.join( root, "internal", "config", "shardsettings" ), { recursive: true } );
	const profile = profileValue === undefined ? {} : { SRO_MUHAN: profileValue };
	fs.writeFileSync( path.join( root, "config", "shard-settings.json" ), JSON.stringify( profile ) );
	fs.writeFileSync(
		path.join( root, "internal", "config", "shardsettings", "shardsettings.go" ),
		`var Settings = []Setting{\n\t{Env: "SRO_MUHAN", NomadVar: "muhan", Default: "${registryDefault}", Catalogue: true},\n}\n`
	);
	process.env.SRO_SERVER_SOURCE_ROOT = root;
}

test("muhanEnabled mirrors the server flag words", ( t ) => {
	withMuhanFlag( t, "off" );
	for ( const word of [ "1", "on", "true", " ON ", "True" ] ) {
		process.env.SRO_MUHAN = word;
		assert.equal( muhanEnabled(), true, word );
	}
	// monster.MuhanEnabled knows no "yes": the bake must not admit the
	// gates on a word the server reads as off.
	for ( const word of [ "0", "off", "false", "no", "yes", "2" ] ) {
		process.env.SRO_MUHAN = word;
		assert.equal( muhanEnabled(), false, word );
	}
});

test("an unset muhan flag takes the shard's deployed posture", ( t ) => {
	// The checkout's own posture: the registry default is off, so a
	// default build bakes the retail table alone.
	for ( const unset of [ undefined, "", "  " ] ) {
		withMuhanFlag( t, unset );
		assert.equal( muhanEnabled(), false, `unset ${JSON.stringify( unset )} reads the shard default` );
	}
	// The registry default decides when the profile is silent...
	withMuhanFlag( t, undefined );
	fixtureServerRoot( t, undefined, "on" );
	assert.equal( muhanEnabled(), true );
	// ...the deployed profile overrides it...
	fixtureServerRoot( t, "on", "off" );
	assert.equal( muhanEnabled(), true );
	fixtureServerRoot( t, "off", "on" );
	assert.equal( muhanEnabled(), false );
	// ...and an explicit build env still wins over both.
	process.env.SRO_MUHAN = "on";
	assert.equal( muhanEnabled(), true );
});

test("a registry the bake cannot read stops the build", ( t ) => {
	withMuhanFlag( t, undefined );
	fixtureServerRoot( t, undefined, "on" );
	fs.writeFileSync(
		path.join( process.env.SRO_SERVER_SOURCE_ROOT, "internal", "config", "shardsettings", "shardsettings.go" ),
		"var Settings = []Setting{}\n"
	);
	assert.throws( () => muhanEnabled(), /cannot see the shard's posture/ );
});

test("flipping the muhan flag changes the resource build fingerprint", async ( t ) => {
	// Otherwise an up-to-date tree would skip the build and keep the
	// flag-off teleport asset after the flag turns on (lane memos hash
	// the same knob list).
	withMuhanFlag( t, "off" );
	const off = await computeResourceBuildFingerprint();
	process.env.SRO_MUHAN = "on";
	const on = await computeResourceBuildFingerprint();
	assert.notEqual( on.hash, off.hash );
	assert.notEqual( buildKnobLines().join( "\n" ), "" );
});

test("muhan overlay rows are collision-free and complete", () => {
	const rows = loadMuhanTeleportDataRows().rows;
	assert.equal( rows.length, 9, "expected the hub and eight gates" );
	const ids = rows.map( ( cols ) => Number( cols[1] ) );
	assert.deepEqual( [ ...ids ].sort( ( a, b ) => a - b ), [ 227, 228, 229, 230, 231, 232, 233, 234, 235 ] );
	assert.equal( new Set( ids ).size, ids.length, "duplicate overlay teleport id" );
	for ( const cols of rows ) {
		assert.equal( cols.length, 13, `${cols[2]} column count` );
		assert.equal( cols[0].trim(), "1", cols[2] );
	}

	const links = loadMuhanTeleportLinkRows().rows;
	assert.equal( links.length, 16, "expected hub-to-gates and gates-to-town links" );
	for ( const cols of links ) {
		assert.equal( cols.length, 21 );
		assert.equal( cols[3], "0", "fee" );
	}
	const pairs = new Set( links.map( ( cols ) => `${cols[1]}>${cols[2]}` ) );
	for ( let gate = 228; gate <= 235; gate++ ) {
		assert.ok( pairs.has( `227>${gate}` ), `hub link 227>${gate}` );
		assert.ok( pairs.has( `${gate}>5` ), `exit link ${gate}>5` );
	}
});
