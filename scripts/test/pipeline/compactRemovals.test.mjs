/*
===========================================================================

compactRemovals.test.mjs - compact bounds what it deletes by the owning roots

A worktree resolves the generated tree and the server projection to the
main checkout, outside the checkout that runs compact. The removal plan
must accept that layout, refuse a projection outside any .generated
folder, and do both before anything is deleted (the compact script calls
it before its first mutation).

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { compactRemovals, compactStatePath, writeCompactState } from "../../build/compactRemovals.mjs";

const MAIN = path.resolve( "/checkouts/main" );
const SHARED = {
	generatedRoot: path.join( MAIN, ".generated" ),
	serverGameDataRoot: path.join( MAIN, "apps", "server", ".generated", "game-data", "1.150", "server" )
};

test("a worktree compacts the main checkout's shared trees", () => {
	const removals = compactRemovals( { ...SHARED, dropGeneratedCache: true } );
	assert.deepEqual( removals.map( removal => removal.target ), [
		path.join( SHARED.generatedRoot, "intermediate" ),
		SHARED.serverGameDataRoot,
		path.join( MAIN, "apps", "server", ".generated", "game-data", "1.150", ".game-data-cache" )
	] );
});

test("the staging cache is removed only when asked", () => {
	const removals = compactRemovals( { ...SHARED, dropGeneratedCache: false } );
	assert.ok( removals.every( removal => !removal.target.endsWith( "intermediate" ) ) );
});

test("a server projection outside any .generated folder is refused", () => {
	assert.throws(
		() => compactRemovals( { ...SHARED, serverGameDataRoot: path.resolve( "/srv/game-data/server" ) } ),
		/must stay below a \.generated folder/
	);
	assert.throws(
		() => compactRemovals( { ...SHARED, serverGameDataRoot: path.join( MAIN, ".generated" ) } ),
		/must stay below a \.generated folder/
	);
});

test("the compact marker lives beside the tree it describes", () => {
	assert.equal( compactStatePath( SHARED.generatedRoot ), path.join( MAIN, ".generated", "compact-assets.json" ) );
});

test("compaction writes and replaces the marker consumed by the release checker", async t => {
	const scratch = await mkdtemp( path.join( os.tmpdir(), "sro-compact-state-" ) );
	t.after( () => rm( scratch, { recursive: true, force: true } ) );
	const generatedRoot = path.join( scratch, "shared-build" );
	const state = {
		format: "sro-compact-assets",
		version: 2,
		packCount: 2,
		publicFiles: [ "/assets/packs/region.pack.zst" ]
	};
	await writeCompactState( generatedRoot, state );
	assert.deepEqual( JSON.parse( await readFile( compactStatePath( generatedRoot ), "utf8" ) ), state );
	const updated = { ...state, packCount: 1, publicFiles: [] };
	await writeCompactState( generatedRoot, updated );
	assert.deepEqual( JSON.parse( await readFile( compactStatePath( generatedRoot ), "utf8" ) ), updated );
	assert.deepEqual( await readdir( generatedRoot ), [ "compact-assets.json" ] );
});
