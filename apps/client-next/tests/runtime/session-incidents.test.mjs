/*
===========================================================================

session-incidents.test.mjs - incident delivery from session.ts

A fatal failure is reported to the Agent before the session ends: retries
keep the reference and the account's bearer, and a failure before login
goes to the Agent the title listed servers from, with no bearer. Split from
session.test.mjs, which covers login, roster and world admission.

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
	/** @type {{ onopen: () => void; onmessage: ( event: { data: ArrayBuffer; } ) => void; } | undefined} */
	let socket;
	let clock = 0;
	const reports = [];
	class Socket {
		static OPEN = 1;
		readyState = 1;
		bufferedAmount = 0;
		constructor() {
			socket = /** @type {any} */ (this);
		}
		send() {}
		close() {}
	}
	const descriptor = Object.getOwnPropertyDescriptor( globalThis, "WebSocket" );
	Object.defineProperty( globalThis, "WebSocket", { value: Socket, configurable: true } );
	t.after( () => {
		if ( descriptor ) Object.defineProperty( globalThis, "WebSocket", descriptor );
		else Reflect.deleteProperty( globalThis, "WebSocket" );
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

test("a failure before login is reported to the listing Agent without a bearer", async t => {
	/** @type {{ url: string; body: any; headers: Record<string, string>; }[]} */
	const reports = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		if ( url.endsWith( "/client/incident" ) ) {
			const body = JSON.parse( options.body );
			reports.push( { url, body, headers: options.headers } );
			return Response.json( { ok: true, id: body.id } );
		}
		return Response.json( [] );
	} );
	const session = createSession();
	t.after( () => session.dispose() );
	// No Agent known yet: nowhere to send, so nothing is sent.
	session.command( { kind: "client-report", incidentKind: "runtime", code: "early_fault", message: "too early" } );
	session.step();
	await settle();
	assert.equal( reports.length, 0 );
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	await settle();
	session.command( {
		kind: "client-report",
		incidentKind: "runtime",
		code: "title_fault",
		message: "Runtime failed"
	} );
	session.step();
	await settle();
	assert.equal( reports.length, 1 );
	const report = defined( reports[0] );
	assert.equal( report.url, `${command.apiBase}/client/incident` );
	assert.equal( report.headers.Authorization, undefined );
	assert.equal( report.body.code, "title_fault" );
	assert.equal( report.body.kind, "runtime" );
});

test("asset failures file once per episode, beside the once-per-code client reports", async t => {
	/** @type {{ url: string; body: any; headers: Record<string, string>; }[]} */
	const reports = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		if ( url.endsWith( "/client/incident" ) ) {
			const body = JSON.parse( options.body );
			reports.push( { url, body, headers: options.headers } );
			return Response.json( { ok: true, id: body.id } );
		}
		return Response.json( [] );
	} );
	const session = createSession();
	t.after( () => session.dispose() );
	session.command( { kind: "servers", apiBase: command.apiBase } );
	session.step();
	await settle();
	// The stream owner hands each terminal episode out once; the
	// session files what it is given, so the same failure arriving
	// again is a second episode, not a duplicate (#2337).
	const asset = {
		kind: "client-report",
		incidentKind: "asset",
		code: "world_asset_failed",
		message: "asset=/assets/a.json transient=true attempts=8 error=Error: /assets/a.json: Load failed"
	};
	session.command( asset );
	session.step();
	await settle();
	session.command( asset );
	session.step();
	await settle();
	const filed = reports.filter( r => r.body.code === "world_asset_failed" );
	assert.equal( filed.length, 2, "one incident per episode: asset reports skip the code dedupe" );
	for ( const report of filed ) {
		assert.equal( report.body.kind, "asset" );
		assert.equal( report.body.category, "software" );
		assert.equal( report.body.message, asset.message );
		assert.equal( typeof report.body.phase, "string", "the incident carries the session phase" );
		assert.ok( report.body.phase.length > 0 );
	}
	// A runtime fault still reports once per code beside them.
	const fault = {
		kind: "client-report",
		incidentKind: "runtime",
		code: "title_fault",
		message: "Runtime failed"
	};
	session.command( fault );
	session.step();
	await settle();
	session.command( fault );
	session.step();
	await settle();
	assert.equal( reports.filter( r => r.body.code === "title_fault" ).length, 1 );
});

