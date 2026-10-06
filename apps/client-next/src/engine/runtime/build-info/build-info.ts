/*
===========================================================================

build-info.ts - which commit the client and the server are running

The developer panel names both builds so a stale deployment shows from inside the
game: the client bundle and the Agent are built separately and can drift
apart. The client's revision and commit subject are stamped by the build
(vite.config.mjs defines import.meta.env.SRO_CLIENT_REVISION and
SRO_CLIENT_SUBJECT from git), and its uptime is this page's. The server's
come from the Agent's GET /title/build, refreshed while open, and its uptime keeps
counting on this page's monotonic clock. Whatever is unknown is left out.
The subjects appear as selectable diagnostic details.

The frame clock drives refresh, timeout and retry; this owner keeps no
timer.

===========================================================================
*/

import { RELEASE_PROTOCOL, RELEASE_PROTOCOL_HEADER } from "@/engine/foundation/release/protocol";

const ROUTE = "/title/build";
// An Agent that is not up yet is asked again, never in a tight loop.
export const RETRY_MS = 30 * 1000;
export const REFRESH_MS = 60 * 1000;
export const REQUEST_TIMEOUT_MS = 10 * 1000;
const TRIGGER_GAP_MS = 5 * 1000;
// Seven hex digits, as git abbreviates.
const SHORT_REVISION = 7;
const REVISION_PATTERN = /^[0-9a-f]{7,64}$/;
// A commit subject longer than this is cut for the hover text.
const MAX_SUBJECT = 200;

interface ServerBuild {
	readonly revision: string;
	readonly subject: string;
	readonly uptimeSeconds: number;
	readonly receivedAtMs: number;
}

/*
================
BuildReadout

The chip's lines (one per known build) and their hover text.
================
*/
export interface BuildReadout {
	readonly lines: readonly string[];
	readonly detail: string;
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
commitSubject

A commit subject on one line, or "" when there is none.
================
*/
export function commitSubject( value: unknown ) {
	return typeof value === "string" ? value.replace( /\s+/g, " " ).trim().slice( 0, MAX_SUBJECT ) : "";
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

`clientRevision` and `clientSubject` are what this bundle was built from
(undefined when the build stamped none).
================
*/
export function createBuildInfo( apiBase: string, clientRevision: unknown, clientSubject: unknown ) {
	const client = shortRevision( clientRevision );
	const clientDetail = commitSubject( clientSubject );
	let server: ServerBuild | null = null;
	let request: AbortController | null = null;
	let requestedAtMs = -Infinity, nextAtMs = 0;
	let stale = false, disposed = false, wasActive = false, lastConnection = "";

	/*
 ================
 cancel

 Detach before abort: a late completion must not overwrite a newer sample.
 ================
 */
	function cancel() {
		const previous = request;
		request = null;
		previous?.abort();
	}

	/*
 ================
 load
 ================
 */
	function load( nowMs: number ) {
		const controller = new AbortController();
		request = controller;
		requestedAtMs = nowMs;
		fetch( apiBase + ROUTE, {
			headers: { [RELEASE_PROTOCOL_HEADER]: String( RELEASE_PROTOCOL ) },
			credentials: "omit",
			cache: "no-store",
			redirect: "error",
			signal: controller.signal
		} ).then( response => {
			if ( !response.ok ) throw new Error( "Build information unavailable" );
			return response.json();
		} ).then( ( body: { build?: { revision?: unknown; subject?: unknown; uptimeSeconds?: unknown; }; } ) => {
			if ( request !== controller || disposed ) return;
			const uptime = body?.build?.uptimeSeconds;
			if ( typeof uptime !== "number" || !Number.isFinite( uptime ) || uptime < 0 ) {
				throw new Error( "Invalid build uptime" );
			}
			const receivedAtMs = performance.now();
			server = {
				revision: shortRevision( body.build?.revision ) ?? "unknown",
				subject: commitSubject( body.build?.subject ),
				uptimeSeconds: uptime,
				receivedAtMs
			};
			stale = false;
			nextAtMs = receivedAtMs + REFRESH_MS;
		} ).catch( () => {
			if ( request !== controller || disposed ) return;
			stale = true;
			nextAtMs = performance.now() + RETRY_MS;
		} ).finally( () => {
			if ( request === controller ) request = null;
		} );
	}
	return {
		/*
  ================
  readout

  Hidden panels do not poll. Reopening or reconnecting refreshes promptly,
  throttled across rapid transitions. All ages use the page monotonic clock.
  ================
  */
		readout( nowMs: number, active = true, connection = "" ): BuildReadout {
			const triggered = active && (!wasActive || connection !== lastConnection);
			wasActive = active;
			lastConnection = connection;
			if ( !active || disposed ) cancel();
			else {
				if ( request && nowMs - requestedAtMs >= REQUEST_TIMEOUT_MS ) {
					cancel();
					stale = true;
					nextAtMs = nowMs + RETRY_MS;
				}
				if ( !request && (nowMs >= nextAtMs || triggered && nowMs - requestedAtMs >= TRIGGER_GAP_MS) ) {
					load( nowMs );
				}
			}
			const lines = [ `Client ${client ?? "unknown"} · page open ${formatUptime( nowMs / 1000 )}` ];
			const details = clientDetail ? [ `Client: ${clientDetail}` ] : [];
			if ( server ) {
				const age = Math.max( 0, nowMs - server.receivedAtMs );
				const outdated = stale || age >= REFRESH_MS + REQUEST_TIMEOUT_MS;
				lines.push(
					outdated ?
						`Agent ${server.revision} · stale (confirmed ${formatUptime( age / 1000 )} ago)` :
						`Agent ${server.revision} · uptime ${formatUptime( server.uptimeSeconds + age / 1000 )}`
				);
				if ( server.subject ) details.push( `Agent: ${server.subject}` );
			} else lines.push( `Agent ${request ? "checking…" : "unavailable"}` );
			return { lines, detail: details.join( "\n" ) };
		},
		/*
  ================
  dispose
  ================
  */
		dispose() {
			disposed = true;
			cancel();
		}
	};
}
