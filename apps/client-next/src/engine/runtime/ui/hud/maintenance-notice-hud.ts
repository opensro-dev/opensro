/*
===========================================================================

maintenance-notice-hud.ts - the maintenance notice's feed state

Port-only, not native (#3925). The login and server-list screens' one
fetch of the public schedule feed (GET /public/v1/schedule, #3520)
while the Experimental row is on: one request in flight, a late
completion never overwrites a newer one, a failure keeps the last good
notice and retries on a shorter fuse - the build information's and the
Event Calendar's discipline. The line is the pure model
(foundation/ui/maintenance-notice.ts); this module owns the wire.

===========================================================================
*/

import { scheduleFeed } from "@/engine/foundation/ui/event-calendar";
import { maintenanceNoticeText } from "@/engine/foundation/ui/maintenance-notice";

const ROUTE = "/public/v1/schedule";
const REFRESH_MS = 60_000;
const RETRY_MS = 15_000;

/*
================
createMaintenanceNoticeHud

loadFeed is the injectable half (a test feeds it a fixture); the default
reads the feed off the public API base like the build information does.
================
*/
export function createMaintenanceNoticeHud(
	loadFeed: ( apiBase: string, signal: AbortSignal ) => Promise<unknown> = ( apiBase, signal ) =>
		fetch( apiBase + ROUTE, { credentials: "omit", cache: "no-store", redirect: "error", signal } )
			.then( response => {
				if ( !response.ok ) throw Error( "Schedule feed unavailable" );
				return response.json();
			} )
) {
	let line: string | null = null;
	let request: AbortController | null = null;
	let nextAtMs = 0;

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
 frame

 Called every frame with the row's state: off aborts and holds nothing;
 on fetches when there is no line yet, refreshes a stale one, and
 retries a failed one on the shorter fuse.
 ================
 */
	function frame( apiBase: string, nowMs: number, on: boolean ) {
		if ( !on ) {
			cancel();
			return;
		}
		if ( request || nowMs < nextAtMs ) return;
		const controller = new AbortController();
		request = controller;
		loadFeed( apiBase, controller.signal ).then( body => {
			if ( request !== controller ) return;
			line = maintenanceNoticeText( scheduleFeed( body ) );
			nextAtMs = nowMs + REFRESH_MS;
		} ).catch( () => {
			if ( request !== controller ) return;
			nextAtMs = nowMs + RETRY_MS;
		} ).finally( () => {
			if ( request === controller ) request = null;
		} );
	}

	/*
 ================
 notice

 The current line, or null while none has landed (or none runs).
 ================
 */
	function notice(): string | null {
		return line;
	}

	return { frame, notice };
}
