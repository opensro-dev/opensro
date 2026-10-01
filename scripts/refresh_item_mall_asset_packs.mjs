/*
===========================================================================

refresh_item_mall_asset_packs.mjs - publish native mall category artwork

The executable creates category controls outside resinfo. Publish the exact
family through the shared pack owner so loose previews and deployed clients
resolve the same images.

===========================================================================
*/
import { imagePublicPath } from "./build/shared/cifResources.mjs";
import { itemMallRuntimeImageReferences } from "./build/shared/cifRuntimeImageCatalog.mjs";
import { publishConvertedImage } from "./build/shared/convertedImages.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

await withGeneratedAssetsLock( "Native Item Mall texture publication", async () => {
	const files = [];
	for ( const reference of itemMallRuntimeImageReferences ) {
		files.push( await publishConvertedImage( imagePublicPath( reference ) ) );
	}
	await publishLooseFamily( { name: "item-mall", files, defaultGroup: "native-ui" } );
	console.log( `Published ${files.length} native Item Mall textures.` );
} );
