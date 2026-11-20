/*
===========================================================================

replaced-windows.mjs - the native windows a later client's import replaces

A few native windows are replaced, not extended, when the build publishes
the later client's version of them: the renewal Item Mall becomes the
desktop Item Mall whenever its catalogue is published (owner, 2026-11-20,
#3971). A gate that pins the native window (the action census, the scene
goldens, the classic window's own tests) cannot hold for the window that
replaced it, and everything outside the window still has to.

This file is the one list. Each entry names the window's control id
prefix and the catalogue whose presence in the asset tree makes the
replacement (the client's own constant, so the
gate and the HUD ask the same question). Read the list through
replacedWindows(), which keeps only the windows this build replaces.

===========================================================================
*/
import { existsSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../scripts/lib/generatedRoot.mjs";
import "../tests/helpers/native-source-loader.mjs";

const { RENEWAL_MALL_CATALOG } = await import( "../src/engine/foundation/ui/renewal-mall.ts" );

export const REPLACED_WINDOWS = Object.freeze( [
	Object.freeze( {
		name: "item-mall",
		controlPrefix: "item-mall",
		catalog: RENEWAL_MALL_CATALOG
	} )
] );

/*
================
replacedWindows

The windows of REPLACED_WINDOWS whose replacement the asset tree at
publicRoot publishes. A native build, or one without the later import,
replaces none.
================
*/
export function replacedWindows( publicRoot = CLIENT_PUBLIC_ROOT ) {
	return REPLACED_WINDOWS.filter( window => existsSync( publicRoot + window.catalog ) );
}

/*
================
classicPublished

The ui-fixture "published" overrides that serve every replaceable window's
catalogue as absent, so the HUD opens the native window whatever the build
publishes. For tests of the native window itself.
================
*/
export function classicPublished() {
	return Object.fromEntries( REPLACED_WINDOWS.map( window => [ window.catalog, { absent: true } ] ) );
}
