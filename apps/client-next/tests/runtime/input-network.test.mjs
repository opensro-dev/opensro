/*
===========================================================================

input-network.test.mjs - tests for the client modules it imports

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
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createInput } = await load( "src/engine/runtime/input/input.ts" );
const { createSimulationInput } = await load( "src/engine/runtime/simulation/worker/input/input.ts" );
const { createCodec } = await load( "src/engine/runtime/simulation/worker/network/codec/codec.ts" );
const { createNetwork } = await load( "src/engine/runtime/simulation/worker/network/network.ts" );
test("Escape is acknowledged without a gameplay action", () => {
	// CGInterface_HandleEscapeKey (69F450) closes windows or opens the system
	// menu; it never cancels the running action.
	const host = createInput(), worker = createSimulationInput();
	host.accept( { kind: "key", code: "Escape", down: true, timeMs: 1 } );
	host.accept( { kind: "key", code: "Escape", down: true, timeMs: 2 } );
	worker.receive( host.drain() );
	assert.equal( worker.commit(), 2 );
	assert.equal( worker.lastAccepted(), 2 );
});
test("input preserves press/release order and acknowledges only committed batches", () => {
	const host = createInput(), worker = createSimulationInput();
	host.accept( { kind: "key", code: "KeyW", down: true, timeMs: 1 } );
	host.accept( { kind: "key", code: "KeyW", down: false, timeMs: 2 } );
	const batch = host.drain();
	assert.equal( host.drain(), null );
	assert.deepEqual( batch.commands.map( c => c.down ), [ true, false ] );
	worker.receive( batch );
	assert.equal( worker.lastAccepted(), 0 );
	assert.equal( worker.commit(), 2 );
	assert.throws( () => worker.receive( batch ), /sequence gap/ );
	host.accept( { kind: "release", timeMs: 3 } );
	worker.receive( host.drain() );
	assert.equal( worker.commit(), 3 );
});
test("malformed input is rejected atomically and overflow is explicit", () => {
	const worker = createSimulationInput();
	assert.throws( () =>
		worker.receive( {
			first: 1,
			last: 2,
			commands: [ { kind: "release", timeMs: 1, sequence: 1 }, {
				kind: "wheel",
				timeMs: 2,
				sequence: 2,
				delta: NaN
			} ]
		} )
	);
	assert.equal( worker.commit(), 0 );
	worker.receive( { first: 1, last: 1, commands: [ { kind: "release", timeMs: 1, sequence: 1 } ] } );
	assert.equal( worker.commit(), 1 );
	const host = createInput();
	for ( let i = 0; i < 513; i++ ) host.accept( { kind: "release", timeMs: i } );
	assert.match( host.error(), /overflow|capacity|exceeded/i );
});
test("codec matches server v2 golden frames and validates direction-specific limits", () => {
	const codec = createCodec();
	assert.deepEqual( [ ...codec.encode( codec.hello( "abc" ) ) ], [ 1, 0, 2, 0, 3, 0, 97, 98, 99 ] );
	assert.deepEqual( [ ...codec.encode( codec.enterWorld( "d", "c", "t" ) ) ], [
		6,
		0,
		1,
		0,
		100,
		1,
		0,
		99,
		1,
		0,
		116
	] );
	const bytes = Uint8Array.of( 99, 0xE3, 0x30, 42, 99 );
	const decoded = codec.decode( bytes.subarray( 1, 4 ) );
	assert.equal( decoded.opcode, 0x30E3 );
	assert.deepEqual( [ ...decoded.payload ], [ 42 ] );
	assert.throws( () => codec.decode( new Uint8Array( 1 ) ) );
	assert.equal( codec.decode( new Uint8Array( 16 << 20 ) ).payload.length, (16 << 20) - 2 );
	assert.throws( () => codec.decode( new Uint8Array( (16 << 20) + 1 ) ) );
	assert.throws( () => codec.encode( { opcode: 256, payload: new Uint8Array( 65535 ) } ) );
	assert.throws( () => codec.hello( "" ) );
	assert.throws( () => codec.hello( "x", new Uint8Array( 15 ) ) );
	const welcome = new Uint8Array( 27 );
	welcome[0] = 2;
	welcome[10] = 16;
	new DataView( welcome.buffer ).setBigUint64( 2, 9007199254740993n, true );
	assert.equal( codec.welcome( welcome ).sessionId, 9007199254740993n );
	welcome[1] = 2;
	assert.throws( () => codec.welcome( welcome ) );
});
function socketHarness( t ) {
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
function welcome() {
	const bytes = new Uint8Array( 29 );
	bytes[0] = 2;
	bytes[2] = 2;
	bytes[12] = 16;
	return bytes;
}
test("network handshake, ordered tick ingress, ping and disposal have one owner", t => {
	const sockets = socketHarness( t ), failures = [], frames = [];
	const network = createNetwork( e => failures.push( e ) );
	network.connect( "ws://localhost/transport", "abc" );
	const socket = sockets[0];
	assert.throws( () => network.send( { opcode: 256, payload: new Uint8Array() } ), /handshake/ );
	socket.onopen();
	assert.equal( socket.sent[0][0], 1 );
	socket.receive( welcome() );
	socket.receive( Uint8Array.of( 0, 1, 11 ) );
	socket.receive( Uint8Array.of( 1, 1, 12 ) );
	assert.equal( frames.length, 0 );
	network.drain( frame => frames.push( frame.opcode ) );
	assert.deepEqual( frames, [ 2, 256, 257 ] );
	socket.receive( Uint8Array.of( 3, 0, 99 ) );
	assert.deepEqual( [ ...socket.sent.at( -1 ) ], [ 4, 0, 99 ] );
	const stale = socket.onmessage;
	network.dispose();
	stale( { data: Uint8Array.of( 0, 1 ).buffer } );
	network.drain( () => assert.fail( "late packet" ) );
	assert.ok( socket.closed );
	assert.deepEqual( failures, [] );
});
test("network rejects oversized ingress and send backpressure instead of silently dropping commands", t => {
	const sockets = socketHarness( t ), failures = [];
	const network = createNetwork( e => failures.push( e ) );
	network.connect( "ws://localhost/transport", "abc" );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( welcome() );
	socket.bufferedAmount = 1 << 20;
	assert.throws( () => network.send( { opcode: 256, payload: new Uint8Array() } ), /backlog/ );
	socket.receive( new Uint8Array( (16 << 20) + 1 ) );
	assert.match( failures[0], /length/ );
	assert.ok( socket.closed );
});

test("server goodbye preserves its reason and drains accepted packets before ending", t => {
	const sockets = socketHarness( t ), failures = [], seen = [];
	const reasons = [];
	const network = createNetwork( ( e, _frame, reason ) => {
		failures.push( e );
		reasons.push( reason );
	} );
	network.connect( "ws://localhost/transport", "ticket" );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( welcome() );
	socket.receive( Uint8Array.of( 0, 1, 11 ) );
	socket.receive( Uint8Array.of( 5, 0, 5 ) );
	assert.ok( socket.closed );
	assert.deepEqual( failures, [] );
	network.drain( f => {
		if ( f.opcode === 256 ) return false;
		seen.push( f.opcode );
	} );
	assert.deepEqual( failures, [] );
	network.drain( f => {
		seen.push( f.opcode );
	} );
	assert.deepEqual( seen, [ 2, 256 ] );
	assert.deepEqual( failures, [ "Server ended transport session: shutdown (5)" ] );
	assert.equal( reasons[0].category, "expected" );
	assert.equal( reasons[0].code, "server_bye_5" );
	network.dispose();
});

test("pre-welcome refusal retains the server reason and malformed goodbye fails closed", t => {
	const sockets = socketHarness( t ), failures = [];
	const network = createNetwork( e => failures.push( e ) );
	for (
		const [bytes, expected] of [ [ Uint8Array.of( 5, 0, 8 ), /unauthorized \(8\)/ ], [
			Uint8Array.of( 5, 0 ),
			/Invalid transport goodbye/
		], [ Uint8Array.of( 5, 0, 5, 0 ), /Invalid transport goodbye/ ] ]
	) {
		network.connect( "ws://localhost/transport", "ticket" );
		const socket = sockets.at( -1 );
		socket.onopen();
		socket.receive( bytes );
		network.drain( () => assert.fail( "refusal admitted as game state" ) );
		assert.match( failures.at( -1 ), expected );
		assert.ok( socket.closed );
	}
	network.dispose();
});

test("native sight modes constrain pitch, follow heading and preserve free yaw", () => {
	const input = createInput(), start = input.camera().yaw;
	input.accept( { kind: "pointer", x: 0, y: 0, buttons: 2, timeMs: 1 } );
	const pitch = input.camera().pitch;
	input.sight( 2 );
	input.accept( { kind: "pointer", x: 20, y: 20, buttons: 2, timeMs: 2 } );
	assert.equal( input.camera().pitch, pitch );
	assert.equal( input.camera().yaw, start + .1 );
	input.sight( 0 );
	input.accept( { kind: "pointer", x: 40, y: 40, buttons: 2, timeMs: 3 } );
	assert.ok( input.camera().pitch > pitch );
	assert.equal( input.camera( 2 ).yaw, start + .1 + .1 );
	input.sight( 1 );
	assert.equal( input.camera( 2 ).yaw, Math.fround( 1.5700000524520874 - 2 + 1.5707963705062866 ) - Math.PI );
	const followed = input.camera().yaw;
	input.sight( 0 );
	assert.equal( input.camera( 0 ).yaw, followed );
	assert.throws( () => input.sight( 3 ), /Invalid sight/ );
});

// 67CCA0: mode 1 orbits while the wheel button is held (buttons 4); the
// right button uses the mouse quickslot and the left button moves.
test("alternate native mouse mode orbits with the wheel button while the wheel still zooms", () => {
	const input = createInput(), start = input.camera().yaw;
	input.mouseMode( 1 );
	for ( const buttons of [ 2, 1 ] ) {
		input.accept( { kind: "pointer", x: 0, y: 0, buttons, timeMs: 1 } );
		input.accept( { kind: "pointer", x: 20, y: 20, buttons, timeMs: 2 } );
		assert.equal( input.camera().yaw, start, `buttons ${buttons} must not orbit in mode 1` );
		input.accept( { kind: "release", timeMs: 3 } );
	}
	input.accept( { kind: "pointer", x: 0, y: 0, buttons: 4, timeMs: 4 } );
	input.accept( { kind: "pointer", x: 20, y: 20, buttons: 4, timeMs: 5 } );
	assert.equal( input.camera().yaw, start + .1 );
	input.accept( { kind: "wheel", delta: 120, timeMs: 6 } );
	assert.equal( input.camera().distance, 86 );
});

test("drop-name hold follows the configurable binding and clears on release, focus loss and rebinding", () => {
	const input = createInput(), key = ( code, down ) => input.accept( { kind: "key", code, down, timeMs: 0 } );
	assert.equal( input.dropNamesHeld(), false );
	key( "KeyZ", true );
	assert.equal( input.dropNamesHeld(), true );
	key( "KeyX", false );
	assert.equal( input.dropNamesHeld(), true );
	key( "KeyZ", false );
	assert.equal( input.dropNamesHeld(), false );
	key( "KeyZ", true );
	input.accept( { kind: "release", timeMs: 1 } );
	assert.equal( input.dropNamesHeld(), false );
	input.dropNameBinding( 66 );
	key( "KeyZ", true );
	assert.equal( input.dropNamesHeld(), false );
	key( "KeyB", true );
	assert.equal( input.dropNamesHeld(), true );
	input.dropNameBinding( 0 );
	assert.equal( input.dropNamesHeld(), false );
	key( "KeyB", true );
	assert.equal( input.dropNamesHeld(), false );
});

test("network pause retains FIFO packets through repeated ticks without replaying consumed prefixes", t => {
	const sockets = socketHarness( t ), failures = [], seen = [];
	const network = createNetwork( e => failures.push( e ) );
	network.connect( "ws://localhost/transport", "ticket" );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( welcome() );
	socket.receive( Uint8Array.of( 0, 1, 11 ) );
	socket.receive( Uint8Array.of( 1, 1, 12 ) );
	network.drain( f => {
		if ( f.opcode === 256 ) return false;
		seen.push( f.opcode );
	} );
	for ( let i = 0; i < 10; i++ ) network.drain( () => false );
	assert.deepEqual( seen, [ 2 ] );
	network.drain( f => {
		seen.push( f.opcode );
	} );
	network.drain( () => assert.fail( "duplicate" ) );
	assert.deepEqual( seen, [ 2, 256, 257 ] );
	assert.deepEqual( failures, [] );
	network.dispose();
});

test("native V blind action is held, rebindable, and released on focus loss", () => {
	const input = createInput(), key = ( code, down ) => input.accept( { kind: "key", code, down, timeMs: 0 } );
	key( "KeyV", true );
	key( "KeyV", true );
	assert.equal( input.blindHeld(), true );
	key( "KeyX", false );
	assert.equal( input.blindHeld(), true );
	key( "KeyV", false );
	assert.equal( input.blindHeld(), false );
	key( "KeyV", true );
	input.accept( { kind: "release", timeMs: 1 } );
	assert.equal( input.blindHeld(), false );
	input.blindBinding( 66 );
	key( "KeyV", true );
	assert.equal( input.blindHeld(), false );
	key( "KeyB", true );
	assert.equal( input.blindHeld(), true );
	input.blindBinding( 0 );
	assert.equal( input.blindHeld(), false );
});
test("a frame the consumer cannot apply ends the session and names that frame for the incident report", t => {
	const sockets = socketHarness( t ), failures = [];
	const network = createNetwork( ( error, frame, reason ) => failures.push( { error, frame, reason } ) );
	network.connect( "ws://localhost/transport", "abc" );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( welcome() );
	socket.receive( Uint8Array.of( 0, 1, 11 ) );
	socket.receive( Uint8Array.of( 0x6f, 0x37, 1, 2, 3 ) );
	network.drain( frame => {
		if ( frame.opcode === 0x376f ) throw Error( "Invalid movement speed channels" );
	} );
	assert.equal( failures.length, 1 );
	assert.match( failures[0].error, /^Packet application failed: Error: Invalid movement speed channels$/ );
	assert.equal( failures[0].frame.opcode, 0x376f );
	assert.equal( failures[0].reason.category, "software" );
	assert.equal( failures[0].reason.code, "packet_application_failed" );
	assert.match( failures[0].reason.stack, /Invalid movement speed channels/ );
	assert.deepEqual( [ ...failures[0].frame.payload ], [ 1, 2, 3 ] );
	assert.ok( socket.closed );
});

/*
================
third-person rear hemisphere
================
*/
test("heading-locked camera stays behind the player through a complete turn", () => {
	const input = createInput();
	input.sight( 1 );
	for ( let i = 0; i < 16; i++ ) {
		const bearing = i * Math.PI / 8;
		const camera = input.camera( (bearing + Math.PI / 2) % (2 * Math.PI) );
		const alongHeading = Math.sin( camera.yaw ) * Math.cos( bearing ) +
			Math.cos( camera.yaw ) * Math.sin( bearing );
		assert.ok( alongHeading < -.999, "camera eye must stay in the rear hemisphere" );
	}
});

