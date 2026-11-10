/*
===========================================================================

goEnvironment.test.mjs - a second checkout adds nothing to the Go build cache

The flag arithmetic, and the behaviour it buys: the same sources built in
two folders against one scratch cache.

===========================================================================
*/
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import { effectiveGoFlags, sharedCacheEnvironment, sharedCacheFlags } from "../../lib/goEnvironment.mjs";

// The toolchain is absent where only the script tests run; the behaviour
// test needs it, the arithmetic does not.
const hasGo = spawnSync( "go", [ "version" ], { windowsHide: true } ).status === 0;

/*
================
sharedCacheFlags
================
*/
test("sharedCacheFlags adds -trimpath and keeps the caller's entries as written", () => {
	assert.equal( sharedCacheFlags( undefined ), "-trimpath" );
	assert.equal( sharedCacheFlags( "  " ), "-trimpath" );
	assert.equal( sharedCacheFlags( "-mod=mod" ), "-mod=mod -trimpath" );
	// A quoted value holds a space: the string is appended to, never rejoined.
	assert.equal( sharedCacheFlags( "'-ldflags=-s -w' -mod=mod" ), "'-ldflags=-s -w' -mod=mod -trimpath" );
});

test("sharedCacheFlags leaves a caller's own -trimpath choice alone", () => {
	assert.equal( sharedCacheFlags( "-trimpath" ), "-trimpath" );
	assert.equal( sharedCacheFlags( "-mod=mod -trimpath -count=1" ), "-mod=mod -trimpath -count=1" );
	assert.equal( sharedCacheFlags( "--trimpath=false" ), "--trimpath=false" );
	// Another flag that merely contains the word is not the flag.
	assert.equal( sharedCacheFlags( "-tags=trimpath" ), "-tags=trimpath -trimpath" );
});

/*
================
sharedCacheEnvironment
================
*/
test("sharedCacheEnvironment adds the flag to the GOFLAGS in force and keeps the rest", () => {
	const seen = [];
	const env = sharedCacheEnvironment( { PATH: "x", GOCACHE: "c" }, ( given ) => {
		seen.push( given );
		return "-mod=mod";
	} );
	assert.deepEqual( env, { PATH: "x", GOCACHE: "c", GOFLAGS: "-mod=mod -trimpath" } );
	assert.deepEqual( seen, [ { PATH: "x", GOCACHE: "c" } ], "the flags are read under the caller's environment" );
});

test("sharedCacheEnvironment replaces the caller's spelling of the name", () => {
	// Windows names are case-insensitive: two spellings would be two rivals.
	const env = sharedCacheEnvironment( { GoFlags: "-mod=mod" }, () => "-mod=mod" );
	assert.deepEqual( env, { GoFlags: "-mod=mod -trimpath" } );
});

/*
================
twoCheckouts

One tiny module written into two folders, each built once against the one
scratch cache under env. Returns how many compiled outputs the cache holds
after the first build and after the second.
================
*/
function twoCheckouts( scratch, name, env ) {
	const cache = path.join( scratch, `${name}-cache` );
	mkdirSync( cache );
	const counts = [];
	for ( const checkout of [ "first", "second" ] ) {
		const folder = path.join( scratch, name, checkout );
		mkdirSync( folder, { recursive: true } );
		writeFileSync( path.join( folder, "go.mod" ), "module example.invalid/cachetest\n\ngo 1.21\n" );
		writeFileSync( path.join( folder, "p.go" ), "package cachetest\n\nfunc F() int { return 1 }\n" );
		const build = spawnSync( "go", [ "build", "./..." ], {
			cwd: folder,
			windowsHide: true,
			encoding: "utf8",
			env: { ...env, GOCACHE: cache, GOTOOLCHAIN: "local" }
		} );
		assert.equal( build.status, 0, build.stderr );
		counts.push( compiledOutputs( cache ) );
	}
	return counts;
}

/*
================
compiledOutputs

The data entries ("<hash>-d") in a Go build cache.
================
*/
function compiledOutputs( cache ) {
	let count = 0;
	for ( const shard of readdirSync( cache, { withFileTypes: true } ) ) {
		if ( !shard.isDirectory() ) continue;
		count += readdirSync( path.join( cache, shard.name ) ).filter( ( name ) => name.endsWith( "-d" ) ).length;
	}
	return count;
}

test( "the same sources in a second folder compile nothing under the shared-cache environment", {
	skip: hasGo ? false : "needs the Go toolchain"
}, () => {
	const scratch = mkdtempSync( path.join( os.tmpdir(), "sro-go-cache-" ) );
	try {
		// The control: Go's default keys the cache by folder, so the second
		// folder compiles again. If this ever stops holding, the flag is no
		// longer what shares the cache and this module can go.
		const plain = { ...process.env, GOFLAGS: "-trimpath=false" };
		const [plainFirst, plainSecond] = twoCheckouts( scratch, "plain", plain );
		assert.ok( plainSecond > plainFirst, `default flags: ${plainFirst} outputs, then ${plainSecond}` );

		const shared = sharedCacheEnvironment( { ...process.env, GOFLAGS: "" } );
		assert.match( effectiveGoFlags( shared ), /(^|\s)-trimpath(\s|$)/ );
		const [sharedFirst, sharedSecond] = twoCheckouts( scratch, "shared", shared );
		assert.equal( sharedSecond, sharedFirst, "the second folder added to the cache" );
	} finally {
		rmSync( scratch, { recursive: true, force: true } );
	}
} );
