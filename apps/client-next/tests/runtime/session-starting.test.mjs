/*
===========================================================================

session-starting.test.mjs - the title waits for a starting server (#246)

A server whose readiness gate never opened answers 503 PROCESS_STARTING
with a Retry-After. The session keeps its waiting phase, repeats the
request after that delay, fails with the server's own message past the
cap, and holds the repeated login command only while the request lasts.
Split from session.test.mjs, which covers the rest of session.ts.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const { createSession } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/simulation/worker/session/session.ts" ) ).href
);
const command = {
	kind: "login",
	apiBase: "http://localhost:8787",
	id: "fixture",
	password: "fixture-password",
	serverId: "shard"
};
const settle = () => new Promise( resolve => setImmediate( resolve ) );

// #246: a server whose gate never opened answers PROCESS_STARTING; the title
// keeps waiting and repeats the request after Retry-After instead of failing.
const STARTING = { ok: false, code: "PROCESS_STARTING", message: "The Agent is starting.", retryAfter: 1 };

test("a starting server is waited for and the server list is repeated after Retry-After", async t => {
	let starting = 2;
	const calls = [];
	t.mock.method( globalThis, "fetch", async url => {
		calls.push( new URL( url ).pathname );
		if ( starting > 0 ) {
			starting--;
			return Response.json( STARTING, { status: 503, headers: { "Retry-After": "1" } } );
		}
		return url.endsWith( "/title/session" ) ? Response.json( { ok: false }, { status: 401 } ) : Response.json( [] );
	} );
	const session = createSession();
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step( 0 );
	await settle();
	// The restore and the list both answered "starting": no failure, still listing.
	assert.equal( session.step( 10 )?.phase ?? "listing-servers", "listing-servers" );
	assert.notEqual( session.step( 20 )?.phase, "failed" );
	// Before Retry-After nothing is repeated; after it the list is asked again.
	const before = calls.length;
	session.step( 500 );
	await settle();
	assert.equal( calls.length, before );
	session.step( 1100 );
	await settle();
	assert.equal( session.step( 1200 ).phase, "signed-out" );
	assert.deepEqual( calls, [ "/title/session", "/title/servers", "/title/servers" ] );
	session.dispose();
});

test("a server that keeps starting past the cap fails with its own message", async t => {
	t.mock.method( globalThis, "fetch", async () => Response.json( STARTING, { status: 503 } ) );
	const session = createSession();
	session.command( { kind: "servers", apiBase: command.apiBase } );
	let state, now = 0;
	session.step( now );
	await settle();
	for ( ; now <= 62_000 && state?.phase !== "failed"; now += 500 ) {
		state = session.step( now ) ?? state;
		await settle();
	}
	assert.equal( state?.phase, "failed" );
	assert.equal( state.code, "PROCESS_STARTING" );
	assert.equal( state.error, "The Agent is starting." );
	session.dispose();
});

// The repeat holds the login command, password included: it lives only
// until the request settles or the player leaves the wait.
test("a login waiting on a starting server is never sent again once the player signs out", async t => {
	const logins = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		if ( url.endsWith( "/title/login" ) ) {
			logins.push( options?.body );
			return Response.json( STARTING, { status: 503 } );
		}
		return Response.json( { ok: true } );
	} );
	const session = createSession();
	session.command( command );
	session.step( 0 );
	await settle();
	assert.notEqual( session.step( 10 )?.phase, "failed" );
	session.command( { kind: "logout" }, 20 );
	for ( let now = 100; now <= 5000; now += 500 ) {
		session.step( now );
		await settle();
	}
	assert.equal( logins.length, 1 );
	session.dispose();
});

test("a login that settles after a starting wait is not repeated again", async t => {
	let logins = 0;
	t.mock.method( globalThis, "fetch", async url => {
		if ( !url.endsWith( "/title/login" ) ) return Response.json( { ok: true } );
		logins++;
		return logins === 1 ?
			Response.json( STARTING, { status: 503 } ) :
			Response.json( { ok: false, message: "Invalid credentials" }, { status: 401 } );
	} );
	const session = createSession();
	session.command( command );
	let state;
	for ( let now = 0; now <= 5000; now += 500 ) {
		state = session.step( now ) ?? state;
		await settle();
	}
	assert.equal( logins, 2 );
	assert.equal( state?.phase, "failed" );
	assert.notEqual( state?.code, "PROCESS_STARTING" );
	session.dispose();
});
