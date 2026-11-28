/*
===========================================================================

attendance.test.mjs - the attendance calendar codec, lane, state and window

Port-only, not native (#3255). The foundation codec
(foundation/gameplay/attendance) reads and writes the server's bytes
(internal/game/item/wire/attendance.go); the worker lane
(gameplay/attendance) keeps the latest feed; the hud state
(runtime/ui/hud/attendance-hud) reconciles it against the Experimental
row; the view (runtime/ui/hud/attendance-view) turns the state into plain
ops, so the grid, the Receive buttons and the empty state are pinned here
without the renderer.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const codec = await load( "foundation/gameplay/attendance.ts" );
const {
	OP_ATTENDANCE_SNAPSHOT,
	OP_ATTENDANCE_REQUEST,
	decodeAttendanceSnapshot,
	encodeAttendanceCheckIn,
	encodeAttendanceClaimContinuity,
	encodeAttendanceClaimAccumulation,
	dayChecked,
	tierClaimed
} = codec;
const { createAttendance } = await load(
	"runtime/simulation/worker/session/world/gameplay/attendance/attendance.ts"
);
const { experimentalOptions } = await load( "foundation/ui/experimental-options.ts" );
const { createAttendanceHud } = await load( "runtime/ui/hud/attendance-hud.ts" );
const { attendanceView, attendancePictures, attendanceCountdownText } = await load(
	"runtime/ui/hud/attendance-view.ts"
);

// The server's layout test, byte for byte (wire/attendance_test.go).
const SERVER_LAYOUT = Uint8Array.from( [
	...[ 0x73, 0x17, 0x03, 0x00 ], // 202611
	...[ 26, 30 ],
	...[ 0x01, 0x00, 0x00, 0x03 ],
	...[ 0b01, 0b10 ],
	...[ 2, 3, 1, 0, 65, 8, 0, 0 ],
	...[ 2, 5, 2, 0, 66, 67, 0xff, 0, 0 ],
	...[ 0x02, 0x01, 0, 0, 0, 0, 0, 0 ],
	...[ 0x0b, 0x0a, 0, 0, 0, 0, 0, 0 ]
] );

const REWARD = "ITEM_CH_TBLADE_09_C_RARE";
const CONTINUITY = [ 3, 8, 16, 23, 30 ];
const ACCUMULATION = [ 5, 10, 15, 21, 27 ];

// A snapshot body, built the way the server wire writes it.
function snapshotBytes( {
	monthKey = 202611,
	today = 4,
	daysInMonth = 30,
	checked = 0,
	continuityClaimed = 0,
	accumulationClaimed = 0,
	nextResetMs = 5_000_000,
	serverNowMs = 1_000_000
} = {} ) {
	const bytes = [];
	const u8 = value => bytes.push( value & 0xff );
	const u16 = value => bytes.push( value & 0xff, (value >> 8) & 0xff );
	const u32 = value => {
		for ( let i = 0; i < 4; i++ ) bytes.push( Number( (BigInt( value ) >> BigInt( 8 * i )) & 0xffn ) );
	};
	const u64 = value => {
		let left = BigInt( value );
		for ( let i = 0; i < 8; i++ ) {
			bytes.push( Number( left & 0xffn ) );
			left >>= 8n;
		}
	};
	const tiers = list => {
		u8( list.length );
		for ( const days of list ) {
			u8( days );
			u16( REWARD.length );
			bytes.push( ...new TextEncoder().encode( REWARD ) );
		}
	};
	u32( monthKey );
	u8( today );
	u8( daysInMonth );
	u32( checked );
	u8( continuityClaimed );
	u8( accumulationClaimed );
	tiers( CONTINUITY );
	tiers( ACCUMULATION );
	u64( nextResetMs );
	u64( serverNowMs );
	return Uint8Array.from( bytes );
}

function frame( options ) {
	return { opcode: OP_ATTENDANCE_SNAPSHOT, payload: snapshotBytes( options ) };
}

// Days 1..3 and 5 checked: a streak of 3 and 4 days counted.
const CHECKED = 0b10111;

function feed( overrides = {} ) {
	return { ...createFeed( { checked: CHECKED } ), ...overrides };
}

function createFeed( options ) {
	const lane = createAttendance();
	lane.receive( frame( options ) );
	return lane.view();
}

function view( state ) {
	return attendanceView( { origin: [ 100, 50 ], tile: "tile.png", state } );
}

function texts( ops ) {
	return ops.filter( op => op.kind === "text" ).map( op => op.value );
}

function buttons( ops ) {
	return ops.filter( op => op.kind === "button" );
}

test("the codec decodes the server's layout byte for byte", () => {
	assert.equal( OP_ATTENDANCE_SNAPSHOT, 0x3fd2 );
	assert.equal( OP_ATTENDANCE_REQUEST, 0x7fd2 );
	const fields = decodeAttendanceSnapshot( SERVER_LAYOUT );
	assert.deepEqual( fields, {
		monthKey: 202611,
		today: 26,
		daysInMonth: 30,
		checked: 0x0300_0001,
		continuityClaimed: 0b01,
		accumulationClaimed: 0b10,
		continuityTiers: [ { days: 3, reward: "A" }, { days: 8, reward: "" } ],
		accumulationTiers: [ { days: 5, reward: "BC" }, { days: 255, reward: "" } ],
		nextResetMs: 0x0102,
		serverNowMs: 0x0a0b
	} );
	assert.equal( dayChecked( fields.checked, 1 ), true );
	assert.equal( dayChecked( fields.checked, 2 ), false );
	assert.equal( dayChecked( fields.checked, 25 ), true );
	assert.equal( dayChecked( fields.checked, 26 ), true );
	assert.equal( dayChecked( fields.checked, 27 ), false );
	assert.equal( dayChecked( fields.checked, 0 ), false );
	assert.equal( dayChecked( fields.checked, 32 ), false );
	assert.equal( tierClaimed( fields.continuityClaimed, 0 ), true );
	assert.equal( tierClaimed( fields.continuityClaimed, 1 ), false );
	assert.equal( tierClaimed( fields.accumulationClaimed, 1 ), true );
});

test("a full month snapshot carries the placeholder tiers", () => {
	const fields = decodeAttendanceSnapshot( snapshotBytes( { checked: 0xffff_ffff, serverNowMs: 123 } ) );
	assert.equal( fields.checked, 0xffff_ffff );
	assert.deepEqual( fields.continuityTiers.map( tier => tier.days ), CONTINUITY );
	assert.deepEqual( fields.accumulationTiers.map( tier => tier.days ), ACCUMULATION );
	assert.ok( [ ...fields.continuityTiers, ...fields.accumulationTiers ].every( tier => tier.reward === REWARD ) );
	assert.equal( fields.serverNowMs, 123, "the server clock rides last" );
	assert.equal( dayChecked( fields.checked, 31 ), true );
});

test("a truncated or malformed snapshot throws", () => {
	const good = snapshotBytes();
	for ( const cut of [ 0, 5, 12, good.length - 9, good.length - 1 ] ) {
		assert.throws( () => decodeAttendanceSnapshot( good.slice( 0, cut ) ), /attendance/, "cut at " + cut );
	}
	assert.throws( () => decodeAttendanceSnapshot( Uint8Array.of( ...good, 0 ) ), /attendance/, "trailing byte" );
	const noToday = good.slice();
	noToday[4] = 0;
	assert.throws( () => decodeAttendanceSnapshot( noToday ), /attendance month/ );
	const noDays = good.slice();
	noDays[5] = 32;
	assert.throws( () => decodeAttendanceSnapshot( noDays ), /attendance month/ );
	const badReward = SERVER_LAYOUT.slice();
	badReward[16] = 0xff;
	assert.throws( () => decodeAttendanceSnapshot( badReward ), /attendance reward/ );
});

test("check-in and claim requests are the server's bodies", () => {
	assert.deepEqual( [ ...encodeAttendanceCheckIn() ], [ 0 ] );
	assert.deepEqual( [ ...encodeAttendanceClaimContinuity( 0 ) ], [ 1, 0 ] );
	assert.deepEqual( [ ...encodeAttendanceClaimContinuity( 4 ) ], [ 1, 4 ] );
	assert.deepEqual( [ ...encodeAttendanceClaimAccumulation( 2 ) ], [ 2, 2 ] );
	for ( const bad of [ -1, 8, 1.5, Number.NaN ] ) {
		assert.throws( () => encodeAttendanceClaimContinuity( bad ), /attendance tier/ );
		assert.throws( () => encodeAttendanceClaimAccumulation( bad ), /attendance tier/ );
	}
});

test("the lane decodes a snapshot, ignores other opcodes and keeps its revision", () => {
	const lane = createAttendance();
	assert.equal( lane.receive( { opcode: 0x1234, payload: new Uint8Array() } ), false );
	assert.equal( lane.view(), null );
	assert.equal( lane.receive( frame( { checked: CHECKED, continuityClaimed: 1 } ) ), true );
	const first = lane.view();
	assert.equal( first.today, 4 );
	assert.equal( first.checked, CHECKED );
	assert.equal( first.continuityClaimed, 1 );
	assert.equal( first.revision, 1 );
	lane.receive( frame( { checked: CHECKED | 8 } ) );
	assert.equal( lane.view().revision, 2 );
	lane.clear();
	assert.equal( lane.view(), null );
	lane.receive( frame() );
	assert.equal( lane.view().revision, 3, "the revision survives a clear" );
});

test("a malformed snapshot is refused whole and leaves the last feed", () => {
	const lane = createAttendance();
	lane.receive( frame( { checked: CHECKED } ) );
	const good = frame();
	const short = { opcode: OP_ATTENDANCE_SNAPSHOT, payload: good.payload.slice( 0, good.payload.length - 3 ) };
	assert.throws( () => lane.receive( short ), /attendance/ );
	assert.equal( lane.view().revision, 1 );
	assert.equal( lane.view().checked, CHECKED );
});

test("the Experimental row is off unless turned on", () => {
	assert.equal( experimentalOptions().attendanceCalendar, false );
	assert.equal( experimentalOptions( { attendanceCalendar: true } ).attendanceCalendar, true );
	assert.equal( experimentalOptions( { attendanceCalendar: 1 } ).attendanceCalendar, false );
});

test("the hud state drops the feed while the Experimental row is off", () => {
	const hud = createAttendanceHud();
	const current = feed();
	hud.reconcile( current, false, 5000 );
	assert.equal( hud.state( 5000 ).feed, null, "off: the feed is never kept" );
	hud.reconcile( current, true, 5000 );
	assert.equal( hud.state( 5000 ).feed, current );
	assert.equal( hud.state( 8000 ).serverNowMs, current.serverNowMs + 3000, "the clock runs from the snapshot" );
	hud.reconcile( current, false, 9000 );
	assert.equal( hud.state( 9000 ).feed, null, "turning the row off drops it" );
	hud.reconcile( null, true, 9000 );
	assert.equal( hud.state( 9000 ).feed, null, "no feed, nothing to show" );
	hud.reconcile( current, true, 9000 );
	hud.reset();
	assert.equal( hud.state( 9000 ).feed, null );
	assert.equal( hud.state( 9000 ).tab, "calendar" );
});

test("the hud returns check-in and claim commands, only when they can matter", () => {
	const hud = createAttendanceHud();
	assert.equal( hud.activate( "attendance-check-in" ), null, "no feed, no request" );
	// Today (day 4) is unchecked; days 1..3 and 5 are checked: streak 3, count 4.
	hud.reconcile( feed( { continuityClaimed: 0, accumulationClaimed: 0 } ), true, 0 );
	assert.deepEqual( hud.activate( "attendance-check-in" ), { kind: "attendance", operation: 0, tier: 0 } );
	assert.deepEqual( hud.activate( "attendance-claim:continuity:0" ), { kind: "attendance", operation: 1, tier: 0 } );
	assert.equal( hud.activate( "attendance-claim:continuity:1" ), null, "8 days is not reached" );
	assert.equal( hud.activate( "attendance-claim:accumulation:0" ), null, "5 days is not reached" );
	assert.equal( hud.activate( "attendance-claim:accumulation:x" ), null );
	assert.equal( hud.activate( "attendance-claim:continuity:9" ), null );

	hud.reconcile( feed( { checked: 0b11111, continuityClaimed: 0b1, accumulationClaimed: 0 } ), true, 0 );
	assert.equal( hud.activate( "attendance-claim:continuity:0" ), null, "already received" );
	assert.deepEqual(
		hud.activate( "attendance-claim:accumulation:0" ),
		{ kind: "attendance", operation: 2, tier: 0 }
	);
	hud.reconcile( feed( { checked: 0b11111 | 8, today: 4 } ), true, 0 );
	assert.equal( hud.activate( "attendance-check-in" ), null, "today is already checked" );
});

test("tabs and days are the window's own", () => {
	const hud = createAttendanceHud();
	hud.reconcile( feed(), true, 0 );
	assert.equal( hud.activate( "attendance-tab:rewards" ), null );
	assert.equal( hud.state( 0 ).tab, "rewards" );
	assert.equal( hud.activate( "attendance-tab:calendar" ), null );
	assert.equal( hud.state( 0 ).tab, "calendar" );
	hud.activate( "attendance-day:3" );
	assert.equal( hud.state( 0 ).selectedDay, 3 );
	hud.activate( "attendance-day:3" );
	assert.equal( hud.state( 0 ).selectedDay, 0, "a second click clears the choice" );
	hud.activate( "attendance-day:99" );
	assert.equal( hud.state( 0 ).selectedDay, 0 );
});

test("the calendar draws the month grid, today, END and Check Attendance", () => {
	const ops = view( { feed: feed(), tab: "calendar", selectedDay: 0, serverNowMs: 1_000_000 } );
	const lines = texts( ops );
	assert.ok( lines.includes( "Attendance Calendar" ) );
	assert.ok( lines.includes( "2026-11 - day 4/30" ) );
	assert.ok( lines.some( line => line.startsWith( "Day turns 04:00 UTC (1:06)" ) ) );
	const cells = buttons( ops ).filter( op => op.id.startsWith( "attendance-day:" ) );
	assert.equal( cells.length, 30, "daysInMonth cells" );
	assert.equal( lines.filter( line => line === "Checked" ).length, 4, "days 1, 2, 3 and 5" );
	assert.equal( lines.filter( line => line === "END" ).length, 1 );
	const end = ops.find( op => op.kind === "text" && op.value === "END" );
	const last = cells[29];
	assert.ok( end.rect[0] >= last.rect[0] && end.rect[1] >= last.rect[1], "END sits on the last day" );
	const washes = ops.filter( op => op.kind === "rect" );
	assert.equal( washes.length, 5, "four checked days and today" );
	const checkIn = buttons( ops ).find( op => op.id === "attendance-check-in" );
	assert.equal( checkIn.label, "Check Attendance" );
	assert.equal( checkIn.disabled, false );
	assert.ok( lines.includes( "Today is not checked yet." ) );
	assert.ok( attendancePictures( ops ).includes( "tile.png" ) );
});

test("Check Attendance is disabled once today is checked", () => {
	const ops = view( { feed: feed( { checked: CHECKED | 8 } ), tab: "calendar", selectedDay: 4, serverNowMs: 0 } );
	assert.equal( buttons( ops ).find( op => op.id === "attendance-check-in" ).disabled, true );
	assert.ok( texts( ops ).includes( "Today is checked." ) );
	assert.ok( texts( ops ).includes( "Day 4: checked" ) );
});

test("a 28-day month ends on its 28th cell", () => {
	const short = feed( { daysInMonth: 28, today: 28 } );
	const ops = view( { feed: short, tab: "calendar", selectedDay: 0, serverNowMs: 0 } );
	assert.equal( buttons( ops ).filter( op => op.id.startsWith( "attendance-day:" ) ).length, 28 );
	assert.equal( texts( ops ).filter( line => line === "END" ).length, 1 );
});

test("the rewards tab lists both columns with Receive gated by earned and claimed", () => {
	const ops = view( {
		feed: feed( { checked: 0b11111, continuityClaimed: 0b1, accumulationClaimed: 0 } ),
		tab: "rewards",
		selectedDay: 0,
		serverNowMs: 0
	} );
	const lines = texts( ops );
	assert.ok( lines.includes( "Continuity" ) && lines.includes( "Accumulation" ) );
	for ( const days of [ ...CONTINUITY, ...ACCUMULATION ] ) assert.ok( lines.includes( days + " days" ) );
	assert.equal( lines.filter( line => line === REWARD ).length, 10 );
	const claims = Object.fromEntries(
		buttons( ops ).filter( op => op.id.startsWith( "attendance-claim:" ) ).map( op => [ op.id, op ] )
	);
	assert.equal( Object.keys( claims ).length, 10 );
	assert.equal( claims["attendance-claim:continuity:0"].disabled, true, "received" );
	assert.equal( claims["attendance-claim:continuity:0"].label, "Received" );
	assert.equal( claims["attendance-claim:continuity:1"].disabled, true, "8 days not reached" );
	assert.equal( claims["attendance-claim:accumulation:0"].disabled, false, "5 days reached" );
	assert.equal( claims["attendance-claim:accumulation:0"].label, "Receive" );
	assert.equal( claims["attendance-claim:accumulation:1"].disabled, true, "10 days not reached" );
	assert.ok( !buttons( ops ).some( op => op.id === "attendance-check-in" ), "the grid's button stays on its tab" );
});

test("the window waits without a feed", () => {
	for ( const tab of [ "calendar", "rewards" ] ) {
		const ops = view( { feed: null, tab, selectedDay: 0, serverNowMs: 0 } );
		assert.ok( texts( ops ).includes( "Waiting for the attendance calendar..." ) );
		assert.deepEqual( buttons( ops ).map( op => op.id ), [ "attendance-tab:calendar", "attendance-tab:rewards" ] );
	}
});

test("the countdown reads the clock the state runs", () => {
	const current = feed();
	assert.equal( attendanceCountdownText( current, current.nextResetMs ), "0:00" );
	assert.equal( attendanceCountdownText( current, current.nextResetMs + 1000 ), "0:00", "never negative" );
	assert.equal( attendanceCountdownText( current, current.nextResetMs - 3 * 3_600_000 - 5 * 60_000 ), "3:05" );
});
