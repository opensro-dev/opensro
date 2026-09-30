/*
===========================================================================

refresh_quickslot_asset_packs.mjs - publish the native quickslot textures

The quickslot bar, its skill-page button and the close buttons of both bar
orientations, in every button state.

===========================================================================
*/
import { imagePublicPath } from "./build/shared/cifResources.mjs";
import { quickslotRuntimeImageReferences } from "./build/shared/cifRuntimeImageCatalog.mjs";
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

await withGeneratedAssetsLock( "Native quickslot texture publication", async () => {
	const references = [
		...quickslotRuntimeImageReferences,
		...buttonStates( "interface/skill/skl_button_up" ),
		...buttonStates( "interface/quick_slot/qsl_hclose_button" ),
		...buttonStates( "interface/quick_slot/qsl_vclose_button" )
	];
	const files = [];
	for ( const reference of references ) files.push( await publishConvertedImage( imagePublicPath( reference ) ) );
	await publishLooseFamily( { name: "quickslots", files, defaultGroup: "native-ui" } );
	console.log( `Published ${files.length} native quickslot textures.` );
} );
