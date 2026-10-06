/*
===========================================================================

icon.ts - native icon metadata to a served image path

Native icon paths are relative to Media/icon; metadata never escapes it.
iconPath is the pure resolution. An owner that resolves the same icons on
every layout (the UI) creates its own remembering resolver with
createIconPaths: the lowercase, validation and concatenation then run once
per icon, and every later call returns the string built then instead of
building an equal one, which the engine compares and hashes without
rereading its characters.

===========================================================================
*/

const ICON_ROOT = "/assets/images/Media_extracted/icon/";
// Distinct icon names are bounded by the client data; the cap only guards
// against unbounded metadata, after which resolution starts over.
const MAX_REMEMBERED_ICONS = 8192;

/*
================
iconPath
================
*/
export function iconPath( value: string | undefined ): string | null {
	if ( !value || value.toLowerCase() === "xxx" ) return null;
	const path = value.replaceAll( "\\", "/" ).toLowerCase();
	if (
		!/^[a-z0-9_/-]+\.ddj$/.test( path ) || path.startsWith( "/" ) ||
		path.split( "/" ).some( part => !part || part === ".." )
	) return null;
	return ICON_ROOT + path.slice( 0, -4 ) + ".png";
}

/*
================
createIconPaths

A remembering iconPath owned by its caller.
================
*/
export function createIconPaths(): ( value: string | undefined ) => string | null {
	const resolved = new Map<string, string | null>();
	return value => {
		if ( !value ) return null;
		let path = resolved.get( value );
		if ( path === undefined ) {
			if ( resolved.size >= MAX_REMEMBERED_ICONS ) resolved.clear();
			path = iconPath( value );
			resolved.set( value, path );
		}
		return path;
	};
}
