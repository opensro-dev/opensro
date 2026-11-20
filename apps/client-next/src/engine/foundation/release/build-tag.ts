/*
===========================================================================

build-tag.ts - the bundle name a client report carries as its build

The Agent groups client incidents by this tag (client_incident.go `build`),
so one release's reports share it. It is the file name of the module that
reports, as the release build hashed it (entry-<hash>.js).

A module the browser runs from a blob: or data: URL has no file name: the
last path segment of blob:https://host/<uuid> is the blob's UUID, unique per
page load, and each such report became a group of one (current_todo #8, 20
UUID groups). Such a URL reports EMBEDDED_BUILD instead.

This file imports nothing, so Node tests read the same rule the client ships.

===========================================================================
*/

// The tag of a module that was not loaded from a served file.
export const EMBEDDED_BUILD = "embedded";
// The tag when the URL has no file name at all (the dev server's root).
export const DEVELOPMENT_BUILD = "development";
// The Agent refuses a longer build name (client_incident.go maxIncidentName).
const MAX_BUILD_TAG = 64;

/*
================
buildTag

moduleUrl is the reporting module's import.meta.url.
================
*/
export function buildTag( moduleUrl: string ): string {
	let url: URL;
	try {
		url = new URL( moduleUrl );
	} catch {
		return DEVELOPMENT_BUILD;
	}
	if ( url.protocol !== "http:" && url.protocol !== "https:" ) {
		return EMBEDDED_BUILD;
	}
	const name = url.pathname.split( "/" ).at( -1 )?.slice( 0, MAX_BUILD_TAG );
	return name || DEVELOPMENT_BUILD;
}
