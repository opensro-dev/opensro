/*
===========================================================================

event-calendar.test.mjs - the Event Calendar's week grid from a feed fixture

Port-only, not native (#3532). The fixture stands for GET
/public/v1/schedule (#3520): serverTime anchors today, and every expected
clock below is read in UTC, as the model reads them.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const {
	scheduleFeed,
	calendarWeek,
	defaultCalendarEvent,
	clockText,
	dateText,
	dayHeading,
	spanText,
	eventStatusText
} = await load( "foundation/ui/event-calendar.ts" );
const { systemMenu, SYSTEM_MENU_HEIGHT } = await load( "foundation/ui/system-menu.ts" );
const { experimentalOptions } = await load( "foundation/ui/experimental-options.ts" );

// Monday 2026-11-23 12:00 UTC on the server.
const FEED = {
	serverTime: "2026-11-23T12:00:00Z",
	events: [
		{ kind: "maintenance", title: "Maintenance", startsAt: "2026-11-22T22:00:00Z", endsAt: null, running: true },
		{
			kind: "fortress",
			title: "Fortress War",
			startsAt: "2026-11-23T18:00:00Z",
			endsAt: "2026-11-23T20:00:00Z",
			running: false
		},
		{
			kind: "capture-the-flag",
			title: "Capture the Flag",
			startsAt: "2026-11-23T14:00:00Z",
			endsAt: "2026-11-23T14:30:00Z",
			running: false
		},
		{
			kind: "capture-the-flag",
			title: "Capture the Flag",
			startsAt: "2026-11-25T14:00:00Z",
			endsAt: "2026-11-25T14:30:00Z",
			running: false
		},
		{
			kind: "halloween",
			title: "Halloween Day",
			startsAt: "2026-11-29T00:00:00Z",
			endsAt: "2026-11-30T02:00:00Z",
			running: false
		},
		{
			kind: "fortress",
			title: "Next Fortress War",
			startsAt: "2026-12-01T18:00:00Z",
			endsAt: "2026-12-01T20:00:00Z",
			running: false
		}
	]
};

test("the grid is seven days, today first and highlighted", () => {
	const week = calendarWeek( scheduleFeed( FEED ) );
	assert.equal( week.length, 7 );
	assert.deepEqual(
		week.map( day => dayHeading( day.startMs, day === week[0] ).title ),
		[ "Today", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun" ]
	);
	assert.equal( dayHeading( week[0].startMs, true ).date, "Nov 23" );
	// The war outside the week (Dec 1) never lists.
	assert.equal( week.flatMap( day => day.events ).length, 5 );
});

test("events bucket by start day in start order, and a running carry-over lists today", () => {
	const week = calendarWeek( scheduleFeed( FEED ) );
	// Today: yesterday's open-ended maintenance first (earliest start), then by start.
	assert.deepEqual(
		week[0].events.map( event => event.title + " " + clockText( event.startsAtMs ) ),
		[ "Maintenance 22:00", "Capture the Flag 14:00", "Fortress War 18:00" ]
	);
	assert.deepEqual( week[2].events.map( event => event.title ), [ "Capture the Flag" ] );
	assert.deepEqual( week[6].events.map( event => event.title ), [ "Halloween Day" ] );
});

test("an empty day still gets its column", () => {
	const week = calendarWeek( scheduleFeed( FEED ) );
	assert.deepEqual( week[1].events, [] );
	assert.deepEqual( week[3].events, [] );
	const none = calendarWeek( scheduleFeed( { serverTime: FEED.serverTime, events: [] } ) );
	assert.equal( none.length, 7 );
	assert.ok( none.every( day => day.events.length === 0 ) );
});

test("clocks, spans and statuses read in server time", () => {
	const week = calendarWeek( scheduleFeed( FEED ) );
	const [maintenance, flag, war] = week[0].events;
	const now = Date.parse( FEED.serverTime );
	assert.equal( spanText( flag ), "14:00-14:30" );
	assert.equal( spanText( maintenance ), "from 22:00" );
	assert.equal( spanText( week[6].events[0] ), "00:00-Mon Nov 30 02:00" );
	assert.equal( dateText( week[6].startMs ), "Sun Nov 29" );
	assert.equal( eventStatusText( maintenance, now ), "Running now" );
	assert.equal( eventStatusText( flag, now ), "Starts in 2h 00m" );
	assert.equal( eventStatusText( war, now ), "Starts in 6h 00m" );
	assert.equal( eventStatusText( flag, Date.parse( "2026-11-23T15:00:00Z" ) ), "Ended" );
	assert.equal( defaultCalendarEvent( week, now ).title, "Maintenance" );
});

test("a malformed feed is refused whole", () => {
	assert.throws( () => scheduleFeed( null ), /Invalid schedule feed/ );
	assert.throws( () => scheduleFeed( { serverTime: "soon", events: [] } ), /Invalid schedule feed/ );
	assert.throws(
		() => scheduleFeed( { serverTime: FEED.serverTime, events: [ { kind: "x" } ] } ),
		/Invalid schedule feed event/
	);
	assert.throws(
		() => scheduleFeed( { serverTime: FEED.serverTime, events: [ { ...FEED.events[1], endsAt: 7 } ] } ),
		/Invalid schedule feed event/
	);
});

test("the System menu gains the calendar row only while its Experimental row is on", () => {
	const layout = Object.fromEntries(
		[ 10, 11, 13, 14 ].map(
			id => [ "node" + id, { id, text: "UIIT_" + id, size: [ 160, 26 ], rect: [ 0, 0, 160, 26 ] } ]
		)
	);
	layout.GDR_SYSTEM_FRAME = { id: 0, text: "GDR_SYSTEM_FRAME", size: [ 214, 212 ], rect: [ 0, 0, 214, 212 ] };
	layout.GDR_SYSTEM_BGTILE = { id: 0, text: "GDR_SYSTEM_BGTILE", size: [ 214, 146 ], rect: [ 0, 0, 214, 146 ] };
	const off = systemMenu( layout, 0, 0, false ), on = systemMenu( layout, 0, 0, true );
	assert.deepEqual( off.buttons.map( button => button.id ), [ 10, 15, 11, 13, 14 ] );
	assert.deepEqual( on.buttons.map( button => button.id ), [ 10, 15, 16, 11, 13, 14 ] );
	assert.equal( off.frame[3], SYSTEM_MENU_HEIGHT );
	assert.equal( on.frame[3], SYSTEM_MENU_HEIGHT + 34 );
});

test("the Experimental row is off unless explicitly turned on", () => {
	assert.equal( experimentalOptions().eventCalendar, false );
	assert.equal( experimentalOptions( { eventCalendar: true } ).eventCalendar, true );
	assert.equal( experimentalOptions( { eventCalendar: 1 } ).eventCalendar, false );
});
