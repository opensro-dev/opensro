/*
===========================================================================

debug-clock.test.mjs - the debug console's local clock and weather slot

The command.txt time family (690C40 ids 100-109) re-seeds, holds and
steps the seed the world-clock sampler sees, and the weather family
(ids 99/105-107) writes the worker's weather slot. Both are pinned
here at the foundation seam with the sampler itself as the reader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
/*
================
load
================
*/
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { sampleWorldClock, worldClockWallTime } = await load( "foundation/gameplay/world-clock.ts" );
const {
	createDebugClock,
	debugClockHold,
	debugClockNoteSeed,
	debugClockResume,
	debugClockSeed,
	debugClockSetFraction,
	debugClockSetHours,
	debugClockStep,
	DEBUG_CLOCK_STEP_MINUTES
} = await load( "foundation/gameplay/debug-clock.ts" );
const { debugWeather, entryEnvironment, environmentPacket } = await load(
	"foundation/gameplay/event-environment.ts"
);

// One game second per 20 real milliseconds.
const SECOND = 20;

test("the wall time reads the seed before the sun mapping", () => {
	const seed = { day: 3, hour: 6, minute: 30, receivedAtMs: 1000 };
	assert.deepEqual( worldClockWallTime( seed, 1000 ), { day: 3, hour: 6, minute: 30, second: 0 } );
	assert.deepEqual( worldClockWallTime( seed, 1000 + 90 * SECOND ), { day: 3, hour: 6, minute: 31, second: 30 } );
	assert.equal( worldClockWallTime( undefined, 1000 ), null );
});

test("a live debug clock passes the server seed through", () => {
	const seed = { day: 3, hour: 6, minute: 30, receivedAtMs: 1000 };
	assert.equal( debugClockSeed( createDebugClock(), seed, 5000 ), seed );
});

test("timeoff holds the sampled clock where it stands", () => {
	const seed = { day: 3, hour: 6, minute: 30, receivedAtMs: 0 };
	const held = debugClockHold( createDebugClock(), seed, 45 * SECOND );
	const early = sampleWorldClock( debugClockSeed( held, seed, 100000 ), 100000 );
	const late = sampleWorldClock( debugClockSeed( held, seed, 900000 ), 900000 );
	assert.deepEqual( late, early, "a held clock advanced" );
	// The held phase keeps its seconds: 45 game seconds past 06:30.
	assert.deepEqual(
		worldClockWallTime( debugClockSeed( held, seed, 100000 ), 100000 ),
		{ day: 3, hour: 6, minute: 30, second: 45 }
	);
});

test("timeon runs the clock again from the held time", () => {
	const seed = { day: 3, hour: 6, minute: 30, receivedAtMs: 0 };
	const held = debugClockHold( createDebugClock(), seed, 0 );
	const running = debugClockResume( held, 5000 );
	assert.deepEqual(
		worldClockWallTime( debugClockSeed( running, seed, 5000 ), 5000 ),
		{ day: 3, hour: 6, minute: 30, second: 0 }
	);
	assert.deepEqual(
		worldClockWallTime( debugClockSeed( running, seed, 5000 + 90 * SECOND ), 5000 + 90 * SECOND ),
		{ day: 3, hour: 6, minute: 31, second: 30 }
	);
});

test("/time re-seeds from the atof hour and runs", () => {
	const seed = { day: 3, hour: 6, minute: 30, receivedAtMs: 0 };
	const set = debugClockSetHours( createDebugClock(), seed, 2000, 18.5 );
	assert.deepEqual(
		worldClockWallTime( debugClockSeed( set, seed, 2000 ), 2000 ),
		{ day: 3, hour: 18, minute: 30, second: 0 }
	);
	// The hour clamps to [0, 24]: 24 is the next day's midnight.
	const clamped = debugClockSetHours( createDebugClock(), seed, 2000, 25 );
	assert.deepEqual(
		worldClockWallTime( debugClockSeed( clamped, seed, 2000 ), 2000 ),
		{ day: 4, hour: 0, minute: 0, second: 0 }
	);
});

test("day and night set the day fraction", () => {
	const seed = { day: 3, hour: 6, minute: 30, receivedAtMs: 0 };
	const day = debugClockSetFraction( createDebugClock(), seed, 2000, 0.5 );
	assert.deepEqual(
		worldClockWallTime( debugClockSeed( day, seed, 2000 ), 2000 ),
		{ day: 3, hour: 12, minute: 0, second: 0 }
	);
	const night = debugClockSetFraction( createDebugClock(), seed, 2000, 1.0 );
	assert.deepEqual(
		worldClockWallTime( debugClockSeed( night, seed, 2000 ), 2000 ),
		{ day: 4, hour: 0, minute: 0, second: 0 }
	);
});

test("a re-seed while held writes the frozen time instead of running", () => {
	const seed = { day: 3, hour: 6, minute: 30, receivedAtMs: 0 };
	const held = debugClockHold( createDebugClock(), seed, 0 );
	const set = debugClockSetHours( held, seed, 2000, 12 );
	const early = sampleWorldClock( debugClockSeed( set, seed, 3000 ), 3000 );
	const late = sampleWorldClock( debugClockSeed( set, seed, 999000 ), 999000 );
	assert.deepEqual( late, early, "a re-seeded held clock ran" );
	assert.deepEqual(
		worldClockWallTime( debugClockSeed( set, seed, 3000 ), 3000 ),
		{ day: 3, hour: 12, minute: 0, second: 0 }
	);
});

