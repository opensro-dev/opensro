/*
===========================================================================

network-close-detail.test.mjs - a lost connection says why (GH #1428)

The prod journal held hundreds of "Transport connection closed/failed"
incidents with no reason. The socket owner now reports the WebSocket close
code, reason and cleanliness, whether an error event came first, the
silence since the last inbound frame and the browser's online flag. The
error text the reconnect rule matches stays exactly as it was.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { closeDetail, createNetwork } = await import(
	"../../src/engine/runtime/simulation/worker/network/network.ts"
);

/*
================
socketHarness

A WebSocket stand-in whose close and error events the test fires.
================
*/
function socketHarness( t ) {
	const sockets = [];
	class Socket {
		static OPEN = 1;
		readyState = 1;
		bufferedAmount = 0;
		closed = false;
		/** @type {(( event: { data: ArrayBuffer; } ) => void) | undefined} */
		onmessage;
		constructor() {
			sockets.push( this );
		}
		send() {}
		close() {
			this.closed = true;
		}
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
welcome
================
*/
function welcome() {
	const bytes = new Uint8Array( 29 );
	bytes[0] = 2;
	bytes[2] = 2;
	bytes[12] = 16;
	return bytes;
}

/*
================
openSession

A welcomed session and the failures it reports.
================
*/
function openSession( t ) {
	const sockets = socketHarness( t );
	/** @type {{ error: string, frame: any, reason: any }[]} */
	const failures = [];
	const network = createNetwork( ( error, frame, reason ) => failures.push( { error, frame, reason } ) );
	network.connect( "ws://localhost/transport", "ticket" );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( welcome() );
	return { network, socket, failures };
}

test("the detail names the close code, reason, cleanliness, silence and online state", () => {
	assert.equal(
		closeDetail( {
			code: 1006,
			reason: "",
			wasClean: false,
			errored: true,
			sinceLastFrameMs: 31200,
			openForMs: 812000,
			online: true
		} ),
		"WebSocket 1006, not clean, after an error event, last frame 31.2 s ago, open 812.0 s, online"
	);
	assert.equal(
		closeDetail( {
			code: 1001,
			reason: "going\naway é",
			wasClean: true,
			errored: false,
			sinceLastFrameMs: null,
			openForMs: null,
			online: false
		} ),
		'WebSocket 1001 "going?away ?", clean, no frame received, never opened, offline'
	);
	// A long server reason is cut, not passed through whole.
	const long = closeDetail( {
		code: 4000,
		reason: "x".repeat( 300 ),
		wasClean: true,
		errored: false,
		sinceLastFrameMs: 0,
		openForMs: 0,
		online: true
	} );
	assert.ok( long.includes( '"' + "x".repeat( 64 ) + '"' ) && !long.includes( "x".repeat( 65 ) ) );
});

test("a plain close ends with the same error text and carries the detail", t => {
	const { network, socket, failures } = openSession( t );
	socket.onclose( { code: 1006, reason: "", wasClean: false } );
	network.drain( () => {} );
	assert.equal( failures.length, 1 );
	// The reconnect rule (world.ts) matches this text exactly.
	assert.equal( failures[0].error, "Transport connection closed" );
	assert.equal( failures[0].reason.code, "connection_closed" );
	assert.match( failures[0].reason.detail, /^WebSocket 1006, not clean, last frame \d+\.\d s ago, open \d+\.\d s/ );
	network.dispose();
});

test("a clean normal close is an expected goodbye, not a lost connection", t => {
	const { network, socket, failures } = openSession( t );
	socket.onclose( { code: 1000, reason: "", wasClean: true } );
	network.drain( () => {} );
	assert.equal( failures.length, 1 );
	// The error text the reconnect rule matches stays exactly as it was;
	// only the category (and with it the incident) changes.
	assert.equal( failures[0].error, "Transport connection closed" );
	assert.equal( failures[0].reason.category, "expected", "a clean goodbye files no incident" );
	assert.equal( failures[0].reason.code, "connection_closed" );
	assert.equal( failures[0].reason.message, "Your session ended." );
	assert.match( failures[0].reason.detail, /^WebSocket 1000, clean, / );
	network.dispose();
});

test("a clean close with any other code, or an unclean normal close, stays unknown", t => {
	for ( const close of [ { code: 1001, wasClean: true }, { code: 1000, wasClean: false } ] ) {
		const { network, socket, failures } = openSession( t );
		socket.onclose( { code: close.code, reason: "", wasClean: close.wasClean } );
		network.drain( () => {} );
		assert.equal( failures.length, 1 );
		assert.equal(
			failures[0].reason.category,
			"unknown",
			`code ${close.code} clean=${close.wasClean} is not the proven goodbye`
		);
		assert.equal( failures[0].reason.code, "connection_closed" );
		assert.equal( failures[0].reason.message, "Connection lost. The cause is unknown." );
		network.dispose();
	}
});

test("an error event waits for the close, which reports the failure with its code", t => {
	const { network, socket, failures } = openSession( t );
	socket.onerror();
	// The error event carries no code; the close that always follows does.
	assert.equal( failures.length, 0 );
	socket.onclose( { code: 1006, reason: "", wasClean: false } );
	assert.equal( failures.length, 1 );
	assert.equal( failures[0].error, "Transport connection failed" );
	assert.equal( failures[0].reason.category, "connection" );
	assert.equal( failures[0].reason.code, "connection_failed" );
	assert.match( failures[0].reason.detail, /^WebSocket 1006, not clean, after an error event, / );
	network.dispose();
});

test("a socket closed before it opened reports that it was never open", t => {
	const sockets = socketHarness( t );
	/** @type {{ error: string, reason: any }[]} */
	const failures = [];
	const network = createNetwork( ( error, _frame, reason ) => failures.push( { error, reason } ) );
	network.connect( "ws://localhost/transport", "ticket" );
	sockets[0].onerror();
	sockets[0].onclose( { code: 1006, reason: "", wasClean: false } );
	assert.equal( failures[0].error, "Transport connection failed" );
	assert.match( failures[0].reason.detail, /no frame received, never opened/ );
	network.dispose();
});