test("world incidents carry the admitted region and a 401 transport-token refusal stops reconnect retries", async t => {
	/** @type {any} */
	let socket;
	let clock = 0, transportUnauthorized = false, transportMints = 0;
	const reports = [];
	class Socket {
		static OPEN = 1;
		readyState = 1;
		bufferedAmount = 0;
		/** @type {(( event: { data: ArrayBuffer; } ) => void) | undefined} */
		onmessage;
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
		else Reflect.deleteProperty( globalThis, "WebSocket" );
	} );
	t.mock.method( performance, "now", () => clock );
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		if ( url.endsWith( "/client/incident" ) ) {
			const body = JSON.parse( options.body );
			reports.push( body );
			return Response.json( { ok: true, id: body.id } );
		}
		if ( url.endsWith( "/auth/transport-token" ) ) {
			transportMints++;
			if ( transportUnauthorized ) {
				return Response.json( { ok: false, code: "UNAUTHORIZED" }, { status: 401 } );
			}
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
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 22387, x: 1, y: 2, z: 3, angle: 0 } }
	};
	const blob = Buffer.from( JSON.stringify( { v: 1, bootstrap } ) ), payload = Buffer.alloc( 9 + blob.length );
	payload[0] = 1;
	payload.writeUInt32LE( blob.length, 5 );
	blob.copy( payload, 9 );
	defined( socket ).receive( 7, payload );
	session.step( 5 );
	transportUnauthorized = true;
	// A browser always follows an error event with the close that carries
	// the code; the failure is reported at the close (GH #1428).
	defined( socket ).onerror?.();
	defined( socket ).onclose?.( { code: 1006, reason: "", wasClean: false } );
	session.step( 100 );
	await settle();
	assert.equal( reports.length, 1 );
	assert.equal( reports[0].region, 22387 );
	assert.equal( transportMints, 1 );
	for ( const stepAt of [ 350, 1000, 2000, 5000, 15000 ] ) {
		clock = stepAt;
		session.step( stepAt );
		await settle();
	}
	assert.equal( transportMints, 2 );
	const finalState = session.step( 16000 );
	assert.equal( finalState === null || finalState.phase === "disconnected", true );
});

test("a refused entry files its incident with the entering character", async t => {
	/** @type {any} */
	let socket;
	const reports = [];
	class Socket {
		static OPEN = 1;
		readyState = 1;
		bufferedAmount = 0;
		/** @type {(( event: { data: ArrayBuffer; } ) => void) | undefined} */
		onmessage;
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
		else Reflect.deleteProperty( globalThis, "WebSocket" );
	} );
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		if ( url.endsWith( "/client/incident" ) ) {
			const body = JSON.parse( options.body );
			reports.push( body );
			return Response.json( { ok: true, id: body.id } );
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
	const welcome = new Uint8Array( 27 );
	welcome[0] = 2;
	welcome[10] = 16;
	welcome.fill( 7, 11 );
	defined( socket ).receive( 2, welcome );
	session.step( 3 );
	await settle();
	// The EnterWorld result refuses the entry (its first byte is not 1):
	// the session bounces back to character select with the name cleared
	// from the published state (#1855).
	defined( socket ).receive( 7, new Uint8Array( 9 ) );
	const bounced = session.step( 4 );
	await settle();
	assert.equal( bounced?.phase, "character-select" );
	assert.equal( bounced?.character, undefined );
	assert.match( String( bounced?.error ), /EnterWorld rejected/ );
	session.command( {
		kind: "client-report",
		incidentKind: "runtime",
		code: "world_entry_rejected",
		message: "Entry rejected (S2) at dock/entering: EnterWorld rejected: 0"
	} );
	session.step( 5 );
	await settle();
	// The world files its own failure incident as well; the frontend's
	// rejection report is the one triage matches by character (#1855).
	const rejection = reports.find( report => report.code === "world_entry_rejected" );
	assert.equal( rejection?.character, "fixture" );
});
