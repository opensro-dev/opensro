/*
===========================================================================

network-wt-entry-fallback.test.mjs - a stalled WebTransport entry falls back (#2632)

A WebTransport link that welcomed and took an EnterWorld but never
delivered its answer has indicted the QUIC path: the world session's
retry must connect over the WebSocket, not present the next admission
to the same stalled path until the entry gives up to character select.
An answered entry keeps the path's credit, and a socket stall never
indicts QUIC.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { createNetwork } = await import(
	"../../src/engine/runtime/simulation/worker/network/network.ts"
);

/*
================
socketHarness

A WebSocket stand-in that records each construction and what is sent.
================
*/
function socketHarness( t ) {
	const sockets = [];
	class Socket {
		static OPEN = 1;
		readyState = 1;
		bufferedAmount = 0;
		/** @type {any} */
		onmessage = null;
		/** @type {any} */
		onclose = null;
		/** @type {any} */
		onerror = null;
		constructor() {
			sockets.push( this );
		}
		send() {}
		close() {}
		receive( bytes ) {
			this.onmessage?.( { data: bytes.buffer } );
		}
	}
	const original = Object.getOwnPropertyDescriptor( globalThis, "WebSocket" );
	Object.defineProperty( globalThis, "WebSocket", { value: Socket, configurable: true, writable: true } );
	t.after( () => {
		if ( original ) Object.defineProperty( globalThis, "WebSocket", original );
		else Reflect.deleteProperty( globalThis, "WebSocket" );
	} );
	return sockets;
}

/*
================
wtHarness

A WebTransport stand-in: a controllable control stream and a closed
promise the test settles the way the path would.
================
*/
function wtHarness( t ) {
	const transports = [];
	class Transport {
		ready = Promise.resolve();
		constructor() {
			this.closed = new Promise( ( resolve, reject ) => {
				this.settle = resolve;
				this.abort = reject;
			} );
			transports.push( this );
		}
		async createBidirectionalStream() {
			this.readable = new ReadableStream( {
				start: controller => {
					this.controller = controller;
				}
			} );
			this.writable = new WritableStream( { write() {} } );
			return { readable: this.readable, writable: this.writable };
		}
		close() {}
		receive( bytes ) {
			const framed = new Uint8Array( bytes.byteLength + 4 );
			new DataView( framed.buffer ).setUint32( 0, bytes.byteLength, true );
			framed.set( bytes, 4 );
			/** @type {ReadableStreamDefaultController} */ (this.controller).enqueue( framed );
		}
	}
	const original = Object.getOwnPropertyDescriptor( globalThis, "WebTransport" );
	Object.defineProperty( globalThis, "WebTransport", { value: Transport, configurable: true, writable: true } );
	t.after( () => {
		if ( original ) Object.defineProperty( globalThis, "WebTransport", original );
		else Reflect.deleteProperty( globalThis, "WebTransport" );
	} );
	return transports;
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
enterWorldAnswer

The EnterWorld result frame (opcode 7) with an empty payload.
================
*/
function enterWorldAnswer() {
	return new Uint8Array( [ 7, 0 ] );
}

/*
================
flushMicrotasks
================
*/
async function flushMicrotasks() {
	await new Promise( resolve => setTimeout( resolve, 10 ) );
	await new Promise( resolve => setTimeout( resolve, 10 ) );
}

/*
================
openWtSession

A welcomed WebTransport session that has presented its entry, and the
failures it reports.
================
*/
async function openWtSession( t ) {
	const transports = wtHarness( t ), sockets = socketHarness( t );
	/** @type {{ error: string, frame: any, reason: any }[]} */
	const failures = [];
	const network = createNetwork( ( error, frame, reason ) => failures.push( { error, frame, reason } ) );
	network.webTransport( true );
	network.connect( "ws://localhost/transport/ws", "ticket" );
	for ( let i = 0; i < 100 && !transports[0]?.controller; i++ ) {
		await new Promise( resolve => setTimeout( resolve, 5 ) );
	}
	const transport = transports[0];
	transport.receive( welcome( false ) );
	await flushMicrotasks();
	network.enterWorld( "0", "Hero", "token" );
	return { network, transport, transports, sockets, failures };
}

test("an entry torn down unanswered sends the next connect to the socket", async t => {
	const { network, transports, sockets } = await openWtSession( t );
	// The world session's stall deadline tears the link down (cancel).
	network.disconnect();
	network.connect( "ws://localhost/transport/ws", "ticket" );
	await flushMicrotasks();
	assert.equal( transports.length, 1, "no second WebTransport attempt" );
	assert.equal( sockets.length, 1, "the retry rides the socket" );
	network.dispose();
});

test("a WebTransport that dies mid-entry sends the next connect to the socket", async t => {
	const { network, transport, transports, sockets, failures } = await openWtSession( t );
	transport.abort( new Error( "connection dropped" ) );
	await flushMicrotasks();
	network.drain( () => {} );
	assert.equal( failures.length, 1 );
	assert.equal( failures[0].error, "Transport connection closed" );
	network.connect( "ws://localhost/transport/ws", "ticket" );
	await flushMicrotasks();
	assert.equal( transports.length, 1, "no second WebTransport attempt" );
	assert.equal( sockets.length, 1, "the retry rides the socket" );
	network.dispose();
});

test("an answered WebTransport entry keeps the path for the next connect", async t => {
	const { network, transport, transports, sockets } = await openWtSession( t );
	transport.receive( enterWorldAnswer() );
	await flushMicrotasks();
	network.disconnect();
	network.connect( "ws://localhost/transport/ws", "ticket" );
	for ( let i = 0; i < 100 && !transports[1]?.controller; i++ ) {
		await new Promise( resolve => setTimeout( resolve, 5 ) );
	}
	assert.equal( transports.length, 2, "the answered path is attempted again" );
	assert.equal( sockets.length, 0, "no socket was needed" );
	network.dispose();
});

test("a socket entry torn down unanswered never indicts WebTransport", async t => {
	const transports = wtHarness( t ), sockets = socketHarness( t );
	const network = createNetwork( () => {} );
	network.connect( "ws://localhost/transport/ws", "ticket" );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( welcome( false ) );
	network.enterWorld( "0", "Hero", "token" );
	network.disconnect();
	network.webTransport( true );
	network.connect( "ws://localhost/transport/ws", "ticket" );
	for ( let i = 0; i < 100 && !transports[0]?.controller; i++ ) {
		await new Promise( resolve => setTimeout( resolve, 5 ) );
	}
	assert.equal( transports.length, 1, "QUIC is attempted after a socket stall" );
	assert.equal( sockets.length, 1, "no second socket was opened" );
	network.dispose();
});
