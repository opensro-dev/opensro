/*
===========================================================================

pack-block-cache.test.mjs - the pack reader keeps few blocks in memory

A block only serves the members beside the one that fetched it, and every
demanded member is verified into the persistent store, so the block cache
(packs/blocks.ts) holds 16 one-MiB blocks, not a scene's worth: on a phone
that memory counts against the tab. Past it the oldest block is dropped
and a later read of it downloads again.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const { createPackBlocks } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/assets/worker/packs/blocks.ts" ) ).href
);

const MIB = 1 << 20;
const DATA_START = 12;

/*
================
largePack

A pack of count members of one MiB each: every member is its own block.
================
*/
function largePack( count ) {
	const entries = Array.from( { length: count }, ( _, i ) => ({
		publicPath: `/assets/m${i}.bin`,
		path: `m${i}.bin`,
		packPath: "large.pack",
		offset: i * MIB,
		length: MIB,
		span: MIB,
		mime: "application/octet-stream",
		sha256: `member-${i}`
	}) );
	return { path: "/packs/large.pack", bytes: DATA_START + count * MIB, sha256: "large", assetCount: count, entries };
}

test("the block cache keeps the last 16 blocks and downloads an older one again", async () => {
	const downloads = [];
	const blocks = createPackBlocks( async ( url, limit, signal, range ) => {
		downloads.push( range.start );
		return new Uint8Array( range.end - range.start + 1 );
	}, new AbortController().signal );
	const pack = largePack( 40 );
	for ( const entry of pack.entries ) await blocks.read( "https://assets.test", pack, DATA_START, entry );
	assert.equal( downloads.length, 40 );
	for ( const entry of pack.entries.slice( -16 ) ) {
		await blocks.read( "https://assets.test", pack, DATA_START, entry );
	}
	assert.equal( downloads.length, 40, "the 16 most recent blocks are still in memory" );
	await blocks.read( "https://assets.test", pack, DATA_START, pack.entries[0] );
	assert.equal( downloads.length, 41, "an older block was let go and is read again" );
	blocks.dispose();
});
