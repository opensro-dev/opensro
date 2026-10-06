/*
===========================================================================
history.mjs - authenticated, bounded reads from configured service journals
===========================================================================
*/
const MAX_RESPONSE_BYTES = 4 * 1024 * 1024;
const FILTERS = new Set( [
	"account",
	"character",
	"session",
	"incident",
	"category",
	"build",
	"opcode",
	"kind",
	"q",
	"from",
	"to",
	"before"
] );
/*
================
createHistory
================
*/
export function createHistory( sources, fetcher = fetch ) {
	for ( const source of sources ) {
		const url = new URL( source.url );
		if (
			url.protocol !== "http:" || ![ "127.0.0.1", "[::1]" ].includes( url.hostname ) || url.username ||
			url.password
		) throw Error( "History requires loopback service URLs" );
	}
	return {
		/*
================
request
================
		*/
		async request( query ) {
			const source = sources.find( row => row.id === query.get( "source" ) );
			if ( !source ) return { status: 404, body: { error: "Unknown history source" } };
			if ( !source.token ) return { status: 503, body: { error: "History operator token is not configured" } };
			const url = new URL( "/internal/operations/history", source.url );
			for ( const [key, value] of query ) {
				if ( !FILTERS.has( key ) ) continue;
				if ( value.length > 256 ) return { status: 400, body: { error: "Filter exceeds 256 characters" } };
				url.searchParams.set( key, value );
			}
			try {
				const response = await fetcher( url, {
					headers: { Authorization: "Bearer " + source.token, "X-SRO-Local-Diagnostics": "1" },
					signal: AbortSignal.timeout( 10000 ),
					redirect: "error"
				} );
				const reader = response.body.getReader(), chunks = [];
				let size = 0;
				try {
					for ( ;; ) {
						const { done, value } = await reader.read();
						if ( done ) break;
						size += value.byteLength;
						if ( size > MAX_RESPONSE_BYTES ) throw Error( "History response exceeded limit" );
						chunks.push( value );
					}
				} finally {
					await reader.cancel();
				}
				const text = Buffer.concat( chunks ).toString( "utf8" );
				return {
					status: response.status,
					body: response.ok ?
						JSON.parse( text ) :
						{ error: "History unavailable: HTTP " + response.status + " " + text.slice( 0, 256 ) }
				};
			} catch ( error ) {
				return { status: 503, body: { error: "History unavailable: " + error.message } };
			}
		}
	};
}
