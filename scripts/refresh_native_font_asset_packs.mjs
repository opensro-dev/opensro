/*
===========================================================================

refresh_native_font_asset_packs.mjs - republish the native UI font atlas

Rebuilds the font atlas and repacks its JSON and PNG in the startup-resident
game-data and game-images groups, then archives the packs they superseded.

===========================================================================
*/
// First: it sizes libuv's thread pool before anything starts it.
import "./build/shared/buildParallelism.mjs";
import path from "node:path";
import { buildFontResources } from "./build/fonts.mjs";
import { refreshPrecompressedSidecars } from "./build/generatedManifestSidecars.mjs";
import { refreshPackGroups, timedStep } from "./build/shared/packGroupRefresh.mjs";
import { publicRoot } from "./build/world/paths.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const ATLAS_JSON = path.join( publicRoot, "assets", "fonts", "native-ui-font-atlas.json" );
const timed = timedStep( "native-font-refresh" );

await withGeneratedAssetsLock( "native-font asset-pack refresh", async () => {
	await timed( "font publication", buildFontResources );
	await timed( "font JSON sidecars", () => refreshPrecompressedSidecars( [ ATLAS_JSON ], { onlyWhenStale: true } ) );
	const { totals, webManifest } = await refreshPackGroups( {
		name: "native-font",
		timed,
		deltas: [
			{ groupName: "game-data", startup: true, files: [ "/assets/fonts/native-ui-font-atlas.json.gz" ] },
			{ groupName: "game-images", startup: true, files: [ "/assets/fonts/native-ui-font-atlas.png" ] }
		]
	} );
	console.log(
		`Native-font closure OK: ${totals.built} pack(s) rebuilt, ${totals.reused} reused, ` +
			`${totals.changed} asset delta(s), ${totals.hydrated} packed member(s) hydrated; ` +
			`${webManifest.files.length} web assets.`
	);
} );
