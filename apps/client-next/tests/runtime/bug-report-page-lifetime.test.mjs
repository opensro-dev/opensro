/*
===========================================================================

bug-report-page-lifetime.test.mjs - the previous page's death certificate

Drives page-lifetime.ts with a fake storage and clock: a page that
dies in the world leaves a marker the next boot turns into
previousPage; a page that ends cleanly leaves nothing; storage that
throws (private mode) never breaks any of it. The last test carries a
dead page's marker through the shared upload's environment filter
(archive.ts), the copy a report actually sends.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readZipEntries } from "../helpers/zip-reader.mjs";

const {
	PAGE_LIFETIME_BEAT_MS,
	beatPageLifetime,
	endPageLifetime,
	startPageLifetime,
	takePreviousPage
} = await import( sourceFileUrl( "src/engine/runtime/bug-report/page-lifetime.ts" ).href );
const { createDiagnosticUpload } = await import( sourceFileUrl( "src/engine/runtime/bug-report/archive.ts" ).href );

const KEY = "sro:page-lifetime:1";
const T0 = 1_700_000_000_000;

/*
================
harness

A fake storage over a Map, a clock the test sets, and the live sample
the marker reads. The module's heartbeat is frame-driven: the tests
offer beats by calling beatPageLifetime with the clock's time, as the
reporter's frame does.
================
*/
function harness() {
	const stored = new Map();
	let nowMs = T0;
	const live = { entities: 3, phase: "entering-world", gpu: null };
	const hooks = {
		storage: {
			getItem: key => stored.get( key ) ?? null,
			setItem: ( key, value ) => stored.set( key, value ),
			removeItem: key => stored.delete( key )
		},
		now: () => nowMs
	};
	return {
		stored,
		live,
		hooks,
		setNow( ms ) {
			nowMs = ms;
		},
		start() {
			startPageLifetime( () => ({ entities: live.entities, phase: live.phase, gpu: live.gpu }), hooks );
		},
		marker() {
			const raw = stored.get( KEY );
			return raw === undefined ? null : JSON.parse( raw );
		}
	};
}

test("a marker left by a killed page becomes the next boot's previousPage", () => {
	const h = harness();
	h.start();
	h.live.entities = 42;
	h.live.phase = "world";
	beatPageLifetime( T0 + 240_000 );
	// The page dies here: no end, no pagehide, the marker survives.
	const previous = takePreviousPage( T0 + 300_000, h.hooks );
	assert.deepEqual( previous, {
		died: true,
		uptimeS: 240,
		lastEntities: 42,
		lastPhase: "world",
		lastGpu: null,
		secondsSinceLastBeat: 60
	} );
	assert.equal( h.stored.has( KEY ), false, "the read cleared the marker" );
	assert.equal( takePreviousPage( T0 + 300_000, h.hooks ), null, "read once: a second read finds nothing" );
});

test("a clean end leaves no marker and no death", () => {
	const h = harness();
	h.start();
	beatPageLifetime( T0 + 90_000 );
	endPageLifetime();
	assert.equal( h.stored.has( KEY ), false, "the marker was removed" );
	assert.equal( takePreviousPage( T0 + 90_000, h.hooks ), null );
});

test("the heartbeat writes on its 30 s boundary, never before", () => {
	const h = harness();
	h.start();
	assert.deepEqual( h.marker(), {
		startedAtMs: T0,
		lastBeatMs: T0,
		entities: 3,
		phase: "entering-world",
		gpu: null
	} );
	h.live.entities = 77;
	h.live.phase = "world";
	beatPageLifetime( T0 + PAGE_LIFETIME_BEAT_MS - 1 );
	assert.equal( h.marker().entities, 3, "a frame inside the boundary writes nothing" );
	beatPageLifetime( T0 + PAGE_LIFETIME_BEAT_MS );
	assert.deepEqual( h.marker(), {
		startedAtMs: T0,
		lastBeatMs: T0 + PAGE_LIFETIME_BEAT_MS,
		entities: 77,
		phase: "world",
		gpu: null
	} );
	endPageLifetime();
});

