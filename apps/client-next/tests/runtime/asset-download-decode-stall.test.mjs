/*
===========================================================================

asset-download-decode-stall.test.mjs - a synchronous decode is not a stall

A world decode parks the asset worker's loop for its whole length. While
it lasts, no other download can be read and no stall timer can run, so a
download whose bytes were only queued behind the decode must not be
killed for the wait (production: several different packs timing out at
the same instant for one player - the whole worker frozen, every 15 s
timer firing together on wake, #2862). The decode proves it is alive
with its own work notes; this file pins both halves against the shipping
loader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createLoader } = await import( "../../src/engine/runtime/assets/worker/loader.ts" );

/*
================
settle
================
*/
const settle = () => new Promise( resolve => setImmediate( resolve ) );

test("a download stalled only by a synchronous world decode is not killed, and the decode reports its work", async t => {
	// Mock timers drive the loader's deadlines. setTime is time passing
	// while the loop is parked: it advances the clock without firing
	// anything, exactly as a synchronous decode defers every timer to
	// the moment the loop frees. Each work note stands for 6 s of the
	// decode, so the notes park the worker for 30 s inside the span.
	t.mock.timers.enable( { apis: [ "setTimeout" ] } );
	let parkedMs = 0;
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: { blocks: [] },
		terrainTextures: { tileCatalog: { referencedTiles: [] } },
		objects: {
			placements: Array.from( { length: 3000 }, ( _, i ) => ({
				objectId: 1,
				uid: i,
				regionId: "257",
				position: { x: i % 97, y: 0, z: Math.floor( i / 97 ) },
				yaw: 0
			}) ),
			resources: { meshes: [], bsr: [], materialSets: [] }
		}
	};
	let assetFetches = 0;
	t.mock.method( globalThis, "fetch", async url => {
		const text = String( url );
		if ( text.endsWith( "/assets/packs/manifest.json" ) ) {
			return new Response( JSON.stringify( { version: 2, groups: [], assets: [] } ) );
		}
		if ( text.endsWith( "/asset.bin" ) ) {
			assetFetches++;
			// One chunk, then silence: this download can only die of the
			// no-progress timer, never of completion.
			return new Response(
				new ReadableStream( {
					/*
					================
					start
					================
					*/
					start( controller ) {
						controller.enqueue( Uint8Array.of( 1, 2, 3 ) );
					}
				} )
			);
		}
		return new Response( JSON.stringify( bundle ) );
	} );
	const messages = [];
	const loader = createLoader( message => {
		messages.push( message );
		if ( message.kind === "job-decoding-progress" ) {
			parkedMs += 6000;
			t.mock.timers.setTime( parkedMs );
		}
	} );
	t.after( () => loader.dispose() );
	loader.receive( { kind: "load", id: 1, url: "https://example.test/asset.bin", limit: 1024 } );
	await settle();
	assert.equal( assetFetches, 1 );
	loader.receive( { kind: "load", id: 2, url: "https://example.test/world.json", limit: 16 << 20, decode: "world" } );
	for ( let turn = 0; turn < 500 && !messages.some( m => m.kind === "world" && m.id === 2 ); turn++ ) await settle();
	const notes = messages.filter( m => m.kind === "job-decoding-progress" && m.id === 2 );
	assert.ok( notes.length >= 2, "the decode must report its work while it runs" );
	assert.ok(
		notes.every( ( note, i ) => i === 0 || note.units > notes[i - 1].units ),
		"decode work notes must increase monotonically"
	);
	assert.ok( messages.some( m => m.kind === "world" && m.id === 2 ), "the world load must still complete" );
	// The download's whole silence was the decode's park. Advance to
	// just past the restarted window and tick once: a stall timer that
	// came due during the park (the old behaviour) would have fired on
	// waking and already retried; the restarted one has at most just
	// fired, and no retry has begun.
	t.mock.timers.setTime( 44999 );
	t.mock.timers.tick( 250 );
	await settle();
	assert.equal( assetFetches, 1, "a download stalled only by the decode must not be retried" );
	assert.ok( !messages.some( m => m.kind === "error" && m.id === 1 ) );
	// Real silence after the park still kills the download: its
	// restarted window runs out and the read is abandoned and retried.
	for ( let turn = 0; turn < 40 && assetFetches < 2; turn++ ) {
		t.mock.timers.tick( 60000 );
		await settle();
	}
	assert.equal( assetFetches, 2, "a truly silent download must still be abandoned and retried" );
});