test("the step rows move a held clock and leave a running one alone", () => {
	const seed = { day: 3, hour: 0, minute: 30, receivedAtMs: 0 };
	const held = debugClockHold( createDebugClock(), seed, 0 );
	const back = debugClockStep( held, -DEBUG_CLOCK_STEP_MINUTES );
	assert.deepEqual(
		worldClockWallTime( debugClockSeed( back, seed, 1000 ), 1000 ),
		{ day: 2, hour: 23, minute: 18, second: 0 }
	);
	const forward = debugClockStep( back, DEBUG_CLOCK_STEP_MINUTES );
	assert.deepEqual(
		worldClockWallTime( debugClockSeed( forward, seed, 1000 ), 1000 ),
		{ day: 3, hour: 0, minute: 30, second: 0 }
	);
	const running = createDebugClock();
	assert.equal( debugClockStep( running, DEBUG_CLOCK_STEP_MINUTES ), running );
});

test("debug weather holds its own slot and clamps the amount", () => {
	let state = entryEnvironment( {} ).state;
	state = debugWeather( state, 2, 200 );
	assert.deepEqual( state.server, { mode: 1, amount: 0 }, "the console write touched the server slot" );
	assert.deepEqual( state.effective, { mode: 2, amount: 200 } );
	state = debugWeather( state, undefined, 999 );
	assert.deepEqual( state.effective, { mode: 2, amount: 255 }, "the amount is a byte" );
	assert.deepEqual( state.server, { mode: 1, amount: 0 }, "power under a hold tuned the server slot" );
	state = debugWeather( state, 3, -5 );
	assert.deepEqual( state.effective, { mode: 3, amount: 0 } );
});

// 7775A0 opens with the interface byte the rain and snow rows set: while it
// stands the packet is discarded whole, so neither port slot may move. Only
// /clear clears the byte, writing clear (1, 255) as it does.
test("console rain and snow hold off server weather until /clear", () => {
	let state = entryEnvironment( {} ).state;
	state = debugWeather( state, 2, 200 );
	state = environmentPacket( state, 0x3bde, Uint8Array.of( 3, 7 ) ).state;
	assert.deepEqual( state.server, { mode: 1, amount: 0 }, "a held 3BDE rewrote the server slot" );
	assert.deepEqual( state.effective, { mode: 2, amount: 200 }, "a held 3BDE displaced the console weather" );
	state = debugWeather( state, 1, 255 );
	assert.deepEqual( state.server, { mode: 1, amount: 255 } );
	assert.deepEqual( state.effective, { mode: 1, amount: 255 } );
	state = environmentPacket( state, 0x3bde, Uint8Array.of( 3, 7 ) ).state;
	assert.deepEqual( state.server, { mode: 3, amount: 7 }, "packets did not land again after /clear" );
	assert.deepEqual( state.effective, { mode: 3, amount: 7 } );
});

test("/power tunes the live amount and never sets the hold", () => {
	let state = entryEnvironment( {} ).state;
	state = debugWeather( state, undefined, 40 );
	assert.deepEqual( state.server, { mode: 1, amount: 40 } );
	state = environmentPacket( state, 0x3bde, Uint8Array.of( 3, 7 ) ).state;
	assert.deepEqual( state.effective, { mode: 3, amount: 7 }, "power held off a later packet" );
	// Under a hold the live weather is the held one, so power tunes it.
	state = debugWeather( state, 2, 200 );
	state = debugWeather( state, undefined, 40 );
	assert.deepEqual( state.effective, { mode: 2, amount: 40 } );
	assert.deepEqual( state.server, { mode: 3, amount: 7 }, "power under a hold tuned the server slot" );
});

// The event weather is a derivation over the server slot (a packet with the
// group on lands as (3, 20)); the held console weather is the live one, so
// it stands over the derivation until /clear, and the group end moving
// underneath it changes nothing either.
test("the console hold stands over the event slot until /clear", () => {
	let state = entryEnvironment( { character: { enterEventGroupIds: [ 1 ] } } ).state;
	assert.deepEqual( state.effective, { mode: 3, amount: 20 } );
	state = debugWeather( state, 2, 200 );
	assert.deepEqual( state.effective, { mode: 2, amount: 200 }, "the event slot overwrote the console weather" );
	const off = new Uint8Array( 5 );
	new DataView( off.buffer ).setUint32( 0, 1, true );
	state = environmentPacket( state, 0x3347, off ).state;
	assert.deepEqual( state.effective, { mode: 2, amount: 200 }, "the event end displaced the held weather" );
	state = debugWeather( state, 1, 255 );
	assert.deepEqual( state.effective, { mode: 1, amount: 255 } );
});

test("timeoff before the first clock lands the hold when the clock arrives", () => {
	const seed = { day: 3, hour: 6, minute: 30, receivedAtMs: 5000 };
	let state = debugClockHold( createDebugClock(), undefined, 1000 );
	assert.equal( state.heldAt, null, "a hold with no clock froze a time" );
	assert.equal( debugClockNoteSeed( state, undefined, 2000 ), state, "a note with no clock landed" );
	assert.equal( debugClockNoteSeed( createDebugClock(), seed, 2000 ).heldAt, null, "a note without a hold landed" );
	state = debugClockNoteSeed( state, seed, 5000 + 45 * SECOND );
	assert.deepEqual( state.heldAt, { day: 3, hour: 6, minute: 30, second: 45 } );
	const early = sampleWorldClock( debugClockSeed( state, seed, 100000 ), 100000 );
	const late = sampleWorldClock( debugClockSeed( state, seed, 900000 ), 900000 );
	assert.deepEqual( late, early, "the landed hold ran" );
	assert.equal( debugClockNoteSeed( state, seed, 7000 ), state, "a second clock re-landed the hold" );
});
