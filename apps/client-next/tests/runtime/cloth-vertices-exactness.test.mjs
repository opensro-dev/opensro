/*
===========================================================================

cloth-vertices-exactness.test.mjs - complete skinning streams and RNG equality

The capture predates the vertex-loop optimization. It covers every packed
Float32 component, not only positions, including disabled and stalled frames.
This establishes preservation of the prior port, not additional native proof.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { clothVertexCase } from "../helpers/cloth-vertex-cases.mjs";
const { createClothVertices } = await import( "../../src/engine/foundation/animation/cloth-vertices.ts" );
const capture = JSON.parse(
	readFileSync( new URL( "../fixtures/cloth/vertices-before.json", import.meta.url ), "utf8" )
);

test("weighted cloth preserves every packed vertex bit and RNG call across option changes and stalls", () => {
	for ( const row of capture.rows ) {
		const fixture = clothVertexCase( row.skinned, row.normals );
		assert.equal( fixture.frames, capture.framesPerCase );
		const cloth = createClothVertices( fixture.primitive, fixture.random ), hash = createHash( "sha256" );
		for ( let frame = 0; frame < fixture.frames; frame++ ) {
			const input = fixture.input( frame );
			const vertices = cloth.update( input.palette, input.seconds, input.enabled, input.motion );
			assert.ok( vertices.every( Number.isFinite ) );
			hash.update( new Uint8Array( vertices.buffer, vertices.byteOffset, vertices.byteLength ) );
		}
		assert.equal( hash.digest( "hex" ), row.sha256, `skinned=${row.skinned}, normals=${row.normals}` );
		assert.equal( fixture.calls(), row.randomCalls );
	}
});
