/*
===========================================================================

refresh_slot_effect_asset_packs.mjs - publish the item-slot effect sheets

The CIFSlotWithHelp overlay sheets are code-selected, so a new one reaches
the packs and the CIF sprite catalog only through this publisher or a full
asset build. Each sheet joins the group that already owns it; every
unrelated pack member and catalog entry is preserved.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { imagePublicPath, registerSpriteResource } from "./build/shared/cifResources.mjs";
import { slotEffectRuntimeImageReferences } from "./build/shared/cifRuntimeImageCatalog.mjs";
import { publishConvertedImage } from "./build/shared/convertedImages.mjs";
import { writeJsonIfChanged } from "./build/shared/jsonOut.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { refreshPrecompressedSidecars } from "./build/generatedManifestSidecars.mjs";
import { publicRoot } from "./build/world/paths.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const SPRITE_CATALOG = path.join( publicRoot, "assets", "cif", "cif-sprite-catalog.json" );

await withGeneratedAssetsLock( "Item-slot effect sheet publication", async () => {
	const files = [];
	for ( const reference of slotEffectRuntimeImageReferences ) {
		files.push( await publishConvertedImage( imagePublicPath( reference ) ) );
	}
	// buildCifResources registers every runtime reference in the shared sprite
	// catalog; update it (and its precompressed sidecars) before publication so
	// the web manifest that publication rewrites carries the new catalog.
	const catalog = JSON.parse( await readFile( SPRITE_CATALOG, "utf8" ) );
	for ( const reference of slotEffectRuntimeImageReferences ) await registerSpriteResource( catalog, reference );
	if ( await writeJsonIfChanged( SPRITE_CATALOG, catalog ) ) await refreshPrecompressedSidecars( [ SPRITE_CATALOG ] );
	await publishLooseFamily( { name: "slot-effects", files, defaultGroup: "game-images" } );
	console.log( `Published ${files.length} item-slot effect sheets.` );
} );
