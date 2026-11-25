/*
===========================================================================

transport-harness.mjs - stand-ins for the browser's two transports

A WebSocket and a WebTransport global a test drives by hand, and the bytes
their server side would send. Nothing here does I/O.

===========================================================================
*/

/*
================
socketHarness

A WebSocket global recording each construction and what is sent.
================
*/
export function socketHarness( t ) {
	const sockets = [];
	class Socket {
		static OPEN = 1;
		readyState = 1;
		bufferedAmount = 0;
		sent = [];
		closed = false;
		constructor() {
			sockets.push( this );
		}
		send( bytes ) {
			this.sent.push( bytes.slice() );
		}
		close() {
			this.closed = true;
		}
		receive( bytes ) {
			this.onmessage?.( { data: bytes.buffer } );
		}
	}
	const original = Object.getOwnPropertyDescriptor( globalThis, "WebSocket" );
	Object.defineProperty( globalThis, "WebSocket", { value: Socket, configurable: true, writable: true } );
	t.after( () => Object.defineProperty( globalThis, "WebSocket", original ) );
	return sockets;
}
/*
================
welcome

The server's welcome message.
================
*/
export function welcome() {
	const bytes = new Uint8Array( 29 );
	bytes[0] = 2;
	bytes[2] = 2;
	bytes[12] = 16;
	return bytes;
}
/*
================
settle
================
*/
export const settle = () => new Promise( resolve => setImmediate( resolve ) );
/*
================
flush

Let the WebTransport attempt's promise chain run to its next rest.
================
*/
export async function flush( rounds = 8 ) {
	for ( let i = 0; i < rounds; i++ ) await settle();
}
/*
================
record

One u32-length-prefixed stream record carrying a frame (server envelope).
================
*/
export function record( frame ) {
	const out = new Uint8Array( frame.byteLength + 4 );
	new DataView( out.buffer ).setUint32( 0, frame.byteLength, true );
	out.set( frame, 4 );
	return out;
}
/*
================
wtHarness

A controllable WebTransport global: sessions the test drives by hand.
Written chunks are recorded; nothing does I/O of its own. The cert-hash
fetch answers from fetchDoc, or not at all (a 404) when it is null.
================
*/
export function wtHarness( t, fetchDoc = null ) {
	const transports = [];
	class Transport {
		constructor( url, options ) {
			this.url = String( url );
			this.options = options;
			this.written = [];
			this.closedFlag = false;
			this.ready = new Promise( ( resolve, reject ) => {
				this.readyResolve = resolve;
				this.readyReject = reject;
			} );
			this.closed = new Promise( ( resolve, reject ) => {
				this.closedResolve = resolve;
				this.closedReject = reject;
			} );
			transports.push( this );
		}
		/*
		================
		createBidirectionalStream
		================
		*/
		createBidirectionalStream() {
			const readable = new ReadableStream( {
				/*
				================
				start
				================
				*/
				start: controller => {
					this.inbound = controller;
				}
			} );
			const writable = new WritableStream( {
				/*
				================
				write
				================
				*/
				write: chunk => {
					this.written.push( chunk.slice() );
				}
			} );
			return Promise.resolve( { readable, writable } );
		}
		/*
		================
		close
		================
		*/
		close() {
			this.closedFlag = true;
			// A browser settles closed with the close info, here
			// the clean application code 0; an abrupt end rejects
			// instead (abort below). It never resolves with nothing.
			this.closedResolve( { closeCode: 0, reason: "" } );
		}
		/*
		================
		abort
		================
		*/
		abort( error ) {
			this.closedFlag = true;
			this.closedReject( error );
		}
		/*
		================
		push
		================
		*/
		push( bytes ) {
			this.inbound.enqueue( bytes );
		}
	}
	const original = Object.getOwnPropertyDescriptor( globalThis, "WebTransport" );
	Object.defineProperty( globalThis, "WebTransport", { value: Transport, configurable: true, writable: true } );
	t.after( () => {
		if ( original ) Object.defineProperty( globalThis, "WebTransport", original );
		else Reflect.deleteProperty( globalThis, "WebTransport" );
	} );
	t.mock.method(
		globalThis,
		"fetch",
		async () => fetchDoc ? { ok: true, json: async () => fetchDoc } : { ok: false }
	);
	return transports;
}
