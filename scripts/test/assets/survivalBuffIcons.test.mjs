/*
===========================================================================

survivalBuffIcons.test.mjs - owned arena HUD art stays distinct and compact

===========================================================================
*/
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import test from "node:test";

test("each survival buff has original 64px artwork", () => {
	const hashes = new Set();
	for (
		const name of [
			"damage-surge",
			"unstoppable",
			"unkillable",
			"arcane-spring",
			"fire-pit",
			"mirror-shell",
			"chain-reaction",
			"fleetfoot"
		]
	) {
		const bytes = readFileSync( new URL( `../../../docs/features/assets/survival/${name}.png`, import.meta.url ) );
		assert.equal( bytes.subarray( 1, 4 ).toString(), "PNG" );
		assert.equal( bytes.readUInt32BE( 16 ), 64 );
		assert.equal( bytes.readUInt32BE( 20 ), 64 );
		assert.ok( bytes.length < 20000 );
		hashes.add( createHash( "sha256" ).update( bytes ).digest( "hex" ) );
	}
	assert.equal( hashes.size, 8 );
});
