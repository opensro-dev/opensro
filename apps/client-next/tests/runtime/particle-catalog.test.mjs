/*
===========================================================================

particle-catalog.test.mjs - tests for program.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const { createEffectPrograms } = await import(
	sourceFileUrl( "src/engine/runtime/assets/worker/effects/program/program.ts" ).href
);
const bytes = new Uint8Array( await readFile( CLIENT_PUBLIC_ROOT + "/assets/effects/programs.json" ) );
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
test("element links come from the LinkMode controller, not only the header (B1F6F0)", () => {
	const path = "skill/europe/wizard_fire_trap_creation.efp";
	const catalog = JSON.parse( new TextDecoder().decode( bytes ) ), effect = structuredClone( catalog.effects[path] );
	// Clear every header link field; the LinkMode controllers still carry them.
	const stack = [ effect.root ];
	while ( stack.length ) {
		const element = stack.pop();
		Object.assign( element, { int0: 0, int1: 0, int2: 0, int3: 0, byte0: 0, byte1: 0 } );
		stack.push( ...element.children );
	}
	const stripped = new TextEncoder().encode( JSON.stringify( { ...catalog, effects: { [path]: effect } } ) );
	const links = model =>
		model.particleGraph.map( e => [
			e.positionDepth,
			e.matrixDepth,
			e.velocityDepth,
			e.followDepth,
			e.keepMatrix,
			e.keepOrigin
		] );
	const shipped = links( createEffectPrograms().decode( bytes, path ).model );
	assert.ok( shipped.some( row => row[3] > 0 ), "the trap's elements follow their parents" );
	assert.deepEqual( links( createEffectPrograms().decode( stripped, path ).model ), shipped );
});
