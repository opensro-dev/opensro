/*
===========================================================================

loading-detail.test.mjs - tests for loading-detail.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { loadingDetailText, loadingFileLabel, transferRateText, LOADING_STALL_MS } = await import(
	sourceFileUrl( "src/engine/foundation/ui/loading-detail.ts" ).href
);

/*
================
progress
================
*/
function progress( overrides ) {
	return {
		bytesReceived: 4_000_000,
		bytesPerSecond: 1_234_567,
		filesReady: 3,
		filesActive: 1,
		cacheHits: 0,
		currentFile: "https://opensro.online/assets/packs/game-images-004-49d89ab4c651.bin",
		...overrides
	};
}

test("pack files are named by their group in player words", () => {
	assert.equal( loadingFileLabel( "/assets/packs/game-images-004-49d89ab4c651.bin" ), "interface images" );
	assert.equal( loadingFileLabel( "/assets/packs/outdoor/outdoor-world-001-ca0f58a9a930.bin.zst" ), "world terrain" );
	assert.equal(
		loadingFileLabel( "https://opensro.online/assets/packs/game-data-002-46b77966d6a9.bin" ),
		"game data"
	);
	assert.equal( loadingFileLabel( "/assets/packs/new-group-001-abcdef.bin" ), "new group" );
	assert.equal( loadingFileLabel( "/assets/char/europe/europeman_knight.glb" ), "characters" );
	assert.equal( loadingFileLabel( "/api/title/servers" ), "" );
	assert.equal( loadingFileLabel( "" ), "" );
});

test("rates switch from KB/s to MB/s at one megabyte per second", () => {
	assert.equal( transferRateText( 640_400 ), "640 KB/s" );
	assert.equal( transferRateText( 999_999 ), "1000 KB/s" );
	assert.equal( transferRateText( 1_000_000 ), "1.0 MB/s" );
	assert.equal( transferRateText( 1_840_000 ), "1.8 MB/s" );
	assert.equal( transferRateText( 12 ), "1 KB/s" );
});

test("the detail line names the step, what is fetched and how fast", () => {
	assert.equal(
		loadingDetailText( "Loading scene models", progress(), 0 ),
		"Loading scene models · interface images · 1.2 MB/s"
	);
	assert.equal(
		loadingDetailText( "Loading characters...", progress(), 0 ),
		"Loading characters · interface images · 1.2 MB/s"
	);
	assert.equal(
		loadingDetailText( "Loading Silkroad Online…", progress(), 0 ),
		"Loading Silkroad Online · interface images · 1.2 MB/s"
	);
});

test("a transfer with no bytes for the stall window says it is waiting", () => {
	assert.equal(
		loadingDetailText( "Loading world", progress(), LOADING_STALL_MS ),
		"Loading world · interface images · waiting for server"
	);
	assert.equal(
		loadingDetailText( "Loading world", progress(), LOADING_STALL_MS - 1 ),
		"Loading world · interface images · 1.2 MB/s"
	);
});

test("with nothing downloading the line is just the step", () => {
	assert.equal( loadingDetailText( "Loading world", progress( { filesActive: 0 } ), 0 ), "Loading world" );
	assert.equal( loadingDetailText( "Loading world", null, 0 ), "Loading world" );
	assert.equal(
		loadingDetailText( "Loading world", progress( { bytesPerSecond: 0 } ), 0 ),
		"Loading world · interface images"
	);
});
