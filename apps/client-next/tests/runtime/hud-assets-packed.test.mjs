/*
===========================================================================

hud-assets-packed.test.mjs - every file the HUD requires ships in a pack

The client loads only paths the pack manifest lists; a loose copy beside it
is never fetched. config\command.txt became a required HUD resource while
the pack builder left it loose, and generation 48 shipped without it
(2026-10-10). This checks the built manifest against the HUD's own list.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";

const { HUD_REQUIRED_ASSETS } = await import( "../../src/engine/runtime/ui/hud/resources.ts" );
const manifestPath = CLIENT_PUBLIC_ROOT + "/assets/packs/manifest.json";

test( "every required HUD asset is listed in the built pack manifest", {
	skip: !existsSync( manifestPath ) && "no built asset packs"
}, () => {
	const packed = new Set(
		JSON.parse( readFileSync( manifestPath, "utf8" ) ).assets.map( entry => entry.path.toLowerCase() )
	);
	const missing = HUD_REQUIRED_ASSETS.filter( path =>
		!packed.has( path.toLowerCase() ) && !packed.has( path.toLowerCase() + ".gz" )
	);
	assert.deepEqual( missing, [], "required HUD assets missing from the pack manifest" );
} );
