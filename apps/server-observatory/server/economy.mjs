/*
===========================================================================

economy.mjs - read-only reads of each GameWorld's economy aggregates

The Economy page's server half. GameWorld serves pre-bucketed aggregates
(contract v1: meta, series, flows, distribution, flags, trade, killstats) on its
loopback control listener under /economy/v1/; it reads its own rollup
tables and latest snapshot copy, never the live store per request. This
owner forwards only those routes and only their validated parameters, adds
the bearer token server-side (the browser never sees it), and bounds the
answer. Phase 1 is read-only: there is no write route here.

===========================================================================
*/
import { boundedText } from "./bounded-body.mjs";

const MAX_RESPONSE_BYTES = 4 * 1024 * 1024;
const REQUEST_TIMEOUT_MS = 10000;
// Contract v1 refuses more than this many buckets (range_too_wide); the
// console checks first so a bad range never costs a GameWorld round trip.
export const MAX_BUCKETS = 720;
const BUCKET_SECONDS = Object.freeze( { "1h": 3600, "1d": 86400 } );

// The parameters each route accepts; anything else is dropped.
const ROUTES = Object.freeze( {
	meta: [],
	series: [ "metric", "bucket", "from", "to", "division" ],
	flows: [ "bucket", "from", "to", "division" ],
	distribution: [ "division" ],
	flags: [ "from", "to" ],
	trade: [ "bucket", "from", "to" ],
	killstats: [ "from", "to" ]
} );

// Kill stats are whole UTC days; the contract refuses a range holding
// more than this many of them (range_too_wide).
export const MAX_KILLSTAT_DAYS = 366;
const DAY_SECONDS = 86400;

const METRIC = /^(gold_minted|gold_burned|gold_net|gold_total|reason:[a-z0-9_]{1,64})$/;
const DIVISION = /^[A-Za-z0-9_-]{1,64}$/;
const SECONDS = /^\d{1,12}$/;

/*
================
economyRoute

The contract route a console path names, or null.
================
*/
export function economyRoute( pathname ) {
	const match = /^\/api\/economy\/([a-z]+)$/.exec( pathname );
	return match && Object.hasOwn( ROUTES, match[1] ) ? match[1] : null;
}

/*
================
economyQuery

The validated query a route forwards, or { error } naming the first bad
parameter. from/to are unix seconds; a bucketed range must hold at most
MAX_BUCKETS buckets.
================
*/
export function economyQuery( route, params ) {
	const query = new URLSearchParams();
	for ( const key of ROUTES[route] ) {
		const value = params.get( key );
		if ( value === null || value === "" ) continue;
		if ( key === "metric" && !METRIC.test( value ) ) return { error: "Unknown metric" };
		if ( key === "bucket" && !Object.hasOwn( BUCKET_SECONDS, value ) ) return { error: "Bucket is 1h or 1d" };
		if ( (key === "from" || key === "to") && !SECONDS.test( value ) ) return { error: key + " is unix seconds" };
		if ( key === "division" && !DIVISION.test( value ) ) return { error: "Unknown division" };
		query.set( key, value );
	}
	if ( route === "series" && !query.has( "metric" ) ) return { error: "A metric is required" };
	if ( route === "killstats" && query.has( "from" ) && query.has( "to" ) ) {
		const from = Number( query.get( "from" ) ), to = Number( query.get( "to" ) );
		if ( from > to ) return { error: "from is after to" };
		// The contract floors both ends to whole UTC days before counting.
		const days = Math.floor( to / DAY_SECONDS ) - Math.floor( from / DAY_SECONDS ) + 1;
		if ( days > MAX_KILLSTAT_DAYS ) return { error: "Range holds more than 366 days" };
	}
	const bucket = query.get( "bucket" );
	if ( bucket && query.has( "from" ) && query.has( "to" ) ) {
		const span = Number( query.get( "to" ) ) - Number( query.get( "from" ) );
		if ( span < 0 ) return { error: "from is after to" };
		if ( span / BUCKET_SECONDS[bucket] > MAX_BUCKETS ) return { error: "Range holds more than 720 buckets" };
	}
	return { query };
}

/*
================
createEconomy

shards are loadShards rows (their url is on the loopback control
listener); token is the economy reader's bearer token, empty when the
console is not configured for it.
================
*/
export function createEconomy( shards, token, fetcher = fetch ) {
	return {
		enabled: Boolean( token ),
		/*
		================
		request
		================
		*/
		async request( pathname, params ) {
			const route = economyRoute( pathname );
			if ( !route ) return { status: 404, body: { error: "Unknown economy route" } };
			const shard = shards.find( row => row.id === params.get( "shard" ) );
			if ( !shard ) return { status: 404, body: { error: "Unknown server" } };
			if ( !token ) return { status: 503, body: { error: "Economy reads are not configured on this console" } };
			const checked = economyQuery( route, params );
			if ( checked.error ) return { status: 400, body: { error: checked.error } };
			const url = new URL( "/economy/v1/" + route, shard.url );
			url.search = checked.query.toString();
			try {
				const response = await fetcher( url, {
					headers: { Authorization: "Bearer " + token, "X-SRO-Local-Diagnostics": "1" },
					signal: AbortSignal.timeout( REQUEST_TIMEOUT_MS ),
					redirect: "error"
				} );
				const text = await boundedText( response, MAX_RESPONSE_BYTES );
				let body;
				try {
					body = JSON.parse( text );
				} catch {
					body = null;
				}
				if ( response.ok && body ) return { status: 200, body };
				// Contract errors keep their code, so the page can say "no snapshot
				// yet" rather than a bare failure.
				const failure = body?.error;
				return {
					status: response.ok ? 502 : response.status,
					body: {
						error: typeof failure?.message === "string" ?
							failure.message.slice( 0, 256 ) :
							"Economy unavailable: HTTP " + response.status,
						code: typeof failure?.code === "string" ? failure.code.slice( 0, 64 ) : undefined
					}
				};
			} catch ( error ) {
				return { status: 503, body: { error: "Economy unavailable: " + error.message } };
			}
		}
	};
}
