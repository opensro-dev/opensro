/*
===========================================================================
economy.test.mjs - economy reads stay confined, validated and read-only
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createEconomy, economyQuery, economyRoute } from "../server/economy.mjs";

const SHARDS = [ { id: "realm", url: "http://127.0.0.1:8791/internal/diagnostics/observatory" } ];

/*
================
recording
================
*/
function recording( answer = () => Response.json( { contract: 1 } ) ) {
	const calls = [];
	return {
		calls,
		fetcher: async ( url, options ) => {
			calls.push( { url, options } );
			return answer( url );
		}
	};
}

test("only the contract routes are forwarded, to the shard's own loopback listener, with the token server-side", async () => {
	const { calls, fetcher } = recording();
	const economy = createEconomy( SHARDS, "secret", fetcher );
	const ok = await economy.request(
		"/api/economy/series",
		new URLSearchParams( {
			shard: "realm",
			metric: "reason:npc_sale",
			bucket: "1h",
			from: "0",
			to: "3600",
			url: "http://attacker.test",
			character: "x"
		} )
	);
	assert.equal( ok.status, 200 );
	assert.equal( calls[0].url.origin, "http://127.0.0.1:8791" );
	assert.equal( calls[0].url.pathname, "/economy/v1/series" );
	assert.equal( calls[0].url.searchParams.get( "metric" ), "reason:npc_sale" );
	assert.equal( calls[0].url.searchParams.has( "url" ), false, "unknown parameters are dropped" );
	assert.equal( calls[0].url.searchParams.has( "character" ), false );
	assert.equal( calls[0].url.searchParams.has( "shard" ), false );
	assert.equal( calls[0].options.headers.Authorization, "Bearer secret" );
	assert.equal( calls[0].options.redirect, "error" );
	for ( const path of [ "/api/economy/rollback", "/api/economy/../player", "/api/economy/series/x" ] ) {
		assert.equal( (await economy.request( path, new URLSearchParams( { shard: "realm" } ) )).status, 404, path );
	}
	assert.equal(
		(await economy.request( "/api/economy/meta", new URLSearchParams( { shard: "https://attacker.test" } ) ))
			.status,
		404
	);
	assert.equal( calls.length, 1, "nothing else reached a GameWorld" );
});

test("bad metrics, buckets, times and over-wide ranges are refused before any GameWorld call", async () => {
	const { calls, fetcher } = recording();
	const economy = createEconomy( SHARDS, "secret", fetcher );
	const refused = [
		{ metric: "gold_minted; drop", bucket: "1h" },
		{ metric: "gold_minted", bucket: "5m" },
		{ metric: "gold_minted", bucket: "1h", from: "yesterday" },
		{ metric: "gold_minted", bucket: "1h", from: "0", to: String( 721 * 3600 ) },
		{ metric: "gold_minted", bucket: "1h", from: "7200", to: "0" },
		{ bucket: "1h" },
		{ metric: "gold_minted", division: "../x" }
	];
	for ( const params of refused ) {
		const result = await economy.request(
			"/api/economy/series",
			new URLSearchParams( { shard: "realm", ...params } )
		);
		assert.equal( result.status, 400, JSON.stringify( params ) );
	}
	assert.equal( calls.length, 0 );
	assert.ok(
		economyQuery(
			"series",
			new URLSearchParams( { metric: "gold_net", bucket: "1h", from: "0", to: String( 720 * 3600 ) } )
		).query
	);
	assert.equal( economyRoute( "/api/economy/flags" ), "flags" );
	assert.equal( economyRoute( "/api/economy/constructor" ), null );
});

