/*
===========================================================================

uiImageEligibility.mjs - UI preload eligibility shared by full and focused builds

The inventory contains published and incoming image paths, never a partial
loose-tree projection. Callers retain original spelling for publication.

===========================================================================
*/

export const INTERACTIVE_IMAGE_PATTERN = /_(focus|press|disable)\.png$/i;
const INTERACTIVE_STATES = [ "focus", "press", "disable" ];

/*
================
uiImagePreloadReason

Inventory keys are case-folded public paths. State images take priority over
interface images; normal companions qualify only when a state exists.
================
*/
export function uiImagePreloadReason( publicPath, inventory ) {
	const lower = publicPath.toLowerCase();
	if ( !lower.endsWith( ".png" ) ) return null;
	if ( INTERACTIVE_IMAGE_PATTERN.test( lower ) ) return "interactive-state";
	if ( lower.includes( "/media_extracted/interface/" ) ) return "native-interface";
	const stem = lower.slice( 0, -4 );
	if ( INTERACTIVE_STATES.some( state => inventory.has( `${stem}_${state}.png` ) ) ) {
		return "interactive-normal";
	}
	return null;
}
