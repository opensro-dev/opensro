/*
===========================================================================

shadow-receiver-exactness.test.mjs - preserve every submitted receiver byte

Recorded pre-optimization outputs include fine/coarse LODs, boundary crossings,
nonflat terrain, duplicate overlays and both projected and blob shadows.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { shadowCases } from "../helpers/shadow-cases.mjs";
const { characterShadowReceiver } = await import( "../../src/engine/foundation/rendering/character-shadow.ts" );
const expected = JSON.parse(
	readFileSync( new URL( "../fixtures/shadow-receiver/before.json", import.meta.url ), "utf8" )
);
test("moving receivers preserve pre-optimization positions, UVs, alpha and index order byte for byte", () => {
	const cases = shadowCases();
	assert.equal( cases.length, expected.hashes.length );
	for ( const [index, input] of cases.entries() ) {
		const mesh = characterShadowReceiver( new Map(), input.projection, input.blob, input.surface );
		let actual = null;
		if ( mesh ) {
			const hash = createHash( "sha256" );
			for ( const field of [ "positions", "uvs", "colors", "indices" ] ) {
				const values = mesh[field];
				hash.update( Buffer.from( values.buffer, values.byteOffset, values.byteLength ) );
			}
			actual = hash.digest( "hex" );
		}
		assert.equal( actual, expected.hashes[index], `receiver ${index}` );
	}
});
