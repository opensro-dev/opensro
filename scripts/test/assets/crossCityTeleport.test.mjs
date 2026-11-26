/*
===========================================================================

crossCityTeleport.test.mjs - the direct town gate links in the bake

Port-only, not native: the server appends the direct town-to-town gate
links behind SRO_CROSS_CITY_TELEPORT. The bake must read the switch the
same way, so the client publishes the rows exactly when the shard admits
them.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { crossCityTeleportEnabled, loadCrossCityTeleportLinkRows } from "../../build/shared/crossCityTeleport.mjs";
import { computeResourceBuildFingerprint } from "../../build/shared/resourceBuildFingerprint.mjs";
import { STUB_FINGERPRINT_KEYS } from "../helpers/fingerprintKeys.mjs";

// The town gates in Silk Road order: Jangan, Donwhang, Hotan, Samarkand,
// Constantinople.
const TOWNS = [ 1, 2, 11, 34, 25 ];

// What one native town gate trip costs.
const HOP_FEE = 5000;

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

test("crossCityTeleportEnabled mirrors the server words", ( t ) => {
	withEnv( t, { SRO_CROSS_CITY_TELEPORT: "off" } );
	for ( const word of [ "1", "on", "true", "yes", " ON " ] ) {
		process.env.SRO_CROSS_CITY_TELEPORT = word;
		assert.equal( crossCityTeleportEnabled(), true, word );
	}
	for ( const word of [ "0", "off", "false", "no" ] ) {
		process.env.SRO_CROSS_CITY_TELEPORT = word;
		assert.equal( crossCityTeleportEnabled(), false, word );
	}
});

/*
================
fixtureServerRoot

A server root whose profile and registry row for the switch are the
test's, set as SRO_SERVER_SOURCE_ROOT for the test.
================
*/
function fixtureServerRoot( t, profileValue ) {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "sro-cross-city-" ) );
	const previous = process.env.SRO_SERVER_SOURCE_ROOT;
	t.after( () => {
		if ( previous === undefined ) delete process.env.SRO_SERVER_SOURCE_ROOT;
		else process.env.SRO_SERVER_SOURCE_ROOT = previous;
		fs.rmSync( root, { recursive: true, force: true } );
	} );
	fs.mkdirSync( path.join( root, "config" ), { recursive: true } );
	fs.mkdirSync( path.join( root, "internal", "config", "shardsettings" ), { recursive: true } );
	const profile = profileValue === undefined ? {} : { SRO_CROSS_CITY_TELEPORT: profileValue };
	fs.writeFileSync( path.join( root, "config", "shard-settings.json" ), JSON.stringify( profile ) );
	fs.writeFileSync(
		path.join( root, "internal", "config", "shardsettings", "shardsettings.go" ),
		`var Settings = []Setting{
	{Env: "SRO_CROSS_CITY_TELEPORT", NomadVar: "cross_city_teleport", Default: "off"},
}
`
	);
	process.env.SRO_SERVER_SOURCE_ROOT = root;
}

test("an unset switch takes the release profile's value, else the registry default", ( t ) => {
	withEnv( t, { SRO_CROSS_CITY_TELEPORT: "" } );
	fixtureServerRoot( t, undefined );
	assert.equal( crossCityTeleportEnabled(), false, "no profile entry: the registry default, off" );
});

test("an unset switch follows a profile that turns it on", ( t ) => {
	withEnv( t, { SRO_CROSS_CITY_TELEPORT: "" } );
	fixtureServerRoot( t, "on" );
	assert.equal( crossCityTeleportEnabled(), true );
});

test("flipping the switch changes the resource build fingerprint", async ( t ) => {
	withEnv( t, { SRO_CROSS_CITY_TELEPORT: "off" } );
	const off = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
	process.env.SRO_CROSS_CITY_TELEPORT = "on";
	const on = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
	assert.notEqual( on.hash, off.hash );
});

test("the overlay links every town pair that is not neighbours, 5000 a trip", () => {
	const rows = loadCrossCityTeleportLinkRows().rows;
	const want = [];
	for ( const [i, source] of TOWNS.entries() ) {
		for ( const [j, target] of TOWNS.entries() ) {
			const hops = Math.abs( i - j );
			if ( hops > 1 ) want.push( `${source}>${target}:${hops * HOP_FEE}` );
		}
	}
	const got = rows.map( ( cols ) => `${cols[1]}>${cols[2]}:${cols[3]}` );
	assert.deepEqual( got.sort(), want.sort() );
	for ( const cols of rows ) {
		assert.equal( cols.length, 21 );
	}
});
