/*
===========================================================================
settings.test.mjs - shard rules reads stay confined, validated and read-only
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createSettings, settingsBody } from "../server/settings.mjs";

const SHARDS = [ { id: "realm", url: "http://127.0.0.1:8791/internal/diagnostics/observatory" } ];
// One contract v1 row as platform/settingsapi writes it.
const ROW = {
	key: "job_kill_notices_per_minute",
	env: "SRO_JOB_KILL_NOTICES_PER_MINUTE",
	group: "Notices",
	label: "Job-kill notices per minute",
	help: "Job-kill notices a division may post a minute.",
	kind: "int",
	unit: "count",
	value: 6,
	enabled: true,
	default: 6,
	native: null,
	min: 1,
	max: 30,
	allowed: null,
	step: 1,
	class: "live",
	source: "env"
};
const BODY = { schemaVersion: 1, settings: [ ROW ], flags: [ { env: "SRO_MOST_WANTED", value: "on", on: true } ] };

/*
================
recording
================
*/
function recording( answer = () => Response.json( BODY ) ) {
	const calls = [];
	return {
		calls,
		fetcher: async ( url, options ) => {
			calls.push( { url, options } );
			return answer( url );
		}
	};
}

test("the one read goes to the shard's own listener with the token server-side", async () => {
	const { calls, fetcher } = recording();
	const settings = createSettings( SHARDS, "secret", fetcher );
	const ok = await settings.request( new URLSearchParams( { shard: "realm", url: "http://attacker.test" } ) );
	assert.equal( ok.status, 200 );
	assert.deepEqual( ok.body, BODY );
	assert.equal( calls[0].url.origin, "http://127.0.0.1:8791" );
	assert.equal( calls[0].url.pathname, "/internal/operations/settings/v1" );
	assert.equal( calls[0].url.search, "", "no browser parameter is forwarded" );
	assert.equal( calls[0].options.headers.Authorization, "Bearer secret" );
	assert.equal( calls[0].options.headers["X-SRO-Local-Diagnostics"], "1" );
	assert.equal( calls[0].options.redirect, "error" );
	for ( const shard of [ "https://attacker.test", "", "other" ] ) {
		assert.equal( (await settings.request( new URLSearchParams( { shard } ) )).status, 404, shard );
	}
	assert.equal( calls.length, 1, "nothing else reached a GameWorld" );
});

test("without the operator token nothing is forwarded", async () => {
	const { calls, fetcher } = recording();
	const settings = createSettings( SHARDS, "", fetcher );
	assert.equal( settings.enabled, false );
	assert.equal( (await settings.request( new URLSearchParams( { shard: "realm" } ) )).status, 503 );
	assert.equal( calls.length, 0 );
});

test("a GameWorld that is not v1, refuses, or fails is a plain error", async () => {
	for (
		const answer of [
			() => Response.json( { ...BODY, schemaVersion: 2 } ),
			() => Response.json( { schemaVersion: 1, settings: {}, flags: [] } ),
			() => new Response( "not json" ),
			() => new Response( "", { status: 403 } )
		]
	) {
		const { fetcher } = recording( answer );
		const result = await createSettings( SHARDS, "secret", fetcher ).request(
			new URLSearchParams( { shard: "realm" } )
		);
		assert.equal( result.status, 502 );
		assert.equal( typeof result.body.error, "string" );
	}
	const down = createSettings( SHARDS, "secret", async () => {
		throw Error( "connect ECONNREFUSED" );
	} );
	assert.equal( (await down.request( new URLSearchParams( { shard: "realm" } ) )).status, 503 );
});

test("only the contract's fields pass, each of its type", () => {
	const body = settingsBody( {
		schemaVersion: 1,
		extra: "dropped",
		settings: [
			{ ...ROW, label: "<img src=x onerror=alert(1)>", script: "dropped", help: "x".repeat( 5000 ) },
			{ ...ROW, key: "Bad Key" },
			{ ...ROW, env: "PATH" },
			{ ...ROW, value: "6" },
			{ ...ROW, kind: "string" },
			{ ...ROW, class: "now" },
			{ ...ROW, allowed: [ 1, "2", 5, null ], source: "elsewhere" },
			{ ...ROW, class: "entry" }
		],
		flags: [ { env: "SRO_X", value: 7, on: "yes" }, { env: "HOME", value: "/root", on: null } ]
	} );
	assert.equal( body.extra, undefined );
	assert.equal( body.settings.length, 3, "rows with a wrong required field are dropped" );
	assert.equal( body.settings[2].class, "entry", "an entry-class row passes" );
	assert.equal( body.settings[0].label, "<img src=x onerror=alert(1)>", "text stays text; the page never parses it" );
	assert.equal( body.settings[0].script, undefined );
	assert.equal( body.settings[0].help.length, 512 );
	assert.deepEqual( body.settings[1].allowed, [ 1, 5 ] );
	assert.equal( body.settings[1].source, "default" );
	assert.deepEqual( body.flags, [ { env: "SRO_X", value: "", on: null } ], "only SRO_ variables, typed" );
});

test("no staff identity from the browser ever reaches a GameWorld", async () => {
	// The GameWorld's two-person rule trusts X-OpenSRO-Staff; only the
	// console's forward_auth identity may set it, never a browser. The read
	// forwards none: its headers are the token and the diagnostics marker
	// only, and nothing the browser sends (query or header) is copied.
	const { calls, fetcher } = recording();
	const settings = createSettings( SHARDS, "secret", fetcher );
	await settings.request(
		new URLSearchParams( { shard: "realm", "X-OpenSRO-Staff": "discord:1", "x-opensro-staff-name": "Mallory" } )
	);
	assert.deepEqual( Object.keys( calls[0].options.headers ).sort(), [ "Authorization", "X-SRO-Local-Diagnostics" ] );
	assert.equal( calls[0].url.search, "" );
	for ( const value of Object.values( calls[0].options.headers ) ) assert.doesNotMatch( value, /discord|Mallory/ );
});
