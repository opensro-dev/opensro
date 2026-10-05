/*
===========================================================================

build-info.test.mjs - the FPS chip's client and server build lines

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { createBuildInfo, formatUptime, shortRevision } = await import(
	"../../src/engine/runtime/build-info/build-info.ts"
);
const REVISION = "0123456789abcdef0123456789abcdef01234567";

/*
================
settle

Lets the fetch promise chain run to its end.
================
*/
async function settle() {
	for ( let i = 0; i < 5; i++ ) await new Promise( resolve => setImmediate( resolve ) );
}

/*
================
withFetch

Runs `body` with a stand-in fetch and performance clock, and restores both.
================
*/
async function withFetch( answer, body ) {
	const fetchBefore = globalThis.fetch, nowBefore = performance.now, requests = [];
	let clock = 0;
	globalThis.fetch = ( url, init ) => {
		requests.push( { url, init } );
		return answer();
	};
	performance.now = () => clock;
	try {
		await body( { requests, advance: ms => clock += ms, now: () => clock } );
	} finally {
		globalThis.fetch = fetchBefore;
		performance.now = nowBefore;
	}
}

test("uptime keeps two units at most", () => {
	assert.equal( formatUptime( 45.9 ), "45s" );
	assert.equal( formatUptime( 725 ), "12m 05s" );
	assert.equal( formatUptime( 3 * 3600 + 7 * 60 + 59 ), "3h 07m" );
	assert.equal( formatUptime( 2 * 86400 + 4 * 3600 ), "2d 04h" );
	assert.equal( formatUptime( -3 ), "0s" );
});

test("only a git revision is shown, abbreviated", () => {
	assert.equal( shortRevision( REVISION ), "0123456" );
	for ( const value of [ "", "main", undefined, 42, "0123456789ABCDEF" ] ) {
		assert.equal( shortRevision( value ), null );
	}
});

test("the server line appears once the Agent answers and its uptime keeps counting", async () => {
	await withFetch(
		() =>
			Promise.resolve(
				new Response( JSON.stringify( { ok: true, build: { revision: REVISION, uptimeSeconds: 60 } } ) )
			),
		async ( { requests, advance, now } ) => {
			const info = createBuildInfo( "/api", "fedcba9876543210" );
			assert.deepEqual( info.lines( now() ), [ "client fedcba9" ] );
			await settle();
			assert.equal( requests.length, 1 );
			assert.equal( requests[0].url, "/api/title/build" );
			assert.equal( requests[0].init.credentials, "omit" );
			advance( 125_000 );
			assert.deepEqual( info.lines( now() ), [ "client fedcba9", "server 0123456 up 3m 05s" ] );
			assert.equal( requests.length, 1, "an answered build is not asked again" );
			info.dispose();
		}
	);
});

test("an unknown build is left out and asked again only after the retry delay", async () => {
	await withFetch( () => Promise.reject( new TypeError( "offline" ) ), async ( { requests, advance, now } ) => {
		const info = createBuildInfo( "/api", undefined );
		assert.deepEqual( info.lines( now() ), [] );
		await settle();
		advance( 1000 );
		assert.deepEqual( info.lines( now() ), [] );
		assert.equal( requests.length, 1 );
		advance( 30_000 );
		info.lines( now() );
		assert.equal( requests.length, 2 );
		info.dispose();
	} );
});

test("an Agent built without a revision stamp shows no server line", async () => {
	await withFetch(
		() =>
			Promise.resolve(
				new Response( JSON.stringify( { ok: true, build: { revision: "", uptimeSeconds: 5 } } ) )
			),
		async ( { now } ) => {
			const info = createBuildInfo( "/api", undefined );
			info.lines( now() );
			await settle();
			assert.deepEqual( info.lines( now() ), [] );
			info.dispose();
		}
	);
});