test("a dead page's last GPU totals ride its death certificate", () => {
	const h = harness();
	h.live.gpu = { buffers: 120, bufferBytes: 40_000_000, textures: 55, textureBytes: 900_000_000 };
	h.start();
	beatPageLifetime( T0 + 120_000 );
	const previous = takePreviousPage( T0 + 150_000, h.hooks );
	assert.deepEqual( previous.lastGpu, {
		buffers: 120,
		bufferBytes: 40_000_000,
		textures: 55,
		textureBytes: 900_000_000
	} );
});

test("a corrupt GPU total reads as none, never as an accusation", () => {
	const h = harness();
	h.stored.set(
		KEY,
		JSON.stringify( {
			startedAtMs: T0,
			lastBeatMs: T0 + 60_000,
			entities: 9,
			phase: "world",
			gpu: { buffers: 3, bufferBytes: -1, textures: 2, textureBytes: 10 }
		} )
	);
	const previous = takePreviousPage( T0 + 60_000, h.hooks );
	assert.equal( previous.lastGpu, null, "a negative figure voids the whole total" );
	assert.equal( previous.lastEntities, 9, "the rest of the certificate stands" );
});

test("a page that enters the world again starts a fresh segment", () => {
	const h = harness();
	h.start();
	h.setNow( T0 + 600_000 );
	h.start();
	assert.equal( h.marker().startedAtMs, T0 + 600_000 );
	endPageLifetime();
});

test("storage that throws never breaks the page", () => {
	const broken = {
		getItem() {
			throw new Error( "denied" );
		},
		setItem() {
			throw new Error( "denied" );
		},
		removeItem() {
			throw new Error( "denied" );
		}
	};
	const hooks = { storage: broken, now: () => T0 };
	startPageLifetime( () => ({ entities: 1, phase: "world" }), hooks );
	beatPageLifetime( T0 + PAGE_LIFETIME_BEAT_MS );
	endPageLifetime();
	assert.equal( takePreviousPage( T0, hooks ), null );
});

test("a corrupt marker is cleared, not inherited", () => {
	const h = harness();
	h.stored.set( KEY, "{not json" );
	assert.equal( takePreviousPage( T0, h.hooks ), null );
	assert.equal( h.stored.has( KEY ), false, "the corrupt marker was cleared" );
	h.stored.set( KEY, JSON.stringify( { startedAtMs: "soon", lastBeatMs: 5 } ) );
	assert.equal( takePreviousPage( T0, h.hooks ), null, "non-numeric stamps accuse nobody" );
	assert.equal( h.stored.has( KEY ), false );
});

test("a dead page's marker rides the shared environment, private fields do not", async () => {
	const files = {
		"environment.json": JSON.stringify( {
			userAgent: "test",
			cpuThreads: 4,
			gpuAllocations: { buffers: 10, bufferBytes: 100, textures: 2, textureBytes: 200 },
			previousPage: {
				died: true,
				uptimeS: 240,
				lastEntities: 42,
				lastPhase: "world",
				lastGpu: { buffers: 120, bufferBytes: 40_000_000, textures: 55, textureBytes: 900_000_000 },
				secondsSinceLastBeat: 60
			},
			preferences: { token: "PRIVATE_STORAGE" }
		} ),
		"timeline.json": "{}",
		"state.json": "{}",
		"movement.json": "{}",
		"network.json": "{}",
		"assets.json": "{}"
	};
	const blob = await createDiagnosticUpload( files, { id: "BR-TEST", clip: null, maxBytes: 2 * 1024 * 1024 } );
	const entries = readZipEntries( await blob.arrayBuffer() );
	const environment = JSON.parse( entries.get( "environment.json" ).data.toString() );
	assert.deepEqual( environment.previousPage, {
		died: true,
		uptimeS: 240,
		lastEntities: 42,
		lastPhase: "world",
		lastGpu: { buffers: 120, bufferBytes: 40_000_000, textures: 55, textureBytes: 900_000_000 },
		secondsSinceLastBeat: 60
	} );
	assert.deepEqual( environment.gpuAllocations, { buffers: 10, bufferBytes: 100, textures: 2, textureBytes: 200 } );
	assert.equal( "preferences" in environment, false );
});
