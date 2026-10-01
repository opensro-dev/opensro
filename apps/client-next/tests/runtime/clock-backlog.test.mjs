/*
===========================================================================

clock-backlog.test.mjs - tests for clock.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const { createClock } = await import( sourceFileUrl( "src/engine/runtime/simulation/worker/clock/clock.ts" ).href );
test("a delayed worker timer catches up in bounded tasks without losing steps or the session", t => {
	const original = new Map(
		[ "performance", "setTimeout", "clearTimeout" ].map(
			k => [ k, Object.getOwnPropertyDescriptor( globalThis, k ) ]
		)
	);
	t.after( () => {
		for ( const [k, v] of original ) Object.defineProperty( globalThis, k, v );
	} );
	let now = 0, callback, steps = 0, delay, cleared = 0;
	const errors = [];
	Object.defineProperty( globalThis, "performance", { configurable: true, value: { now: () => now } } );
	globalThis.setTimeout = ( fn, ms ) => {
		callback = fn;
		delay = ms;
		return 1;
	};
	globalThis.clearTimeout = () => cleared++;
	const clock = createClock( () => steps++, e => errors.push( e ) );
	clock.start();
	const interval = delay;
	now = defined( interval ) * 60 + .01;
	defined( callback )();
	assert.equal( steps, 4 );
	assert.equal( delay, 0 );
	assert.deepEqual( errors, [] );
	for ( let i = 0; i < 14; i++ ) defined( callback )();
	assert.equal( steps, 60 );
	assert.ok( delay > 0 );
	assert.deepEqual( errors, [] );
	clock.dispose();
	defined( callback )();
	assert.equal( steps, 60 );
	assert.equal( cleared, 1 );
	const broken = createClock( () => {
		throw Error( "bad simulation" );
	}, e => errors.push( e ) );
	broken.start();
	now += defined( interval );
	defined( callback )();
	assert.match( String( errors[0] ), /bad simulation/ );
});

test("expensive fixed ticks yield at the work budget and preserve remaining debt", t => {
	let now = 0, callback, delay, steps = 0;
	t.mock.method( performance, "now", () => now );
	t.mock.method( globalThis, "setTimeout", ( fn, ms ) => {
		callback = fn;
		delay = ms;
		return 1;
	} );
	t.mock.method( globalThis, "clearTimeout", () => {} );
	const clock = createClock( () => {
		steps++;
		now += 5;
	}, error => {
		throw error;
	} );
	clock.start();
	now = 64;
	defined( callback )();
	assert.equal( steps, 1 );
	assert.equal( delay, 0 );
	assert.deepEqual( clock.sample(), {
		wakes: 1,
		steps: 1,
		stepsInWake: 1,
		wakeMs: 5,
		maxStepMs: 5,
		debtMs: 37,
		originMs: performance.timeOrigin + 16
	} );
	const detached = clock.sample();
	detached.steps = 999;
	assert.equal( clock.sample().steps, 1 );
	defined( callback )();
	assert.equal( steps, 2 );
	assert.equal( delay, 0 );
	defined( callback )();
	assert.equal( steps, 3 );
	assert.equal( delay, 0 );
	defined( callback )();
	assert.equal( steps, 4 );
	assert.equal( delay, 0 );
	defined( callback )();
	assert.equal( steps, 5 );
	assert.ok( delay > 0 );
	assert.equal( clock.sample().debtMs, 0 );
	assert.equal( clock.sample().steps, 5 );
	clock.dispose();
});

test("long suspension advances elapsed time once and bounds historical work across repeated resumes", t => {
	let now = 0, callback, delay, steps = 0, elapsed = 0;
	t.mock.method( performance, "now", () => now );
	t.mock.method( globalThis, "setTimeout", ( fn, ms ) => {
		callback = fn;
		delay = ms;
		return 1;
	} );
	t.mock.method( globalThis, "clearTimeout", () => {} );
	const clock = createClock( skipped => {
		steps++;
		elapsed += 16 + skipped;
	}, error => {
		throw error;
	} );
	clock.start();
	for ( const at of [ 3600000, 7200000, 86400000 ] ) {
		now = at;
		const before = steps;
		defined( callback )();
		assert.equal( steps - before, 4 );
		assert.equal( elapsed, at );
		assert.equal( delay, 16 );
		assert.equal( clock.sample().debtMs, 0 );
	}
	now += 16;
	defined( callback )();
	assert.equal( elapsed, now );
	clock.dispose();
});

test("the sample's origin maps every step's simulation time to its deadline", t => {
	let now = 1000, callback;
	const deadlines = [];
	t.mock.method( performance, "now", () => now );
	t.mock.method( globalThis, "setTimeout", fn => {
		callback = fn;
		return 1;
	} );
	t.mock.method( globalThis, "clearTimeout", () => {} );
	let simulationMs = 0;
	const clock = createClock( skippedMs => {
		// simulation.ts advances its time by the skipped span, steps, then adds one tick.
		simulationMs += skippedMs;
		deadlines.push( simulationMs );
		simulationMs += 16;
	}, error => {
		throw error;
	} );
	clock.start();
	// A late wake runs a catch-up burst; a long stall skips ahead.
	for ( const at of [ 1100, 1180, 4000 ] ) {
		now = at;
		defined( callback )();
	}
	const origin = clock.sample().originMs - performance.timeOrigin;
	assert.equal( origin, 1016 );
	// Each late wake runs a burst of at most four steps, each for its own deadline.
	const expected = [ 0, 16, 32, 48, 64, 80, 96, 112 ];
	assert.deepEqual( deadlines.slice( 0, expected.length ), expected );
	// After the skip, simulation time still names the step's deadline.
	assert.ok( deadlines.at( -1 ) + origin <= 4000 && deadlines.at( -1 ) + origin > 4000 - 4 * 16 );
	clock.dispose();
});
