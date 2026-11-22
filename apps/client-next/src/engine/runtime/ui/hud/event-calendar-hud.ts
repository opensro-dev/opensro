/*
===========================================================================

event-calendar-hud.ts - the Event Calendar's feed state

Port-only, not native (#3532). One fetch of the public schedule feed
(GET /public/v1/schedule, #3520) while the window is open, refreshed on
the build information's discipline: one request in flight, a late
completion never overwrites a newer one, a failure keeps the last good
week and retries on a shorter fuse. The parse and the grid are the pure
model (foundation/ui/event-calendar.ts); this module owns the wire and
the selected event.

===========================================================================
*/

import {
	calendarWeek,
	defaultCalendarEvent,
	scheduleFeed,
	type CalendarDay,
	type CalendarEvent,
	type ScheduleFeed
} from "@/engine/foundation/ui/event-calendar";

const ROUTE = "/public/v1/schedule";
const REFRESH_MS = 60_000;
const RETRY_MS = 15_000;

/*
================
EventCalendarState
================
*/
export interface EventCalendarState {
	readonly status: "idle" | "loading" | "ready" | "failed";
	readonly week: readonly CalendarDay[];
	readonly selected: CalendarEvent | null;
}

/*
================
createEventCalendarHud

loadFeed is the injectable half (a test feeds it a fixture); the default
reads the feed off the public API base like the build information does.
================
*/
export function createEventCalendarHud(
	loadFeed: ( apiBase: string, signal: AbortSignal ) => Promise<unknown> = ( apiBase, signal ) =>
		fetch( apiBase + ROUTE, { credentials: "omit", cache: "no-store", redirect: "error", signal } )
			.then( response => {
				if ( !response.ok ) throw Error( "Schedule feed unavailable" );
				return response.json();
			} )
) {
	let feed: ScheduleFeed | null = null;
	let week: readonly CalendarDay[] = [];
	let selected: CalendarEvent | null = null;
	let failed = false;
	let request: AbortController | null = null;
	let nextAtMs = 0;
	let receivedAtMs = 0;

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
 state
 ================
 */
	function state(): EventCalendarState {
		return {
			status: feed ? "ready" : request ? "loading" : failed ? "failed" : "idle",
			week,
			selected
		};
	}

	/*
 ================
 serverNowMs

 The server's clock at a local instant: the feed's serverTime advanced
 by the local time since it landed.
 ================
 */
	function serverNowMs( nowMs: number ): number {
		return feed ? feed.serverTimeMs + (nowMs - receivedAtMs) : nowMs;
	}

	/*
 ================
 frame

 Called every frame with the window's openness: closed aborts and
 forgets nothing; open fetches when there is no week yet, refreshes a
 stale one, and retries a failed one on the shorter fuse.
 ================
 */
	function frame( apiBase: string, nowMs: number, open: boolean ) {
		if ( !open ) {
			cancel();
			return;
		}
		if ( request || nowMs < nextAtMs ) return;
		const controller = new AbortController();
		request = controller;
		loadFeed( apiBase, controller.signal ).then( body => {
			if ( request !== controller ) return;
			feed = scheduleFeed( body );
			week = calendarWeek( feed );
			receivedAtMs = nowMs;
			failed = false;
			nextAtMs = nowMs + REFRESH_MS;
			const kept = selected &&
				week.flatMap( day => day.events ).find( event =>
					event.kind === selected!.kind && event.startsAtMs === selected!.startsAtMs
				);
			selected = kept ?? defaultCalendarEvent( week, serverNowMs( nowMs ) );
		} ).catch( () => {
			if ( request !== controller ) return;
			failed = true;
			nextAtMs = nowMs + RETRY_MS;
		} ).finally( () => {
			if ( request === controller ) request = null;
		} );
	}

	/*
 ================
 select
 ================
 */
	function select( event: CalendarEvent ) {
		selected = event;
	}

	/*
 ================
 reset

 Leaving the session drops the week and the request with it.
 ================
 */
	function reset() {
		cancel();
		feed = null;
		week = [];
		selected = null;
		failed = false;
		nextAtMs = 0;
	}

	return { state, serverNowMs, frame, select, reset };
}
