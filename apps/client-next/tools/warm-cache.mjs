/*
===========================================================================

warm-cache.mjs - one checkout pays the cold type-check, every worktree reuses it

Measured 2026-11-08 (issue #2907) in a fresh worktree on this machine:
`pnpm run typecheck` took 223 s cold and 13 s with
temp/cache/tsc/client.tsbuildinfo present; generate-execution-map took
104 s cold and 1 s warm. Both caches live under the checkout's own temp/,
so every new worktree started cold and paid minutes before its first gate.

The caches are content-addressed, so they can be shared safely. tsc's
.tsbuildinfo stores paths relative to its own location and revalidates
every input hash before trusting it; the execution graph is stored under
a key that hashes every input (execution-flow.mjs). This module copies
them between a checkout's temp/cache and one shared store,
<main checkout>/.state/client-warm-cache:

- seedWarmCaches: shared -> local, only files the checkout lacks. Runs
  before a type-check (the `typecheck` script, verify-test-types.mjs) and,
  for the execution graph alone, inside executionFlow() on a cache miss.
- publishWarmCaches: local -> shared once a check has paid the cold cost,
  so the next fresh worktree starts warm.

The .tsbuildinfo store sits under a key hashing the TypeScript version,
both tsconfigs and every file under src/, tests/ and tools/, so an entry
is only seeded into a checkout whose inputs hash identically - and tsc
revalidates regardless, so a stale cache could cost speed, never a wrong
verdict. The store is a cache, never an input: every failure here warns on
stderr and lets the check run cold. Deleting .state/client-warm-cache
costs one cold run per checkout, nothing else.

CLI: node tools/warm-cache.mjs seed|publish (always exits 0).

===========================================================================
*/

import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { resolveMainCheckout } from "../../../scripts/lib/generatedRoot.mjs";
import { root, ts } from "./project.mjs";

const BUILDINFOS = [ "client", "tests" ];
const KEY_INPUT_DIRS = [ "src", "tests", "tools" ];
const KEY_INPUT_FILES = [ "tsconfig.json", "tsconfig.tests.json" ];

/*
================
sharedWarmCacheRoot

The one store every checkout on this machine shares: .state of the main
checkout, resolved through scripts/lib/generatedRoot.mjs like every other
shared tree (a worktree follows its .git file to the hub).
================
*/
export function sharedWarmCacheRoot() {
	return path.join( resolveMainCheckout( path.resolve( root, "..", ".." ) ), ".state", "client-warm-cache" );
}

/*
================
warmCacheKey

Hash of everything the two tsc programs are computed from. Over-including
(extra inputs) only cools the cache sooner; under-including is safe too,
because tsc revalidates the buildinfo itself - the key just keeps the
store honest and bounded.
================
*/
export function warmCacheKey( base = root ) {
	const hash = createHash( "sha256" );
	hash.update( ts.version );
	const addBytes = ( label, bytes ) => {
		hash.update( label );
		hash.update( "\0" );
		hash.update( bytes );
		hash.update( "\0" );
	};
	const addFile = file => {
		if ( fs.existsSync( file ) ) {
			addBytes( path.relative( base, file ).replaceAll( "\\", "/" ), fs.readFileSync( file ) );
		}
	};
	for ( const file of KEY_INPUT_FILES ) addFile( path.join( base, file ) );
	const walk = directory => {
		if ( !fs.existsSync( directory ) ) return;
		const entries = fs.readdirSync( directory, { withFileTypes: true } )
			.sort( ( a, b ) => a.name.localeCompare( b.name ) );
		for ( const entry of entries ) {
			const full = path.join( directory, entry.name );
			if ( entry.isDirectory() ) walk( full );
			else if ( entry.isFile() ) addFile( full );
		}
	};
	for ( const directory of KEY_INPUT_DIRS ) walk( path.join( base, directory ) );
	return hash.digest( "hex" );
}

/*
================
copyAtomically

Write-then-rename, the execution-flow cache's pattern: concurrent checks
may seed or publish the same file at once, and a reader must never see a
half-written cache.
================
*/
function copyAtomically( from, to ) {
	fs.mkdirSync( path.dirname( to ), { recursive: true } );
	const temporary = `${to}.${process.pid}.tmp`;
	fs.copyFileSync( from, temporary );
	fs.renameSync( temporary, to );
}

