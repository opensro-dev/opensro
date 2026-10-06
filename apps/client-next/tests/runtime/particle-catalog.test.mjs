/*
===========================================================================

particle-catalog.test.mjs - tests for program.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const { createEffectPrograms } = await import(
	sourceFileUrl( "src/engine/runtime/assets/worker/effects/program/program.ts" ).href
);
const bytes = new Uint8Array( await readFile( "../../.generated/client-public/assets/effects/programs.json" ) );
test("stone keeps its native source-colour blend pair and temptation retains loop identity", () => {
	const decoder = createEffectPrograms(),
		stone = decoder.decode( bytes, "battle/status_bad_stone_on.efp" ).model,
		temptation = decoder.decode( bytes, "battle/status_bad_temptation.efp" ).model;
	// The resource's own D3D pair: SRCCOLOR (3) as the source factor.
	assert.ok( stone.primitives.some( p => p.geometry.material.blendPair?.source === 3 ) );
	assert.equal( temptation.particleGraph.filter( e => e.loop ).length, 2 );
	assert.ok( temptation.particleGraph.filter( e => e.loop ).every( e => e.frames === 20 ) );
});
test("every published effect decodes with native fog exclusion", () => {
	// battle/status_bad_sleep.efp carries the only SetSpherePos flag 0 (0x37),
	// whose native scratch input is ported by its intent (particle-program.ts).
	const decoder = createEffectPrograms(), catalog = JSON.parse( new TextDecoder().decode( bytes ) ), failures = [];
	for ( const path of Object.keys( catalog.effects ) ) {
		try {
			const { model } = decoder.decode( bytes, path );
			for ( const primitive of model.primitives ) {
				assert.equal( primitive.geometry.material.fogDisabled, true, path );
			}
		} catch ( error ) {
			failures.push( { path, error: error.message } );
		}
	}
	assert.deepEqual( failures, [] );
});
