/*
===========================================================================
history.test.mjs - source confinement and explicit collector failures
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHistory } from "../server/history.mjs";

test("history confines requests and credentials to configured loopback sources", async () => {
	const calls = [];
	const owner = createHistory(
		[ { id: "realm", url: "http://127.0.0.1:8791", token: "secret" } ],
		async ( url, options ) => {
			calls.push( { url, options } );
			return Response.json( { events: [] } );
		}
	);
	assert.equal( (await owner.request( new URLSearchParams( { source: "https://attacker.test" } ) )).status, 404 );
	assert.equal( calls.length, 0 );
	assert.equal(
		(await owner.request(
			new URLSearchParams( { source: "realm", session: "boot:42", account: "A&B", url: "http://attacker.test" } )
		)).status,
		200
	);
	assert.equal( calls[0].url.origin, "http://127.0.0.1:8791" );
	assert.equal( calls[0].url.pathname, "/internal/operations/history" );
	assert.equal( calls[0].url.searchParams.get( "account" ), "A&B" );
	assert.equal( calls[0].url.searchParams.has( "url" ), false );
	assert.equal( calls[0].options.redirect, "error" );
	assert.equal( calls[0].options.headers.Authorization, "Bearer secret" );
	assert.throws( () => createHistory( [ { url: "https://external.test" } ] ) );
});

test("missing or failed collectors never appear as empty successful history", async () => {
	const sources = [ { id: "agent", url: "http://127.0.0.1:8787", token: "secret" } ];
	const query = new URLSearchParams( { source: "agent" } );
	for (
		const fetcher of [ async () => {
			throw Error( "connection refused" );
		}, async () => new Response( "not found", { status: 404 } ) ]
	) {
		const result = await createHistory( sources, fetcher ).request( query );
		assert.notEqual( result.status, 200 );
		assert.match( result.body.error, /unavailable/ );
	}
	const result = await createHistory( [ { ...sources[0], token: "" } ] ).request( query );
	assert.equal( result.status, 503 );
});
