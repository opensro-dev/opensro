/*
===========================================================================

corrected-text.mjs - the UI system catalog as the next asset build publishes it

The reviewed English corrections (scripts/build/shared/englishCorrections.json)
reach the published catalog only when the assets are rebuilt, so a test of a
newly corrected line would read the shared tree's older text until then. This
serves the built catalog with the named keys' corrections applied through
the build's own policy (correctedEnglish), which accepts an already-corrected
cell, so the result is the same before and after the rebuild. Only named
keys: later build steps rewrite some other cells, which the policy (rightly)
refuses to correct from their retail text.

===========================================================================
*/
import { readFileSync } from "node:fs";
import path from "node:path";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { correctedEnglish } from "../../../../scripts/build/shared/englishCorrections.mjs";

export const UI_SYSTEM_CATALOG = "/assets/text/textuisystem.en.json";

/*
================
correctedUiSystemCatalog

{ [UI_SYSTEM_CATALOG]: bytes } for uiFixture's published argument, with
each of keys corrected.
================
*/
export function correctedUiSystemCatalog( keys ) {
	const file = path.join( CLIENT_PUBLIC_ROOT, ...UI_SYSTEM_CATALOG.split( "/" ).filter( Boolean ) );
	const catalog = JSON.parse( readFileSync( file, "utf8" ) );
	const entries = { ...catalog.entries };
	for ( const key of keys ) entries[key] = correctedEnglish( "textuisystem.txt", key, entries[key] );
	return { [UI_SYSTEM_CATALOG]: Buffer.from( JSON.stringify( { ...catalog, entries } ) ) };
}
