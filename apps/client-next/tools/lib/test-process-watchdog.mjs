/*
===========================================================================

test-process-watchdog.mjs - a test process dies at a memory ceiling or
when its runner is gone

Preloaded (NODE_OPTIONS --import) into every test process by
tools/run-tests.mjs, beside the input recorder. The runner's
--max-old-space-size bounds only the JS heap; typed arrays and Buffers
live outside it. So a runaway test building them still grew without
limit, and a test whose runner had died (a caller that stopped reading
early) ran on as an orphan (2026-10-27: 25-40 GB per process).

A worker thread does the watching, because a worker keeps running while
the test's own thread spins in a synchronous loop, where no timer of the
test's can fire. Every WATCH_INTERVAL_MS it kills the process when:

	the process's resident memory passes SRO_TEST_RSS_MB, or
	the runner (SRO_TEST_RUNNER_PID) no longer exists.

Only a process's main thread starts a watchdog: the watchdog's own worker
and a test's worker threads load this preload too, and one watchdog per
process is enough. Inactive unless SRO_TEST_RUNNER_PID is set.

===========================================================================
*/
import { isMainThread, Worker } from "node:worker_threads";

// How often the watchdog looks, in milliseconds.
const WATCH_INTERVAL_MS = 100;

/*
================
WATCHDOG_SOURCE

The worker's body, as CommonJS source for an eval worker (it needs no file
of its own). process.kill on the process's own pid ends the whole
process from a worker thread; process.exit would end only the worker.
================
*/
const WATCHDOG_SOURCE = `
const { readFileSync } = require( "node:fs" );
const { workerData } = require( "node:worker_threads" );
const { runnerPid, limitBytes, intervalMs } = workerData;

function runnerAlive() {
	try {
		process.kill( runnerPid, 0 );
	} catch ( error ) {
		// EPERM: the pid exists but belongs to someone else, so it is alive.
		return error.code === "EPERM";
	}
	// Linux: a runner that died but that its parent has not reaped yet is a
	// zombie, and a zombie still answers kill 0. Its status says so. Where
	// /proc does not exist (Windows), kill 0 alone decides.
	try {
		const status = readFileSync( "/proc/" + runnerPid + "/status", "utf8" ), at = status.indexOf( "State:" );
		return at < 0 || status.slice( at + "State:".length ).trim()[0] !== "Z";
	} catch {
		return true;
	}
}

setInterval( () => {
	let reason = null;
	const rss = process.memoryUsage.rss();
	if ( limitBytes > 0 && rss > limitBytes ) {
		reason = "resident memory " + Math.round( rss / 1048576 ) + " MB passed the ceiling of " +
			Math.round( limitBytes / 1048576 ) + " MB";
	} else if ( !runnerAlive() ) {
		reason = "its test runner (pid " + runnerPid + ") is gone";
	}
	if ( reason ) {
		process._rawDebug( "[test watchdog] killing test process " + process.pid + ": " + reason );
		process.kill( process.pid, "SIGKILL" );
	}
}, intervalMs );
`;

/*
================
startWatchdog

Starts the watchdog for this process. It is unref'd, so it never keeps a
finished test process alive.
================
*/
function startWatchdog() {
	const runnerPid = Number( process.env.SRO_TEST_RUNNER_PID );
	if ( !isMainThread || !Number.isInteger( runnerPid ) || runnerPid <= 0 ) return;
	const limitMb = Number( process.env.SRO_TEST_RSS_MB ) || 0;
	const watchdog = new Worker( WATCHDOG_SOURCE, {
		eval: true,
		workerData: { runnerPid, limitBytes: limitMb * 1048576, intervalMs: WATCH_INTERVAL_MS },
		// The watchdog needs no preload of its own; the recorder would log
		// it as part of the test.
		execArgv: []
	} );
	watchdog.unref();
}

startWatchdog();
