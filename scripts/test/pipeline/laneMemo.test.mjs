/*
===========================================================================

laneMemo.test.mjs - a lane replays only while nothing it read has changed

The lane memo (scripts/build/shared/laneMemo.mjs) skips a resource-build
lane on a whole-build fingerprint miss. A replay must return the recorded
result and re-claim the lane's files; any change to its code, its upstream
or one of its outputs must run it again instead.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, rm, utimes, writeFile, mkdir } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { createLaneMemo, decodeResult, encodeResult } from "../../build/shared/laneMemo.mjs";
import { abandonPublication, beginPublication, claimPublicPaths } from "../../build/shared/publicationLedger.mjs";

const OUTPUT = "/assets/lane-memo-test/out.json";

/*
================
withLane

A temp public root holding the lane's one output, a code module, an entry
file and a memo root; run( fixture ) builds lanes over them.
================
*/
async function withLane( run ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-lane-memo-" ) );
	const publicRoot = path.join( root, "public" ), memoRoot = path.join( root, "memo" );
	const output = path.join( publicRoot, OUTPUT.slice( 1 ) );
	const module = path.join( root, "step.mjs" ), entry = path.join( root, "entry.mjs" );
	await mkdir( path.dirname( output ), { recursive: true } );
	await writeFile( output, "{}" );
	await writeFile( module, "export const step = 1;\n" );
	await writeFile( entry, "// entry\n" );
	const logs = [];
	const memo = ( upstream = { data: { extracted: "a" }, startup: "s" } ) =>
		createLaneMemo( { upstream, entry, publicRoot, memoRoot, log: line => logs.push( line ) } );
	beginPublication( "lane-memo-test", { complete: false } );
	try {
		await run( { memo, module, output, logs } );
	} finally {
		abandonPublication();
		await rm( root, { recursive: true, force: true } );
	}
}

/*
================
produce

A lane run that claims its output and returns a result with a Set and a Map.
================
*/
function produce( counter ) {
	return async () => {
		counter.runs++;
		claimPublicPaths( [ OUTPUT ] );
		return { count: 3, files: new Set( [ OUTPUT ] ), byName: new Map( [ [ "a", 1 ] ] ) };
	};
}

test("plain results round-trip with their Maps and Sets; anything else is not recorded", () => {
	const value = { a: 1, list: [ 1, "x" ], set: new Set( [ "y" ] ), map: new Map( [ [ "k", { v: 2 } ] ] ) };
	assert.deepEqual( decodeResult( encodeResult( value ) ), value );
	assert.equal( encodeResult( { run() {} } ), undefined );
	assert.equal( encodeResult( new Date( 0 ) ), undefined );
});

test("an unchanged lane replays its result; code, upstream and output changes run it", async () => {
	await withLane( async ( { memo, module, output, logs } ) => {
		const counter = { runs: 0 }, inputs = { modules: [ module ] };
		const first = memo();
		await first.lane( "lane", inputs, produce( counter ) );
		await first.commit();
		assert.equal( counter.runs, 1 );

		const replayed = await memo().lane( "lane", inputs, produce( counter ) );
		assert.equal( counter.runs, 1, "an unchanged lane must not run" );
		assert.deepEqual( replayed, { count: 3, files: new Set( [ OUTPUT ] ), byName: new Map( [ [ "a", 1 ] ] ) } );

		const moved = memo( { data: { extracted: "b" }, startup: "s" } );
		await moved.lane( "lane", inputs, produce( counter ) );
		assert.equal( counter.runs, 2 );
		assert.match( logs.at( -1 ), /data extracted changed/ );
		await moved.commit();

		await writeFile( module, "export const step = 2;\n" );
		const edited = memo( { data: { extracted: "b" }, startup: "s" } );
		await edited.lane( "lane", inputs, produce( counter ) );
		assert.equal( counter.runs, 3 );
		assert.match( logs.at( -1 ), /code .*step\.mjs changed/ );
		await edited.commit();

		await utimes( output, new Date( 2000, 0, 1 ), new Date( 2000, 0, 1 ) );
		await memo( { data: { extracted: "b" }, startup: "s" } ).lane( "lane", inputs, produce( counter ) );
		assert.equal( counter.runs, 4 );
		assert.match( logs.at( -1 ), /output \/assets\/lane-memo-test\/out\.json changed/ );
	} );
});

