/*
===========================================================================

refresh_footprint_asset_packs.mjs - publish the terrain footprint textures

The renderer's terrain dependency must be published, not merely present in
the converted-image tree. Only the sand and snow footstep decals are packed;
every unrelated pack member is preserved.

===========================================================================
*/
import { imagePublicPath } from "./build/shared/cifResources.mjs";
import { runtimeCifImageReferences } from "./build/shared/cifRuntimeImageCatalog.mjs";
import { publishConvertedImage } from "./build/shared/convertedImages.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const FOOTPRINT_DDJ = /^effect\/footstep_(sand|snow)\.ddj$/;

await withGeneratedAssetsLock( "Terrain footprint texture publication", async () => {
	const references = runtimeCifImageReferences.filter( file => FOOTPRINT_DDJ.test( file ) );
	if ( new Set( references ).size !== 2 ) throw new Error( "Footprint catalog must contain sand and snow" );
	const files = [];
	for ( const reference of references ) files.push( await publishConvertedImage( imagePublicPath( reference ) ) );
	await publishLooseFamily( { name: "footprints", files, defaultGroup: "game-images" } );
	console.log( "Published sand and snow footprint textures." );
} );
