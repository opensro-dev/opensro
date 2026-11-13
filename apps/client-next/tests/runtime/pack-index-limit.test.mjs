/*
===========================================================================

pack-index-limit.test.mjs - a pack index past 16 MiB still loads

The browser read /assets/packs/manifest.json with a 16 MiB cap. The
Alexandria variant's index reached 17.5 MB, so every asset lookup failed
with "Response exceeds byte limit" while the server served each file
correctly. The index limit is PACK_INDEX_LIMIT_BYTES (packs.ts), and the
build refuses an index past the same value (packFormat.mjs).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createLoader } = await import( "../../src/engine/runtime/assets/worker/loader.ts" );

// Past the old 16 MiB cap, well inside the new limit.
const INDEX_BYTES = 18 << 20;

/*
================
settle
================
*/
const settle = () => new Promise( resolve => setImmediate( resolve ) );

/*
================
paddedIndex

A valid empty pack index, padded with JSON whitespace to INDEX_BYTES.
================
*/
function paddedIndex() {
	const body = JSON.stringify( { format: "sro-asset-pack-index", version: 2, groups: [], assets: [] } );
	return body.slice( 0, -1 ) + " ".repeat( INDEX_BYTES - body.length ) + "}";
}

test("an asset loads when the pack index is larger than 16 MiB", async t => {
	const index = paddedIndex();
	assert.ok( index.length > 16 << 20 );
	let indexReads = 0;
	t.mock.method( globalThis, "fetch", async url => {
		const text = String( url );
		if ( text.endsWith( "/assets/packs/manifest.json" ) ) {
			indexReads++;
			return new Response( index );
		}
		if ( text.endsWith( "/hd/packs/manifest.json" ) ) return new Response( "", { status: 404 } );
		return new Response( Uint8Array.of( 1, 2, 3, 4 ) );
	} );
	const messages = [];
	const loader = createLoader( message => messages.push( message ) );
	t.after( () => loader.dispose() );
	loader.receive( { kind: "load", id: 1, url: "https://example.test/assets/ui/button.bin", limit: 1024 } );
	for ( let turn = 0; turn < 400 && !messages.some( m => m.id === 1 && m.kind !== "progress" ); turn++ ) {
		await settle();
	}
	// The empty index was read and parsed, so the asset is honestly absent
	// from it; before the fix the index read itself failed on its size.
	const answer = messages.find( m => m.id === 1 && m.kind === "error" );
	assert.ok( indexReads >= 1, "the load must consult the pack index" );
	assert.ok( answer, "the load must answer" );
	assert.doesNotMatch( answer.error, /byte limit/ );
	assert.equal( answer.absent, true, answer.error );
});
