/*
===========================================================================

bounded-body.mjs - a GameWorld answer read with a size limit

The console's readers forward answers from GameWorld listeners; one that
streams past its reader's limit is cut off with an error instead of being
buffered whole.

===========================================================================
*/

/*
================
boundedText

The response body as UTF-8, or an error once it passes limit bytes.
================
*/
export async function boundedText( response, limit ) {
	const reader = response.body.getReader(), chunks = [];
	let size = 0;
	try {
		for ( ;; ) {
			const { done, value } = await reader.read();
			if ( done ) break;
			size += value.byteLength;
			if ( size > limit ) throw Error( "response exceeded " + limit + " bytes" );
			chunks.push( value );
		}
	} finally {
		await reader.cancel();
	}
	return Buffer.concat( chunks ).toString( "utf8" );
}
