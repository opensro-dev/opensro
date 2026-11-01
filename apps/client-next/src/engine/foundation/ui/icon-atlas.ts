/*
===========================================================================

icon-atlas.ts - the icon family's atlas manifest as a pure mapping

Port-only, not native. The asset build packs every interface icon into
one edge-padded sheet (scripts/build/ui_icon_atlas.py) beside the
per-icon files, and publishes the manifest below /assets/ui-atlas/. This
module turns that manifest into the two lookups its consumers need:
which sheet an icon draws from, and the icon's uv rect inside it. A
quadrilateral's uv remaps linearly into that rect (device/ui.ts), which
holds for mirrored quads' negative extents as it does for plain ones.

An absent, malformed or wrong-versioned manifest maps nothing: the
interface keeps reading the per-icon files, exactly as before the atlas.

===========================================================================
*/

// Where the manifest and its sheets are published.
export const UI_ATLAS_MANIFEST_PATH = "/assets/ui-atlas/manifest.json";

/*
================
IconAtlasEntry

One icon's place in its sheet: the sheet's public path and the icon's uv
rect (x, y, width, height) inside it.
================
*/
export interface IconAtlasEntry {
	readonly sheet: string;
	readonly uv: readonly [number, number, number, number];
}

/*
================
IconAtlas

The installed mapping. entry() is the hot path: the quad packer calls it
for every plain quad, so it builds no string and allocates nothing.
================
*/
export interface IconAtlas {
	/** The sheets the manifest names, in manifest order. */
	readonly sheets: readonly string[];
	entry( iconPath: string ): IconAtlasEntry | null;
}

/*
================
iconAtlas

Parse and validate a manifest. Anything unexpected rejects the whole
manifest rather than half-installing it: the per-icon path is the
fallback for every icon or none of them.
================
*/
export function iconAtlas( manifest: unknown ): IconAtlas | null {
	if ( typeof manifest !== "object" || manifest === null || Array.isArray( manifest ) ) return null;
	const version = (manifest as Record<string, unknown>).version;
	const sheets = (manifest as Record<string, unknown>).sheets;
	const sheetSize = (manifest as Record<string, unknown>).sheetSize;
	const icons = (manifest as Record<string, unknown>).icons;
	if (
		version !== 1 || !Array.isArray( sheets ) || !Array.isArray( sheetSize ) || sheets.length !== sheetSize.length
	) {
		return null;
	}
	const parsedSheets: string[] = [];
	const sizes: (readonly [number, number])[] = [];
	for ( let index = 0; index < sheets.length; index++ ) {
		const path = sheets[index]!, size = sheetSize[index]!;
		if (
			typeof path !== "string" || !path.startsWith( "/assets/ui-atlas/" ) ||
			!Array.isArray( size ) || size.length !== 2 ||
			!size.every( value => typeof value === "number" && Number.isInteger( value ) && value > 0 )
		) return null;
		parsedSheets.push( path );
		sizes.push( [ size[0]!, size[1]! ] );
	}
	if ( typeof icons !== "object" || icons === null || Array.isArray( icons ) ) return null;
	const entries = new Map<string, IconAtlasEntry>();
	for ( const [path, rect] of Object.entries( icons ) ) {
		if ( !Array.isArray( rect ) || rect.length !== 5 ) return null;
		const [index, x, y, width, height] = rect;
		if (
			typeof index !== "number" || !Number.isInteger( index ) || index < 0 || index >= parsedSheets.length ||
			typeof x !== "number" || typeof y !== "number" || typeof width !== "number" || typeof height !== "number" ||
			!Number.isInteger( x ) || !Number.isInteger( y ) || !Number.isInteger( width ) ||
			!Number.isInteger( height ) ||
			width <= 0 || height <= 0 || x < 0 || y < 0
		) return null;
		const [sheetWidth, sheetHeight] = sizes[index]!;
		if ( x + width > sheetWidth || y + height > sheetHeight ) return null;
		entries.set( path, {
			sheet: parsedSheets[index]!,
			uv: [ x / sheetWidth, y / sheetHeight, width / sheetWidth, height / sheetHeight ]
		} );
	}
	if ( entries.size === 0 ) return null;
	return {
		sheets: parsedSheets,
		entry( iconPath: string ) {
			return entries.get( iconPath ) ?? null;
		}
	};
}
