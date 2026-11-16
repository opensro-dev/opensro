/*
===========================================================================

actionGraph.test.mjs - the action cache keys on content and replays it

Temp trees stand in for the build (no game data). The key moves with
input bytes, code, env and params, and not with a touch; an unchanged
rerun runs nothing and lays nothing down; a deleted output comes back
from the store; another worktree's output root is filled without a run;
a failure records nothing and stops what depends on it; two builds
storing at once both finish with whole files; an in-place write through
a hard link is caught; two actions writing one file are refused.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, stat, utimes, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { openFileHashCache } from "../../build/shared/fileHashCache.mjs";
import { actionKey, defineAction } from "../../build/graph/action.mjs";
import { orderActions, runGraph } from "../../build/graph/graph.mjs";
import { objectPath, openStore, readRecord } from "../../build/graph/store.mjs";

/*
================
fixture

A temp tree with src/ (inputs), out/ (outputs), store/ and a hash cache;
code hashes come from a map the test edits.
================
*/
async function fixture( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-action-graph-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const code = new Map( [ [ path.resolve( root, "tool.mjs" ), "code-1" ] ] );
	const open = async () =>
		openStore( {
			root: path.join( root, "store" ),
			hashCache: await openFileHashCache( path.join( root, "hash-cache.json" ) ),
			codeHashOf: async entry => code.get( entry )
		} );
	await writeFile( path.join( root, "a.txt" ), "alpha" );
	return { root, code, open, src: root, out: path.join( root, "out" ) };
}

/*
================
upper

An action that writes its input upper-cased; runs counts its runs.
================
*/
function upper( f, runs, overrides = {} ) {
	return {
		name: "upper",
		inputRoot: f.src,
		inputs: [ "a.txt" ],
		outputRoot: f.out,
		outputs: [ "A.txt" ],
		code: [ path.join( f.root, "tool.mjs" ) ],
		env: [ "SRO_TEST_KNOB" ],
		run: async ( { outputRoot } ) => {
			runs.push( "upper" );
			const text = await readFile( path.join( f.src, "a.txt" ), "utf8" );
			await writeFile( path.join( outputRoot, "A.txt" ), text.toUpperCase() );
		},
		...overrides
	};
}

async function build( f, specs, options = {} ) {
	const store = await f.open();
	try {
		return await runGraph( specs, { store, jobs: 4, env: {}, ...options } );
	} finally {
		await store.shutdown();
	}
}

test("the key moves with bytes, code, env and params, not with a touch", async ( t ) => {
	const f = await fixture( t );
	const store = await f.open();
	const key = ( spec, env = {} ) =>
		actionKey( defineAction( spec ), {
			hashInput: file => store.hashFile( file ),
			codeHashOf: entry => store.codeHash( entry ),
			env
		} );
	const spec = upper( f, [] );
	const first = await key( spec );
	const later = new Date( Date.now() + 60000 );
	await utimes( path.join( f.root, "a.txt" ), later, later );
	assert.equal( await key( spec ), first, "a touch keeps the key" );
	await writeFile( path.join( f.root, "a.txt" ), "alphA" );
	const changed = await key( spec );
	assert.notEqual( changed, first, "a byte change moves it" );
	assert.notEqual( await key( spec, { SRO_TEST_KNOB: "on" } ), changed, "a named variable moves it" );
	assert.equal( await key( spec, { SRO_OTHER: "on" } ), changed, "an unnamed variable does not" );
	assert.notEqual( await key( { ...spec, params: { tier: 2 } } ), changed, "params move it" );
	f.code.set( path.join( f.root, "tool.mjs" ), "code-2" );
	const recoded = await openStore( {
		root: store.root,
		hashCache: await openFileHashCache( path.join( f.root, "other-cache.json" ) ),
		codeHashOf: async entry => f.code.get( entry )
	} );
	assert.notEqual(
		await actionKey( defineAction( spec ), {
			hashInput: file => recoded.hashFile( file ),
			codeHashOf: entry => recoded.codeHash( entry )
		} ),
		changed,
		"a code change moves it"
	);
});

test("an unchanged rerun runs nothing and lays nothing down", async ( t ) => {
	const f = await fixture( t );
	const runs = [];
	const first = await build( f, [ upper( f, runs ) ] );
	assert.deepEqual( [ first.upper.hit, runs.length ], [ false, 1 ] );
	assert.equal( await readFile( path.join( f.out, "A.txt" ), "utf8" ), "ALPHA" );
	const second = await build( f, [ upper( f, runs ) ] );
	assert.deepEqual( [ second.upper.hit, second.upper.placed, runs.length ], [ true, 0, 1 ] );
	const later = new Date( Date.now() + 60000 );
	await utimes( path.join( f.root, "a.txt" ), later, later );
	const touched = await build( f, [ upper( f, runs ) ] );
	assert.deepEqual( [ touched.upper.hit, runs.length ], [ true, 1 ], "a touch-only input is a hit" );
});

