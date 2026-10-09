/*
===========================================================================

session.test.mjs - tests for session.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { defined } from "../helpers/defined.mjs";

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
const response = {
	ok: true,
	sessionToken: "private-test-token",
	divisionId: "shard",
	transportUrl: "http://localhost:9000",
	nextScene: "character-select"
};
const settle = () => new Promise( resolve => setImmediate( resolve ) );

test("incident retries preserve the reference and account credential", async t => {
	let socket, clock = 0;
	const reports = [];
	class Socket {
		static OPEN = 1;
		readyState = 1;
		bufferedAmount = 0;
		constructor() {
			socket = this;
		}
		send() {}
		close() {}
	}
	const descriptor = Object.getOwnPropertyDescriptor( globalThis, "WebSocket" );
	Object.defineProperty( globalThis, "WebSocket", { value: Socket, configurable: true } );
	t.after( () => {
		if ( descriptor ) Object.defineProperty( globalThis, "WebSocket", descriptor );
		else delete globalThis.WebSocket;
	} );
	t.mock.method( performance, "now", () => clock );
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		if ( url.endsWith( "/client/incident" ) ) {
			const body = JSON.parse( options.body );
			reports.push( { body, headers: options.headers } );
			return reports.length === 1 ?
				Response.json( { ok: false }, { status: 503 } ) :
				Response.json( { ok: true, id: body.id } );
		}
		return Response.json( url.endsWith( "/title/login" ) ? response : { ok: true, token: "fixture-ticket" } );
	} );
	const session = createSession();
	t.after( () => session.dispose() );
	session.command( command );
	session.step( 0 );
	await settle();
	session.step( 1 );
	session.command( { kind: "enter-world", character: "fixture" } );
	await settle();
	session.step( 2 );
	defined( socket ).onopen();
	defined( socket ).onmessage( { data: Uint8Array.of( 2, 0, 255 ).buffer } );
	await settle();
	session.step( 3 );
	assert.equal( reports.length, 1 );
	assert.match( reports[0].body.id, /^[a-f0-9]{32}$/ );
	assert.notEqual( reports[0].body.build, "2" );
	clock = 7000;
	session.step( 7000 );
	await settle();
	assert.equal( reports.length, 2 );
	assert.deepEqual( reports[1], reports[0] );
	clock = 60000;
	session.step( 60000 );
	await settle();
	assert.equal( reports.length, 2 );
});

test("repeated logout commands preserve pending cookie removal and allow a failed request to retry", async t => {
	const pending = [];
	t.mock.method(
		globalThis,
		"fetch",
		( url, options ) =>
			url.endsWith( "/title/logout" ) ?
				new Promise( resolve => pending.push( { resolve, signal: options.signal } ) ) :
				Promise.resolve( Response.json( response ) )
	);
	const session = createSession();
	session.command( command );
	session.step();
	await settle();
	session.step();
	session.command( { kind: "logout" } );
	session.step();
	for ( let i = 0; i < 60; i++ ) session.command( { kind: "logout" } );
	assert.equal( pending.length, 1 );
	assert.equal( pending[0].signal.aborted, false );
	pending[0].resolve( Response.json( { ok: false } ) );
	await settle();
	assert.equal( session.step().phase, "failed" );
	session.command( { kind: "logout" } );
	session.step();
	assert.equal( pending.length, 2 );
	pending[1].resolve( Response.json( { ok: true } ) );
	await settle();
	session.command( { kind: "logout" } );
	assert.equal( pending.length, 2, "completed response remains owned until its tick" );
	assert.equal( session.step().phase, "signed-out" );
	session.dispose();
});

test("fresh worker restores browser session without credentials or token publication", async t => {
	const calls = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		calls.push( { url, options } );
		return Response.json( response );
	} );
	const session = createSession();
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	await settle();
	const state = session.step();
	assert.equal( state.phase, "character-select" );
	assert.equal( state.divisionId, "shard" );
	assert.ok( !JSON.stringify( state ).includes( response.sessionToken ) );
	assert.equal( calls.length, 1 );
	assert.ok( calls[0].url.endsWith( "/title/session" ) );
	assert.equal( calls[0].options.credentials, "include" );
	assert.deepEqual( JSON.parse( calls[0].options.body ), {} );
	session.dispose();
	assert.equal( calls.length, 1, "refresh teardown must not log out" );
});

test("absent or expired browser session falls back to server selection once", async t => {
	const calls = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		calls.push( { url, options } );
		return url.endsWith( "/title/session" ) ? Response.json( { ok: false }, { status: 401 } ) : Response.json( [] );
	} );
	const session = createSession();
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	await settle();
	assert.equal( session.step().phase, "signed-out" );
	assert.deepEqual( calls.map( c => new URL( c.url ).pathname ), [ "/title/session", "/title/servers" ] );
	assert.equal( calls[1].options.credentials, "omit" );
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	await settle();
	session.step();
	assert.equal( calls.filter( c => c.url.endsWith( "/title/session" ) ).length, 1 );
	session.dispose();
});

test("explicit login supersedes delayed restoration and logout waits for cookie removal", async t => {
	let restore;
	t.mock.method(
		globalThis,
		"fetch",
		async url =>
			url.endsWith( "/title/session" ) ?
				new Promise( resolve => restore = resolve ) :
				Response.json( url.endsWith( "/title/logout" ) ? { ok: true } : response )
	);
	const session = createSession();
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	session.command( command );
	session.step();
	await settle();
	assert.equal( session.step().phase, "character-select" );
	defined( restore )( Response.json( { ...response, divisionId: "other" } ) );
	await settle();
	assert.equal( session.step(), null );
	session.command( { kind: "logout" } );
	assert.equal( session.step().phase, "authenticating" );
	await settle();
	assert.equal( session.step().phase, "signed-out" );
	session.dispose();
});

test("disposing an admitted session with a full journal does not enqueue a reset or mask failure", async t => {
	let socket;
	class Socket {
		static OPEN = 1;
		readyState = 1;
		bufferedAmount = 0;
		constructor() {
			socket = this;
		}
		send() {}
		close() {}
		receive( op, payload ) {
			const bytes = new Uint8Array( payload.length + 2 );
			new DataView( bytes.buffer ).setUint16( 0, op, true );
			bytes.set( payload, 2 );
			this.onmessage?.( { data: bytes.buffer } );
		}
	}
	const descriptor = Object.getOwnPropertyDescriptor( globalThis, "WebSocket" );
	Object.defineProperty( globalThis, "WebSocket", { value: Socket, configurable: true } );
	t.after( () => {
		if ( descriptor ) Object.defineProperty( globalThis, "WebSocket", descriptor );
		else delete globalThis.WebSocket;
	} );
	t.mock.method(
		globalThis,
		"fetch",
		async url => Response.json( url.endsWith( "/title/login" ) ? response : { ok: true, token: "fixture-ticket" } )
	);
	const session = createSession();
	session.command( command );
	session.step( 0 );
	await settle();
	session.step( 1 );
	session.command( { kind: "enter-world", character: "fixture" } );
	await settle();
	session.step( 2 );
	defined( socket ).onopen();
	const welcome = new Uint8Array( 27 );
	welcome[0] = 2;
	welcome[10] = 16;
	welcome.fill( 7, 11 );
	defined( socket ).receive( 2, welcome );
	session.step( 3 );
	await settle();
	session.step( 4 );
	const bootstrap = {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 25256, x: 1, y: 2, z: 3, angle: 0 } }
	};
	const blob = Buffer.from( JSON.stringify( { v: 1, bootstrap } ) ), payload = Buffer.alloc( 9 + blob.length );
	payload[0] = 1;
	payload.writeUInt32LE( blob.length, 5 );
	blob.copy( payload, 9 );
	defined( socket ).receive( 7, payload );
	session.step( 5 );
	const inflight = session.takeWorld();
	assert.ok( inflight );
	for ( let i = 0; i < 8192; i++ ) {
		defined( socket ).receive( 0x7777, Uint8Array.of( 1 ) );
		session.step( 5 );
	}
	assert.doesNotThrow( () => session.dispose() );
	assert.equal( session.takeWorld(), null );
});

test("an Agent edge route resolves against the API URL, so the socket stays on the client origin", async t => {
	const dialed = [];
	class Socket {
		static OPEN = 1;
		readyState = 0;
		bufferedAmount = 0;
		constructor( url ) {
			dialed.push( String( url ) );
		}
		send() {}
		close() {}
	}
	const descriptor = Object.getOwnPropertyDescriptor( globalThis, "WebSocket" );
	Object.defineProperty( globalThis, "WebSocket", { value: Socket, configurable: true } );
	t.after( () => {
		if ( descriptor ) Object.defineProperty( globalThis, "WebSocket", descriptor );
		else delete globalThis.WebSocket;
	} );
	for (
		const [transportUrl, socket] of [ [ "/shards/shard", "wss://192.168.1.20:5180/shards/shard/transport/ws" ], [
			"https://play.example.com/eu",
			"wss://play.example.com/eu/transport/ws"
		] ]
	) {
		t.mock.method(
			globalThis,
			"fetch",
			async url =>
				Response.json(
					url.endsWith( "/title/login" ) ?
						{ ...response, transportUrl } :
						{ ok: true, token: "fixture-ticket" }
				)
		);
		const session = createSession();
		session.command( { ...command, apiBase: "https://192.168.1.20:5180/api" } );
		session.step( 0 );
		await settle();
		session.step( 1 );
		session.command( { kind: "enter-world", character: "fixture" } );
		await settle();
		session.step( 2 );
		assert.equal( dialed.at( -1 ), socket );
		session.dispose();
		t.mock.restoreAll();
	}
});

test("native login argument is validated, published and cleared on retry and success", async t => {
	let body = {
		ok: false,
		nativeTitleStatus: 2,
		nativeTitleArgument: 5 * 65536 + 2,
		code: "INVALID_CREDENTIALS",
		message: "Rejected"
	};
	t.mock.method( globalThis, "fetch", async () => Response.json( body ) );
	const session = createSession();
	session.command( command );
	session.step();
	await settle();
	assert.equal( session.step().nativeTitleArgument, 327682 );
	for ( const argument of [ -1, 1.5, 4294967296, "327681", null ] ) {
		body = { ...body, nativeTitleArgument: argument };
		session.command( command );
		assert.equal( session.step().nativeTitleArgument, undefined );
		await settle();
		assert.match( session.step().error, /Invalid native title argument/ );
	}
	body = response;
	session.command( command );
	session.step();
	await settle();
	assert.equal( session.step().nativeTitleArgument, undefined );
	session.dispose();
});
test("authentication commits on a tick and never publishes the bearer", async t => {
	const requests = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		requests.push( { url, options } );
		return Response.json( response );
	} );
	const session = createSession();
	session.command( command );
	assert.equal( session.step().phase, "authenticating" );
	await settle();
	const state = session.step();
	assert.equal( state.phase, "character-select" );
	assert.equal( state.divisionId, "shard" );
	assert.ok( !JSON.stringify( state ).includes( response.sessionToken ) );
	assert.ok( Object.isFrozen( state ) );
	assert.equal( session.step(), null );
	assert.equal( requests[0].url, "http://localhost:8787/title/login" );
	assert.deepEqual( JSON.parse( requests[0].options.body ), {
		id: "fixture",
		password: "fixture-password",
		serverId: "shard"
	} );
	session.command( { kind: "logout" } );
	assert.equal( session.step().phase, "authenticating" );
	await settle();
	assert.equal( session.step().phase, "signed-out" );
	session.dispose();
});
test("logout rejects late authentication even when fetch ignores abort", async t => {
	let resolveRequest;
	let signal;
	t.mock.method( globalThis, "fetch", ( url, options ) => {
		if ( url.endsWith( "/title/logout" ) ) return Promise.resolve( Response.json( { ok: true } ) );
		signal = options.signal;
		return new Promise( resolve => resolveRequest = resolve );
	} );
	const session = createSession();
	session.command( command );
	session.step();
	session.command( { kind: "logout" } );
	assert.ok( defined( signal ).aborted );
	assert.equal( session.step().phase, "authenticating" );
	defined( resolveRequest )( Response.json( response ) );
	await settle();
	assert.equal( session.step().phase, "signed-out" );
	assert.equal( session.step(), null );
	session.dispose();
});
test("new login wins when responses arrive in reverse order", async t => {
	const pending = [];
	t.mock.method( globalThis, "fetch", () => new Promise( resolve => pending.push( resolve ) ) );
	const session = createSession();
	session.command( command );
	session.command( { ...command, serverId: "second" } );
	session.step();
	pending[1]( Response.json( { ...response, divisionId: "second" } ) );
	await settle();
	assert.equal( session.step().divisionId, "second" );
	pending[0]( Response.json( response ) );
	await settle();
	assert.equal( session.step(), null );
	session.dispose();
});
test("wrong shard, malformed transport URL and oversized HTTP response fail closed", async t => {
	let reply;
	t.mock.method( globalThis, "fetch", async () => reply() );
	for (
		const value of [ { ...response, divisionId: "wrong" }, {
			...response,
			transportUrl: "https://user:password@host"
		}, { ...response, sessionToken: "" } ]
	) {
		reply = () => Response.json( value );
		const session = createSession();
		session.command( command );
		session.step();
		await settle();
		assert.equal( session.step().phase, "failed" );
		session.dispose();
	}
	reply = () => new Response( "x".repeat( 65537 ) );
	const session = createSession();
	session.command( command );
	session.step();
	await settle();
	assert.equal( session.step().phase, "failed" );
	session.dispose();
});

test("native login rejection survives non-success HTTP status", async t => {
	t.mock.method(
		globalThis,
		"fetch",
		async () =>
			Response.json( {
				ok: false,
				nativeTitleStatus: 5,
				code: "RATE_LIMITED",
				message: "Too many login attempts."
			}, { status: 429 } )
	);
	const session = createSession();
	session.command( command );
	session.step();
	await settle();
	const state = session.step();
	assert.equal( state.phase, "failed" );
	assert.equal( state.nativeTitleStatus, 5 );
	assert.equal( state.code, "RATE_LIMITED" );
	session.dispose();
});

const character = {
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
		items: [ { refObjId: 11, plus: 0 }, { refObjId: 3644, plus: 3 } ],
		avatars: [],
		heightScale: 1,
		volumeScale: 1
	}
};
const roster = { characterRosterContractVersion: 2, action: 2, nativeResult: 1, characters: [ character ] };

test("restoration validates the remembered character against the roster before ordinary world admission", async t => {
	let rows = roster;
	const calls = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		calls.push( { url, options } );
		return Response.json(
			url.endsWith( "/title/session" ) ?
				{ ...response, resumeCharacter: "fixture" } :
				url.endsWith( "/character/list" ) ?
				rows :
				{ ok: false }
		);
	} );
	for ( const eligible of [ false, true ] ) {
		rows = { ...roster, characters: [ { ...character, deletePending: !eligible } ] };
		calls.length = 0;
		const session = createSession();
		session.command( { kind: "servers", apiBase: command.apiBase } );
		session.step();
		await settle();
		assert.equal( session.step().phase, "loading-roster" );
		await settle();
		const state = session.step();
		assert.equal( state.phase, eligible ? "connecting" : "character-select" );
		assert.equal( state.restoringWorld, eligible ? true : undefined );
		assert.equal( calls[1].options.headers.Authorization, "Bearer " + response.sessionToken );
		if ( eligible ) {
			await settle();
			const failed = session.step();
			assert.equal( failed.phase, "character-select" );
			assert.equal( failed.restoringWorld, false );
			assert.equal( failed.characters[0].name, "fixture" );
		} else assert.equal( calls.length, 2, "deleted characters must not request admission" );
		session.dispose();
	}
});

test("logout cancels a remembered-character roster response even when fetch ignores abort", async t => {
	let finish;
	const calls = [];
	t.mock.method( globalThis, "fetch", async url => {
		calls.push( url );
		if ( url.endsWith( "/character/list" ) ) return new Promise( resolve => finish = resolve );
		return Response.json(
			url.endsWith( "/title/session" ) ? { ...response, resumeCharacter: "fixture" } : { ok: true }
		);
	} );
	const session = createSession();
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	await settle();
	assert.equal( session.step().phase, "loading-roster" );
	session.command( { kind: "logout" } );
	session.step();
	await settle();
	assert.equal( session.step().phase, "signed-out" );
	defined( finish )( Response.json( roster ) );
	await settle();
	assert.equal( session.step(), null );
	assert.ok( !calls.some( url => url.includes( "/auth/" ) ) );
	session.dispose();
});
test("world lifecycle supersedes successful and failed title completions even when abort is ignored", async t => {
	let finish, signal, requests = 0;
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		requests++;
		if ( url.endsWith( "/title/login" ) ) return Response.json( response );
		if ( url.endsWith( "/character/list" ) ) {
			signal = options.signal;
			return new Promise( ( resolve, reject ) =>
				finish = bad => bad ? reject( new Error( "late failure" ) ) : resolve( Response.json( roster ) )
			);
		}
		return new Promise( () => {} );
	} );
	for ( const bad of [ false, true ] ) {
		const session = createSession();
		session.command( command );
		session.step( 0 );
		await settle();
		session.step( 1 );
		session.command( { kind: "roster" } );
		session.step( 2 );
		session.command( { kind: "enter-world", character: "fixture" } );
		assert.equal( defined( signal ).aborted, true );
		assert.equal( session.step( 3 ).phase, "connecting" );
		defined( finish )( bad );
		await settle();
		assert.equal( session.step( 4 ), null );
		const before = requests;
		session.command( { kind: "roster" } );
		const state = session.step( 5 );
		assert.equal( state.phase, "connecting" );
		assert.match( state.error, /Leave the world/ );
		assert.equal( requests, before );
		session.command( { kind: "disconnect" } );
		assert.equal( session.step( 6 ).phase, "character-select" );
		session.command( { kind: "logout" } );
		assert.equal( session.step( 7 ).phase, "authenticating" );
		session.dispose();
	}
});
test("restart cannot bypass admission by clearing a connecting world", async t => {
	t.mock.method(
		globalThis,
		"fetch",
		async url => url.endsWith( "/title/login" ) ? Response.json( response ) : new Promise( () => {} )
	);
	const session = createSession();
	session.command( command );
	session.step();
	await settle();
	session.step();
	session.command( { kind: "enter-world", character: "fixture" } );
	assert.equal( session.step().phase, "connecting" );
	session.command( { kind: "restart" } );
	const state = session.step();
	assert.equal( state.phase, "connecting" );
	assert.match( state.error, /not ready/ );
	session.dispose();
});
test("invalid world entry preserves the active title request", async t => {
	let finish, signal;
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		if ( url.endsWith( "/title/login" ) ) return Response.json( response );
		signal = options.signal;
		return new Promise( resolve => finish = () => resolve( Response.json( roster ) ) );
	} );
	const session = createSession();
	session.command( command );
	session.step( 0 );
	await settle();
	session.step( 1 );
	session.command( { kind: "roster" } );
	session.step( 2 );
	session.command( { kind: "enter-world", character: "" } );
	assert.equal( defined( signal ).aborted, false );
	assert.equal( session.step( 3 ).phase, "loading-roster" );
	defined( finish )();
	await settle();
	assert.equal( session.step( 4 ).phase, "character-select" );
	session.dispose();
});
test("roster uses the private bearer and preserves one immutable visual contract", async t => {
	const requests = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		requests.push( { url, options } );
		return Response.json( url.endsWith( "/title/login" ) ? response : roster );
	} );
	const session = createSession();
	session.command( command );
	session.step();
	await settle();
	session.step();
	session.command( { kind: "roster" } );
	assert.equal( session.step().phase, "loading-roster" );
	await settle();
	const state = session.step();
	assert.equal( requests[1].url, "http://localhost:8787/character/list" );
	assert.equal( requests[1].options.headers.Authorization, "Bearer private-test-token" );
	assert.deepEqual( JSON.parse( JSON.stringify( state.characters ) ), [ character ] );
	assert.ok( Object.isFrozen( state.characters[0].visualLoadout.items ) );
	assert.ok( Object.isFrozen( state.characters[0].visualLoadout.items[1] ) );
	assert.ok( !JSON.stringify( state ).includes( response.sessionToken ) );
	session.dispose();
});
test("server listing is unauthenticated and rejects duplicate routing identities", async t => {
	const server = {
		id: "shard",
		name: "Shard",
		onlinePlayers: 1,
		capacity: 100,
		nativeServerId: 1,
		nativeFarmId: 1,
		isTest: false,
		operating: true,
		transportUrl: "http://localhost:9000"
	};
	let rows = [ server ];
	t.mock.method( globalThis, "fetch", async ( _url, options ) => {
		assert.equal( options.headers.Authorization, undefined );
		return Response.json( rows );
	} );
	const session = createSession();
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	await settle();
	assert.deepEqual( session.step().servers, [ server ] );
	rows = [ server, server ];
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	await settle();
	assert.equal( session.step().phase, "failed" );
	session.dispose();
});
test("invalid roster version and duplicate character identities fail atomically", async t => {
	let payload;
	t.mock.method(
		globalThis,
		"fetch",
		async url => Response.json( url.endsWith( "/title/login" ) ? response : payload )
	);
	for (
		const value of [ { ...roster, characterRosterContractVersion: 1 }, {
			...roster,
			characters: [ {
				...character,
				visualLoadout: {
					...character.visualLoadout,
					items: [ { refObjId: 11, plus: 0 }, { refObjId: 11, plus: 0 } ]
				}
			} ]
		}, {
			...roster,
			characters: [ character, character ]
		}, {
			...roster,
			characters: [ { ...character, visualLoadout: { ...character.visualLoadout, heightScale: 0 } } ]
		} ]
	) {
		payload = value;
		const session = createSession();
		session.command( command );
		session.step();
		await settle();
		session.step();
		session.command( { kind: "roster" } );
		session.step();
		await settle();
		const state = session.step();
		assert.equal( state.phase, "failed" );
		assert.equal( state.characters, undefined );
		session.dispose();
	}
});
test("logout supersedes pending roster without publishing another account's characters", async t => {
	let resolveRoster;
	t.mock.method(
		globalThis,
		"fetch",
		async url =>
			url.endsWith( "/title/login" ) ?
				Response.json( response ) :
				url.endsWith( "/title/logout" ) ?
				Response.json( { ok: true } ) :
				new Promise( resolve => resolveRoster = resolve )
	);
	const session = createSession();
	session.command( command );
	session.step();
	await settle();
	session.step();
	session.command( { kind: "roster" } );
	session.step();
	session.command( { kind: "logout" } );
	session.step();
	defined( resolveRoster )( Response.json( roster ) );
	await settle();
	assert.equal( session.step().phase, "signed-out" );
	assert.equal( session.step(), null );
	session.dispose();
});

test("malformed character mutation fails its operation without losing the authenticated roster", async t => {
	t.mock.method(
		globalThis,
		"fetch",
		async url =>
			Response.json(
				url.endsWith( "/title/login" ) ?
					response :
					url.endsWith( "/character/list" ) ?
					roster :
					{ action: 3, nativeResult: 1, characterRosterContractVersion: 2, character: { id: 1 } }
			)
	);
	const session = createSession();
	session.command( command );
	session.step();
	await settle();
	session.step();
	session.command( { kind: "roster" } );
	session.step();
	await settle();
	const prior = session.step();
	session.command( { kind: "delete-character", operationId: 1, characterName: "fixture" } );
	assert.equal( session.step().characterOperation.status, "pending" );
	await settle();
	const state = session.step();
	assert.equal( state.phase, "character-select" );
	assert.equal( state.characters, prior.characters );
	assert.equal( state.characterOperation.status, "failed" );
	assert.equal( state.characterOperation.operationId, 1 );
	session.dispose();
});
test("cancelling an absent character operation does not abort authentication", async t => {
	let resolveRequest, signal;
	t.mock.method( globalThis, "fetch", ( _url, options ) => {
		signal = options.signal;
		return new Promise( resolve => resolveRequest = resolve );
	} );
	const session = createSession();
	session.command( command );
	session.step();
	session.command( { kind: "cancel-character-operation" } );
	assert.equal( defined( signal ).aborted, false );
	defined( resolveRequest )( Response.json( response ) );
	await settle();
	assert.equal( session.step().phase, "character-select" );
	session.dispose();
});
test("cancelled character completion cannot replace a newer operation", async t => {
	const requests = [];
	t.mock.method(
		globalThis,
		"fetch",
		async url =>
			url.endsWith( "/title/login" ) ?
				Response.json( response ) :
				new Promise( resolve => requests.push( resolve ) )
	);
	const session = createSession();
	session.command( command );
	session.step();
	await settle();
	session.step();
	session.command( { kind: "check-name", operationId: 1, characterName: "first" } );
	session.step();
	session.command( { kind: "cancel-character-operation" } );
	session.step();
	session.command( { kind: "check-name", operationId: 2, characterName: "second" } );
	session.step();
	requests[0]( Response.json( { action: 4, nativeResult: 1 } ) );
	await settle();
	assert.equal( session.step(), null );
	requests[1]( Response.json( { action: 4, nativeResult: 1 } ) );
	await settle();
	const state = session.step();
	assert.equal( state.characterOperation.operationId, 2 );
	assert.equal( state.characterOperation.status, "succeeded" );
	session.dispose();
});

test("deletion and recovery preserve roster order and reject a mismatched reply identity", async t => {
	let operationReply;
	const other = { ...character, id: 2, name: "other" }, rows = { ...roster, characters: [ character, other ] };
	t.mock.method(
		globalThis,
		"fetch",
		async url =>
			Response.json(
				url.endsWith( "/title/login" ) ? response : url.endsWith( "/character/list" ) ? rows : operationReply
			)
	);
	const session = createSession();
	session.command( command );
	session.step();
	await settle();
	session.step();
	session.command( { kind: "roster" } );
	session.step();
	await settle();
	session.step();
	for ( const [index, kind, action] of [ [ 1, "delete-character", 3 ], [ 2, "restore-character", 5 ] ] ) {
		operationReply = {
			action,
			nativeResult: 1,
			characterRosterContractVersion: 2,
			character: { ...character, deletePending: action === 3 }
		};
		session.command( { kind, operationId: index, characterName: "fixture" } );
		session.step();
		await settle();
		const value = session.step();
		assert.equal( value.characterOperation.status, "succeeded" );
		assert.deepEqual( value.characters.map( r => r.name ), [ "fixture", "other" ] );
		assert.equal( value.characters[0].deletePending, action === 3 );
	}
	operationReply = {
		action: 3,
		nativeResult: 1,
		characterRosterContractVersion: 2,
		character: { ...other, deletePending: true }
	};
	session.command( { kind: "delete-character", operationId: 3, characterName: "fixture" } );
	session.step();
	await settle();
	const value = session.step();
	assert.equal( value.characterOperation.status, "failed" );
	assert.deepEqual( value.characters.map( r => r.deletePending ), [ false, false ] );
	session.dispose();
});

test("entry admission failure returns the authenticated roster without another login", async t => {
	t.mock.method(
		globalThis,
		"fetch",
		async url =>
			Response.json(
				url.endsWith( "/title/login" ) ? response : url.endsWith( "/character/list" ) ? roster : { ok: false }
			)
	);
	const session = createSession();
	session.command( command );
	session.step();
	await settle();
	session.step();
	session.command( { kind: "roster" } );
	session.step();
	await settle();
	const prior = session.step();
	session.command( { kind: "enter-world", character: "fixture" } );
	session.step();
	await settle();
	const value = session.step();
	assert.equal( value.phase, "character-select" );
	assert.equal( value.characters, prior.characters );
	assert.match( value.error, /Admission request failed/ );
	session.dispose();
});

test("raw native shard name survives login, roster and restoration, and clears on logout", async t => {
	const name = "Native#$Tdisplay suffix";
	t.mock.method(
		globalThis,
		"fetch",
		async url =>
			Response.json(
				url.endsWith( "/title/logout" ) ?
					{ ok: true } :
					url.endsWith( "/characters" ) ?
					[] :
					{ ...response, nativeServerName: name }
			)
	);
	for ( const restore of [ false, true ] ) {
		const session = createSession();
		session.command( restore ? { kind: "servers", apiBase: command.apiBase } : command );
		session.step();
		await settle();
		assert.equal( session.step().nativeServerName, name );
		session.command( { kind: "logout" } );
		session.step();
		await settle();
		assert.equal( session.step().nativeServerName, undefined );
		session.dispose();
	}
});

const { RELEASE_PROTOCOL, RELEASE_PROTOCOL_HEADER } = await import(
	sourceFileUrl( path.join( root, "src/engine/foundation/release/protocol.ts" ) ).href
);

test("every title and agent request declares the release protocol it was built for", async t => {
	const calls = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		calls.push( { url, options } );
		return url.endsWith( "/title/session" ) ? Response.json( { ok: false }, { status: 401 } ) : Response.json( [] );
	} );
	const session = createSession();
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	await settle();
	session.step();
	assert.ok( calls.length >= 2 );
	for ( const call of calls ) {
		assert.equal( call.options.headers[RELEASE_PROTOCOL_HEADER], String( RELEASE_PROTOCOL ), call.url );
	}
	session.dispose();
});

test("a server speaking another release protocol marks the session outdated and names the remedy", async t => {
	t.mock.method(
		globalThis,
		"fetch",
		async () => Response.json( { error: "client-outdated", protocol: RELEASE_PROTOCOL + 1 }, { status: 426 } )
	);
	const session = createSession();
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	await settle();
	const state = session.step();
	assert.equal( state.releaseOutdated, true );
	assert.match( state.error, /newer version/i );
	// Every later state keeps saying so: the page must be refreshed.
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	await settle();
	assert.equal( session.step().releaseOutdated, true );
	session.dispose();
});

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
