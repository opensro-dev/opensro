/*
===========================================================================

chatlog.mjs - the staff console's read of each shard's moderation chat log

Forwards a named search to the GameWorld's operator route, with the same
loopback confinement and bounded read as the history journal
(history.mjs). The operator token stays on this server. Only the filters
below ever leave it, so a request cannot steer the upstream call.

===========================================================================
*/
import { readService, requireLoopback } from "./history.mjs";

const CHAT_LOG_PATH = "/internal/operations/chatlog";
const FILTERS = new Set( [ "account", "character", "from", "to", "limit" ] );
const MAX_FILTER_LENGTH = 256;

/*
================
createChatLog
================
*/
export function createChatLog( sources, fetcher = fetch ) {
	for ( const source of sources ) requireLoopback( source.url, "Chat log" );
	return {
		/*
		================
		request
		================
		*/
		async request( query ) {
			const source = sources.find( row => row.id === query.get( "source" ) );
			if ( !source ) return { status: 404, body: { error: "Unknown shard" } };
			if ( !source.token ) return { status: 503, body: { error: "Chat log operator token is not configured" } };
			const url = new URL( CHAT_LOG_PATH, source.url );
			for ( const [key, value] of query ) {
				if ( !FILTERS.has( key ) ) continue;
				if ( value.length > MAX_FILTER_LENGTH ) return { status: 400, body: { error: "Filter is too long" } };
				url.searchParams.set( key, value );
			}
			return readService( url, source.token, fetcher, "Chat log" );
		}
	};
}
