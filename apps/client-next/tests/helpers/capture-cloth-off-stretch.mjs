/*
===========================================================================

capture-cloth-off-stretch.mjs - freeze the renderer's far-cloth oracle

The far-cloth scenario (character-cloth-palette-fixture.mjs offStretch)
drawn by the renderer as it was before off cloth frames stopped building a
CPU skeleton: every off frame then anchored the cloth from this frame's
palette. Run from a checkout whose apps/client-next/src tree matches
BASELINE_COMMIT, with this generator, the fixture and cloth-vertex-cases.mjs
restored into it from the commit introducing this oracle, without
replacing src. Command from apps/client-next:
node tests/helpers/capture-cloth-off-stretch.mjs

===========================================================================
*/
import { execFileSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { captureClothPalettes } from "./character-cloth-palette-fixture.mjs";
const BASELINE_COMMIT = "c6b42997";
const root = fileURLToPath( new URL( "../../../../", import.meta.url ) );
execFileSync( "git", [ "diff", "--exit-code", BASELINE_COMMIT, "--", "apps/client-next/src" ], {
	cwd: root,
	stdio: "pipe"
} );
const result = captureClothPalettes( true, false, { gpuTakes: true, offStretch: "mixed" } );
const fixture = {
	provenance: {
		sourceCommit: BASELINE_COMMIT,
		owner: "apps/client-next/src/engine/foundation/animation/cloth-stream.ts",
		command: "node tests/helpers/capture-cloth-off-stretch.mjs",
		scope:
			"The far-cloth scenario: whole-frame draw order and presentation RNG, per-primitive palettes and cloth vertices, and every word of every cloth stream, each frame."
	},
	frames: result.frames.map( frame => frame.digest ),
	streams: result.frames.map( frame => frame.streams ),
	primitives: Object.fromEntries(
		Object.keys( result.frames[0].primitives ).map( name => [
			name,
			result.frames.map( frame => frame.primitives[name] )
		] )
	)
};
writeFileSync(
	new URL( "../fixtures/cloth/off-stretch-before.json", import.meta.url ),
	JSON.stringify( fixture, null, "\t" ) + "\n"
);
