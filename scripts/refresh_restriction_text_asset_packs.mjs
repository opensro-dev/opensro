/*
===========================================================================

refresh_restriction_text_asset_packs.mjs - complete the restriction notices

Fills the English restriction notices into the UI system text catalog and
repacks it in the group that already owns it.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { refreshPrecompressedSidecars } from "./build/generatedManifestSidecars.mjs";
import { writeJsonIfChanged } from "./build/shared/jsonOut.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { completeRestrictionText } from "./build/shared/textResources.mjs";
import { publicRoot } from "./build/world/paths.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

await withGeneratedAssetsLock( "Restriction notice English correction", async () => {
	const target = path.join( publicRoot, "assets", "text", "textuisystem.en.json" );
	const catalog = JSON.parse( await readFile( target, "utf8" ) );
	completeRestrictionText( catalog.entries );
	await writeJsonIfChanged( target, catalog );
	await refreshPrecompressedSidecars( [ target ], { onlyWhenStale: true } );
	await publishLooseFamily( { name: "restriction-text", files: [ "/assets/text/textuisystem.en.json.gz" ] } );
	console.log( "Refreshed restriction English catalog, compressed sidecars and asset pack." );
} );
