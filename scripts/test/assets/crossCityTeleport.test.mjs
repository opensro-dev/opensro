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
import { test } from "node:test";
import { crossCityTeleportEnabled, loadCrossCityTeleportLinkRows } from "../../build/shared/crossCityTeleport.mjs";
import { computeResourceBuildFingerprint } from "../../build/shared/resourceBuildFingerprint.mjs";

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

test("an unset switch takes the registry default, off", ( t ) => {
	withEnv( t, { SRO_CROSS_CITY_TELEPORT: "" } );
	assert.equal( crossCityTeleportEnabled(), false );
});

test("flipping the switch changes the resource build fingerprint", async ( t ) => {
	withEnv( t, { SRO_CROSS_CITY_TELEPORT: "off" } );
	const off = await computeResourceBuildFingerprint();
	process.env.SRO_CROSS_CITY_TELEPORT = "on";
	const on = await computeResourceBuildFingerprint();
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
