/*
===========================================================================
player-operations.mjs - bounded requests to configured shard authorities
===========================================================================
*/
const MAX_BODY_BYTES = 4096;
const MAX_RESPONSE_BYTES = 2 * 1024 * 1024;
/*
================
readBody
================
*/
export async function readBody( request ) {
	let bytes = 0;
	const chunks = [];
	for await ( const chunk of request ) {
		bytes += chunk.length;
		if ( bytes > MAX_BODY_BYTES ) throw Error( "Request too large" );
		chunks.push( chunk );
	}
	return JSON.parse( Buffer.concat( chunks ).toString( "utf8" ) );
}
/*
================
createPlayerOperations
================
*/
export function createPlayerOperations( shards, token, fetcher = fetch ) {
	/*
 ================
 request
 ================
 */
	async function request( shardID, character, rescue ) {
		if ( !token ) return { status: 503, body: { error: "Player operations are not configured" } };
		const shard = shards.find( row => row.id === shardID );
		if ( !shard ) return { status: 404, body: { error: "Unknown shard" } };
		const url = new URL( "/internal/operations/player", shard.url );
		if ( character ) url.searchParams.set( "character", character );
		const response = await fetcher( url, {
			method: rescue ? "POST" : "GET",
			headers: {
				"X-SRO-Local-Diagnostics": "1",
				Authorization: "Bearer " + token,
				"Content-Type": "application/json"
			},
			...(rescue ? { body: JSON.stringify( rescue ) } : {}),
			signal: AbortSignal.timeout( 15000 ),
			redirect: "error"
		} );
		const reader = response.body.getReader(), chunks = [];
		let size = 0;
		try {
			for ( ;; ) {
				const { done, value } = await reader.read();
				if ( done ) break;
				size += value.byteLength;
				if ( size > MAX_RESPONSE_BYTES ) throw Error( "Player diagnostic exceeded limit" );
				chunks.push( value );
			}
		} finally {
			await reader.cancel();
		}
		const text = Buffer.concat( chunks ).toString( "utf8" );
		if ( response.status === 404 && text.trim() === "404 page not found" ) {
			return {
				status: 503,
				body: {
					error:
						"Player recovery is not enabled on this running shard. Its GameWorld release must include the operator endpoint."
				}
			};
		}
		return { status: response.status, body: response.ok ? JSON.parse( text ) : { error: text.trim() } };
	}
	return { request };
}
