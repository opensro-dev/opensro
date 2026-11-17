/*
===========================================================================

shardSwitchPosture.test.mjs - a cached posture still follows its files

shardSwitchPosture keeps the profile's and the registry's text between
calls (the bake asks once per skill row). A rewrite must still be seen,
even one that keeps the file's size: the cache is keyed on size and
mtime together.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { shardSwitchPosture } from "../../build/shared/shardSwitch.mjs";

const ENV = "SRO_POSTURE_TEST";

/*
================
fixtureServerRoot

A server root with a profile and a registry row for ENV, set as
SRO_SERVER_SOURCE_ROOT for the test.
================
*/
function fixtureServerRoot( t, profile, registryDefault ) {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "sro-posture-" ) );
	fs.mkdirSync( path.join( root, "config" ) );
	fs.mkdirSync( path.join( root, "internal", "config", "shardsettings" ), { recursive: true } );
	fs.writeFileSync( path.join( root, "config", "shard-settings.json" ), JSON.stringify( profile ) );
	fs.writeFileSync(
		path.join( root, "internal", "config", "shardsettings", "shardsettings.go" ),
		`var Settings = []Setting{\n\t{Env: "${ENV}", NomadVar: "posture_test", Default: "${registryDefault}"},\n}\n`
	);
	const previous = process.env.SRO_SERVER_SOURCE_ROOT;
	process.env.SRO_SERVER_SOURCE_ROOT = root;
	t.after( () => {
		if ( previous === undefined ) delete process.env.SRO_SERVER_SOURCE_ROOT;
		else process.env.SRO_SERVER_SOURCE_ROOT = previous;
		fs.rmSync( root, { recursive: true, force: true } );
	} );
	return root;
}

/*
================
rewriteLater

Writes a file and moves its mtime a minute on, so the rewrite is a new
stat even on a file system with a coarse clock.
================
*/
function rewriteLater( file, text ) {
	const before = fs.statSync( file ).mtime;
	fs.writeFileSync( file, text );
	const later = new Date( before.getTime() + 60_000 );
	fs.utimesSync( file, later, later );
}

test("a same-size profile rewrite changes the posture", t => {
	const root = fixtureServerRoot( t, { [ENV]: "on" }, "off" );
	assert.equal( shardSwitchPosture( ENV ), "on" );
	assert.equal( shardSwitchPosture( ENV ), "on" );
	const profile = path.join( root, "config", "shard-settings.json" );
	const rewritten = JSON.stringify( { [ENV]: "no" } );
	assert.equal( rewritten.length, fs.statSync( profile ).size, "the rewrite keeps the size" );
	rewriteLater( profile, rewritten );
	assert.equal( shardSwitchPosture( ENV ), "no" );
});

test("a registry rewrite changes the posture when the profile is silent", t => {
	const root = fixtureServerRoot( t, {}, "on" );
	assert.equal( shardSwitchPosture( ENV ), "on" );
	const registry = path.join( root, "internal", "config", "shardsettings", "shardsettings.go" );
	rewriteLater( registry, fs.readFileSync( registry, "utf8" ).replace( 'Default: "on"', 'Default: "no"' ) );
	assert.equal( shardSwitchPosture( ENV ), "no" );
});
