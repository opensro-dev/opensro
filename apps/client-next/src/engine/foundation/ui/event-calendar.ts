/*
===========================================================================

event-calendar.ts - the week's scheduled events as a seven-day grid

Port-only, not native (#3532): neither native client has this window. The
model under it: the public schedule feed (GET /public/v1/schedule, the
#3520 feed the website's schedule panel reads) parsed strictly, its events
bucketed over the seven server days starting today, and the text the
window draws (clocks, spans, countdowns). The fetch and selection state
live in runtime/ui/hud/event-calendar-hud.ts; the window in ui.ts.

Every time is server time. The feed's serverTime anchors today and every
timestamp it carries is UTC, so days and clocks are read in UTC throughout.

===========================================================================
*/

const DAY_MS = 86_400_000;
const WEEKDAYS = [ "Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat" ];
const MONTHS = [ "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec" ];

/*
================
CalendarEvent

One event of the feed in epoch milliseconds. endsAtMs is null for an
open-ended event (a maintenance with no announced end).
================
*/
export interface CalendarEvent {
	readonly kind: string;
	readonly title: string;
	readonly startsAtMs: number;
	readonly endsAtMs: number | null;
	readonly running: boolean;
}

/*
================
ScheduleFeed

The parsed feed: the server's clock when it answered, and its events.
================
*/
export interface ScheduleFeed {
	readonly serverTimeMs: number;
	readonly events: readonly CalendarEvent[];
}

/*
================
CalendarDay

One column of the grid: the server day's UTC start and its events in
start order.
================
*/
export interface CalendarDay {
	readonly startMs: number;
	readonly events: readonly CalendarEvent[];
}

/*
================
scheduleFeed

The strict reader for the feed body. The server generates the body from
one typed shape, so a malformed row refuses the whole answer rather than
drawing a week with a silent hole in it.
================
*/
export function scheduleFeed( value: unknown ): ScheduleFeed {
	if ( !value || typeof value !== "object" || Array.isArray( value ) ) throw Error( "Invalid schedule feed" );
	const raw = value as Record<string, unknown>;
	const serverTimeMs = typeof raw.serverTime === "string" ? Date.parse( raw.serverTime ) : NaN;
	if ( !Number.isFinite( serverTimeMs ) || !Array.isArray( raw.events ) ) throw Error( "Invalid schedule feed" );
	const events = raw.events.map( ( row ): CalendarEvent => {
		if ( !row || typeof row !== "object" || Array.isArray( row ) ) throw Error( "Invalid schedule feed event" );
		const event = row as Record<string, unknown>;
		const startsAtMs = typeof event.startsAt === "string" ? Date.parse( event.startsAt ) : NaN;
		const endsAtMs = event.endsAt === null ?
			null :
			typeof event.endsAt === "string" ?
			Date.parse( event.endsAt ) :
			NaN;
		if (
			typeof event.kind !== "string" || !event.kind || typeof event.title !== "string" || !event.title ||
			!Number.isFinite( startsAtMs ) || endsAtMs === undefined ||
			(typeof endsAtMs === "number" && !Number.isFinite( endsAtMs )) ||
			typeof event.running !== "boolean"
		) throw Error( "Invalid schedule feed event" );
		return {
			kind: event.kind,
			title: event.title,
			startsAtMs,
			endsAtMs: endsAtMs as number | null,
			running: event.running
		};
	} );
	return { serverTimeMs, events };
}

