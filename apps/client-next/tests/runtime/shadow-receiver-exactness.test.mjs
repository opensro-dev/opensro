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
const { terrainCellKey } = await import( "../../src/engine/foundation/rendering/terrain-interaction.ts" );
const {
	characterShadowReceiver,
	characterShadowTopology,
	projectShadowReceiver,
	shadowReceiverBounds,
	shadowProjection
} = await import( "../../src/engine/foundation/rendering/character-shadow.ts" );
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

/*
================
Retained topology follows moving point rejection and projection
================
*/
test("one clipped topology follows every subcell point without retaining old UVs or fade", () => {
	for ( const input of shadowCases().slice( 0, 20 ) ) {
		const bounds = shadowReceiverBounds( input.projection.point, input.blob );
		const cells = new Map();
		const topology = characterShadowTopology( cells, bounds, input.blob !== undefined, input.surface );
		for ( const offset of [ 0, .000001, 3.125, 9.999999, 12, 19.999999 ] ) {
			const projection = shadowProjection(
				[ bounds.tx * 20 + offset, offset - 7, bounds.tz * 20 + offset ],
				47 + offset
			);
			const actual = projectShadowReceiver( topology, projection, input.blob );
			const expected = characterShadowReceiver( cells, projection, input.blob, input.surface );
			assert.deepEqual( actual, expected, `subcell ${bounds.tx},${bounds.tz} at ${offset}` );
		}
	}
});

/*
================
Raw heightfield topology
================
*/
test("raw heightfield topology preserves alternating diagonals across negative and positive cells", () => {
	const cells = new Map();
	for ( let z = -1; z <= 1; z++ ) {
		for ( let x = -1; x <= 1; x++ ) {
			cells.set( terrainCellKey( x, z ), {
				heights: Float32Array.from( { length: 289 }, ( _, index ) => Math.sin( index + x ) * 7 + z )
			} );
		}
	}
	for ( const blob of [ undefined, 20, 47 ] ) {
		for ( const start of [ -320, -20, 0, 300 ] ) {
			const bounds = shadowReceiverBounds( [ start, 3, start ], blob );
			const topology = characterShadowTopology( cells, bounds, blob !== undefined );
			for ( const offset of [ 0, .000001, 12.5, 19.999999 ] ) {
				const projection = shadowProjection( [ start + offset, offset, start + offset ], 60 );
				assert.deepEqual(
					projectShadowReceiver( topology, projection, blob ),
					characterShadowReceiver( cells, projection, blob )
				);
			}
		}
	}
});
