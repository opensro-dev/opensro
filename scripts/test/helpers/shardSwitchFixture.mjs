/*
===========================================================================

shardSwitchFixture.mjs - a shard switch's posture under the test's control

A bake switch reads its posture from the server source tree: the release
profile (apps/server/config/shard-settings.json), else the registry row's
Default (internal/config/shardsettings/shardsettings.go). A test that let
the checkout decide broke the day the shard turned the switch on (#4547).
Each switch suite builds a server root of its own through here, and sets
the switch environment with withEnv; both restore the caller's values.

===========================================================================
*/
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

// The server root the process started with: every fixture restores it,
// whatever order several fixtures' after-hooks run in.
const ORIGINAL_SERVER_ROOT = process.env.SRO_SERVER_SOURCE_ROOT;

/*
================
withEnv

Set environment values for one test and restore the caller's after.
================
*/
export function withEnv( t, values ) {
	for ( const [name, value] of Object.entries( values ) ) {
		const saved = process.env[name];
		t.after( () => {
			if ( saved === undefined ) delete process.env[name];
			else process.env[name] = saved;
		} );
		process.env[name] = value;
	}
}

/*
================
registryRow

One shardsettings.go Settings row, as the posture reader parses it.
================
*/
export function registryRow( { env, nomadVar, defaultValue, catalogue = false } ) {
	const tail = catalogue ? ", Catalogue: true" : "";
	return `\t{Env: "${env}", NomadVar: "${nomadVar}", Default: "${defaultValue}"${tail}},`;
}

/*
================
registryOf

A shardsettings.go holding only the given rows.
================
*/
export function registryOf( rows ) {
	return `var Settings = []Setting{\n${rows.map( registryRow ).join( "\n" )}\n}\n`;
}

/*
================
fixtureServerRoot

A server source tree holding only what a posture reads: the release
profile object and the registry text. Set as SRO_SERVER_SOURCE_ROOT for
the test and removed after it. Returns the root.
================
*/
export function fixtureServerRoot( t, { profile, registry } ) {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "sro-switch-" ) );
	t.after( () => {
		if ( ORIGINAL_SERVER_ROOT === undefined ) delete process.env.SRO_SERVER_SOURCE_ROOT;
		else process.env.SRO_SERVER_SOURCE_ROOT = ORIGINAL_SERVER_ROOT;
		fs.rmSync( root, { recursive: true, force: true } );
	} );
	fs.mkdirSync( path.join( root, "config" ), { recursive: true } );
	fs.mkdirSync( path.join( root, "internal", "config", "shardsettings" ), { recursive: true } );
	fs.writeFileSync( path.join( root, "config", "shard-settings.json" ), JSON.stringify( profile ) );
	fs.writeFileSync( path.join( root, "internal", "config", "shardsettings", "shardsettings.go" ), registry );
	process.env.SRO_SERVER_SOURCE_ROOT = root;
	return root;
}

/*
================
switchServerRoot

fixtureServerRoot for one switch: row is its registry row, profileValue
its release-profile value (undefined leaves the key out).
================
*/
export function switchServerRoot( t, row, profileValue ) {
	const profile = profileValue === undefined ? {} : { [row.env]: profileValue };
	return fixtureServerRoot( t, { profile, registry: registryOf( [ row ] ) } );
}
