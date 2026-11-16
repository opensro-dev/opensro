/*
===========================================================================

actionDiscoveredReads.test.mjs - steps that find their inputs as they run

Temp trees stand in for the build (no game data). A step with
discoverRoots notes what it reads (reads.mjs) and its record keeps those
files with their digests: an unchanged tree reuses the outputs, a changed
read reruns, a probe that found nothing reruns once the file appears,
and a read outside every root fails the step with nothing recorded. Two
such steps running at once each record only their own reads, and a hit
hands back the small result the storing run returned.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { openFileHashCache } from "../../build/shared/fileHashCache.mjs";
import { runGraph } from "../../build/graph/graph.mjs";
import { noteRead } from "../../build/graph/reads.mjs";
import { openStore } from "../../build/graph/store.mjs";

/*
================
fixture

A temp tree with data/ (the discover root), out/, a store and a hash
cache; code hashes are fixed.
================
*/
async function fixture( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-action-reads-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const data = path.join( root, "data" );
	await mkdir( data, { recursive: true } );
	await writeFile( path.join( data, "model.bsr" ), "mesh.bms" );
	await writeFile( path.join( data, "mesh.bms" ), "vertices-1" );
	await writeFile( path.join( data, "other.bsr" ), "other.bms" );
	await writeFile( path.join( data, "other.bms" ), "other-vertices" );
	const open = async () =>
		openStore( {
			root: path.join( root, "store" ),
			hashCache: await openFileHashCache( path.join( root, "hash-cache.json" ) ),
			codeHashOf: async () => "code-1"
		} );
	return { root, data, out: path.join( root, "out" ), open };
}

/*
================
model

A step that reads its BSR, then the mesh the BSR names, and probes an
optional override beside it: the shape of a character model build.
================
*/
function model( f, runs, name = "model" ) {
	return {
		name,
		outputRoot: f.out,
		outputs: [ `${name}.glb` ],
		code: [ path.join( f.root, "tool.mjs" ) ],
		discoverRoots: { data: f.data },
		run: async ( { outputRoot } ) => {
			runs.push( name );
			const bsr = path.join( f.data, `${name}.bsr` );
			noteRead( bsr );
			const mesh = path.join( f.data, await readFile( bsr, "utf8" ) );
			noteRead( mesh );
			const override = path.join( f.data, `${name}.override` );
			noteRead( override );
			const body = existsSync( override ) ? await readFile( override, "utf8" ) : await readFile( mesh, "utf8" );
			await writeFile( path.join( outputRoot, `${name}.glb` ), `glb(${body})` );
		}
	};
}

/*
================
build
================
*/
async function build( f, specs ) {
	const store = await f.open();
	try {
		return await runGraph( specs, { store, jobs: 4, env: {} } );
	} finally {
		await store.shutdown();
	}
}

test("an unchanged tree reuses a discovered step, a changed read reruns it", async ( t ) => {
	const f = await fixture( t ), runs = [];
	await build( f, [ model( f, runs ) ] );
	assert.deepEqual( runs, [ "model" ] );
	await build( f, [ model( f, runs ) ] );
	assert.deepEqual( runs, [ "model" ], "nothing it read changed" );
	// The mesh is not named by the action; only the run found it.
	await writeFile( path.join( f.data, "mesh.bms" ), "vertices-two" );
	await build( f, [ model( f, runs ) ] );
	assert.deepEqual( runs, [ "model", "model" ] );
	assert.equal( await readFile( path.join( f.out, "model.glb" ), "utf8" ), "glb(vertices-two)" );
});

test("a probe that found nothing reruns the step once the file appears", async ( t ) => {
	const f = await fixture( t ), runs = [];
	await build( f, [ model( f, runs ) ] );
	await writeFile( path.join( f.data, "model.override" ), "override" );
	await build( f, [ model( f, runs ) ] );
	assert.deepEqual( runs, [ "model", "model" ] );
	assert.equal( await readFile( path.join( f.out, "model.glb" ), "utf8" ), "glb(override)" );
});

test("a read outside every discover root fails the step and records nothing", async ( t ) => {
	const f = await fixture( t ), runs = [];
	const outside = path.join( f.root, "elsewhere.txt" );
	await writeFile( outside, "x" );
	const leaky = {
		...model( f, runs ),
		run: async ( { outputRoot } ) => {
			runs.push( "leaky" );
			noteRead( outside );
			await writeFile( path.join( outputRoot, "model.glb" ), "glb" );
		}
	};
	await assert.rejects( build( f, [ leaky ] ), /outside its discoverRoots/ );
	await assert.rejects( build( f, [ leaky ] ), /outside its discoverRoots/ );
	assert.deepEqual( runs, [ "leaky", "leaky" ], "the failed run left no record to reuse" );
});

test("steps running at once each record only their own reads", async ( t ) => {
	const f = await fixture( t ), runs = [];
	await build( f, [ model( f, runs, "model" ), model( f, runs, "other" ) ] );
	assert.deepEqual( runs.toSorted(), [ "model", "other" ] );
	await writeFile( path.join( f.data, "other.bms" ), "other-vertices-2" );
	await build( f, [ model( f, runs, "model" ), model( f, runs, "other" ) ] );
	assert.deepEqual( runs.slice( 2 ), [ "other" ], "model read nothing of other's" );
});

test("a hit hands back the result the run that stored it returned", async ( t ) => {
	const f = await fixture( t ), runs = [];
	const counted = {
		...model( f, runs ),
		run: async ( context ) => {
			await model( f, runs ).run( context );
			return { clips: [ "stand", "walk" ], bytes: 17 };
		}
	};
	const first = await build( f, [ counted ] );
	const second = await build( f, [ counted ] );
	assert.deepEqual( runs, [ "model" ] );
	assert.equal( second.model.hit, true );
	assert.deepEqual( second.model.result, { clips: [ "stand", "walk" ], bytes: 17 } );
	assert.deepEqual( second.model.result, first.model.result );
});