test("a deleted or edited output comes back from the store", async ( t ) => {
	const f = await fixture( t );
	const runs = [];
	await build( f, [ upper( f, runs ) ] );
	await rm( path.join( f.out, "A.txt" ) );
	const restored = await build( f, [ upper( f, runs ) ] );
	assert.deepEqual( [ restored.upper.hit, restored.upper.placed, runs.length ], [ true, 1, 1 ] );
	assert.equal( await readFile( path.join( f.out, "A.txt" ), "utf8" ), "ALPHA" );
	await writeFile( path.join( f.out, "A.txt" ), "edited" );
	await build( f, [ upper( f, runs ) ] );
	assert.equal( await readFile( path.join( f.out, "A.txt" ), "utf8" ), "ALPHA" );
	assert.equal( runs.length, 1 );
});

test("another worktree's output root is filled without a run", async ( t ) => {
	const f = await fixture( t );
	const runs = [];
	await build( f, [ upper( f, runs ) ] );
	const elsewhere = path.join( f.root, "other-worktree" );
	const result = await build( f, [ upper( f, runs, { outputRoot: elsewhere } ) ] );
	assert.deepEqual( [ result.upper.hit, runs.length ], [ true, 1 ] );
	assert.equal( await readFile( path.join( elsewhere, "A.txt" ), "utf8" ), "ALPHA" );
});

test("a change reruns the action and what reads its output", async ( t ) => {
	const f = await fixture( t );
	const runs = [];
	const echo = {
		name: "echo",
		deps: [ "upper" ],
		inputRoot: f.out,
		inputs: [ "A.txt" ],
		outputRoot: f.out,
		outputs: [ "B.txt" ],
		run: async () => {
			runs.push( "echo" );
			await writeFile( path.join( f.out, "B.txt" ), `${await readFile( path.join( f.out, "A.txt" ), "utf8" )}!` );
		}
	};
	await build( f, [ echo, upper( f, runs ) ] );
	assert.deepEqual( runs, [ "upper", "echo" ] );
	await writeFile( path.join( f.root, "a.txt" ), "beta" );
	await build( f, [ echo, upper( f, runs ) ] );
	assert.deepEqual( runs, [ "upper", "echo", "upper", "echo" ] );
	assert.equal( await readFile( path.join( f.out, "B.txt" ), "utf8" ), "BETA!" );
});

test("a failure records nothing and stops its dependents", async ( t ) => {
	const f = await fixture( t );
	const runs = [];
	let fail = true;
	const broken = upper( f, runs, {
		run: async ( { outputRoot } ) => {
			runs.push( "upper" );
			await writeFile( path.join( outputRoot, "A.txt" ), "half" );
			if ( fail ) throw new Error( "one lane failed" );
		}
	} );
	const after = {
		name: "after",
		deps: [ "upper" ],
		outputRoot: f.out,
		outputs: [ "C.txt" ],
		run: async () => runs.push( "after" )
	};
	await assert.rejects( build( f, [ broken, after ] ), /one lane failed/ );
	assert.deepEqual( runs, [ "upper" ] );
	fail = false;
	await assert.rejects( build( f, [ broken, after ] ), /declared output C.txt was not written/ );
	assert.deepEqual( runs, [ "upper", "upper", "after" ], "the failed action runs again, then its dependent" );
});

test("two builds storing the same action at once both finish whole", async ( t ) => {
	const f = await fixture( t );
	const runs = [];
	const one = path.join( f.root, "one" ), two = path.join( f.root, "two" );
	await Promise.all( [
		build( f, [ upper( f, runs, { outputRoot: one } ) ] ),
		build( f, [ upper( f, runs, { outputRoot: two } ) ] )
	] );
	for ( const root of [ one, two ] ) assert.equal( await readFile( path.join( root, "A.txt" ), "utf8" ), "ALPHA" );
	const store = await f.open();
	const result = await runGraph( [ upper( f, runs ) ], { store, env: {} } );
	assert.equal( result.upper.hit, true );
	const record = await readRecord( store, result.upper.key );
	assert.equal( await readFile( objectPath( store, record.outputs[0].sha256 ), "utf8" ), "ALPHA" );
});

// Enough identical outputs stored at once that two publishes of one digest
// overlap (the race is a window between a stat and a rename).
const TWINS = 300;
const TWIN_BYTES = Buffer.alloc( 256 * 1024, 7 );

test("outputs with the same bytes stored at once all replay", async ( t ) => {
	// Two images that convert to one PNG publish one object; the second
	// writer must not swap it under the first one's record.
	const f = await fixture( t );
	for ( const link of [ false, true ] ) {
		const runs = [];
		const twin = index => ({
			name: `twin-${link}-${index}`,
			outputRoot: path.join( f.root, `twins-${link}` ),
			outputs: [ `${index}.txt` ],
			params: { index, link },
			link,
			run: async ( { outputRoot } ) => {
				runs.push( index );
				await writeFile( path.join( outputRoot, `${index}.txt` ), TWIN_BYTES );
			}
		});
		const specs = Array.from( { length: TWINS }, ( _, index ) => twin( index ) );
		await build( f, specs, { jobs: TWINS } );
		assert.equal( runs.length, TWINS );
		const again = await build( f, specs, { jobs: TWINS } );
		assert.equal( runs.length, TWINS, `every twin replays (link ${link})` );
		assert.ok( Object.values( again ).every( result => result.hit ) );
	}
});

