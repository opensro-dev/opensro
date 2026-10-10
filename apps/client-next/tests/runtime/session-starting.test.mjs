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

// A GameWorld restart: the player logs in while the shard still starts. The
// character list answers "starting" (the GameWorld's PROCESS_STARTING, or the
// Agent proxy's bare 503) until readiness opens. The dock must keep loading
// and fill when the list arrives; it must never show an empty roster, which
// players read as deleted characters.
const LOGIN = {
	ok: true,
	sessionToken: "private-test-token",
	divisionId: "shard",
	transportUrl: "http://localhost:9000",
	nextScene: "character-select"
};
const CHARACTER = {
	id: 1,
	name: "fixture",
	level: 1,
	raceIndex: 0,
	gender: 0,
	figureIndex: 0,
	heightIndex: 0,
	volumeIndex: 0,
	weaponIndex: 0,
	protectorIndex: 0,
	maxHp: 100,
	maxMp: 100,
	armorSelected: true,
	weaponSelected: true,
	deletePending: false,
	visualLoadout: {
		modelCodename: "CHAR_TEST",
		animationSetName: "test",
		items: [ { refObjId: 11, plus: 0 } ],
		avatars: [],
		heightScale: 1,
		volumeScale: 1
	}
};
const ROSTER = { characterRosterContractVersion: 2, action: 2, nativeResult: 1, characters: [ CHARACTER ] };

/*
================
rosterAfterStarting

Logs in, asks for the roster, and steps the session while the list answers
with refusal() for the first starting answers, then with the real list.
Returns every published phase and how many lists were requested.
================
*/
async function rosterAfterStarting( t, startingAnswers, refusal ) {
	let lists = 0;
	t.mock.method( globalThis, "fetch", async url => {
		if ( url.endsWith( "/title/login" ) ) return Response.json( LOGIN );
		if ( url.endsWith( "/character/list" ) ) {
			lists++;
			return lists <= startingAnswers ? refusal() : Response.json( ROSTER );
		}
		return Response.json( { ok: true } );
	} );
	const session = createSession();
	session.command( command );
	session.step( 0 );
	await settle();
	session.step( 1 );
	session.command( { kind: "roster" } );
	const phases = [];
	let state;
	for ( let now = 2; now <= 8000; now += 250 ) {
		const next = session.step( now );
		if ( next ) {
			state = next;
			phases.push( { phase: next.phase, characters: next.characters?.length } );
		}
		await settle();
	}
	session.dispose();
	return { phases, state, lists };
}

test("a roster answered PROCESS_STARTING keeps loading and fills, never an empty dock", async t => {
	const { phases, state, lists } = await rosterAfterStarting(
		t,
		2,
		() => Response.json( STARTING, { status: 503, headers: { "Retry-After": "1" } } )
	);
	assert.equal( lists, 3, "the list was repeated after each starting answer" );
	assert.equal( state.phase, "character-select" );
	assert.equal( state.characters.length, 1 );
	assert.ok(
		!phases.some( p => p.phase === "character-select" && !p.characters ) ||
			phases.every( p => p.phase !== "failed" ),
		"no failure while starting"
	);
	assert.ok(
		!phases.some( p => p.phase === "character-select" && p.characters === 0 ),
		"never an empty roster from a starting answer"
	);
});

test("the Agent's bare 503 for a not-yet-operating shard is waited for the same way", async t => {
	const { state, lists } = await rosterAfterStarting(
		t,
		2,
		() => new Response( "shard unavailable\n", { status: 503, headers: { "Content-Type": "text/plain" } } )
	);
	assert.equal( lists, 3 );
	assert.equal( state.phase, "character-select" );
	assert.equal( state.characters.length, 1 );
});

test("a roster that keeps starting past the cap fails clearly instead of showing an empty dock", async t => {
	t.mock.method( globalThis, "fetch", async url => {
		if ( url.endsWith( "/title/login" ) ) return Response.json( LOGIN );
		if ( url.endsWith( "/character/list" ) ) return Response.json( STARTING, { status: 503 } );
		return Response.json( { ok: true } );
	} );
	const session = createSession();
	session.command( command );
	session.step( 0 );
	await settle();
	session.step( 1 );
	session.command( { kind: "roster" } );
	let state, empty = false;
	for ( let now = 2; now <= 62_000 && state?.phase !== "failed"; now += 500 ) {
		state = session.step( now ) ?? state;
		if (
			state?.phase === "character-select" && Array.isArray( state.characters ) && state.characters.length === 0
		) {
			empty = true;
		}
		await settle();
	}
	assert.equal( state?.phase, "failed" );
	assert.equal( state.code, "PROCESS_STARTING" );
	assert.equal( empty, false, "no empty roster was ever published" );
	session.dispose();
});
