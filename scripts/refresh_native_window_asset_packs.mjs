/*
===========================================================================

refresh_native_window_asset_packs.mjs - publish native RGB16 window textures

scripts/tools/refresh_native_window_images.py re-decodes the window DDJ
families whose RGB16 payloads the generic converter cannot express and
prints the public paths it wrote; this packs them.

===========================================================================
*/
import { execFileSync } from "node:child_process";
import path from "node:path";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";
import { pythonExecutable } from "./build/shared/pythonRun.mjs";

const IMAGE_SCRIPT = path.join( import.meta.dirname, "tools", "refresh_native_window_images.py" );

await withGeneratedAssetsLock( "Native window texture publication", async () => {
	const files = JSON.parse(
		execFileSync( pythonExecutable(), [ IMAGE_SCRIPT ], { encoding: "utf8", env: process.env } )
	);
	await publishLooseFamily( { name: "native-window", files, defaultGroup: "native-ui" } );
	console.log( `Published ${files.length} native RGB16 window textures.` );
} );
