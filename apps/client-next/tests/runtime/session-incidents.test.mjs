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
