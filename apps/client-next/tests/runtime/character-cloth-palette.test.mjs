/*
===========================================================================

character-cloth-palette.test.mjs - palette sharing preserves rendered cloth

The frozen pre-optimization renderer output includes draw order, per-instance
palettes, cloth vertex bytes, materials and the native presentation RNG trace.
This is an old-versus-new behavioral oracle, not two copies of the new owner.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { captureClothPalettes } from "../helpers/character-cloth-palette-fixture.mjs";
const baseline = JSON.parse(
	readFileSync( new URL( "../fixtures/cloth/palette-owner-before.json", import.meta.url ), "utf8" )
);

test("an unrelated lazy clip cannot reset standing peers' cloth state or random sequence", () => {
	const actual = captureClothPalettes( true, true );
	assert.deepEqual( actual.frames.map( frame => frame.digest ), baseline.frames );
	assert.equal( actual.gpuCalls, 0 );
});

for ( const gpuAvailable of [ false, true ] ) {
	test(`cloth and static palettes retain exact renderer output with GPU callback ${gpuAvailable}`, () => {
		const actual = captureClothPalettes( gpuAvailable );
		assert.equal(
			actual.gpuCalls,
			0,
			"cloth consumers require CPU-valid palette data even when GPU evaluation is available"
		);
		assert.equal( actual.frames.length, baseline.frames.length );
		assert.ok( actual.frames.some( frame => frame.poseEligibility.gpuSamples > 0 ) );
		for ( const frame of actual.frames ) {
			assert.equal( frame.poseEligibility.sharedPaletteSamples, frame.poseEligibility.gpuSamples );
			assert.equal( frame.poseEligibility.clothSamples, frame.poseEligibility.gpuSamples );
			assert.equal( frame.poseEligibility.gpuPaletteSamples, 0 );
		}
		assert.ok(
			actual.boneWriteBytes < baseline.boneWriteBytes,
			"identical body palettes must reduce real queue upload bytes while preserving rendered output"
		);
		for ( let frame = 0; frame < actual.frames.length; frame++ ) {
			for ( const [name, digest] of Object.entries( actual.frames[frame].primitives ) ) {
				assert.equal(
					digest,
					baseline.primitives[name][frame],
					`frame ${frame}, primitive ${name}: instance transforms, palettes or cloth vertices differ`
				);
			}
			assert.equal(
				actual.frames[frame].digest,
				baseline.frames[frame],
				`frame ${frame}: draw order, material or RNG differs`
			);
		}
		assert.ok( actual.frames.some( frame => frame.randomCalls > 0 ), "the oracle must exercise cloth RNG" );
	});
}
