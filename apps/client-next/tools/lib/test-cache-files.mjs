/*
===========================================================================

test-cache-files.mjs - where the client test cache keeps its two files

The records (index.json) live in the main checkout's .state, shared by every
worktree (ops #335); the size and mtime memo (stats.json) names absolute
paths, so it stays in each checkout's own .state. The runner writes both and
tools/list-asset-backed-tests.mjs reads the records, so the location has one
owner. SRO_TEST_CACHE_DIR holds both in one folder: the runner's own tests
use it, and so does a release build, whose records live in a folder that
outlives its variant roots. The records file keeps one cache key, and a
release's feature flags make its key differ from the hub's, so sharing the
hub's file would make each discard the other's records.

===========================================================================
*/
import path from "node:path";
import { resolveMainCheckout } from "../../../../scripts/lib/generatedRoot.mjs";

const CACHE_SUBDIR = path.join( ".state", "client-test-cache" );

/*
================
recordsPath
================
*/
export function recordsPath( repoRoot, env = process.env ) {
	return path.join(
		env.SRO_TEST_CACHE_DIR ?? path.join( resolveMainCheckout( repoRoot ), CACHE_SUBDIR ),
		"index.json"
	);
}

/*
================
statsPath
================
*/
export function statsPath( repoRoot, env = process.env ) {
	return path.join( env.SRO_TEST_CACHE_DIR ?? path.join( repoRoot, CACHE_SUBDIR ), "stats.json" );
}
