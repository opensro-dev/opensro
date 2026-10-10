/*
===========================================================================
player-operations.test.mjs - edge identity, CSRF and upstream confinement
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { allowedRequest, allowedMutation } from "../server/security.mjs";
import { createPlayerOperations } from "../server/player-operations.mjs";

test("remote console requires the edge secret, identity and exact origin", () => {
	const edge = { origin: "https://console.example", operator: "operator", secret: "s".repeat( 48 ) };
	const req = {
		socket: { remoteAddress: "127.0.0.1" },
		headers: {
			host: "console.example",
			origin: edge.origin,
			"x-sro-operator": "operator",
			"x-sro-console-auth": edge.secret,
			"x-sro-console": "1",
			"content-type": "application/json"
		}
	};
	assert.equal( allowedRequest( req, 5190, edge ), true );
	assert.equal( allowedMutation( req, 5190, edge ), true );
	for (
		const change of [
			{ "x-sro-console-auth": "forged" },
			{ "x-sro-operator": "other" },
			{ host: "attacker.example" },
			{ origin: "https://attacker.example" },
			{ "sec-fetch-site": "cross-site" }
		]
	) assert.equal( allowedRequest( { ...req, headers: { ...req.headers, ...change } }, 5190, edge ), false );
	assert.equal( allowedRequest( { ...req, socket: { remoteAddress: "192.0.2.1" } }, 5190, edge ), false );
	for (
		const change of [ { origin: undefined }, { "x-sro-console": undefined }, { "content-type": "text/plain" } ]
	) {
		assert.equal( allowedMutation( { ...req, headers: { ...req.headers, ...change } }, 5190, edge ), false );
	}
});

test("player requests cannot choose an upstream and preserve authority refusals", async () => {
	const calls = [];
	const owner = createPlayerOperations(
		[ { id: "realm", url: "http://127.0.0.1:8791/internal/diagnostics/observatory" } ],
		"test-secret",
		async ( url, options ) => {
			calls.push( { url, options } );
			return new Response( "unknown rescue town", { status: 409 } );
		}
	);
	assert.equal( (await owner.request( "https://attacker.invalid", "Viper" )).status, 404 );
	assert.equal( calls.length, 0 );
	const result = await owner.request( "realm", "Viper & extra", undefined );
	assert.equal( result.status, 409 );
	assert.equal( result.body.error, "unknown rescue town" );
	assert.equal( calls[0].url.origin, "http://127.0.0.1:8791" );
	assert.equal( calls[0].url.searchParams.get( "character" ), "Viper & extra" );
	assert.equal( calls[0].options.redirect, "error" );
});

/*
================
Unavailable shard operations remain an explicit refusal, never a successful rescue.
================
*/
test("an older GameWorld gives an actionable unavailable message", async () => {
	const owner = createPlayerOperations(
		[ { id: "realm", url: "http://127.0.0.1:8791" } ],
		"test-token",
		async () => new Response( "404 page not found\n", { status: 404 } )
	);
	const result = await owner.request( "realm", "Viper" );
	assert.equal( result.status, 503 );
	assert.match( result.body.error, /not enabled/ );
});

test("clear-PK forwards the authenticated audit envelope without rescue or item fields", async () => {
	const calls = [];
	const command = {
		id: "request-42",
		operator: "audited-operator",
		character: "Viper",
		reason: "Correct PK record",
		action: "clear-pk"
	};
	const after = { shard: "realm", player: { name: "Viper", pk: { penalty: 0, dailyCount: 2, totalCount: 9 } } };
	const owner = createPlayerOperations(
		[ { id: "realm", url: "http://127.0.0.1:8791" } ],
		"test-token",
		async ( url, options ) => {
			calls.push( { url, options } );
			return Response.json( after );
		}
	);
	assert.deepEqual( await owner.request( "realm", null, command ), { status: 200, body: after } );
	assert.equal( calls.length, 1 );
	assert.equal( calls[0].url.pathname, "/internal/operations/player" );
	assert.equal( calls[0].options.method, "POST" );
	assert.equal( calls[0].options.headers.Authorization, "Bearer test-token" );
	assert.deepEqual( JSON.parse( calls[0].options.body ), command );
	const disabled = createPlayerOperations( [], "", () => assert.fail( "disabled gateway must not contact a shard" ) );
	assert.equal( (await disabled.request( "realm", null, command )).status, 503 );
});

test("reset-stats forwards the authenticated audit envelope unchanged", async () => {
	const calls = [];
	const command = {
		id: "request-43",
		operator: "audited-operator",
		character: "Viper",
		reason: "Player asked for a stat reset",
		action: "reset-stats"
	};
	const after = { shard: "realm", player: { name: "Viper", strength: 109, intellect: 109, statPoints: 267 } };
	const owner = createPlayerOperations(
		[ { id: "realm", url: "http://127.0.0.1:8791" } ],
		"test-token",
		async ( url, options ) => {
			calls.push( { url, options } );
			return Response.json( after );
		}
	);
	assert.deepEqual( await owner.request( "realm", null, command ), { status: 200, body: after } );
	assert.equal( calls.length, 1 );
	assert.equal( calls[0].url.pathname, "/internal/operations/player" );
	assert.equal( calls[0].options.headers.Authorization, "Bearer test-token" );
	assert.deepEqual( JSON.parse( calls[0].options.body ), command );
});
