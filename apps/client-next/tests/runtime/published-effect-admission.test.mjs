/*
===========================================================================

published-effect-admission.test.mjs - tests for the client modules it imports

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

async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const { createCharacterResources, assetRequestBudget, readBytes, createEffectDecoder } = {
	...(await load( "src/engine/runtime/characters/resources/resources.ts" )),
	...(await load( "src/engine/foundation/assets/asset-budget.ts" )),
	...(await load( "src/engine/foundation/assets/read-bytes.ts" )),
	...(await load( "src/engine/runtime/assets/worker/effects/effects.ts" ))
};

test("published EasyFX closure fits the actual character-resource request and decodes melee hits", async () => {
	const bytes = await readFile( CLIENT_PUBLIC_ROOT + "/assets/effects/programs.json" ), requests = [];
	const owner = createCharacterResources(
		{
			available: () => 1,
			request( url, limit, decode ) {
				requests.push( { url, limit, decode } );
				return 1;
			},
			take: () => null,
			cancel() {}
		},
		{ retainCharacterModels() {} },
		"http://fixture.invalid"
	);
	const decoder = createEffectDecoder();
	try {
		owner.begin( 0 );
		owner.ready( "/assets/effects/programs.json#hiteffect%2Fhit_1_cut_critical.efp" );
		assert.equal( requests.length, 1 );
		const request = requests[0];
		assert.equal( request.decode, "effect" );
		assert.ok(
			bytes.length <= request.limit,
			`Published catalog ${bytes.length} exceeds runtime admission ${request.limit}`
		);
		const admitted = await readBytes( new Response( bytes ).body, request.limit );
		const result = decoder.model( admitted, "hiteffect/hit_1_cut_critical.efp" );
		assert.ok( result.model.primitives.length > 0 );
		assert.ok( result.model.clips[0].duration > 0 );
		assert.ok( result.imagePaths.length > 0 );
		// The expanded catalog does not expand individual model or image admission.
		assert.equal( assetRequestBudget( "character" ), 16 * 1048576 );
		assert.equal( assetRequestBudget( "effects" ), 32 * 1048576 );
		await assert.rejects( readBytes( new Response( bytes ).body, bytes.length - 1 ), /byte limit/ );
	} finally {
		decoder.dispose();
		owner.dispose();
	}
});
