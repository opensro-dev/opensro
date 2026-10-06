/*
===========================================================================

icon.ts - native icon metadata to a served image path

Native icon paths are relative to Media/icon; metadata never escapes it.
The UI asks for the same few hundred icons on every layout, so resolved
paths are remembered: the lowercase, validation and concatenation run once
per icon, and every later call returns the string built then instead of
building an equal one, which the engine can compare and hash without
rereading its characters.

===========================================================================
*/

const ICON_ROOT = "/assets/images/Media_extracted/icon/";
// Distinct icon names are bounded by the client data; the cap only guards
// against unbounded metadata, after which resolution starts over.
const MAX_REMEMBERED_ICONS = 8192;

const resolved = new Map<string, string | null>();

/*
================
resolveIconPath
================
*/
function resolveIconPath( value: string ): string | null {
	if ( value.toLowerCase() === "xxx" ) return null;
	const path = value.replaceAll( "\\", "/" ).toLowerCase();
	if (
		!/^[a-z0-9_/-]+\.ddj$/.test( path ) || path.startsWith( "/" ) ||
		path.split( "/" ).some( part => !part || part === ".." )
	) return null;
	return ICON_ROOT + path.slice( 0, -4 ) + ".png";
}

/*
================
iconPath
================
*/
export function iconPath( value: string | undefined ): string | null {
	if ( !value ) return null;
	let path = resolved.get( value );
	if ( path === undefined ) {
		if ( resolved.size >= MAX_REMEMBERED_ICONS ) resolved.clear();
		path = resolveIconPath( value );
		resolved.set( value, path );
	}
	return path;
}
