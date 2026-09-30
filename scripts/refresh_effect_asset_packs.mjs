/*
===========================================================================

refresh_effect_asset_packs.mjs - publish the effect program closure

Publishes the complete generated EFP dependency closure through the same
pack authority the asset worker reads. Updating a loose JSON file alone
leaves the packed copy stale.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { refreshPrecompressedSidecars } from "./build/generatedManifestSidecars.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { publicRoot } from "./build/world/paths.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const EFFECT_RECORDS = [
	"/assets/skill/effectRecords.json",
	"/assets/skill/namedEffectRecords.json",
	"/assets/effects/programs.json"
];

await withGeneratedAssetsLock( "Effect program pack publication", async () => {
	const catalog = JSON.parse(
		await readFile( path.join( publicRoot, "assets", "effects", "programs.json" ), "utf8" )
	);
	await refreshPrecompressedSidecars( EFFECT_RECORDS.map( file => path.join( publicRoot, file ) ), {
		onlyWhenStale: true
	} );
	const files = [ ...EFFECT_RECORDS.map( file => file + ".gz" ), ...new Set( Object.values( catalog.textures ) ) ];
	await publishLooseFamily( {
		name: "effects",
		files,
		defaultGroup: file => file.endsWith( ".gz" ) ? "game-data" : "game-images"
	} );
	console.log( `Published ${files.length} effect resources.` );
} );
