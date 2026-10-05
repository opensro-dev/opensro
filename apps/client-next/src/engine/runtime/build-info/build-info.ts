/*
===========================================================================

build-info.ts - which commit the client and the server are running

The FPS chip names both builds so a stale deployment shows from inside the
game: the client bundle and the Agent are built separately and can drift
apart. The client revision is stamped by the build (vite.config.mjs
defines import.meta.env.SRO_CLIENT_REVISION from git); the server's comes
from the Agent's GET /title/build, asked once, and its uptime keeps
counting on this page's monotonic clock. Whatever is unknown is left out.

The frame clock drives the one request and its retry; this owner keeps no
timer.

===========================================================================
*/

import { RELEASE_PROTOCOL, RELEASE_PROTOCOL_HEADER } from "@/engine/foundation/release/protocol";

const ROUTE = "/title/build";
// An Agent that is not up yet is asked again, never in a tight loop.
const RETRY_MS = 30 * 1000;
// Seven hex digits, as git abbreviates.
const SHORT_REVISION = 7;
const REVISION_PATTERN = /^[0-9a-f]{7,64}$/;

interface ServerBuild {
	readonly revision: string;
	readonly uptimeSeconds: number;
	readonly receivedAtMs: number;
}

/*
================
shortRevision

A full git revision cut to its usual abbreviation; anything that is not
one is unknown.
================
*/
export function shortRevision( value: unknown ) {
	return typeof value === "string" && REVISION_PATTERN.test( value ) ? value.slice( 0, SHORT_REVISION ) : null;
}

/*
================
formatUptime

Two units at most: 45s, 12m 05s, 3h 07m, 2d 04h.
================
*/
export function formatUptime( seconds: number ) {
	const total = Math.max( 0, Math.floor( seconds ) ),
		days = Math.floor( total / 86400 ),
		hours = Math.floor( total % 86400 / 3600 ),
		minutes = Math.floor( total % 3600 / 60 ),
		rest = total % 60;
	const pad = ( value: number ) => String( value ).padStart( 2, "0" );
	if ( days ) return `${days}d ${pad( hours )}h`;
	if ( hours ) return `${hours}h ${pad( minutes )}m`;
	if ( minutes ) return `${minutes}m ${pad( rest )}s`;
	return `${rest}s`;
}

/*
================
createBuildInfo

`clientRevision` is the revision this bundle was built from (undefined
when the build stamped none).
================
*/
export function createBuildInfo( apiBase: string, clientRevision: unknown ) {
	const lifetime = new AbortController(), client = shortRevision( clientRevision );
	let server: ServerBuild | null = null, loading = false, retryAtMs = 0;

	/*
	================
	load

	A promise chain, not an async function: the frame starts it.
	================
	*/
	function load( nowMs: number ) {
		loading = true;
		retryAtMs = nowMs + RETRY_MS;
		fetch( apiBase + ROUTE, {
			headers: { [RELEASE_PROTOCOL_HEADER]: String( RELEASE_PROTOCOL ) },
			credentials: "omit",
			cache: "no-store",
			redirect: "error",
			signal: lifetime.signal
		} ).then( response =>
			response.json().then( ( body: { build?: { revision?: unknown; uptimeSeconds?: unknown; }; } ) => {
				const revision = shortRevision( body.build?.revision ), uptime = Number( body.build?.uptimeSeconds );
				if ( !response.ok || !revision || !Number.isFinite( uptime ) ) return;
				server = { revision, uptimeSeconds: uptime, receivedAtMs: performance.now() };
			} )
		).catch( () => {
			// Unknown until the next retry; the chip simply leaves it out.
		} ).finally( () => {
			loading = false;
		} );
	}

	return {
		/*
		================
		lines

		The chip's build lines at `nowMs` (performance.now() time). The first
		call starts the one request; a failed one is retried after RETRY_MS.
		================
		*/
		lines( nowMs: number ): readonly string[] {
			if ( !server && !loading && nowMs >= retryAtMs && !lifetime.signal.aborted ) load( nowMs );
			const lines: string[] = [];
			if ( client ) lines.push( `client ${client}` );
			if ( server ) {
				const uptime = server.uptimeSeconds + (nowMs - server.receivedAtMs) / 1000;
				lines.push( `server ${server.revision} up ${formatUptime( uptime )}` );
			}
			return lines;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			lifetime.abort();
		}
	};
}
