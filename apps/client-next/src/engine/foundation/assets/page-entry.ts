/*
===========================================================================

page-entry.ts - the entry bundle a page loads

A release build's page loads one content-hashed module script
(/assets/index-<hash>.js). Two pages name the same entry exactly when they
come from the same release, so comparing entries detects that the live
page is newer than the running one without any build-time version stamp.

===========================================================================
*/

/*
================
pageEntryBundle

The path of the first module script a page's HTML loads, or null when it
loads none. `origin` resolves a relative src.
================
*/
export function pageEntryBundle( html: string, origin: string ): string | null {
	for ( const tag of html.matchAll( /<script\b[^>]*>/gi ) ) {
		const attributes = tag[0];
		if ( !/\btype\s*=\s*["']?module["']?/i.test( attributes ) ) continue;
		const src = /\bsrc\s*=\s*["']([^"']+)["']/i.exec( attributes )?.[1];
		if ( !src ) continue;
		try {
			return new URL( src, origin ).pathname;
		} catch {
			return null;
		}
	}
	return null;
}
