/*
===========================================================================

build-tag.test.mjs - the build a client incident is grouped by

A served bundle reports its file name; a module run from a blob: or data:
URL reports one shared tag instead of the blob's per-load UUID, which made
each such report a group of its own.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { buildTag, DEVELOPMENT_BUILD, EMBEDDED_BUILD } from "../../src/engine/foundation/release/build-tag.ts";

test("a served bundle reports its file name", () => {
	assert.equal( buildTag( "https://play.example/assets/entry-CKV5iG0Q.js" ), "entry-CKV5iG0Q.js" );
	assert.equal( buildTag( "http://localhost:5173/src/engine/runtime/session.ts?worker_file" ), "session.ts" );
});

test("a blob or data module reports the embedded tag, never the blob's UUID", () => {
	const blob = "blob:https://play.example/3f2a1c9e-1111-4222-8333-123456789abc";
	assert.equal( buildTag( blob ), EMBEDDED_BUILD );
	assert.equal( buildTag( blob ), buildTag( "blob:https://play.example/00000000-0000-4000-8000-000000000000" ) );
	assert.equal( buildTag( "data:text/javascript,export{}" ), EMBEDDED_BUILD );
});

test("a URL with no file name reports development and a long one is cut to the Agent's limit", () => {
	assert.equal( buildTag( "https://play.example/" ), DEVELOPMENT_BUILD );
	assert.equal( buildTag( "not a url" ), DEVELOPMENT_BUILD );
	assert.equal( buildTag( `https://play.example/${"a".repeat( 80 )}.js` ).length, 64 );
});
