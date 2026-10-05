/*
===========================================================================

gameRelativePath.test.mjs - built asset paths do not depend on the checkout

A linked worktree names its own files as the main checkout's, so its built
assets match the main checkout's byte for byte, even across drives.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { gameRelativePath } from "../../build/world/paths.mjs";

const game = path.resolve( "/game" ), main = path.join( game, "rebuild" );
const image = [ ".generated", "intermediate", "images", "box.png" ];

test("the main checkout's files are named under it", () => {
	const roots = { checkout: main, mainCheckout: main, game };
	assert.equal(
		gameRelativePath( path.join( main, ...image ), roots ),
		"rebuild/.generated/intermediate/images/box.png"
	);
});

test("a worktree's files are named as the main checkout's", () => {
	const worktree = path.resolve( "/elsewhere/wt-release" );
	const roots = { checkout: worktree, mainCheckout: main, game };
	assert.equal(
		gameRelativePath( path.join( worktree, ...image ), roots ),
		"rebuild/.generated/intermediate/images/box.png"
	);
});

test("game data outside the checkout keeps its own path", () => {
	const roots = { checkout: path.resolve( "/elsewhere/wt-release" ), mainCheckout: main, game };
	assert.equal( gameRelativePath( path.join( game, "extracted", "Map", "a.m" ), roots ), "extracted/Map/a.m" );
});
