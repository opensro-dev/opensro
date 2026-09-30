/*
===========================================================================

refresh_return_scroll_asset_packs.mjs - publish the return-scroll textures

The return-scroll casting gauge and its cancel button in every state.

===========================================================================
*/
import { imagePublicPath } from "./build/shared/cifResources.mjs";
import { returnScrollRuntimeImageReferences } from "./build/shared/cifRuntimeImageCatalog.mjs";
import { publishConvertedImage } from "./build/shared/convertedImages.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

// The CIFButton state family (sub_5419c0); none of these ships a _disable.
const BUTTON_STATES = [ "", "_focus", "_press" ];

/*
================
buttonStates
================
*/
function buttonStates( stem ) {
	return BUTTON_STATES.map( state => `${stem}${state}.ddj` );
}

await withGeneratedAssetsLock( "Native return-scroll texture publication", async () => {
	const references = [
		...returnScrollRuntimeImageReferences,
		...buttonStates( "interface/ifcommon/com_casting_cancel" )
	];
	const files = [];
	for ( const reference of references ) files.push( await publishConvertedImage( imagePublicPath( reference ) ) );
	await publishLooseFamily( { name: "return-scrolls", files, defaultGroup: "native-ui" } );
	console.log( `Published ${files.length} native return-scroll textures.` );
} );
