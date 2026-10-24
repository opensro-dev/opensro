/*
===========================================================================

stream-position.test.mjs - the client half of exactly-once resume (#1466)

The network owner counts the frames its consumer applied (never the ones
merely received), acknowledges that position about once a second, presents
it in the HELLO of a resume, keeps it across a resumed WELCOME and restarts
it on a fresh one. The counted opcode set is the server's, read from the
fixture its own test reads.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createCodec, MAX_RESERVED_OPCODE, OP_STREAM_ACK } = await load(
	"src/engine/runtime/simulation/worker/network/codec/codec.ts"
);
const { createNetwork } = await load( "src/engine/runtime/simulation/worker/network/network.ts" );
const OPCODES = JSON.parse(
	readFileSync( path.join( root, "../server/internal/transport/testdata/stream_position_opcodes.json" ), "utf8" )
);
const RESUME_TOKEN = new Uint8Array( 16 ).fill( 7 );

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
	t.after( () => Object.defineProperty( globalThis, "WebSocket", original ) );
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

function gameFrame( n ) {
	return Uint8Array.of( 0x26, 0x31, n );
}

function u32( bytes, at ) {
	return new DataView( bytes.buffer, bytes.byteOffset ).getUint32( at, true );
}

function acks( socket ) {
	return socket.sent.filter( b => b[0] === OP_STREAM_ACK && b[1] === 0 ).map( b => u32( b, 2 ) );
}

test("the counted opcode set is the server's", () => {
	for ( const op of OPCODES.uncounted ) assert.ok( op <= MAX_RESERVED_OPCODE, `0x${op.toString( 16 )}` );
	for ( const op of OPCODES.counted ) assert.ok( op > MAX_RESERVED_OPCODE, `0x${op.toString( 16 )}` );
});

test("a resume HELLO carries the position; a fresh one carries none", () => {
	const codec = createCodec();
	assert.equal( codec.hello( "abc" ).payload.length, 7 );
	const hello = codec.hello( "abc", RESUME_TOKEN, 0x01020304 ).payload;
	assert.equal( hello.length, 4 + 16 + 3 + 4 );
	assert.equal( u32( hello, hello.length - 4 ), 0x01020304 );
	assert.deepEqual( [ ...codec.encode( codec.streamAck( 2 ) ) ], [ 0x0b, 0, 2, 0, 0, 0 ] );
});

test("only applied frames count, and a resume presents and keeps that position", t => {
	const sockets = socketHarness( t ), failures = [];
	let clock = 0;
	t.mock.method( performance, "now", () => clock );
	const network = createNetwork( e => failures.push( e ) );
	network.connect( "ws://localhost/t", "abc" );
	sockets[0].onopen();
	sockets[0].receive( welcome( false ) );
	for ( let n = 1; n <= 3; n++ ) sockets[0].receive( gameFrame( n ) );
	// The consumer applies two; the third waits for data.
	let applied = 0;
	network.drain( f => f.opcode <= MAX_RESERVED_OPCODE || ++applied <= 2 );
	assert.deepEqual( acks( sockets[0] ), [ 2 ], "the welcome is not counted, the held frame is not" );
	sockets[0].receive( gameFrame( 4 ) );
	network.drain( () => true );
	clock += 500;
	network.drain( () => true );
	assert.deepEqual( acks( sockets[0] ), [ 2 ], "at most one acknowledgement a second" );
	clock += 600;
	network.drain( () => true );
	assert.deepEqual( acks( sockets[0] ), [ 2, 4 ] );
	// The socket dies with one frame received but not applied.
	sockets[0].receive( gameFrame( 5 ) );
	network.connect( "ws://localhost/t", "abc", RESUME_TOKEN );
	sockets[1].onopen();
	const hello = sockets[1].sent[0];
	assert.equal( u32( hello, hello.length - 4 ), 4, "the received-only frame is asked for again" );
	sockets[1].receive( welcome( true ) );
	sockets[1].receive( gameFrame( 5 ) );
	clock += 1000;
	network.drain( () => true );
	assert.deepEqual( acks( sockets[1] ), [ 5 ] );
	assert.deepEqual( failures, [] );
});

test("a fresh WELCOME after a refused resume restarts the position", t => {
	const sockets = socketHarness( t );
	let clock = 0;
	t.mock.method( performance, "now", () => clock );
	const network = createNetwork( () => {} );
	network.connect( "ws://localhost/t", "abc" );
	sockets[0].onopen();
	sockets[0].receive( welcome( false ) );
	sockets[0].receive( gameFrame( 1 ) );
	sockets[0].receive( gameFrame( 2 ) );
	network.drain( () => true );
	network.connect( "ws://localhost/t", "abc", RESUME_TOKEN );
	sockets[1].onopen();
	sockets[1].receive( welcome( false ) );
	sockets[1].receive( gameFrame( 1 ) );
	clock += 1000;
	network.drain( () => true );
	assert.deepEqual( acks( sockets[1] ), [ 1 ] );
	network.connect( "ws://localhost/t", "abc" );
	sockets[2].onopen();
	assert.equal( sockets[2].sent[0].length, 2 + 7, "a token-less HELLO has no position" );
});
