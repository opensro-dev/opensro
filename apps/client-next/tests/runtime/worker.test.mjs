/*
===========================================================================

worker.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createClock } = await load( "src/engine/runtime/simulation/worker/clock/clock.ts" );
const { createSimulation } = await load( "src/engine/runtime/simulation/worker/simulation.ts" );
// Camera input stays on the display thread (contracts/input.ts); the worker
// receives keys and focus release only.
test("worker acknowledges input without owning or returning camera state", t => {
	const timers = scheduler( t ), messages = [];
	const simulation = createSimulation( ( message, transfer ) =>
		messages.push( structuredClone( message, { transfer } ) )
	);
	simulation.receive( { kind: "start", version: 3 } );
	simulation.receive( {
		kind: "input",
		batch: {
			first: 1,
			last: 3,
			commands: [
				{ kind: "key", sequence: 1, code: "KeyW", down: true, timeMs: 1 },
				{ kind: "key", sequence: 2, code: "KeyW", down: false, timeMs: 2 },
				{ kind: "release", sequence: 3, timeMs: 3 }
			]
		}
	} );
	timers.wake( 16 );
	const snapshot = messages.find( m => m.kind === "snapshot" );
	const { originMs, ...counters } = snapshot.clock;
	assert.deepEqual( counters, { wakes: 0, steps: 0, stepsInWake: 0, wakeMs: 0, maxStepMs: 0, debtMs: 0 } );
	assert.ok( originMs > 0, "the clock publishes its origin from the first snapshot" );
	timers.wake( 32 );
	assert.equal( messages.filter( m => m.kind === "snapshot" )[1].clock.steps, 1 );
	assert.equal( snapshot.camera, undefined );
	assert.equal( new DataView( snapshot.buffer ).getUint32( 24, true ), 3 );
	simulation.receive( { kind: "stop" } );
});
function scheduler( t ) {
	let now = 0, id = 0;
	const jobs = new Map();
	t.mock.method( performance, "now", () => now );
	t.mock.method( globalThis, "setTimeout", callback => {
		jobs.set( ++id, callback );
		return id;
	} );
	t.mock.method( globalThis, "clearTimeout", key => jobs.delete( key ) );
	return {
		jobs,
		wake( at ) {
			now = at;
			const entry = jobs.entries().next().value;
			assert.ok( entry, "scheduled callback exists" );
			jobs.delete( entry[0] );
			entry[1]();
		}
	};
}
test("simulation clock is independent of display updates and does not discard elapsed ticks", t => {
	const timers = scheduler( t );
	let count = 0, error = null;
	const clock = createClock( () => count++, e => error = e );
	clock.start();
	clock.start();
	assert.equal( timers.jobs.size, 1 );
	timers.wake( 4 );
	assert.equal( count, 0 );
	timers.wake( 16 );
	assert.equal( count, 1 );
	timers.wake( 80 );
	assert.equal( count, 5 );
	assert.equal( error, null );
	clock.dispose();
	assert.equal( timers.jobs.size, 0 );
});
test("worker bounds transferred snapshots and resumes with current state after backpressure", t => {
	const timers = scheduler( t ), messages = [];
	const simulation = createSimulation( ( message, transfer ) =>
		messages.push( structuredClone( message, { transfer } ) )
	);
	simulation.receive( { kind: "start", version: 3 } );
	for ( let tick = 1; tick <= 8; tick++ ) timers.wake( tick * 16 );
	assert.equal( messages.length, 3, "only three pooled buffers can be outstanding" );
	const buffer = messages.shift().buffer;
	simulation.receive( { kind: "recycle", buffer } );
	timers.wake( 144 );
	const newest = new DataView( messages.at( -1 ).buffer );
	assert.equal( newest.getUint32( 4, true ), 9 );
	assert.equal( newest.getFloat64( 8, true ), 144 );
	simulation.receive( { kind: "stop" } );
	assert.equal( timers.jobs.size, 0 );
});
test("worker rejects incompatible protocol before starting", t => {
	const timers = scheduler( t ), messages = [];
	const simulation = createSimulation( message => messages.push( message ) );
	simulation.receive( { kind: "start", version: 99 } );
	assert.equal( messages[0].kind, "failure" );
	assert.equal( timers.jobs.size, 0 );
});

test("worker input acknowledgement is committed on a tick and survives snapshot backpressure", t => {
	const timers = scheduler( t ), messages = [];
	const simulation = createSimulation( ( message, transfer ) =>
		messages.push( structuredClone( message, { transfer } ) )
	);
	simulation.receive( { kind: "start", version: 3 } );
	for ( let tick = 1; tick <= 3; tick++ ) timers.wake( tick * 16 );
	simulation.receive( {
		kind: "input",
		batch: { first: 1, last: 1, commands: [ { kind: "release", timeMs: 49, sequence: 1 } ] }
	} );
	timers.wake( 64 );
	assert.equal( messages.length, 3 );
	simulation.receive( { kind: "recycle", buffer: messages.shift().buffer } );
	timers.wake( 80 );
	assert.equal( new DataView( messages.at( -1 ).buffer ).getUint32( 24, true ), 1 );
	simulation.receive( { kind: "stop" } );
});

test("worker resumes with current elapsed time without replaying missing simulation ticks", t => {
	const timers = scheduler( t ), messages = [];
	const simulation = createSimulation( ( message, transfer ) =>
		messages.push( structuredClone( message, { transfer } ) )
	);
	simulation.receive( { kind: "start", version: 3 } );
	timers.wake( 3600000 );
	assert.equal( messages.filter( m => m.kind === "snapshot" ).length, 3 );
	const first = messages[0], last = messages.at( -1 );
	assert.equal( new DataView( first.buffer ).getFloat64( 8, true ), 3600000 - 48 );
	simulation.receive( { kind: "recycle", buffer: last.buffer } );
	timers.wake( 3600016 );
	assert.equal( new DataView( messages.at( -1 ).buffer ).getFloat64( 8, true ), 3600016 );
	assert.equal( new DataView( messages.at( -1 ).buffer ).getUint32( 4, true ), 5 );
	simulation.receive( { kind: "stop" } );
});

test("between-tick session commands under worker clock debt do not jump ahead of the next step deadline", t => {
	const timers = scheduler( t ), messages = [];
	const simulation = createSimulation( ( message, transfer ) =>
		messages.push( structuredClone( message, { transfer } ) )
	);
	simulation.receive( { kind: "start", version: 3 } );
	timers.wake( 16 );
	const firstSnapshot = messages.find( m => m.kind === "snapshot" );
	assert.ok( firstSnapshot.clock.originMs > 0 );
	simulation.receive( { kind: "recycle", buffer: firstSnapshot.buffer } );
	// Advance wall time to create 688 ms of worker clock debt before the next wake runs.
	simulation.receive( { kind: "session", command: { kind: "world-ready" } } );
	timers.wake( 16 + 16 + 688 );
	const afterReady = messages.filter( m => m.kind === "snapshot" ).at( -1 );
	assert.equal( afterReady.clock.debtMs, 0 );
	// If worker debt accumulates during a walk (e.g. 688.2 ms ahead of timeMs) and a between-tick
	// move command arrives, clamping its timestamp to timeMs prevents world.now from jumping ahead
	// of subsequent fixed ticks and freezing movement for 43 ticks.
	const scheduledTimeMs = new DataView( afterReady.buffer ).getFloat64( 8, true );
	simulation.receive( {
		kind: "session",
		command: {
			kind: "gameplay",
			command: { kind: "move", destination: { regionId: 25000, x: 100, y: 0, z: 100, angle: 0 } }
		}
	} );
	simulation.receive( { kind: "recycle", buffer: afterReady.buffer } );
	timers.wake( 16 + 16 + 688 + 16 + 688.2 );
	const afterMoveTick = messages.filter( m => m.kind === "snapshot" ).at( -1 );
	assert.ok(
		new DataView( afterMoveTick.buffer ).getFloat64( 8, true ) > scheduledTimeMs,
		"subsequent fixed tick advances simulation time past the clamped command timestamp"
	);
	simulation.receive( { kind: "stop" } );
});
