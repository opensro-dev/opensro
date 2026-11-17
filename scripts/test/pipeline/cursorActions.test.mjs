/*
===========================================================================

cursorActions.test.mjs - the native cursors replay from the action store

A temp game root holds a stand-in SRO_Client.exe and a temp public tree
receives the PNGs (no game data, no Python: the extractor is injected).
A warm run extracts nothing and still claims all ten cursors; a changed
client or a new pefile extracts again; a touched client does not.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, utimes, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

// The public tree is resolved when the build modules load: point it at a
// temp tree first, so claims name /assets/cursors/... paths under it.
const scratch = await mkdtemp( path.join( os.tmpdir(), "sro-cursor-actions-" ) );
process.env.SRO_GENERATED_ROOT = path.join( scratch, "generated" );
const { RETAIL_CURSOR_IDS, extractRetailCursors } = await import( "../../build/cursors.mjs" );
const { openStore } = await import( "../../build/graph/store.mjs" );
const { openFileHashCache } = await import( "../../build/shared/fileHashCache.mjs" );
const { abandonPublication, beginPublication, recordClaims } = await import(
	"../../build/shared/publicationLedger.mjs"
);
const { publicRoot } = await import( "../../build/world/paths.mjs" );

test.after( () => rm( scratch, { recursive: true, force: true } ) );

/*
================
fixture

A stand-in client, the real public cursor folder of the temp generated
root, a store of the test's own. runs counts the extractions; the fake
extractor writes each id as its PNG's bytes.
================
*/
async function fixture( t ) {
	const root = await mkdtemp( path.join( scratch, "case-" ) );
	const roots = { game: path.join( root, "game" ), cursors: path.join( publicRoot, "assets", "cursors" ) };
	await rm( roots.cursors, { recursive: true, force: true } );
	await mkdir( roots.game, { recursive: true } );
	await mkdir( roots.cursors, { recursive: true } );
	const client = path.join( roots.game, "SRO_Client.exe" );
	await writeFile( client, "client v1" );
	const store = await openStore( {
		root: path.join( root, "store" ),
		hashCache: await openFileHashCache( path.join( root, "hash-cache.json" ) )
	} );
	t.after( () => store.shutdown() );
	const runs = [];
	const run = ( toolchain = "python 3.13.0; pefile 2024.8.26" ) =>
		extractRetailCursors( {
			store,
			toolchain,
			roots,
			extract: async ids => {
				runs.push( ids.length );
				for ( const id of ids ) {
					await writeFile( path.join( roots.cursors, `sro_client_cursor_${id}.png` ), id );
				}
			}
		} );
	return { client, runs, run };
}

test("a warm run extracts nothing and still claims every cursor", async t => {
	const f = await fixture( t );
	await f.run();
	assert.deepEqual( f.runs, [ RETAIL_CURSOR_IDS.length ] );
	const claims = new Set();
	beginPublication( "cursor-actions-test" );
	try {
		await recordClaims( claims, () => f.run() );
	} finally {
		abandonPublication();
	}
	assert.equal( f.runs.length, 1, "the warm run is a store hit" );
	assert.deepEqual(
		[ ...claims ].sort(),
		RETAIL_CURSOR_IDS.map( id => `/assets/cursors/sro_client_cursor_${id}.png` ).sort()
	);
});

test("a changed client or a new pefile extracts again; a touched client does not", async t => {
	const f = await fixture( t );
	await f.run();
	const later = new Date( Date.now() + 60_000 );
	await utimes( f.client, later, later );
	await f.run();
	assert.equal( f.runs.length, 1, "a touch keeps the key" );
	await writeFile( f.client, "client v2" );
	await f.run();
	assert.equal( f.runs.length, 2 );
	await f.run( "python 3.13.0; pefile 2025.1.1" );
	assert.equal( f.runs.length, 3 );
});
