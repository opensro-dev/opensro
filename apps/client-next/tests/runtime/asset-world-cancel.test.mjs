/*
===========================================================================

asset-world-cancel.test.mjs - a cancel posted during a world decode settles

A synchronous world decode parks the asset worker's loop for its whole
length, so a cancel posted to the worker during the decode is not even
received until the decode ends: on a slow device the main thread's 60 s
cancellation window expires on a healthy worker
("asset-cancel-unacknowledged", region-6a6c). The decode must yield to
the message loop as it works and abandon the job when the job was
cancelled meanwhile: the worker acknowledges at once, delivers no world
for the cancelled id, and the freed slot admits the next load.

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

// Big enough to span many decode work reports: the cancel below lands
// while the decode is still running, not after it.
const PLACEMENTS = 20000;

test("a world load cancelled mid-decode is acknowledged and abandoned, and its slot admits the next load", async t => {
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: { blocks: [] },
		terrainTextures: { tileCatalog: { referencedTiles: [] } },
		objects: {
			placements: Array.from( { length: PLACEMENTS }, ( _, i ) => ({
				objectId: 1,
				uid: i,
				regionId: "257",
				position: { x: i % 97, y: 0, z: Math.floor( i / 97 ) },
				yaw: 0
			}) ),
			resources: { meshes: [], bsr: [], materialSets: [] }
		}
	};
	t.mock.method( globalThis, "fetch", async url => {
		const text = String( url );
		if ( text.endsWith( "/assets/packs/manifest.json" ) ) {
			return new Response( JSON.stringify( { version: 2, groups: [], assets: [] } ) );
		}
		if ( text.endsWith( "/asset.bin" ) ) return new Response( Uint8Array.of( 4, 5, 6 ) );
		return new Response( JSON.stringify( bundle ) );
	} );
	const messages = [];
	const loader = createLoader( message => messages.push( message ) );
	t.after( () => loader.dispose() );
	loader.receive( { kind: "load", id: 2, url: "https://example.test/world.json", limit: 16 << 20, decode: "world" } );
	// Wait until the decode is under way (it has reported work), then
	// cancel it the way a region crossing cancels a terrain part.
	for (
		let turn = 0;
		turn < 500 && !messages.some( m => m.kind === "job-decoding-progress" && m.id === 2 );
		turn++
	) await settle();
	assert.ok(
		messages.some( m => m.kind === "job-decoding-progress" && m.id === 2 ),
		"the world decode must be under way before the cancel"
	);
	loader.receive( { kind: "cancel", id: 2 } );
	for (
		let turn = 0;
		turn < 500 && !messages.some( m => m.kind === "released" && m.id === 2 );
		turn++
	) await settle();
	assert.ok(
		messages.some( m => m.kind === "released" && m.id === 2 ),
		"the worker must acknowledge the cancel while the decode would still be running"
	);
	// Give the abandoned execution every chance to (wrongly) deliver.
	for ( let turn = 0; turn < 20; turn++ ) await settle();
	assert.ok(
		!messages.some( m => (m.kind === "world" || m.kind === "error") && m.id === 2 ),
		"a cancelled world load delivers no result"
	);
	const lastUnits = messages.filter( m => m.kind === "job-decoding-progress" && m.id === 2 ).at( -1 )?.units ?? 0;
	assert.ok(
		lastUnits < PLACEMENTS,
		`the decode must be abandoned on cancel, not run to completion (reported ${lastUnits} of ${PLACEMENTS})`
	);
	// The settled execution freed its slot: the next load completes.
	loader.receive( { kind: "load", id: 3, url: "https://example.test/asset.bin", limit: 1024 } );
	for (
		let turn = 0;
		turn < 500 && !messages.some( m => m.kind === "bytes" && m.id === 3 );
		turn++
	) await settle();
	assert.ok( messages.some( m => m.kind === "bytes" && m.id === 3 ), "the next load completes after the cancel" );
});
