/*
===========================================================================

transport-report.test.mjs - the client reports how it connected (#1449)

After each WELCOME the network owner tells the server which fallback, if
any, this connect took (OpTransportReport, 0x000C). With no WebTransport
attempt ahead of the socket the report says "no fallback"; the reason is
telemetry the session history records, never a gate on the session.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createCodec, OP_TRANSPORT_REPORT } = await load(
	"src/engine/runtime/simulation/worker/network/codec/codec.ts"
);
const { createNetwork } = await load( "src/engine/runtime/simulation/worker/network/network.ts" );

/*
================
socketHarness

A fake WebSocket that records what the client sends.
================
*/
function socketHarness( t ) {
	const sockets = [];
	class Socket {
		static OPEN = 1;
		readyState = 1;
		bufferedAmount = 0;
		sent = [];
		/** @type {any} */
		onmessage = null;
		/** @type {any} */
		onclose = null;
		/** @type {any} */
		onerror = null;
		constructor() {
			sockets.push( this );
		}
		send( bytes ) {
			this.sent.push( bytes.slice() );
		}
		close() {}
		receive( bytes ) {
			this.onmessage?.( { data: bytes.buffer } );
		}
	}
	const original = Object.getOwnPropertyDescriptor( globalThis, "WebSocket" );
	Object.defineProperty( globalThis, "WebSocket", { value: Socket, configurable: true, writable: true } );
	t.after( () => {
		if ( original ) {
			Object.defineProperty( globalThis, "WebSocket", original );
		} else {
			Reflect.deleteProperty( globalThis, "WebSocket" );
		}
	} );
	return sockets;
}

/*
================
welcome

A v2 WELCOME frame; resumed sets its resumed flag.
================
*/
function welcome( resumed ) {
	const bytes = new Uint8Array( 29 );
	bytes[0] = 2;
	bytes[2] = 2;
	bytes[3] = resumed ? 1 : 0;
	bytes[12] = 16;
	return bytes;
}

/*
================
reports

The report frames one socket sent, as [version, reason] pairs.
================
*/
function reports( socket ) {
	return socket.sent
		.filter( b => b[0] === OP_TRANSPORT_REPORT && b[1] === 0 )
		.map( b => [ b[2], b[3] ] );
}

test("the report frame carries version and reason", () => {
	const codec = createCodec();
	assert.deepEqual( [ ...codec.encode( codec.transportReport( 2 ) ) ], [ 0x0c, 0, 1, 2 ] );
	assert.throws( () => codec.transportReport( 5 ) );
	assert.throws( () => codec.transportReport( -1 ) );
});

test("each WELCOME is answered with one no-fallback report", t => {
	const sockets = socketHarness( t ), failures = [];
	const network = createNetwork( e => failures.push( e ) );
	network.connect( "ws://localhost/t", "abc" );
	sockets[0].onopen();
	sockets[0].receive( welcome( false ) );
	assert.deepEqual( reports( sockets[0] ), [ [ 1, 0 ] ] );
	// A resume on a new socket reports for that attachment too.
	network.connect( "ws://localhost/t", "abc", new Uint8Array( 16 ).fill( 7 ) );
	sockets[1].onopen();
	sockets[1].receive( welcome( true ) );
	assert.deepEqual( reports( sockets[1] ), [ [ 1, 0 ] ] );
	assert.deepEqual( failures, [] );
});
