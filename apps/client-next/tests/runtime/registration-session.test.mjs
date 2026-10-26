/*
===========================================================================

registration-session.test.mjs - signup delivery and title request lifecycle

Uses the production HTTP/session owners. Signup must not authenticate, retry
duplicate writes, retain stale completions or lose actionable server errors.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";

const { createSession } = await import( "../../src/engine/runtime/simulation/worker/session/session.ts" );
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
/** @type {import("../../src/engine/contracts/session").SessionCommand} */
const command = {
	kind: "register",
	apiBase: "http://localhost:8787",
	username: "PlayerOne",
	password: "password123"
};

/*
================
settle
================
*/
const settle = () => new Promise( resolve => setImmediate( resolve ) );

test("registration sends username and password and stays signed out after success", async t => {
	const calls = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		calls.push( { url, options } );
		return Response.json( { ok: true, emailConfirmationRequired: false }, { status: 201 } );
	} );
	const session = createSession();
	t.after( () => session.dispose() );
	session.command( command );
	assert.equal( session.step()?.registration?.status, "pending" );
	await settle();
	const state = session.step();
	assert.equal( state?.registration?.status, "succeeded" );
	assert.equal( state?.phase, "signed-out" );
	assert.equal( calls.length, 1 );
	assert.equal( calls[0].url, command.apiBase + "/title/register" );
	assert.deepEqual( JSON.parse( calls[0].options.body ), { username: command.username, password: command.password } );
	assert.equal( calls[0].options.credentials, "omit" );
});

for ( const status of [ 400, 403, 409, 429, 503 ] ) {
	test(`registration displays the server's refusal (${status}) and allows retry`, async t => {
		t.mock.method(
			globalThis,
			"fetch",
			async () => Response.json( { ok: false, message: "Registration refusal" }, { status } )
		);
		const session = createSession();
		t.after( () => session.dispose() );
		session.command( command );
		await settle();
		assert.deepEqual( session.step()?.registration, { status: "failed", message: "Registration refusal" } );
		session.command( command );
		assert.equal( session.step()?.registration?.status, "pending" );
	});
}

test("registration blocks repeated submits and discards a response after logout", async t => {
	const requests = [];
	t.mock.method(
		globalThis,
		"fetch",
		( url, options ) => new Promise( resolve => requests.push( { url, options, resolve } ) )
	);
	const session = createSession();
	t.after( () => session.dispose() );
	session.command( command );
	session.command( command );
	assert.equal( requests.length, 1 );
	session.command( { kind: "logout" } );
	assert.equal( requests[0].options.signal.aborted, true );
	requests[0].resolve( Response.json( { ok: true }, { status: 201 } ) );
	await settle();
	assert.equal( session.step()?.registration, undefined );
});

test("registration reports a network failure", async t => {
	t.mock.method( globalThis, "fetch", async () => {
		throw Error( "offline" );
	} );
	const session = createSession();
	t.after( () => session.dispose() );
	session.command( command );
	await settle();
	assert.equal( session.step()?.registration?.status, "failed" );
});

test("registration can be disabled in saved client preferences", () => {
	assert.equal( experimentalOptions().registration, true );
	assert.equal( experimentalOptions( { registration: false } ).registration, false );
});