/*
================
calendarWeek

The seven server days starting with the day the feed's serverTime falls
in, today first. An event lists on the day it starts; one that started
before today and has not ended (an open-ended maintenance, a war running
over midnight) also lists today, so the first column always answers "what
is on now". Events outside the week are left out.
================
*/
export function calendarWeek( feed: ScheduleFeed ): readonly CalendarDay[] {
	const todayStart = Math.floor( feed.serverTimeMs / DAY_MS ) * DAY_MS;
	const days: CalendarDay[] = [];
	for ( let i = 0; i < 7; i++ ) {
		const startMs = todayStart + i * DAY_MS, endMs = startMs + DAY_MS;
		const events = feed.events.filter( event => {
			if ( event.startsAtMs >= startMs && event.startsAtMs < endMs ) return true;
			return i === 0 && event.startsAtMs < startMs &&
				(event.endsAtMs === null || event.endsAtMs > startMs);
		} ).sort( ( a, b ) => a.startsAtMs - b.startsAtMs );
		days.push( { startMs, events } );
	}
	return days;
}

/*
================
defaultCalendarEvent

The event the detail panel opens on: the first one running, else the
soonest still to come, else the most recent one that started.
================
*/
export function defaultCalendarEvent( week: readonly CalendarDay[], nowMs: number ): CalendarEvent | null {
	const all = week.flatMap( day => day.events );
	return all.find( event => event.running ) ??
		all.filter( event => event.startsAtMs > nowMs ).sort( ( a, b ) => a.startsAtMs - b.startsAtMs )[0] ??
		all.filter( event => event.startsAtMs <= nowMs ).sort( ( a, b ) => b.startsAtMs - a.startsAtMs )[0] ??
		null;
}

/*
================
clockText

A server clock reading, "14:00".
================
*/
export function clockText( atMs: number ): string {
	const at = new Date( atMs );
	return String( at.getUTCHours() ).padStart( 2, "0" ) + ":" + String( at.getUTCMinutes() ).padStart( 2, "0" );
}

/*
================
dateText

A server date, "Tue Nov 24" (no year: the grid never leaves the week,
and the detail panel names a day inside it).
================
*/
export function dateText( atMs: number ): string {
	const at = new Date( atMs );
	return WEEKDAYS[at.getUTCDay()]! + " " + MONTHS[at.getUTCMonth()]! + " " + at.getUTCDate();
}

/*
================
dayHeading

A column's heading: "Today" over the first column, the weekday over
the rest, each with its date under it.
================
*/
export function dayHeading( dayStartMs: number, today: boolean ): { title: string; date: string; } {
	const at = new Date( dayStartMs );
	return {
		title: today ? "Today" : WEEKDAYS[at.getUTCDay()]!,
		date: MONTHS[at.getUTCMonth()]! + " " + at.getUTCDate()
	};
}

/*
================
spanText

An event's run in server time: "14:00-16:00" inside one day, the end's
date named when it runs past midnight, "from 14:00" while open-ended.
================
*/
export function spanText( event: CalendarEvent ): string {
	if ( event.endsAtMs === null ) return "from " + clockText( event.startsAtMs );
	const sameDay = Math.floor( event.endsAtMs / DAY_MS ) === Math.floor( event.startsAtMs / DAY_MS );
	return clockText( event.startsAtMs ) + "-" +
		(sameDay ? clockText( event.endsAtMs ) : dateText( event.endsAtMs ) + " " + clockText( event.endsAtMs ));
}

/*
================
eventStatusText

Where an event stands at a server instant: running, the wait until it
starts (the two largest units), or ended.
================
*/
export function eventStatusText( event: CalendarEvent, nowMs: number ): string {
	if ( event.running ) return "Running now";
	if ( event.startsAtMs > nowMs ) {
		const minutes = Math.floor( (event.startsAtMs - nowMs) / 60_000 );
		if ( minutes < 1 ) return "Starts in under a minute";
		if ( minutes < 60 ) return "Starts in " + minutes + "m";
		const hours = Math.floor( minutes / 60 ), rest = minutes % 60;
		if ( hours < 24 ) return "Starts in " + hours + "h " + String( rest ).padStart( 2, "0" ) + "m";
		return "Starts in " + Math.floor( hours / 24 ) + "d " + (hours % 24) + "h";
	}
	if ( event.endsAtMs !== null && event.endsAtMs <= nowMs ) return "Ended";
	return "Under way";
}
