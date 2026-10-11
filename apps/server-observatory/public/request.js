/*
===========================================================================

request.js - the console pages' JSON calls to this console's own server

Every answer is JSON; a failed one carries { error }, which becomes the
thrown message so pages can show it as it is.

===========================================================================
*/

/*
================
requestJSON
================
*/
export async function requestJSON( url, options = {} ) {
	const response = await fetch( url, { cache: "no-store", signal: AbortSignal.timeout( 20000 ), ...options } );
	const text = await response.text();
	let result;
	try {
		result = JSON.parse( text );
	} catch {
		throw Error( text || "Console returned an empty response" );
	}
	if ( !response.ok ) throw Error( result.error ?? "Console request failed" );
	return result;
}

/*
================
postJSON

A same-origin console mutation (security.mjs allowedMutation).
================
*/
export function postJSON( url, body ) {
	return requestJSON( url, {
		method: "POST",
		headers: { "Content-Type": "application/json", "X-SRO-Console": "1" },
		body: JSON.stringify( body )
	} );
}
