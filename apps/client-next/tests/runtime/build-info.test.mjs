/*
===========================================================================

build-info.test.mjs - the FPS chip's client and server build lines

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { commitSubject, createBuildInfo, formatUptime, shortRevision } = await import(
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

test("commit subjects are one line and bounded", () => {
	assert.equal( commitSubject( "Fix  the\nchip " ), "Fix the chip" );
	assert.equal( commitSubject( "x".repeat( 300 ) ).length, 200 );
	for ( const value of [ undefined, null, 7 ] ) assert.equal( commitSubject( value ), "" );
});

test("refresh replaces a deployed Agent and a same-build restart resets uptime", async () => {
	let build = { revision: REVISION, subject: "First Agent", uptimeSeconds: 600 };
	await withFetch(
		() => Promise.resolve( new Response( JSON.stringify( { build } ) ) ),
		async ( { requests, advance, now } ) => {
			const info = createBuildInfo( "/api", "fedcba9876543210", "Client change" );
			info.readout( now() );
			await settle();
			assert.match( info.readout( now() ).lines[1], /Agent 0123456 · uptime 10m/ );
			assert.equal( requests[0].init.credentials, "omit" );
			build = { revision: "abcdef0123456789", subject: "New Agent", uptimeSeconds: 2 };
			advance( 60000 );
			info.readout( now() );
			await settle();
			assert.match( info.readout( now() ).lines[1], /Agent abcdef0 · uptime 2s/ );
			assert.match( info.readout( now() ).detail, /New Agent/ );
			build.uptimeSeconds = 1;
			advance( 60000 );
			info.readout( now() );
			await settle();
			assert.match( info.readout( now() ).lines[1], /uptime 1s/ );
			assert.equal( requests.length, 3 );
			info.dispose();
		}
	);
});

test("failure marks retained information stale, throttles retries and recovers", async () => {
	let failed = false;
	await withFetch(
		() =>
			failed ?
				Promise.reject( new Error( "offline" ) ) :
				Promise.resolve(
					new Response( JSON.stringify( { build: { revision: REVISION, uptimeSeconds: 20 } } ) )
				),
		async ( { requests, advance, now } ) => {
			const info = createBuildInfo( "/api", undefined, undefined );
			info.readout( now() );
			await settle();
			failed = true;
			advance( 60000 );
			info.readout( now() );
			await settle();
			assert.match( info.readout( now() ).lines[1], /stale/ );
			advance( 29000 );
			info.readout( now() );
			assert.equal( requests.length, 2 );
			failed = false;
			advance( 1000 );
			info.readout( now() );
			await settle();
			assert.match( info.readout( now() ).lines[1], /uptime 20s/ );
			info.dispose();
		}
	);
});

test("hanging requests expire and late replies cannot replace the newer sample", async () => {
	const replies = [];
	await withFetch( () => new Promise( resolve => replies.push( resolve ) ), async ( { requests, advance, now } ) => {
		const info = createBuildInfo( "/api", undefined, undefined );
		info.readout( now() );
		advance( 10000 );
		info.readout( now() );
		assert.equal( requests[0].init.signal.aborted, true );
		advance( 30000 );
		info.readout( now() );
		replies[1]( new Response( JSON.stringify( { build: { revision: REVISION, uptimeSeconds: 1 } } ) ) );
		await settle();
		replies[0]( new Response( JSON.stringify( { build: { revision: "abcdef0123456789", uptimeSeconds: 99 } } ) ) );
		await settle();
		assert.match( info.readout( now() ).lines[1], /Agent 0123456 · uptime 1s/ );
		info.dispose();
		advance( 60000 );
		info.readout( now() );
		assert.equal( requests.length, 2 );
	} );
});

test("hidden diagnostics do not fetch; reopening and reconnecting refresh without duplicates", async () => {
	await withFetch(
		() => Promise.resolve( new Response( JSON.stringify( { build: { revision: "", uptimeSeconds: 5 } } ) ) ),
		async ( { requests, advance, now } ) => {
			const info = createBuildInfo( "/api", undefined, undefined );
			info.readout( now(), false );
			assert.equal( requests.length, 0 );
			info.readout( now(), true, "world" );
			info.readout( now(), true, "world" );
			await settle();
			assert.equal( requests.length, 1 );
			assert.match( info.readout( now() ).lines[0], /Client unknown/ );
			assert.match( info.readout( now() ).lines[1], /Agent unknown/ );
			info.readout( now(), false );
			advance( 6000 );
			info.readout( now(), true, "world" );
			await settle();
			assert.equal( requests.length, 2 );
			advance( 6000 );
			info.readout( now(), true, "reconnecting" );
			await settle();
			assert.equal( requests.length, 3 );
			info.dispose();
		}
	);
});

test("closing diagnostics aborts pending work and invalid uptime is not accepted", async () => {
	await withFetch(
		() => Promise.resolve( new Response( JSON.stringify( { build: { revision: REVISION, uptimeSeconds: -1 } } ) ) ),
		async ( { requests, now } ) => {
			const info = createBuildInfo( "/api", undefined, undefined );
			info.readout( now() );
			await settle();
			assert.equal( info.readout( now() ).lines[1], "Agent unavailable" );
			info.dispose();
			const next = createBuildInfo( "/api", undefined, undefined );
			next.readout( now() );
			next.readout( now(), false );
			assert.equal( requests.at( -1 ).init.signal.aborted, true );
			await settle();
			assert.equal( next.readout( now(), false ).lines[1], "Agent unavailable" );
			next.dispose();
		}
	);
});
