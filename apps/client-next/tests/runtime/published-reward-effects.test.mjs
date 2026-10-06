/*
===========================================================================

published-reward-effects.test.mjs - tests for the client modules it imports

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
import { readFileSync, existsSync } from "node:fs";
const source = "src/engine/runtime/assets/worker/effects/program/program.ts";

const { createEffectPrograms } = await import( sourceFileUrl( source ).href );
const bytes = readFileSync( CLIENT_PUBLIC_ROOT + "/assets/effects/programs.json" );
for (
	const path of [
		"system/system_levelup.efp",
		"battle/hwan_g.efp",
		"battle/hwan_y.efp",
		"battle/hwan_v.efp",
		"battle/hwn_blue_indraft.efp",
		"battle/hwn_red_indraft.efp",
		"battle/hwn_violet_indraft.efp"
	]
) {
	test( "published reward program admits geometry and textures: " + path, () => {
		const decoder = createEffectPrograms(), { model, imagePaths } = decoder.decode( bytes, path );
		assert.ok( model.primitives.length > 0 );
		assert.ok( model.clips.some( c => c.duration > 0 ) );
		for ( const image of imagePaths ) assert.ok( existsSync( CLIENT_PUBLIC_ROOT + image ), image );
		decoder.clear();
	} );
}
