/*
===========================================================================

session-bug-report.test.mjs - the reporter files with the session token

#4109: the report lane read only the login cookie, a copy of the session
a signed-in player can lose while still in the world (expired with the
session, or cleared by another tab), and the dialog then told them to
log in. The composed report now rides a session command: this owner
posts it with the token its game session was admitted with and
publishes the Agent's answer by operation. Without an identity it
answers NO_SESSION and posts nothing, so the reporter's own cookie
fetch stays the title screens' lane.

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
const login = {
	kind: "login",
	apiBase: "http://localhost:8787",
	id: "fixture",
	password: "fixture-password",
	serverId: "shard"
};
const loginAnswer = {
	ok: true,
	sessionToken: "private-test-token",
	divisionId: "shard",
	transportUrl: "http://localhost:9000",
	nextScene: "character-select"
};
const settle = () => new Promise( resolve => setImmediate( resolve ) );

/*
================
fixture

A session owner against a mocked Agent: the login answer above, and
the given answer for the report post, whose calls are recorded.
================
*/
function fixture( t, reportAnswer ) {
	/** @type {{ url: string; options: any; }[]} */
	const calls = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		if ( url.endsWith( "/title/bug-report" ) ) {
			calls.push( { url, options } );
			return reportAnswer();
		}
		return Response.json( url.endsWith( "/title/login" ) ? loginAnswer : { ok: true } );
	} );
	const session = createSession();
	t.after( () => session.dispose() );
	return { session, calls };
}

/*
================
signIn
================
*/
async function signIn( session ) {
	session.command( login );
	session.step( 0 );
	await settle();
	session.step( 1 );
}

test("a composed report is filed with the session token and answered", async t => {
	const { session, calls } = fixture( t, () => Response.json( { ok: true, id: "42" } ) );
	await signIn( session );
	session.command( {
		kind: "bug-report",
		operationId: 7,
		description: "The bridge eats my character",
		meta: '{"context":[]}',
		mediaField: "screenshot",
		media: new Blob( [ "png-bytes" ], { type: "image/jpeg" } )
	} );
	await settle();
	const state = session.step( 2 );
	assert.equal( calls.length, 1 );
	assert.equal( calls[0].options.method, "POST" );
	assert.equal( calls[0].options.headers.Authorization, "Bearer private-test-token" );
	assert.equal( calls[0].options.credentials, "include" );
	const form = calls[0].options.body;
	assert.equal( form.get( "description" ), "The bridge eats my character" );
	assert.equal( form.get( "meta" ), '{"context":[]}' );
	assert.equal( form.get( "screenshot" )?.name, "screenshot.jpg" );
	assert.deepEqual( state.bugReportResult, { operationId: 7, ok: true } );
});

test("without a session the filing answers NO_SESSION and posts nothing", async t => {
	const { session, calls } = fixture( t, () => Response.json( { ok: true } ) );
	session.command( { kind: "bug-report", operationId: 3, description: "stuck", meta: "{}" } );
	const state = session.step( 0 );
	assert.deepEqual( state.bugReportResult, { operationId: 3, ok: false, code: "NO_SESSION" } );
	assert.equal( calls.length, 0 );
});

test("the Agent's refusal code and wait ride the answer", async t => {
	const { session } = fixture(
		t,
		() => Response.json( { ok: false, code: "RATE_LIMITED", retryAfter: 42 }, { status: 429 } )
	);
	await signIn( session );
	session.command( { kind: "bug-report", operationId: 9, description: "again", meta: "{}" } );
	await settle();
	const state = session.step( 2 );
	assert.deepEqual( state.bugReportResult, { operationId: 9, ok: false, code: "RATE_LIMITED", retryAfter: 42 } );
});