test("killstats forwards only from/to; ranges the contract refuses never reach a GameWorld", async () => {
	const { calls, fetcher } = recording();
	const economy = createEconomy( SHARDS, "secret", fetcher );
	const ok = await economy.request(
		"/api/economy/killstats",
		new URLSearchParams( {
			shard: "realm",
			from: "1699900000",
			to: "1700500000",
			metric: "gold_minted",
			bucket: "1h",
			division: "x",
			character: "x"
		} )
	);
	assert.equal( ok.status, 200 );
	assert.equal( calls[0].url.pathname, "/economy/v1/killstats" );
	assert.equal( calls[0].url.searchParams.get( "from" ), "1699900000" );
	assert.equal( calls[0].url.searchParams.get( "to" ), "1700500000" );
	for ( const key of [ "metric", "bucket", "division", "character", "shard" ] ) {
		assert.equal( calls[0].url.searchParams.has( key ), false, key + " is dropped" );
	}
	// No range at all forwards bare: the GameWorld serves its default window.
	const bare = await economy.request( "/api/economy/killstats", new URLSearchParams( { shard: "realm" } ) );
	assert.equal( bare.status, 200 );
	assert.equal( calls[1].url.search, "" );
	const refused = [
		{ from: "yesterday", to: "1700500000" },
		{ from: "1700500000", to: "1699900000" },
		// 367 whole UTC days: the contract's range_too_wide, refused here.
		{ from: "0", to: String( 366 * 86400 ) },
		// 366.5 days of seconds still floors to 367 whole days.
		{ from: "0", to: String( 366 * 86400 + 43200 ) }
	];
	for ( const params of refused ) {
		const result = await economy.request(
			"/api/economy/killstats",
			new URLSearchParams( { shard: "realm", ...params } )
		);
		assert.equal( result.status, 400, JSON.stringify( params ) );
	}
	assert.equal( calls.length, 2, "refusals never reached a GameWorld" );
	// Exactly 366 whole days is the contract's widest allowed range.
	assert.ok(
		economyQuery( "killstats", new URLSearchParams( { from: "0", to: String( 365 * 86400 ) } ) ).query
	);
	// An unaligned pair spanning one extra day floors to the same 366 days.
	assert.ok(
		economyQuery(
			"killstats",
			new URLSearchParams( { from: "43200", to: String( 366 * 86400 - 1 ) } )
		).query
	);
	assert.equal( economyRoute( "/api/economy/killstats" ), "killstats" );
});

test("a killstats range the GameWorld refuses keeps its contract code", async () => {
	const economy = createEconomy(
		SHARDS,
		"secret",
		recording( () =>
			Response.json(
				{ error: { code: "range_too_wide", message: "range holds more than 366 days" } },
				{ status: 400 }
			)
		).fetcher
	);
	const result = await economy.request(
		"/api/economy/killstats",
		new URLSearchParams( { shard: "realm", from: "0", to: "100" } )
	);
	assert.equal( result.status, 400 );
	assert.deepEqual( result.body, { error: "range holds more than 366 days", code: "range_too_wide" } );
});

test("an unconfigured console, contract errors and dead GameWorlds never look like empty data", async () => {
	const off = createEconomy( SHARDS, "", recording().fetcher );
	assert.equal( off.enabled, false );
	assert.equal( (await off.request( "/api/economy/meta", new URLSearchParams( { shard: "realm" } ) )).status, 503 );
	const snapshotless = createEconomy(
		SHARDS,
		"secret",
		recording( () =>
			Response.json( { error: { code: "no_snapshot", message: "No snapshot yet" } }, { status: 409 } )
		).fetcher
	);
	const result = await snapshotless.request( "/api/economy/distribution", new URLSearchParams( { shard: "realm" } ) );
	assert.equal( result.status, 409 );
	assert.deepEqual( result.body, { error: "No snapshot yet", code: "no_snapshot" } );
	const dead = createEconomy( SHARDS, "secret", async () => {
		throw Error( "connection refused" );
	} );
	const failure = await dead.request( "/api/economy/meta", new URLSearchParams( { shard: "realm" } ) );
	assert.equal( failure.status, 503 );
	assert.match( failure.body.error, /unavailable/ );
	const garbage = createEconomy( SHARDS, "secret", recording( () => new Response( "<html>" ) ).fetcher );
	assert.equal(
		(await garbage.request( "/api/economy/meta", new URLSearchParams( { shard: "realm" } ) )).status,
		502
	);
});
