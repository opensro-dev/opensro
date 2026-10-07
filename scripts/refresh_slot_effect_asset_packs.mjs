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

const SPRITE_CATALOG_PUBLIC_PATH = "/assets/cif/cif-sprite-catalog.json";
const SPRITE_CATALOG = path.join( publicRoot, SPRITE_CATALOG_PUBLIC_PATH.slice( 1 ) );

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
	// A loose catalog cannot replace an older packed representation. Preserve
	// whichever identity/gzip members the existing index owns, including both
	// when present, and let the publication owner keep their original groups.
	const previous = JSON.parse(
		await readFile( path.join( publicRoot, "assets", "packs", "manifest.json" ), "utf8" )
	);
	const catalogFiles = previous.assets.filter( row =>
		row.path === SPRITE_CATALOG_PUBLIC_PATH || row.path === SPRITE_CATALOG_PUBLIC_PATH + ".gz"
	).map( row => row.path );
	await publishLooseFamily( {
		name: "slot-effects",
		files: [ ...files, ...(catalogFiles.length ? catalogFiles : [ SPRITE_CATALOG_PUBLIC_PATH ]) ],
		defaultGroup: file => file === SPRITE_CATALOG_PUBLIC_PATH ? "game-data" : "game-images"
	} );
	console.log( `Published ${files.length} item-slot effect sheets.` );
} );
