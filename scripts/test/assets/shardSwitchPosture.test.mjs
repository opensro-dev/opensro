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
import path from "node:path";
import test from "node:test";

import { shardSwitchPosture } from "../../build/shared/shardSwitch.mjs";
import { fixtureServerRoot, registryOf } from "../helpers/shardSwitchFixture.mjs";

const ENV = "SRO_POSTURE_TEST";

/*
================
postureServerRoot

A server root with the given profile and a registry row for ENV.
================
*/
function postureServerRoot( t, profile, registryDefault ) {
	const row = { env: ENV, nomadVar: "posture_test", defaultValue: registryDefault };
	return fixtureServerRoot( t, { profile, registry: registryOf( [ row ] ) } );
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
	const root = postureServerRoot( t, { [ENV]: "on" }, "off" );
	assert.equal( shardSwitchPosture( ENV ), "on" );
	assert.equal( shardSwitchPosture( ENV ), "on" );
	const profile = path.join( root, "config", "shard-settings.json" );
	const rewritten = JSON.stringify( { [ENV]: "no" } );
	assert.equal( rewritten.length, fs.statSync( profile ).size, "the rewrite keeps the size" );
	rewriteLater( profile, rewritten );
	assert.equal( shardSwitchPosture( ENV ), "no" );
});

test("a registry rewrite changes the posture when the profile is silent", t => {
	const root = postureServerRoot( t, {}, "on" );
	assert.equal( shardSwitchPosture( ENV ), "on" );
	const registry = path.join( root, "internal", "config", "shardsettings", "shardsettings.go" );
	rewriteLater( registry, fs.readFileSync( registry, "utf8" ).replace( 'Default: "on"', 'Default: "no"' ) );
	assert.equal( shardSwitchPosture( ENV ), "no" );
});
