/*
===========================================================================

test-cache-files.mjs - where the client test cache keeps its files

The records live in the main checkout's .state, shared by every worktree
(ops #335), one file per cache key (index-<digest>.json). A single shared
file held one key, so a run under another key (a release, whose feature
flags differ from the hub's) replaced every record: on 11-17 the hub's
file held 3 records after the r194 release check had written 814. Each key
now keeps its own file, and a file no run has written for RECORDS_MAX_AGE_DAYS
is pruned. The size and mtime memo (stats.json) names absolute paths, so it
stays in each checkout's own .state. The runner writes both and
tools/list-asset-backed-tests.mjs reads the records, so the location has
one owner. SRO_TEST_CACHE_DIR (the runner's own tests) holds them in one
folder.

===========================================================================
*/
import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { resolveMainCheckout } from "../../../../scripts/lib/generatedRoot.mjs";

const CACHE_SUBDIR = path.join( ".state", "client-test-cache" );
const RECORDS_FILE = /^index-[0-9a-f]{16}\.json$/;
// A key unused this long is a retired Node version or flag set.
const RECORDS_MAX_AGE_DAYS = 14;
const DAY_MS = 24 * 60 * 60 * 1000;

/*
================
recordsDir
================
*/
export function recordsDir( repoRoot, env = process.env ) {
	return env.SRO_TEST_CACHE_DIR ?? path.join( resolveMainCheckout( repoRoot ), CACHE_SUBDIR );
}

/*
================
recordsPath

The records file of one cache key.
================
*/
export function recordsPath( repoRoot, key, env = process.env ) {
	const digest = createHash( "sha256" ).update( key ).digest( "hex" ).slice( 0, 16 );
	return path.join( recordsDir( repoRoot, env ), `index-${digest}.json` );
}

/*
================
recordFiles

Every key's records file, oldest first, so a reader that merges them lets
the latest write win.
================
*/
export function recordFiles( repoRoot, env = process.env ) {
	const dir = recordsDir( repoRoot, env );
	let names;
	try {
		names = fs.readdirSync( dir ).filter( name => RECORDS_FILE.test( name ) );
	} catch {
		return [];
	}
	return names.map( name => path.join( dir, name ) ).map( file => {
		try {
			return { file, written: fs.statSync( file ).mtimeMs };
		} catch {
			return null;
		}
	} ).filter( Boolean ).sort( ( a, b ) => a.written - b.written ).map( entry => entry.file );
}

/*
================
pruneRecordFiles

Removes the records of keys no run has written for RECORDS_MAX_AGE_DAYS. A
lost race with another run's write costs that key one cold run, never a
wrong reuse.
================
*/
export function pruneRecordFiles( repoRoot, now = Date.now(), env = process.env ) {
	for ( const file of recordFiles( repoRoot, env ) ) {
		try {
			if ( now - fs.statSync( file ).mtimeMs > RECORDS_MAX_AGE_DAYS * DAY_MS ) fs.rmSync( file, { force: true } );
		} catch {
			// another run removed or replaced it first
		}
	}
}

/*
================
statsPath
================
*/
export function statsPath( repoRoot, env = process.env ) {
	return path.join( env.SRO_TEST_CACHE_DIR ?? path.join( repoRoot, CACHE_SUBDIR ), "stats.json" );
}
