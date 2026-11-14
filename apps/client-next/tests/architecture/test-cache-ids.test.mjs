/*
===========================================================================

test-cache-ids.test.mjs - a cache record written in one checkout holds in another

ops #335: absolute input ids kept every fresh worktree cold. A target inside
the checkout is stored relative to it and resolved against the checkout that
validates the record; a target outside it is left alone.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { CHECKOUT_PREFIX, decodeTarget, encodeTarget } from "../../tools/lib/test-cache-ids.mjs";

const HUB = path.resolve( "/sro/rebuild" );
const WORKTREE = path.resolve( "/sro/worktrees/fix-1" );
const SHARED = path.resolve( "/sro/rebuild/.generated/client-public/assets/a.bin" );

test("a target inside the checkout is named relative to it, with forward slashes", () => {
	const id = encodeTarget( WORKTREE, path.join( WORKTREE, "apps", "client-next", "src", "a.ts" ) );
	assert.equal( id, CHECKOUT_PREFIX + "apps/client-next/src/a.ts" );
});

test("the same id resolves inside whichever checkout validates it", () => {
	const id = encodeTarget( HUB, path.join( HUB, "apps", "client-next", "src", "a.ts" ) );
	assert.equal( decodeTarget( WORKTREE, id ), path.join( WORKTREE, "apps", "client-next", "src", "a.ts" ) );
});

test("a target outside the checkout keeps its absolute path both ways", () => {
	assert.equal( encodeTarget( WORKTREE, SHARED ), SHARED );
	assert.equal( decodeTarget( WORKTREE, SHARED ), SHARED );
});

test("a sibling folder that shares the checkout's name prefix is outside it", () => {
	const sibling = path.resolve( "/sro/worktrees/fix-10/a.ts" );
	assert.equal( encodeTarget( WORKTREE, sibling ), sibling );
});

test( "on Windows the checkout is matched without regard to case", { skip: process.platform !== "win32" }, () => {
	const id = encodeTarget( WORKTREE, WORKTREE.toUpperCase() + "\\src\\a.ts" );
	assert.equal( id, CHECKOUT_PREFIX + "src/a.ts" );
} );