test("the same key twice in one build runs once", async ( t ) => {
	const f = await fixture( t );
	const runs = [];
	const result = await build( f, [
		upper( f, runs ),
		upper( f, runs, { name: "upper-again", outputRoot: path.join( f.root, "again" ) } )
	] );
	assert.equal( runs.length, 1 );
	assert.equal( result.upper.key, result["upper-again"].key );
	assert.equal( await readFile( path.join( f.root, "again", "A.txt" ), "utf8" ), "ALPHA" );
});

test("two actions writing one file are refused before either runs", async ( t ) => {
	const f = await fixture( t );
	const runs = [];
	await assert.rejects(
		build( f, [ upper( f, runs ), upper( f, runs, { name: "upper-twin", params: { other: true } } ) ] ),
		/both write/
	);
	assert.deepEqual( runs, [] );
});

test("independent actions run at once within the budget", async ( t ) => {
	const f = await fixture( t );
	let running = 0, peak = 0;
	const slow = name => ({
		name,
		outputRoot: f.out,
		outputs: [ `${name}.txt` ],
		params: { name },
		run: async () => {
			running += 1;
			peak = Math.max( peak, running );
			await new Promise( resolve => setTimeout( resolve, 30 ) );
			await writeFile( path.join( f.out, `${name}.txt` ), name );
			running -= 1;
		}
	});
	await build( f, [ slow( "x" ), slow( "y" ), slow( "z" ) ], { jobs: 2 } );
	assert.equal( peak, 2 );
});

test("an in-place write through a hard link is caught, not replayed", async ( t ) => {
	const f = await fixture( t );
	const runs = [];
	const linked = upper( f, runs, { link: true } );
	await build( f, [ linked ] );
	await rm( path.join( f.out, "A.txt" ) );
	await build( f, [ linked ] );
	const before = await stat( path.join( f.out, "A.txt" ) );
	if ( before.nlink < 2 ) return t.skip( "this volume does not hard link" );
	// A tool rewriting the output in place rewrites the stored object too.
	await writeFile( path.join( f.out, "A.txt" ), "XXXXX" );
	const result = await build( f, [ linked ] );
	assert.equal( result.upper.hit, false );
	assert.equal( runs.length, 2 );
	assert.equal( await readFile( path.join( f.out, "A.txt" ), "utf8" ), "ALPHA" );
});

test("a damaged object is never recorded again under its name", async ( t ) => {
	// A writes "ALPHA" through a link; a tool rewrites it in place, which
	// damages the stored object; B then produces "ALPHA" too and must not
	// record the damaged object as its bytes (#3652 review).
	const f = await fixture( t );
	const writer = ( name ) => ({
		name,
		outputRoot: path.join( f.root, name ),
		outputs: [ "out.txt" ],
		params: { name },
		link: true,
		run: async ( { outputRoot } ) => {
			await writeFile( path.join( outputRoot, "out.txt" ), "ALPHA" );
		}
	});
	await build( f, [ writer( "a" ) ] );
	const linked = await stat( path.join( f.root, "a", "out.txt" ) );
	if ( linked.nlink < 2 ) return t.skip( "this volume does not hard link" );
	await writeFile( path.join( f.root, "a", "out.txt" ), "XXXXX" );
	await build( f, [ writer( "b" ) ] );
	await rm( path.join( f.root, "b", "out.txt" ) );
	const replay = await build( f, [ writer( "b" ) ] );
	assert.equal( replay.b.hit, true );
	assert.equal( await readFile( path.join( f.root, "b", "out.txt" ), "utf8" ), "ALPHA" );
});

test("a malformed graph is refused before anything runs", () => {
	const spec = name => ({ name, outputRoot: "out", outputs: [ `${name}.out` ], run: async () => {} });
	assert.throws( () => orderActions( [ spec( "a" ), spec( "a" ) ] ), /two actions are named a/ );
	assert.throws( () => orderActions( [ { ...spec( "a" ), deps: [ "b" ] } ] ), /unknown action b/ );
	assert.throws(
		() => orderActions( [ { ...spec( "a" ), deps: [ "b" ] }, { ...spec( "b" ), deps: [ "a" ] } ] ),
		/cycle/
	);
	assert.throws( () => defineAction( { ...spec( "a" ), outputs: [ "../escape" ] } ), /inside its root/ );
	assert.throws( () => defineAction( { ...spec( "a" ), outputs: [] } ), /declares no outputs/ );
});
