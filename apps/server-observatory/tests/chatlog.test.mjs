/*
===========================================================================
chatlog.test.mjs - the chat log read stays confined to its shard's route
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createChatLog } from "../server/chatlog.mjs";
import { searchURL } from "../public/chat-log.js";

const SHARD = { id: "realm", url: "http://127.0.0.1:8791", token: "secret" };

test("a search reaches only the named shard's route, with only the known filters", async () => {
	const calls = [];
	const owner = createChatLog( [ SHARD ], async ( url, options ) => {
		calls.push( { url, options } );
		return Response.json( { lines: [], truncated: false, skipped: 0 } );
	} );
	assert.equal( (await owner.request( new URLSearchParams( { source: "https://attacker.test" } ) )).status, 404 );
	assert.equal( calls.length, 0 );
	const result = await owner.request(
		new URLSearchParams( { source: "realm", character: "A&B", from: "1", q: "x", url: "http://attacker.test" } )
	);
	assert.equal( result.status, 200 );
	assert.equal( calls[0].url.origin, SHARD.url );
	assert.equal( calls[0].url.pathname, "/internal/operations/chatlog" );
	assert.equal( calls[0].url.searchParams.get( "character" ), "A&B" );
	assert.deepEqual( [ ...calls[0].url.searchParams.keys() ], [ "character", "from" ] );
	assert.equal( calls[0].options.redirect, "error" );
	assert.equal( calls[0].options.headers.Authorization, "Bearer secret" );
});

test("a shard outside loopback is refused at startup", () => {
	assert.throws( () => createChatLog( [ { id: "far", url: "https://external.test" } ] ) );
});

test("no token, an oversized filter and a dead shard are errors, never an empty log", async () => {
	const owner = createChatLog( [ { ...SHARD, token: "" } ], async () => assert.fail( "must not call out" ) );
	assert.equal( (await owner.request( new URLSearchParams( { source: "realm" } ) )).status, 503 );
	const fetched = createChatLog( [ SHARD ], async () => {
		throw Error( "refused" );
	} );
	const dead = await fetched.request( new URLSearchParams( { source: "realm" } ) );
	assert.equal( dead.status, 503 );
	assert.match( dead.body.error, /Chat log unavailable/ );
	const long = await fetched.request( new URLSearchParams( { source: "realm", account: "a".repeat( 257 ) } ) );
	assert.equal( long.status, 400 );
});

test("the page sends only what was filled in, with times as Unix milliseconds", () => {
	const blank = new URL(
		searchURL( { source: "realm", account: " ", character: "", from: "", to: "" } ),
		"http://x"
	);
	assert.deepEqual( [ ...blank.searchParams.keys() ], [ "source" ] );
	const from = "2026-10-31T12:30";
	const full = new URL(
		searchURL( { source: "realm", account: "", character: " Alpha ", from, to: "" } ),
		"http://x"
	);
	assert.equal( full.searchParams.get( "character" ), "Alpha" );
	assert.equal( full.searchParams.get( "from" ), String( new Date( from ).getTime() ) );
});
