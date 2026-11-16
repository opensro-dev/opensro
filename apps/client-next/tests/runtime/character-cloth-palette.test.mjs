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

test("moving cloth uploads current placements through membership, fade, lights, preview and device changes", () => {
	const actual = captureClothPalettes( false, false, { moving: true, variants: true } );
	assert.ok( actual.frames.every( frame => frame.draws === 0 || frame.clothPlacementWrites > 0 ) );
});

test("an unrelated lazy clip cannot reset standing peers' cloth state or random sequence", () => {
	const actual = captureClothPalettes( true, true );
	assert.deepEqual( actual.frames.map( frame => frame.digest ), baseline.frames );
	assert.ok( actual.gpuCalls > 0, "cloth palettes are offered to the GPU" );
});

for ( const gpuAvailable of [ false, true ] ) {
	test(`cloth and static palettes retain exact renderer output with GPU callback ${gpuAvailable}`, () => {
		const actual = captureClothPalettes( gpuAvailable );
		// Cloth batches offer their palettes to the GPU as the body's are; this
		// device declines, so the CPU builds them and the output is unchanged.
		assert.equal( actual.gpuCalls > 0, gpuAvailable, "cloth palettes are offered to the GPU" );
		assert.equal( actual.frames.length, baseline.frames.length );
		assert.ok(
			actual.frames.some( frame => frame.draws > 0 && frame.clothPlacementWrites === 0 ),
			"retained cloth with unchanged placement must keep simulating without placement uploads"
		);
		assert.ok( actual.frames.some( frame => frame.poseEligibility.gpuSamples > 0 ) );
		for ( const frame of actual.frames ) {
			assert.equal( frame.poseEligibility.sharedPaletteSamples, frame.poseEligibility.gpuSamples );
			assert.equal( frame.poseEligibility.clothSamples, frame.poseEligibility.gpuSamples );
			assert.equal( frame.poseEligibility.gpuPaletteSamples, frame.poseEligibility.gpuSamples );
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

test("with the GPU sampling cloth batches' palettes, the output is the baseline's and the CPU builds fewer skeletons", () => {
	const declined = captureClothPalettes( true ), sampled = captureClothPalettes( true, false, { gpuTakes: true } );
	assert.ok( sampled.gpuCalls > 0 );
	for ( let frame = 0; frame < sampled.frames.length; frame++ ) {
		for ( const [name, digest] of Object.entries( sampled.frames[frame].primitives ) ) {
			assert.equal(
				digest,
				baseline.primitives[name][frame],
				`frame ${frame}, primitive ${name}: palettes or cloth vertices differ`
			);
		}
		assert.equal( sampled.frames[frame].digest, baseline.frames[frame], `frame ${frame}: draws or RNG differ` );
	}
	// The cloth solver asks for its skeleton only on the frames it re-anchors.
	assert.ok(
		sampled.cpuEvaluations < declined.cpuEvaluations,
		`${sampled.cpuEvaluations} CPU skeletons with the GPU, ${declined.cpuEvaluations} without`
	);
});

const offStretch = JSON.parse(
	readFileSync( new URL( "../fixtures/cloth/off-stretch-before.json", import.meta.url ), "utf8" )
);
for ( const gpuTakes of [ false, true ] ) {
	test(`wearers crossing the cloth distance draw and step as before, every stream word equal (GPU sampling ${gpuTakes})`, () => {
		const actual = captureClothPalettes( true, false, { gpuTakes, offStretch: "mixed" } );
		assert.equal( actual.frames.length, offStretch.frames.length );
		for ( let frame = 0; frame < actual.frames.length; frame++ ) {
			for ( const [name, digest] of Object.entries( actual.frames[frame].primitives ) ) {
				assert.equal( digest, offStretch.primitives[name][frame], `frame ${frame}, primitive ${name}` );
			}
			assert.equal(
				actual.frames[frame].streams,
				offStretch.streams[frame],
				`frame ${frame}: cloth stream words`
			);
			assert.equal(
				actual.frames[frame].digest,
				offStretch.frames[frame],
				`frame ${frame}: draws or RNG differ`
			);
		}
		assert.ok( actual.frames.some( frame => frame.randomCalls > 0 ), "the scenario steps cloth" );
	});
}

test("a wearer beyond the cloth distance builds no CPU skeleton after its first frame there", () => {
	const actual = captureClothPalettes( true, false, { gpuTakes: true, offStretch: "far" } );
	// Frame 0 and the device loss at frame 60 make every batch, whose cloth
	// stops on its first frame: those frames anchor it from a skeleton.
	const built = actual.frames.flatMap( ( frame, i ) =>
		i > 0 && frame.cpuEvaluations > actual.frames[i - 1].cpuEvaluations ? [ i ] : []
	);
	assert.deepEqual( built, [ 60 ], "CPU skeletons only where a batch is made" );
	assert.equal( actual.frames.reduce( ( sum, frame ) => sum + frame.randomCalls, 0 ), 0, "nothing steps" );
});
