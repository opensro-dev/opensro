/*
===========================================================================

generatedRoot.test.mjs - the generated tree's one location rule and its gate

Both built trees are the main checkout's, so a linked worktree reads them
with no environment; SRO_GENERATED_ROOT and SRO_SERVER_GAME_DATA_ROOT move
them, only an absolute path is accepted, and the gate refuses a path to the
tree built anywhere but the owners.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import {
	GENERATED_ROOT_ENV,
	MAIN_CHECKOUT_ROOT,
	resolveGeneratedRoot,
	resolveMainCheckout
} from "../../lib/generatedRoot.mjs";
import { findGeneratedPaths } from "../../checks/check_generated_root.mjs";
import { resolveServerGameDataRoot, SERVER_GAME_DATA_ROOT_ENV } from "../../build/world/paths.mjs";

const repository = fileURLToPath( new URL( "../../../", import.meta.url ) );

test("without an override the tree is the main checkout's .generated", () => {
	assert.equal( resolveGeneratedRoot( {} ), path.join( MAIN_CHECKOUT_ROOT, ".generated" ) );
	assert.equal( MAIN_CHECKOUT_ROOT, resolveMainCheckout( repository ) );
});

test("a linked worktree resolves both built trees to the main checkout's", () => {
	const scratch = fs.mkdtempSync( path.join( os.tmpdir(), "sro-worktree-" ) );
	try {
		const main = path.join( scratch, "main" ), worktree = path.join( scratch, "wt" );
		fs.mkdirSync( path.join( main, ".git", "worktrees", "wt" ), { recursive: true } );
		fs.mkdirSync( worktree );
		fs.writeFileSync( path.join( worktree, ".git" ), `gitdir: ${path.join( main, ".git", "worktrees", "wt" )}` );
		assert.equal( resolveMainCheckout( worktree ), main );
		assert.equal( resolveMainCheckout( main ), main );
		assert.equal( resolveGeneratedRoot( {}, resolveMainCheckout( worktree ) ), path.join( main, ".generated" ) );
		assert.equal(
			resolveServerGameDataRoot( {}, resolveMainCheckout( worktree ) ),
			path.join( main, "apps", "server", ".generated", "game-data", "1.150", "server" )
		);
		fs.writeFileSync( path.join( worktree, ".git" ), "not a link" );
		assert.throws( () => resolveMainCheckout( worktree ), /Unreadable worktree link/ );
	} finally {
		fs.rmSync( scratch, { recursive: true, force: true } );
	}
});

test("an absolute override moves the whole tree", () => {
	const shared = path.resolve( repository, "..", "shared-build", ".generated" );
	assert.equal( resolveGeneratedRoot( { [GENERATED_ROOT_ENV]: shared } ), shared );
});

test("a relative override is refused instead of following the working directory", () => {
	assert.throws( () => resolveGeneratedRoot( { [GENERATED_ROOT_ENV]: "../main/.generated" } ), /absolute/ );
});

test("the gate finds every hand-built path to the tree and ignores prose and other trees", () => {
	const flagged = [
		'const root = path.resolve( "../../.generated/client-public" );',
		"readFile( '.generated/client-public/assets/packs/manifest.json' )",
		"new URL( '../../../.generated/intermediate/images/a.png', import.meta.url )",
		'const publicRoot = path.join( rebuildRoot, ".generated", "client-public" );',
		'root := filepath.Join("..", "..", ".generated", "client-public")',
		'OUT_DIR = REPO_ROOT / ".generated" / "client-public" / "assets"',
		'const catalog = ".generated/observatory/items.json";'
	];
	for ( const line of flagged ) {
		assert.equal( findGeneratedPaths( line ).length, 1, line );
	}
	const ignored = [
		"// reads .generated/client-public/assets via the owner",
		" * Needs the full asset build (.generated/client-public).",
		"# OUTPUT lives under .generated/intermediate",
		'projection = filepath.Join(repository, "apps", "server", ".generated", "game-data")',
		'raw, err := os.ReadFile(filepath.Join("..", "item", "loot", ".generated", "equipment.json"))',
		"index.generatedAt = previous.generatedAt;",
		'const root = CLIENT_PUBLIC_ROOT + "/assets/packs/manifest.json";'
	];
	for ( const line of ignored ) {
		assert.deepEqual( findGeneratedPaths( line ), [], line );
	}
});

test("the server game-data projection follows SRO_SERVER_GAME_DATA_ROOT, as the Go server does", () => {
	assert.equal(
		resolveServerGameDataRoot( {} ),
		path.join( MAIN_CHECKOUT_ROOT, "apps", "server", ".generated", "game-data", "1.150", "server" )
	);
	const shared = path.resolve(
		repository,
		"..",
		"main",
		"apps",
		"server",
		".generated",
		"game-data",
		"1.150",
		"server"
	);
	assert.equal( resolveServerGameDataRoot( { [SERVER_GAME_DATA_ROOT_ENV]: shared } ), shared );
	assert.throws( () => resolveServerGameDataRoot( { [SERVER_GAME_DATA_ROOT_ENV]: "../main/server" } ), /absolute/ );
});