/*
================
Gameplay ping lifecycle
================
*/
test("gameplay ping matches echo tokens, expires and resets on reconnect", t => {
	const sockets = socketHarness( t );
	const failures = [];
	const network = createNetwork( error => failures.push( error ) );
	let clock = 100;
	t.mock.method( performance, "now", () => clock );
	network.connect( "ws://localhost/world", "ticket" );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( welcome() );
	assert.equal( network.pingMs(), null );
	const ping = socket.sent.at( -1 );
	assert.equal( ping[0], 3 );
	const wrong = ping.slice();
	wrong[0] = 4;
	wrong[2] ^= 1;
	clock += 42;
	socket.receive( wrong );
	assert.equal( network.pingMs(), null );
	const pong = ping.slice();
	pong[0] = 4;
	socket.receive( pong );
	assert.equal( network.pingMs(), 42 );
	clock += 5;
	socket.receive( pong );
	assert.equal( network.pingMs(), 42 );
	clock += 15000;
	assert.equal( network.pingMs(), null );
	network.disconnect();
	assert.equal( network.pingMs(), null );
	network.connect( "ws://localhost/world", "ticket" );
	sockets[1].onopen();
	sockets[1].receive( welcome() );
	assert.equal( network.pingMs(), null );
	sockets[1].receive( pong );
	assert.equal( network.pingMs(), null );
	network.dispose();
	assert.deepEqual( failures, [] );
});
