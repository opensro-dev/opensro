/*
===========================================================================

maintenance-notice.test.mjs - the title maintenance notice and coming-soon rows

Port-only, not native (#3925). The notice line (foundation model + feed
owner) shows the public schedule's running maintenance and hides
without one; the feed owner never fetches while the row is off. A
coming-soon server row bands "Coming soon", stays a disabled control,
and the list's accept refuses it even as the draft. The server-list
listing itself (flag on/off, no transport URL, refused login) is pinned
Go-side (agent/server's TestTitleServerListComingSoonShards).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readFileSync } from "node:fs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { scheduleFeed } = await load( "foundation/ui/event-calendar.ts" );
const { maintenanceNoticeText } = await load( "foundation/ui/maintenance-notice.ts" );
const { serverStateBand } = await load( "foundation/ui/title-status.ts" );
const { createMaintenanceNoticeHud } = await load( "runtime/ui/hud/maintenance-notice-hud.ts" );
const { createSessionDecoder } = await load( "runtime/simulation/worker/session/decode/decode.ts" );
const { createTitleUi } = await import( "../../src/engine/runtime/ui/title/title.ts" );
const { initialCreation } = await import( "../../src/engine/foundation/ui/character-create.ts" );

// Monday 2026-11-23 12:00 UTC on the server; the maintenance gate has
// been up since Sunday 22:00 and announces no end.
const MAINTENANCE_FEED = {
	serverTime: "2026-11-23T12:00:00Z",
	events: [
		{ kind: "maintenance", title: "Maintenance", startsAt: "2026-11-22T22:00:00Z", endsAt: null, running: true },
		{
			kind: "capture-the-flag",
			title: "Capture the Flag",
			startsAt: "2026-11-23T14:00:00Z",
			endsAt: "2026-11-23T14:30:00Z",
			running: false
		}
	]
};
const QUIET_FEED = {
	serverTime: "2026-11-23T12:00:00Z",
	events: [
		{
			kind: "capture-the-flag",
			title: "Capture the Flag",
			startsAt: "2026-11-23T14:00:00Z",
			endsAt: "2026-11-23T14:30:00Z",
			running: false
		}
	]
};
const ENDED_FEED = {
	serverTime: "2026-11-23T12:00:00Z",
	events: [
		{
			kind: "maintenance",
			title: "Maintenance",
			startsAt: "2026-11-24T09:00:00Z",
			endsAt: "2026-11-24T11:00:00Z",
			running: true
		}
	]
};

test("the running maintenance shows start and the release note", () => {
	assert.equal(
		maintenanceNoticeText( scheduleFeed( MAINTENANCE_FEED ) ),
		"Maintenance since Sun Nov 22 22:00 - a release is in progress, login opens when it ends."
	);
});

test("an announced end shows as the expected end", () => {
	assert.equal(
		maintenanceNoticeText( scheduleFeed( ENDED_FEED ) ),
		"Maintenance Tue Nov 24 09:00 to Tue Nov 24 11:00 - login opens when it ends."
	);
});

test("no running maintenance hides the notice", () => {
	assert.equal( maintenanceNoticeText( scheduleFeed( QUIET_FEED ) ), null );
	const done = { ...MAINTENANCE_FEED, events: [ { ...MAINTENANCE_FEED.events[0], running: false } ] };
	assert.equal( maintenanceNoticeText( scheduleFeed( done ) ), null );
});

test("the feed owner never fetches while the row is off", async () => {
	let calls = 0;
	const hud = createMaintenanceNoticeHud( async () => {
		calls++;
		return MAINTENANCE_FEED;
	} );
	hud.frame( "https://fixture.invalid", 1000, false );
	await new Promise( resolve => setImmediate( resolve ) );
	assert.equal( calls, 0 );
	assert.equal( hud.notice(), null );
});

test("the feed owner fetches while on and keeps the last good line over a failure", async () => {
	let body = MAINTENANCE_FEED;
	let fail = false;
	const hud = createMaintenanceNoticeHud( async () => {
		if ( fail ) throw Error( "down" );
		return body;
	} );
	hud.frame( "https://fixture.invalid", 1000, true );
	await new Promise( resolve => setImmediate( resolve ) );
	assert.match( hud.notice() ?? "", /^Maintenance since / );

	// Inside the refresh hour nothing refetches; after it, a failure
	// keeps the line and a quiet feed clears it.
	hud.frame( "https://fixture.invalid", 2000, true );
	fail = true;
	hud.frame( "https://fixture.invalid", 61_001, true );
	await new Promise( resolve => setImmediate( resolve ) );
	assert.match( hud.notice() ?? "", /^Maintenance since / );
	fail = false;
	body = QUIET_FEED;
	hud.frame( "https://fixture.invalid", 61_001 + 15_000, true );
	await new Promise( resolve => setImmediate( resolve ) );
	assert.equal( hud.notice(), null );
});

test("the server rows decode the coming-soon mark, absent is native", () => {
	const rows = createSessionDecoder().servers( [
		{
			id: "open",
			name: "Open",
			onlinePlayers: 3,
			capacity: 100,
			nativeServerId: 1,
			nativeFarmId: 1,
			isTest: false,
			operating: true,
			transportUrl: "https://127.0.0.1:9002"
		},
		{
			id: "announced",
			name: "Announced",
			onlinePlayers: 0,
			capacity: 100,
			nativeServerId: 2,
			nativeFarmId: 1,
			isTest: false,
			operating: false,
			comingSoon: true,
			transportUrl: ""
		}
	] );
	assert.equal( rows[0].comingSoon, false );
	assert.equal( rows[1].comingSoon, true );
});

const TEXTS = {
	UIO_STT_SERVER_TEST: "Check",
	UIO_STT_SERVER_35: "35",
	UIO_STT_SERVER_75: "75",
	UIO_STT_EXCELLENT: "Excellent",
	UIO_STT_CROWDEDNESS: "Crowded",
	UIO_STT_SERVER_FULL: "Full",
	UIO_STT_SERVER_MAX_FULL: "Max Full"
};

test("a coming-soon row bands Coming soon, natives band as before", () => {
	const text = key => TEXTS[key];
	assert.deepEqual(
		serverStateBand( { operating: false, comingSoon: true, onlinePlayers: 0, capacity: 100 }, text ),
		{ label: "Coming soon", color: 0xffb541 }
	);
	assert.equal( serverStateBand( { operating: false, onlinePlayers: 0, capacity: 100 }, text ).label, "Check" );
	assert.equal( serverStateBand( { operating: true, onlinePlayers: 10, capacity: 100 }, text ).label, "Excellent" );
	assert.equal( serverStateBand( { operating: true, onlinePlayers: 50, capacity: 100 }, text ).label, "Crowded" );
	assert.equal( serverStateBand( { operating: true, onlinePlayers: 100, capacity: 100 }, text ).label, "Full" );
});

/*
================
titleFixture

Complete real metadata requests synchronously at the asset boundary,
then render the login screen with the server list open.
================
*/
function titleFixture() {
	let serial = 0;
	const jobs = new Map();
	const owner = createTitleUi( {
		available: () => 8,
		request: url => {
			const bytes = readFileSync( CLIENT_PUBLIC_ROOT + new URL( url ).pathname );
			jobs.set( ++serial, bytes.buffer.slice( bytes.byteOffset, bytes.byteOffset + bytes.byteLength ) );
			return serial;
		},
		take: id => {
			const buffer = jobs.get( id );
			jobs.delete( id );
			return buffer ? { kind: "bytes", id, buffer } : null;
		},
		cancel: id => jobs.delete( id )
	}, "https://fixture.invalid" );
	/** @type {import("../../src/engine/contracts/frontend").FrontendSnapshot} */
	const state = {
		phase: "login",
		generation: 1,
		elapsed: 10,
		alpha: 1,
		logoAlpha: 0,
		error: null,
		race: 0,
		dialog: null,
		creation: {
			selection: { ...initialCreation( 0, 1 ), name: "Fixture" },
			protectorFloor: 1,
			explain: "figure",
			phase: "editing",
			alpha: 1,
			ready: true,
			yaw: 0,
			zoom: false,
			camera: { eye: [ 0, 0, 1 ], target: [ 0, 0, 0 ], fov: 1, near: 1, far: 100 }
		}
	};
	const servers = [
		{
			id: "open",
			name: "Open",
			onlinePlayers: 3,
			capacity: 100,
			nativeServerId: 1,
			nativeFarmId: 1,
			isTest: false,
			operating: true,
			comingSoon: false,
			transportUrl: "https://127.0.0.1:9002"
		},
		{
			id: "announced",
			name: "Announced",
			onlinePlayers: 0,
			capacity: 100,
			nativeServerId: 2,
			nativeFarmId: 1,
			isTest: false,
			operating: false,
			comingSoon: true,
			transportUrl: ""
		}
	];
	let clock = 1000;
	const render = ( /** @type {string} */ draft ) =>
		owner.render( state, 1024, 768, "", "", servers, "open", false, true, {
			hover: null,
			pressed: null,
			focus: null,
			now: clock += 500,
			draft,
			offset: 0,
			message: ""
		} );
	render( "" );
	render( "" );
	return { owner, render };
}

test("a coming-soon row cannot be entered from the list", t => {
	const { owner, render } = titleFixture();
	t.after( () => owner.dispose() );
	const output = render( "announced" );
	const row = output.controls.find( control => control.id === "server:announced" );
	assert.ok( row, "the coming-soon row still renders as a control" );
	assert.equal( row.disabled, true );
	const accept = output.controls.find( control => control.id === "native:server-accept" );
	assert.ok( accept, "the accept button renders" );
	assert.equal( accept.disabled, true, "the draft being a coming-soon shard never enables entry" );
	const open = output.controls.find( control => control.id === "server:open" );
	assert.equal( open?.disabled, false );
});
