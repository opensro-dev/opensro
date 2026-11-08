/*
===========================================================================

warm-cache.test.mjs - the shared cache warms a fresh checkout, keyed by content

tools/warm-cache.mjs copies tsc buildinfos and execution graphs between a
checkout's temp/cache and the shared .state store, so a fresh worktree
skips the cold type-check a sibling already paid for (issue #2907). Pinned
here: a publish then a seed restores byte-identical caches, seeding never
overwrites a file the checkout already has, and a checkout whose inputs
differ (another content key) gets no buildinfo. A shared cache can only
ever change speed, never a verdict: tsc revalidates its buildinfo, and
the execution graph is looked up by its own input hash.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { publishWarmCaches, seedWarmCaches, warmCacheKey } from "../../tools/warm-cache.mjs";

/*
================
makeCheckout

A minimal client tree: the inputs warmCacheKey hashes, plus optionally the
caches a completed cold run leaves behind.
================
*/
function makeCheckout( base, marker ) {
	for ( const directory of [ "src", "tests", "tools" ] ) {
		fs.mkdirSync( path.join( base, directory ), { recursive: true } );
	}
	fs.writeFileSync( path.join( base, "tsconfig.json" ), "{}\n" );
	fs.writeFileSync( path.join( base, "tsconfig.tests.json" ), "{}\n" );
	fs.writeFileSync( path.join( base, "src", "input.ts" ), `// ${marker}\n` );
}

/*
================
writeCaches
================
*/
function writeCaches( base, marker ) {
	const tsc = path.join( base, "temp", "cache", "tsc" );
	const graphs = path.join( base, "temp", "cache", "execution-flow" );
	fs.mkdirSync( tsc, { recursive: true } );
	fs.mkdirSync( graphs, { recursive: true } );
	fs.writeFileSync( path.join( tsc, "client.tsbuildinfo" ), `buildinfo-${marker}` );
	fs.writeFileSync( path.join( graphs, "graphkey.json" ), `{"graph":"${marker}"}` );
}

/*
================
fixture

One scratch tree under the checkout's temp/ (the test recorder ignores
the OS temporary directory), removed when the test ends.
================
*/
function fixture( t, name ) {
	const base = path.join( root, "temp", `warm-cache-test-${name}-${process.pid}` );
	fs.rmSync( base, { recursive: true, force: true } );
	fs.mkdirSync( base, { recursive: true } );
	t.after( () => fs.rmSync( base, { recursive: true, force: true } ) );
	return {
		a: path.join( base, "a" ),
		b: path.join( base, "b" ),
		store: path.join( base, "store" )
	};
}

test("publish then seed restores the caches byte for byte", ( t ) => {
	const { a, b, store } = fixture( t, "roundtrip" );
	makeCheckout( a, "same" );
	makeCheckout( b, "same" );
	writeCaches( a, "paid" );

	assert.ok( publishWarmCaches( a, store ) >= 2, "publish copies the buildinfo and the graph" );
	const key = warmCacheKey( a );
	assert.equal(
		fs.readFileSync( path.join( store, "tsc", key, "client.tsbuildinfo" ), "utf8" ),
		"buildinfo-paid",
		"the store holds the buildinfo under the content key"
	);

	assert.ok( seedWarmCaches( b, store ) >= 2, "seed copies both caches into the fresh checkout" );
	assert.equal(
		fs.readFileSync( path.join( b, "temp", "cache", "tsc", "client.tsbuildinfo" ), "utf8" ),
		"buildinfo-paid"
	);
	assert.equal(
		fs.readFileSync( path.join( b, "temp", "cache", "execution-flow", "graphkey.json" ), "utf8" ),
		'{"graph":"paid"}'
	);
});

test("seed never overwrites a cache the checkout already has", ( t ) => {
	const { a, b, store } = fixture( t, "no-overwrite" );
	makeCheckout( a, "same" );
	makeCheckout( b, "same" );
	writeCaches( a, "paid" );
	publishWarmCaches( a, store );
	writeCaches( b, "mine" );

	assert.equal( seedWarmCaches( b, store ), 0, "nothing is seeded over existing files" );
	assert.equal(
		fs.readFileSync( path.join( b, "temp", "cache", "tsc", "client.tsbuildinfo" ), "utf8" ),
		"buildinfo-mine"
	);
});

test("different input content gets no buildinfo", ( t ) => {
	const { a, b, store } = fixture( t, "key-mismatch" );
	makeCheckout( a, "one" );
	makeCheckout( b, "two" );
	writeCaches( a, "paid" );
	publishWarmCaches( a, store );

	assert.notEqual( warmCacheKey( b ), warmCacheKey( a ), "different inputs hash to a different key" );
	seedWarmCaches( b, store );
	assert.equal(
		fs.existsSync( path.join( b, "temp", "cache", "tsc", "client.tsbuildinfo" ) ),
		false,
		"no buildinfo crosses to a checkout whose inputs differ"
	);
});