/*
================
isNewer
================
*/
function isNewer( from, to ) {
	if ( !fs.existsSync( to ) ) return true;
	return fs.statSync( from ).mtimeMs > fs.statSync( to ).mtimeMs;
}

/*
================
seedWarmCaches

Shared store -> this checkout's temp/cache, only what it lacks. Returns
how many files were copied. Never throws: a cold run is the worst case.
================
*/
export function seedWarmCaches( base = root, store = sharedWarmCacheRoot() ) {
	let seeded = 0;
	try {
		const key = warmCacheKey( base );
		for ( const name of BUILDINFOS ) {
			const from = path.join( store, "tsc", key, `${name}.tsbuildinfo` );
			const to = path.join( base, "temp", "cache", "tsc", `${name}.tsbuildinfo` );
			if ( fs.existsSync( from ) && !fs.existsSync( to ) ) {
				copyAtomically( from, to );
				seeded++;
			}
		}
		const sharedGraphs = path.join( store, "execution-flow" );
		if ( fs.existsSync( sharedGraphs ) ) {
			for ( const file of fs.readdirSync( sharedGraphs ) ) {
				if ( !file.endsWith( ".json" ) ) continue;
				const to = path.join( base, "temp", "cache", "execution-flow", file );
				if ( !fs.existsSync( to ) ) {
					copyAtomically( path.join( sharedGraphs, file ), to );
					seeded++;
				}
			}
		}
		if ( seeded > 0 ) console.error( `warm-cache: seeded ${seeded} cache file(s) from ${store}` );
	} catch ( error ) {
		console.error( `warm-cache: seed skipped (${error.message}); running cold` );
	}
	return seeded;
}

/*
================
seedExecutionGraph

The execution graph for one key, copied from the shared store into this
checkout's cache. Returns the parsed graph, or null on any miss or
failure (the caller then computes it, exactly as before this module).
================
*/
export function seedExecutionGraph( base, key ) {
	try {
		const store = sharedWarmCacheRoot();
		const from = path.join( store, "execution-flow", `${key}.json` );
		if ( !fs.existsSync( from ) ) return null;
		const to = path.join( base, "temp", "cache", "execution-flow", `${key}.json` );
		if ( !fs.existsSync( to ) ) copyAtomically( from, to );
		return JSON.parse( fs.readFileSync( to, "utf8" ) );
	} catch {
		return null;
	}
}

/*
================
publishWarmCaches

This checkout's temp/cache -> the shared store, for files newer than what
the store holds. Returns how many files were copied. Never throws.
================
*/
export function publishWarmCaches( base = root, store = sharedWarmCacheRoot() ) {
	let published = 0;
	try {
		const key = warmCacheKey( base );
		for ( const name of BUILDINFOS ) {
			const from = path.join( base, "temp", "cache", "tsc", `${name}.tsbuildinfo` );
			const to = path.join( store, "tsc", key, `${name}.tsbuildinfo` );
			if ( fs.existsSync( from ) && isNewer( from, to ) ) {
				copyAtomically( from, to );
				published++;
			}
		}
		const localGraphs = path.join( base, "temp", "cache", "execution-flow" );
		if ( fs.existsSync( localGraphs ) ) {
			for ( const file of fs.readdirSync( localGraphs ) ) {
				if ( !file.endsWith( ".json" ) ) continue;
				const from = path.join( localGraphs, file );
				const to = path.join( store, "execution-flow", file );
				if ( isNewer( from, to ) ) {
					copyAtomically( from, to );
					published++;
				}
			}
		}
		if ( published > 0 ) console.error( `warm-cache: published ${published} cache file(s) to ${store}` );
	} catch ( error ) {
		console.error( `warm-cache: publish skipped (${error.message})` );
	}
	return published;
}

/*
================
main
================
*/
function main() {
	const mode = process.argv[2];
	if ( mode === "seed" ) seedWarmCaches();
	else if ( mode === "publish" ) publishWarmCaches();
	else {
		console.error( "usage: node tools/warm-cache.mjs seed|publish" );
		return;
	}
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === path.resolve( import.meta.filename ) ) {
	main();
}
