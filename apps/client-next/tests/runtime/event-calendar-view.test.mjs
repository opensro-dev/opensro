/*
===========================================================================

event-calendar-view.test.mjs - the Event Calendar window as draw operations

Port-only, not native (#3532). The view (runtime/ui/hud/event-calendar-
view.ts) turns the feed state into plain ops, so its layout is pinned
here without the renderer: today first and tinted, six events then
"+N more", an empty day, the detail panel, and the loading/failed
lines. Times are the fixture's server times, read in UTC.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { scheduleFeed, calendarWeek } = await load( "foundation/ui/event-calendar.ts" );
const {
	eventCalendarView,
	eventCalendarPictures,
	EVENT_CALENDAR_WIDTH,
	EVENT_CALENDAR_HEIGHT
} = await load( "runtime/ui/hud/event-calendar-view.ts" );

// Monday 2026-11-23 12:00 UTC on the server.
const FEED = {
	serverTime: "2026-11-23T12:00:00Z",
	events: [
		{ kind: "maintenance", title: "Maintenance", startsAt: "2026-11-22T22:00:00Z", endsAt: null, running: true },
		{
			kind: "capture-the-flag",
			title: "Capture the Flag",
			startsAt: "2026-11-23T14:00:00Z",
			endsAt: "2026-11-23T14:30:00Z",
			running: false
		},
		{
			kind: "fortress",
			title: "Fortress War",
			startsAt: "2026-11-25T18:00:00Z",
			endsAt: "2026-11-25T20:00:00Z",
			running: false
		}
	]
};
const BURST = {
	serverTime: "2026-11-23T12:00:00Z",
	events: Array.from( { length: 8 }, ( _, index ) => ({
		kind: "event",
		title: "Event " + (index + 1),
		startsAt: "2026-11-23T" + String( index ).padStart( 2, "0" ) + ":00:00Z",
		endsAt: null,
		running: false
	}) )
};
const ORIGIN = [ 100, 50 ];
const TILE = "tile.png";

function ready( feed, selected = null ) {
	const parsed = scheduleFeed( feed );
	return {
		state: { status: "ready", week: calendarWeek( parsed ), selected },
		serverNowMs: parsed.serverTimeMs
	};
}

function texts( ops ) {
	return ops.filter( op => op.kind === "text" );
}

test("today's column stands first and tinted, with the week around it", () => {
	const { state, serverNowMs } = ready( FEED );
	const ops = eventCalendarView( { origin: ORIGIN, tile: TILE, state, serverNowMs } );
	const lines = texts( ops );
	assert.equal( lines[0].value, "Today" );
	assert.equal( lines[1].value, "Nov 23" );
	assert.ok( lines.some( line => line.value === "Tue" && line.rect[0] > lines[0].rect[0] ) );
	const tint = ops.find( op => op.kind === "rect" );
	assert.deepEqual( tint.color, [ 1, .85, .5, .08 ] );
	assert.deepEqual( tint.rect, [ 119, 98, 108, 252 ] );
	assert.ok( lines.some( line => line.value === "Mon Nov 23 - Sun Nov 29" ) );
	assert.ok( lines.some( line => line.value === "All times are server time." ) );
	const tiles = ops.filter( op => op.kind === "tile" );
	assert.deepEqual( tiles.map( op => op.rect ), [
		[ 116, 94, EVENT_CALENDAR_WIDTH - 32, 268 ],
		[ 116, 372, EVENT_CALENDAR_WIDTH - 32, 96 ]
	] );
	const pictures = eventCalendarPictures( ops );
	assert.ok( pictures.includes( TILE ) );
	assert.ok( pictures.some( path => path.endsWith( "interface/option/opt_key02.png" ) ) );
});

test("a day lists six events, then counts the rest", () => {
	const { state, serverNowMs } = ready( BURST );
	const ops = eventCalendarView( { origin: ORIGIN, tile: TILE, state, serverNowMs } );
	const buttons = ops.filter( op => op.kind === "button" );
	assert.deepEqual(
		buttons.map( button => button.id ),
		[ 0, 1, 2, 3, 4, 5 ].map( index => "event-calendar-event:0:" + index )
	);
	assert.ok( texts( ops ).some( line => line.value === "+2 more" ) );
});

test("an empty day says so, and a selected event fills the detail panel", () => {
	const { state, serverNowMs } = ready( FEED );
	const picked = state.week[0].events.find( event => event.title === "Capture the Flag" );
	const ops = eventCalendarView( {
		origin: ORIGIN,
		tile: TILE,
		state: { ...state, selected: picked },
		serverNowMs
	} );
	const lines = texts( ops );
	assert.ok( lines.some( line => line.value === "No events" ) );
	const detail = lines.find( line => line.value === "Capture the Flag" && line.rect[1] === ORIGIN[1] + 332 );
	assert.ok( detail );
	assert.deepEqual( detail.rect, [ 128, 382, EVENT_CALENDAR_WIDTH - 56, 16 ] );
	assert.deepEqual( detail.color, [ 1, .85, .5, 1 ] );
	assert.ok( lines.some( line => line.value === "Mon Nov 23  14:00-14:30" ) );
	assert.ok( lines.some( line => line.value === "Starts in 2h 00m" ) );
	const button = ops.find( op => op.kind === "button" && op.label === "Capture the Flag" );
	assert.equal( button.selected, true );
	assert.deepEqual( button.color, [ 1, .85, .5, 1 ] );
});

test("no selection and an empty week name the empty detail", () => {
	const { state, serverNowMs } = ready( { serverTime: FEED.serverTime, events: [] } );
	const ops = eventCalendarView( { origin: ORIGIN, tile: TILE, state, serverNowMs } );
	assert.ok( texts( ops ).some( line => line.value === "No events this week." ) );
	assert.equal( ops.filter( op => op.kind === "button" ).length, 0 );
});

test("the loading and failed lines stand in for the grid", () => {
	for (
		const [status, line] of [
			[ "loading", "Loading the schedule..." ],
			[ "idle", "Loading the schedule..." ],
			[ "failed", "The schedule could not be loaded." ]
		]
	) {
		const ops = eventCalendarView( {
			origin: ORIGIN,
			tile: TILE,
			state: { status, week: [], selected: null },
			serverNowMs: 0
		} );
		assert.deepEqual( texts( ops ).map( op => op.value ), [ line, "All times are server time." ] );
		assert.equal( EVENT_CALENDAR_HEIGHT, 470 );
	}
});
