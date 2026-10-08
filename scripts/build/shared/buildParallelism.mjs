/*
===========================================================================

buildParallelism.mjs - how many cores the asset build may use

One setting, SRO_BUILD_JOBS, sizes every parallel stage of the asset build:
the concurrent resource-build lanes, outdoor region builders, pack builds,
JSON compression workers, image conversion processes (convert_images.py
reads the same variable) and libuv's thread pool, which runs the zlib and
zstd compression the pack builder awaits. The default leaves one core for
the rest of the machine.

Outputs never depend on the setting - every stage writes the same bytes
whatever the concurrency - so it is not part of the build fingerprint.

Import this module FIRST in a build entry point: libuv reads
UV_THREADPOOL_SIZE once, when the first asynchronous file, zlib or crypto
call starts the pool, and later assignments are ignored.

===========================================================================
*/
import { availableParallelism } from "node:os";

export const BUILD_JOBS_ENV = "SRO_BUILD_JOBS";
// libuv's own ceiling for its thread pool.
const MAX_THREADPOOL_SIZE = 1024;

/*
================
buildJobs

SRO_BUILD_JOBS when set (a positive integer), else every core but one.
================
*/
export function buildJobs( env = process.env ) {
	const raw = env[BUILD_JOBS_ENV];
	if ( raw === undefined || raw === "" ) return Math.max( 1, availableParallelism() - 1 );
	const jobs = Number( raw );
	if ( !Number.isInteger( jobs ) || jobs < 1 ) {
		throw new Error( `${BUILD_JOBS_ENV} must be a positive integer; got ${JSON.stringify( raw )}` );
	}
	return jobs;
}

// The pool must exist with this size before anything starts it.
process.env.UV_THREADPOOL_SIZE ??= String( Math.min( MAX_THREADPOOL_SIZE, Math.max( 4, buildJobs() ) ) );
