/*
===========================================================================

published-effect-admission.test.mjs - tests for resources.ts, asset-budget.ts,
read-bytes.ts and the worker's effect decoder

Since #3951 an effect request carries one published effect file, never the
whole closure: every published file must fit the request the character
resources actually issue, and a melee hit read at that admission decodes.

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
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { readEffectIndexSync, readEffectProgramSync } from "../../../../scripts/lib/effectCatalog.mjs";

async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const {
	createCharacterResources,
	assetRequestBudget,
	readBytes,
	createEffectDecoder,
	effectProgramFile,
	effectProgramIndexFiles,
	effectProgramModel
} = {
	...(await load( "src/engine/runtime/characters/resources/resources.ts" )),
	...(await load( "src/engine/foundation/assets/asset-budget.ts" )),
	...(await load( "src/engine/foundation/assets/read-bytes.ts" )),
	...(await load( "src/engine/runtime/assets/worker/effects/effects.ts" )),
	...(await load( "src/engine/foundation/assets/effect-programs.ts" ))
};
const HIT = "hiteffect/hit_1_cut_critical.efp";

test("every published effect file fits the actual character-resource request and melee hits decode", async () => {
	const requests = [];
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
		owner.ready( effectProgramModel( HIT ) );
		assert.equal( requests.length, 1 );
		const request = requests[0];
		assert.equal( request.decode, "effect" );
		const index = readEffectIndexSync( CLIENT_PUBLIC_ROOT );
		assert.ok( index.effects.length > 1000, "the native closure holds over a thousand effects" );
		for ( const file of effectProgramIndexFiles( index ) ) {
			const size = readPublishedAssetBytesSync( file, CLIENT_PUBLIC_ROOT ).length;
			assert.ok(
				size <= request.limit,
				`Published ${file} (${size}) exceeds runtime admission ${request.limit}`
			);
		}
		const bytes = readPublishedAssetBytesSync( effectProgramFile( HIT ), CLIENT_PUBLIC_ROOT );
		const admitted = await readBytes( new Response( bytes ).body, request.limit );
		const result = decoder.model( decoder.program( admitted ), readEffectProgramSync( HIT ).meshes, HIT );
		assert.ok( result.model.primitives.length > 0 );
		assert.ok( result.model.clips[0].duration > 0 );
		assert.ok( result.imagePaths.length > 0 );
		// One effect is an ordinary request: no catalogue-sized admission remains.
		assert.equal( assetRequestBudget( "effect" ), 16 * 1048576 );
		assert.equal( assetRequestBudget( "character" ), 16 * 1048576 );
		assert.equal( assetRequestBudget( "effects" ), 32 * 1048576 );
		await assert.rejects( readBytes( new Response( bytes ).body, bytes.length - 1 ), /byte limit/ );
	} finally {
		owner.dispose();
	}
});
