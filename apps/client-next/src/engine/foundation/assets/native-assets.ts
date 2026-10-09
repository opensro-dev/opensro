/*
===========================================================================

native-assets.ts - the assets the browser itself loads by URL

Every other asset is read through the asset worker, from verified packs.
These few are loaded by the browser directly: the boot screen draws before
any script runs (index.html images and loading.css backgrounds), and a CSS
cursor must name a URL. A release publishes a separate URL only for the
files listed here; the release build (tools/beta/build.mjs) refuses a page
or stylesheet that references any other asset URL.

===========================================================================
*/
import type { WorldCursor } from "../ui/world-cursor";

const BOOT_IMAGES = "/assets/images/Media_extracted/interface/";

/*
================
worldCursors

Retail cursor resources 0x95-0xA6 (WorldCursor), extracted one file each.
================
*/
export function worldCursors(): WorldCursor[] {
	return [ 0x95, 0x96, 0x97, 0x98, 0x99, 0x9a, 0xa0, 0xa1, 0xa3, 0xa6 ];
}

/*
================
cursorAssetUrl
================
*/
export function cursorAssetUrl( value: WorldCursor ): string {
	return `/assets/cursors/sro_client_cursor_0x${value.toString( 16 )}.png`;
}

/*
================
bootAssetUrls

The images index.html and loading.css draw before any script runs.
================
*/
export function bootAssetUrls(): string[] {
	return [
		BOOT_IMAGES + "outer/logo-big.png",
		BOOT_IMAGES + "loading/nowloading.png",
		BOOT_IMAGES + "loading/loading_form.png",
		BOOT_IMAGES + "loading/gauge_loading.png"
	];
}

/*
================
nativeAssetUrls
================
*/
export function nativeAssetUrls(): string[] {
	return [ ...bootAssetUrls(), ...worldCursors().map( cursorAssetUrl ) ];
}
