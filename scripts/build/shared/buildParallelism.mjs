/*
===========================================================================

buildParallelism.mjs - how many jobs the asset build may run at once

One setting, SRO_BUILD_JOBS, sizes every parallel stage of the asset build:
the concurrent resource-build lanes, outdoor region builders, pack builds,
JSON compression workers, texture encoders, image conversion processes
(convert_images.py reads the same variable) and libuv's thread pool, which
runs the gzip member compression the pack builder awaits.

The default is the smaller of two limits: every core but one, and the jobs
that fit in the memory budget (SRO_BUILD_MEMORY_MB, else half the memory
that was free when the build started) at JOB_MEMORY_MB each. A core-only
default ran 15 lanes on a 16-core laptop whose other programs held 10 GB,
and the system stopped the build for low memory (2026-11-14). The budget
is taken once, at load, and exported as SRO_BUILD_JOBS so every stage and
every Python child agrees on one number for the whole run. A Python step
run on its own (build_parallelism.py) has no free-memory reading and
defaults to every core but one; set SRO_BUILD_JOBS for it by hand.

Outputs never depend on the setting - every stage writes the same bytes
whatever the concurrency - so it is not part of the build fingerprint.

Import this module FIRST in a build entry point: libuv reads
UV_THREADPOOL_SIZE once, when the first asynchronous file, zlib or crypto
call starts the pool, and later assignments are ignored.

===========================================================================
*/
import { availableParallelism, freemem } from "node:os";

export const BUILD_JOBS_ENV = "SRO_BUILD_JOBS";
export const BUILD_MEMORY_ENV = "SRO_BUILD_MEMORY_MB";
// What one job may hold at its peak. Profiled 2026-11-14 on the full build
// with 15 jobs: the outdoor pass grew the build process by about 720 MB
// (about 50 MB a region builder) and the process reached 2 GB across the
// resource lanes (about 130 MB a lane); 256 leaves headroom for the largest.
export const JOB_MEMORY_MB = 256;
// The share of the free memory at start a default build may plan to use.
const FREE_MEMORY_SHARE = 0.5;
const BYTES_PER_MB = 1024 * 1024;
// libuv's own ceiling for its thread pool.
const MAX_THREADPOOL_SIZE = 1024;

/*
================
positiveInteger
================
*/
function positiveInteger( env, name ) {
	const raw = env[name];
	if ( raw === undefined || raw === "" ) return null;
	const value = Number( raw );
	if ( !Number.isInteger( value ) || value < 1 ) {
		throw new Error( `${name} must be a positive integer; got ${JSON.stringify( raw )}` );
	}
	return value;
}

/*
================
buildJobs

SRO_BUILD_JOBS when set (a positive integer), else every core but one,
capped by the jobs that fit in the memory budget. machine is injectable
for tests: { cores, freeMB }.
================
*/
export function buildJobs( env = process.env, machine = currentMachine() ) {
	const jobs = positiveInteger( env, BUILD_JOBS_ENV );
	if ( jobs !== null ) return jobs;
	const budgetMB = positiveInteger( env, BUILD_MEMORY_ENV ) ?? Math.floor( machine.freeMB * FREE_MEMORY_SHARE );
	const coreJobs = Math.max( 1, machine.cores - 1 );
	return Math.max( 1, Math.min( coreJobs, Math.floor( budgetMB / JOB_MEMORY_MB ) ) );
}

/*
================
currentMachine
================
*/
function currentMachine() {
	return { cores: availableParallelism(), freeMB: Math.floor( freemem() / BYTES_PER_MB ) };
}

// One number for the whole run and every child process; the pool must exist
// with this size before anything starts it. An empty value means unset, as
// buildJobs reads it (`??=` would keep the empty string).
if ( !process.env[BUILD_JOBS_ENV] ) process.env[BUILD_JOBS_ENV] = String( buildJobs() );
// libuv reads an empty UV_THREADPOOL_SIZE as one thread.
if ( !process.env.UV_THREADPOOL_SIZE ) {
	process.env.UV_THREADPOOL_SIZE = String( Math.min( MAX_THREADPOOL_SIZE, Math.max( 4, buildJobs() ) ) );
}