test("a lane keyed on a computed digest replays while it holds and runs when it moves", async () => {
	// The npc lane keys on its server roster exports, not the server's
	// sources: equal exports replay however much Go changed (r204m, #4353).
	await withLane( async ( { memo, module, logs } ) => {
		const counter = { runs: 0 };
		let digest = "rosters-a";
		const inputs = { modules: [ module ], key: async () => digest };
		const first = memo();
		await first.lane( "lane", inputs, produce( counter ) );
		await first.commit();
		await memo().lane( "lane", inputs, produce( counter ) );
		assert.equal( counter.runs, 1, "an unchanged key must not run the lane" );
		digest = "rosters-b";
		await memo().lane( "lane", inputs, produce( counter ) );
		assert.equal( counter.runs, 2 );
		assert.match( logs.at( -1 ), /roots changed/ );
	} );
});

test("a record written before folder keys replays and learns them; a moved folder then names itself", async () => {
	await withLane( async ( { memo, module, logs } ) => {
		const counter = { runs: 0 }, inputs = { modules: [ module ] };
		const first = memo( { data: { extracted: "a" }, startup: "s" } );
		await first.lane( "lane", inputs, produce( counter ) );
		await first.commit();
		assert.equal( counter.runs, 1 );

		// The same root hash, now carrying per-folder keys beside it:
		// keys the record predates are instrumentation, not a change.
		const widened = memo( { data: { extracted: "a", "extracted/A": "f1", "extracted/B": "f2" }, startup: "s" } );
		await widened.lane( "lane", inputs, produce( counter ) );
		assert.equal( counter.runs, 1, "folder keys a record predates must not rerun the lane" );

		// A folder that actually moves reruns the lane and names itself.
		const movedFolder = memo( {
			data: { extracted: "a", "extracted/A": "f1", "extracted/B": "f3" },
			startup: "s"
		} );
		await movedFolder.lane( "lane", inputs, produce( counter ) );
		assert.equal( counter.runs, 2 );
		assert.match( logs.at( -1 ), /data extracted\/B changed/ );
	} );
});

test("a data root the record never held reruns the lane; only a recorded root's folder keys are learned", async () => {
	await withLane( async ( { memo, module, logs } ) => {
		const counter = { runs: 0 }, inputs = { modules: [ module ] };
		const first = memo( { data: { extracted: "a" }, startup: "s" } );
		await first.lane( "lane", inputs, produce( counter ) );
		await first.commit();

		// A new root, or a folder key under a root the record lacks, is new
		// input the record never covered, not instrumentation.
		await memo( { data: { extracted: "a", textdata: "t" }, startup: "s" } ).lane(
			"lane",
			inputs,
			produce( counter )
		);
		assert.equal( counter.runs, 2 );
		assert.match( logs.at( -1 ), /data textdata changed/ );
		await memo( { data: { extracted: "a", "textdata/A": "t1" }, startup: "s" } ).lane(
			"lane",
			inputs,
			produce( counter )
		);
		assert.equal( counter.runs, 3 );
		assert.match( logs.at( -1 ), /data textdata\/A changed/ );
	} );
});

test("a record is only written by commit", async () => {
	await withLane( async ( { memo, module } ) => {
		const counter = { runs: 0 }, inputs = { modules: [ module ] };
		await memo().lane( "lane", inputs, produce( counter ) );
		await memo().lane( "lane", inputs, produce( counter ) );
		assert.equal( counter.runs, 2, "an uncommitted run (a failed build) is never replayed" );
	} );
});
