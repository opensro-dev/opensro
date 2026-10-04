/*
===========================================================================

world.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { defined } from "../helpers/defined.mjs";
/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createEntities } = await load( "src/engine/runtime/simulation/worker/session/world/entities/entities.ts" );
const { createWorldSession } = await load( "src/engine/runtime/simulation/worker/session/world/world.ts" );
const { createSessionHttp } = await load( "src/engine/runtime/simulation/worker/session/http/http.ts" );
const { createPresentation } = await load( "src/engine/runtime/presentation/presentation.ts" );
const { createSimulationInput } = await load( "src/engine/runtime/simulation/worker/input/input.ts" );

test("journal admits the expanded startup catalogue but still rejects excessive queued bytes atomically", () => {
	const owner = createEntities();
	const state = { localGid: 1, skillCatalog: [ { id: 1, name: "x".repeat( 10 << 20 ) } ] };
	owner.publish( { kind: "gameplay", state } );
	assert.throws(
		() =>
			owner.publish( {
				kind: "gameplay",
				state: { localGid: 2, skillCatalog: [ { id: 2, name: "y".repeat( 7 << 20 ) } ] }
			} ),
		/journal backlog exceeded/
	);
	const batch = owner.take();
	assert.equal( batch.events.length, 1 );
	assert.equal( batch.events[0].state, state );
	owner.ack( batch.sequence );
	owner.publish( { kind: "gameplay", state: { localGid: 1 } } );
	assert.equal( owner.take().events.length, 1 );
});
test("world reset clears old projection and preserves subsequent events atomically", () => {
	const p = createPresentation();
	p.apply( {
		sequence: 1,
		events: [ { kind: "bootstrap", value: { old: true } }, { kind: "gameplay", state: { localGid: 1 } }, {
			kind: "native",
			opcode: 256,
			payload: new Uint8Array( [ 1 ] )
		} ]
	} );
	assert.equal( p.apply( { sequence: 2, events: [ { kind: "reset", epoch: 1 } ] } ), true );
	assert.equal( p.bootstrap(), null );
	assert.equal( p.gameplay(), null );
	assert.deepEqual( p.takeNative(), [] );
	assert.throws( () =>
		p.apply( { sequence: 3, events: [ { kind: "reset", epoch: 2 }, { kind: "synchronized", epoch: 1 } ] } )
	);
	assert.equal(
		p.apply( {
			sequence: 3,
			events: [ { kind: "reset", epoch: 2 }, { kind: "bootstrap", value: { next: true } } ]
		} ),
		true
	);
	assert.deepEqual( p.bootstrap(), { next: true } );
	p.dispose();
	assert.equal( p.gameplay(), null );
});
const fixture = JSON.parse(
	fs.readFileSync(
		path.resolve( root, "../server/internal/game/enterworld/testdata/monster_spawn_fixture.json" ),
		"utf8"
	)
);
const bootstrap = {
	protocolVersion: 2,
	nativeResult: 1,
	refObjSnapshot: fixture.refObjSnapshot,
	localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 25256, x: 1, y: 2, z: 3, angle: 0 } }
};
const rows = fixture.packets.map( p => ({ opcode: p.opcode, payload: Buffer.from( p.payloadHex, "hex" ) }) );
/*
================
flush
================
*/
function flush( owner ) {
	const batch = owner.take();
	if ( batch ) owner.ack( batch.sequence );
	return batch;
}

test("body state follows the reliable entity journal without changing life or membership", () => {
	const owner = createEntities();
	owner.bootstrap( bootstrap );
	flush( owner );
	for ( const row of rows ) owner.receive( row );
	const spawn = flush( owner ).events.find( e => e.kind === "spawn" ).entity;
	const packet = value => {
		const payload = Buffer.alloc( 6 );
		payload.writeUInt32LE( spawn.gid );
		payload[4] = 4;
		payload[5] = value;
		return { opcode: 0x3122, payload };
	};
	owner.receive( packet( 4 ) );
	const invisible = owner.take(), state = invisible.events.find( e => e.kind === "state" ).entity;
	assert.equal( state.appearanceState[2], 4 );
	assert.equal( state.appearanceState[0], spawn.appearanceState[0] );
	assert.equal( owner.count(), 1 );
	owner.receive( packet( 4 ) );
	owner.ack( invisible.sequence );
	assert.equal( owner.take(), null, "repeated setter has no side effects" );
	owner.receive( packet( 0 ) );
	assert.equal( flush( owner ).events.find( e => e.kind === "state" ).entity.appearanceState[2], 0 );
	assert.equal( state.appearanceState[2], 4, "older detached journal remains unchanged" );
	owner.dispose();
});

test("stalled presentation retains one sampled pose per mover without dropping lifecycle changes", () => {
	const owner = createEntities();
	owner.bootstrap( { ...bootstrap, refObjSnapshot: [ { refObjId: 1, kind: "npc" } ] } );
	flush( owner );
	for ( let gid = 1; gid <= 140; gid++ ) {
		const p = Buffer.alloc( 49 );
		p.writeUInt32LE( 1 );
		p.writeUInt32LE( gid, 4 );
		p.writeUInt16LE( 257, 8 );
		p[25] = 3;
		p.writeFloatLE( 8, 32 );
		p.writeFloatLE( 22, 36 );
		p[45] = 1;
		owner.receive( { opcode: 0x30d7, payload: p }, 0 );
		const move = Buffer.alloc( 14 );
		move.writeUInt32LE( gid );
		move[4] = 1;
		move.writeUInt16LE( 257, 5 );
		move.writeInt16LE( 10000, 7 );
		owner.receive( { opcode: 0xb738, payload: move }, 0 );
	}
	const inflight = owner.take(), before = JSON.stringify( inflight );
	for ( let tick = 1; tick <= 600; tick++ ) owner.step( tick * 16 );
	assert.equal( JSON.stringify( inflight ), before, "offered batches are immutable" );
	assert.equal( owner.take(), null );
	owner.die( 1 );
	owner.step( 9616 );
	owner.ack( inflight.sequence );
	const next = owner.take();
	assert.equal( next.events.length, 141, "84,000 sampled poses collapse to 140 plus reliable death" );
	const states = next.events.filter( e => e.kind === "state" && e.entity.gid === 1 );
	assert.equal( states.length, 2 );
	assert.notEqual( states[0].entity.appearanceState?.[0], 2 );
	assert.equal( states[1].entity.appearanceState[0], 2 );
	assert.equal( next.events.find( e => e.entity?.gid === 2 ).entity.x, owner.read( 2 ).x );
	owner.ack( next.sequence );
	owner.step( 9632 );
	const after = flush( owner );
	assert.equal( after.events.length, 139 );
	owner.dispose();
});

test("reliable journal still rejects 8193 events and teardown never publishes a reset", () => {
	const owner = createEntities();
	for ( let i = 0; i < 8192; i++ ) {
		owner.publish( { kind: "native", opcode: 0x7777, payload: Uint8Array.of( i & 255 ) } );
	}
	assert.throws(
		() => owner.publish( { kind: "native", opcode: 0x7777, payload: new Uint8Array() } ),
		/8192 events queued/
	);
	assert.doesNotThrow( () => owner.dispose() );
	assert.equal( owner.take(), null );
});
test("native grouped region removals commit at finalize across packed and split chunks", () => {
	for ( const split of [ 1, 3, 4, 7, 8 ] ) {
		const owner = createEntities();
		owner.bootstrap( bootstrap );
		flush( owner );
		for ( const row of rows ) owner.receive( row );
		flush( owner );
		const gid = fixture.expect.gid;
		const ids = Buffer.alloc( 8 );
		ids.writeUInt32LE( gid );
		ids.writeUInt32LE( 0xfefefefe, 4 );
		owner.receive( { opcode: 0x30cb, payload: Uint8Array.of( 2, 2, 0 ) } );
		owner.receive( { opcode: 0x3417, payload: ids.subarray( 0, split ) } );
		owner.receive( { opcode: 0x3417, payload: ids.subarray( split ) } );
		assert.equal( owner.count(), 1 );
		assert.equal( owner.take(), null );
		owner.receive( { opcode: 0x330a, payload: new Uint8Array() } );
		assert.equal( owner.count(), 0 );
		assert.deepEqual( flush( owner ).events.map( e => e.kind ), [ "despawn", "despawn", "synchronized" ] );
		for ( const row of rows ) owner.receive( row );
		assert.equal( owner.count(), 1, "the returning region can respawn its identities" );
		owner.dispose();
	}
});
test("grouped removal count, incomplete data and operation bounds remain enforced", () => {
	const owner = createEntities();
	owner.bootstrap( bootstrap );
	flush( owner );
	assert.throws(
		() => owner.receive( { opcode: 0x30cb, payload: Uint8Array.of( 3, 0, 0 ) } ),
		/Invalid object-list/
	);
	owner.receive( { opcode: 0x30cb, payload: Uint8Array.of( 2, 1, 0 ) } );
	assert.throws( () => owner.receive( { opcode: 0x3417, payload: new Uint8Array( 5 ) } ), /declared count/ );
	owner.receive( { opcode: 0x3417, payload: Uint8Array.of( 1, 0, 0 ) } );
	assert.throws( () => owner.receive( { opcode: 0x330a, payload: new Uint8Array() } ), /Incomplete/ );
	owner.receive( { opcode: 0x3417, payload: Uint8Array.of( 0 ) } );
	owner.receive( { opcode: 0x330a, payload: new Uint8Array() } );
	flush( owner );
	owner.receive( { opcode: 0x30cb, payload: Uint8Array.of( 2, 0, 0 ) } );
	owner.receive( { opcode: 0x3417, payload: new Uint8Array() } );
	owner.receive( { opcode: 0x330a, payload: new Uint8Array() } );
	owner.dispose();
});
test("server-pinned monster rows commit atomically and synchronize only after acknowledgement", () => {
	const owner = createEntities();
	owner.bootstrap( bootstrap );
	flush( owner );
	owner.receive( rows[0] );
	owner.receive( rows[1] );
	assert.equal( owner.count(), 0 );
	assert.equal( owner.take(), null );
	owner.receive( rows[2] );
	assert.equal( owner.count(), 1 );
	assert.equal( owner.synchronized(), false );
	const batch = owner.take();
	assert.equal( owner.take(), null );
	const entity = batch.events.find( e => e.kind === "spawn" ).entity;
	for ( const key of [ "gid", "refObjId", "regionId", "x", "y", "z", "name" ] ) {
		assert.equal( entity[key], fixture.expect[key] );
	}
	assert.throws( () => owner.ack( batch.sequence + 1 ) );
	owner.ack( batch.sequence );
	assert.equal( owner.synchronized(), true );
});
test("truncated groups and duplicate spawn identities never publish a partial list", () => {
	const owner = createEntities();
	owner.bootstrap( bootstrap );
	flush( owner );
	owner.receive( rows[0] );
	assert.throws( () => owner.receive( rows[2] ), /Incomplete/ );
	assert.equal( owner.count(), 0 );
	assert.equal( owner.take(), null );
	const other = createEntities();
	other.bootstrap( bootstrap );
	flush( other );
	other.receive( { opcode: 0x30cb, payload: Uint8Array.of( 1, 2, 0 ) } );
	other.receive( rows[1] );
	other.receive( rows[1] );
	assert.throws( () => other.receive( rows[2] ), /Duplicate/ );
	assert.equal( other.count(), 0 );
});
test("lifecycle remains ordered across backpressure and presentation applies before ack", () => {
	const owner = createEntities(), view = createPresentation();
	owner.bootstrap( bootstrap );
	const first = owner.take();
	for ( const row of rows ) owner.receive( row );
	assert.equal( owner.take(), null );
	view.apply( first );
	owner.ack( first.sequence );
	const next = owner.take();
	view.apply( next );
	owner.ack( next.sequence );
	assert.equal( view.count(), 1 );
	const payload = Buffer.alloc( 4 );
	payload.writeUInt32LE( fixture.expect.gid );
	owner.receive( { opcode: 0x36ab, payload } );
	const removal = owner.take();
	view.apply( removal );
	owner.ack( removal.sequence );
	assert.equal( view.count(), 0 );
	assert.throws( () => view.apply( removal ), /gap/ );
});
test("unknown native families remain byte-exact in the acknowledged journal", () => {
	const owner = createEntities();
	const payload = Uint8Array.of( 8, 9, 10 );
	owner.receive( { opcode: 0x7777, payload } );
	payload[0] = 0;
	assert.deepEqual( [ ...owner.take().events[0].payload ], [ 8, 9, 10 ] );
});
test("object-list staging rejects oversized chunks before copying and releases its reservation", () => {
	const owner = createEntities();
	owner.receive( { opcode: 0x30cb, payload: Uint8Array.of( 1, 10, 0 ) } );
	const payload = new Uint8Array( (1 << 20) - 2 );
	for ( let i = 0; i < 7; i++ ) owner.receive( { opcode: 0x3417, payload } );
	let copied = false;
	payload.slice = () => {
		copied = true;
		throw new Error( "must not copy" );
	};
	assert.throws( () => owner.receive( { opcode: 0x3417, payload } ), /staging capacity/ );
	assert.equal( copied, false );
	assert.equal( owner.take(), null );
	owner.clear();
	flush( owner );
	owner.receive( { opcode: 0x30cb, payload: Uint8Array.of( 1, 0, 0 ) } );
	owner.receive( { opcode: 0x330a, payload: new Uint8Array() } );
	const batch = owner.take();
	owner.ack( batch.sequence );
	assert.equal( owner.synchronized(), true );
});
/*
================
socketHarness
================
*/
function socketHarness( t ) {
	const sockets = [];
	/*
================
Socket
================
	*/
	class Socket {
		static OPEN = 1;
		readyState = 1;
		bufferedAmount = 0;
		sent = [];
		constructor( url ) {
			this.url = url;
			sockets.push( this );
		}
		/*
================
send
================
		*/
		send( bytes ) {
			this.sent.push( bytes.slice() );
		}
		/*
================
close
================
		*/
		close() {}
		/*
================
receive
================
		*/
		receive( op, payload ) {
			const bytes = new Uint8Array( payload.length + 2 );
			new DataView( bytes.buffer ).setUint16( 0, op, true );
			bytes.set( payload, 2 );
			this.onmessage?.( { data: bytes.buffer } );
		}
	}
	const original = Object.getOwnPropertyDescriptor( globalThis, "WebSocket" );
	Object.defineProperty( globalThis, "WebSocket", { value: Socket, configurable: true } );
	t.after( () => Object.defineProperty( globalThis, "WebSocket", original ) );
	return sockets;
}
const settle = () => new Promise( r => setImmediate( r ) );
// Wall-clock bound for asynchronous admission work (digests, decoding).
const WORLD_ADMISSION_BUDGET_MS = 10000;
/*
================
welcome
================
*/
function welcome( resumed = false ) {
	const p = new Uint8Array( 27 );
	p[0] = 2;
	p[1] = +resumed;
	p[10] = 16;
	p.fill( 7, 11 );
	return p;
}
/*
================
entered
================
*/
function entered( value = bootstrap ) {
	const blob = Buffer.from( JSON.stringify( { v: 1, bootstrap: value } ) ), p = Buffer.alloc( 9 + blob.length );
	p[0] = 1;
	p.writeUInt32LE( blob.length, 5 );
	blob.copy( p, 9 );
	return p;
}

test("restart completion survives an immediate transport BYE and retires the admitted world once", async t => {
	const sockets = socketHarness( t ), world = createWorldSession( async () => "ticket" );
	world.enter( "fixture", "shard", "http://localhost:9000" );
	await settle();
	world.step( 1 );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( 2, welcome() );
	world.step( 2 );
	await settle();
	world.step( 3 );
	socket.receive( 7, entered() );
	for ( const row of rows ) socket.receive( row.opcode, row.payload );
	world.step( 4 );
	flush( world );
	world.step( 5 );
	world.ready();
	world.depart( 2 );
	assert.deepEqual( [ ...socket.sent.at( -1 ) ], [ 0xb7, 0x70, 2 ] );
	assert.equal( world.status().phase, "world" );
	socket.receive( 0xb0b7, Uint8Array.of( 1, 5, 2 ) );
	world.step( 100 );
	flush( world );
	world.step( 5100 );
	assert.equal( world.status().phase, "world" );
	assert.equal( world.takeDeparture(), 0 );
	flush( world );
	socket.receive( 0x315a, new Uint8Array() );
	socket.receive( 5, Uint8Array.of( 0 ) );
	world.step( 5200 );
	assert.equal( world.status().phase, "disconnected" );
	assert.equal( world.status().admitted, false );
	assert.equal( world.status().error, undefined );
	assert.equal( world.takeDeparture(), 2 );
	assert.equal( world.takeDeparture(), 0 );
	world.step( 9000 );
	assert.equal( sockets.length, 1 );
	world.dispose();
});
test("fresh admission on reconnect; WELCOME cannot reuse an old bootstrap barrier", async t => {
	const sockets = socketHarness( t ), mints = [];
	const world = createWorldSession( async kind => {
		mints.push( kind );
		return `ticket-${mints.length}`;
	} );
	world.enter( "fixture", "shard", "http://localhost:9000" );
	await settle();
	world.step( 1 );
	const first = sockets[0];
	assert.equal( first.url.toString(), "ws://localhost:9000/transport/ws" );
	first.onopen();
	first.receive( 2, welcome() );
	world.step( 2 );
	await settle();
	world.step( 3 );
	assert.equal( first.sent.at( -1 )[0], 6 );
	first.receive( 7, entered() );
	for ( const row of rows ) first.receive( row.opcode, row.payload );
	world.step( 4 );
	assert.equal( world.status().phase, "entering-world" );
	const batch = world.take();
	world.ack( batch.sequence );
	world.step( 5 );
	assert.equal( world.status().phase, "world" );
	first.receive( 0x32a6, Uint8Array.of( 123, 0, 0, 0, 0, 0, 0, 0 ) );
	world.step( 5 );
	flush( world );
	world.ready();
	first.bufferedAmount = 1 << 20;
	assert.throws( () => world.command( { kind: "select", gid: fixture.expect.gid } ), /outbound backlog/ );
	first.bufferedAmount = 0;
	const sent = first.sent.length;
	world.command( { kind: "select", gid: fixture.expect.gid } );
	assert.equal( first.sent.length, sent + 1 );
	const input = createSimulationInput();
	input.receive( {
		first: 1,
		last: 1,
		commands: [ { kind: "key", code: "Escape", down: true, timeMs: 5, sequence: 1 } ]
	} );
	const beforeEscape = first.sent.length;
	input.commit();
	// Escape is UI-only (69F450): no action cancel reaches the wire.
	assert.equal( first.sent.length, beforeEscape );
	assert.equal( input.lastAccepted(), 1 );
	world.disconnect();
	world.reconnect();
	await settle();
	world.step( 6 );
	const second = sockets[1];
	second.onopen();
	second.receive( 2, welcome( true ) );
	world.step( 7 );
	assert.equal( world.status().phase, "entering-world" );
	// Resumed server traffic may precede its fresh EnterWorld response.
	second.receive( 0x7777, Uint8Array.of( 1 ) );
	world.step( 8 );
	assert.equal( world.status().phase, "entering-world" );
	await settle();
	world.step( 9 );
	second.receive( 7, entered() );
	for ( const row of rows ) second.receive( row.opcode, row.payload );
	world.step( 10 );
	assert.equal( world.status().phase, "entering-world" );
	let pending;
	while ( (pending = world.take()) ) world.ack( pending.sequence );
	world.step( 11 );
	assert.equal( world.status().phase, "world" );
	assert.deepEqual( mints, [ "transport", "enterworld", "transport", "enterworld" ] );
	world.dispose();
});
test("logout/disposal rejects a late token and opens no socket", async t => {
	const sockets = socketHarness( t );
	let resolve;
	const world = createWorldSession( () => new Promise( r => resolve = r ) );
	world.enter( "fixture", "shard", "http://localhost:9000" );
	world.dispose();
	defined( resolve )( "late" );
	await settle();
	world.step( 1 );
	assert.equal( sockets.length, 0 );
});
test("unexpected close retries with a new ticket and rejects a lost resume", async t => {
	const sockets = socketHarness( t ), mints = [];
	const world = createWorldSession( async kind => {
		mints.push( kind );
		return "ticket";
	} );
	world.enter( "fixture", "shard", "http://localhost:9000" );
	await settle();
	world.step( 1 );
	const first = sockets[0];
	first.onopen();
	first.receive( 2, welcome() );
	world.step( 2 );
	await settle();
	world.step( 3 );
	first.receive( 7, entered() );
	for ( const row of rows ) first.receive( row.opcode, row.payload );
	world.step( 4 );
	flush( world );
	world.step( 5 );
	assert.equal( world.status().phase, "world" );
	first.onclose();
	world.step( 6 );
	assert.equal( world.status().phase, "reconnecting" );
	world.step( 255 );
	assert.equal( sockets.length, 1 );
	world.step( 256 );
	await settle();
	world.step( 257 );
	assert.equal( sockets.length, 2 );
	assert.deepEqual( mints, [ "transport", "enterworld", "transport" ] );
	sockets[1].onopen();
	sockets[1].receive( 2, welcome( false ) );
	world.step( 258 );
	assert.equal( world.status().phase, "disconnected" );
	assert.match( world.status().error, /could not resume/ );
	world.dispose();
});

test("a dropped session keeps retrying through the resume grace, then gives up", async t => {
	const sockets = socketHarness( t );
	const world = createWorldSession( async () => "ticket" );
	world.enter( "fixture", "shard", "http://localhost:9000" );
	await settle();
	world.step( 1 );
	const first = sockets[0];
	first.onopen();
	first.receive( 2, welcome() );
	world.step( 2 );
	await settle();
	world.step( 3 );
	first.receive( 7, entered() );
	for ( const row of rows ) first.receive( row.opcode, row.payload );
	world.step( 4 );
	flush( world );
	world.step( 5 );
	assert.equal( world.status().phase, "world" );
	first.onclose();
	world.step( 6 );
	assert.equal( world.status().phase, "reconnecting" );
	// Every attempt fails at once, as while the network is down.
	let clock = 7, failures = 0;
	while ( clock < 40000 && world.status().phase === "reconnecting" ) {
		world.step( clock );
		await settle();
		world.step( clock );
		const latest = sockets[sockets.length - 1];
		if ( sockets.length - 1 > failures ) {
			failures++;
			latest.onclose();
			world.step( clock );
		}
		clock += 50;
	}
	assert.ok( failures > 3, `retried ${failures} times, more than the old fixed three` );
	assert.equal( world.status().phase, "disconnected" );
	assert.ok( clock >= 30000 && clock < 40000, `gave up at ${clock} ms, after the 30 s resume grace` );
	world.dispose();
});

// EncodeCosSpawnBand2 and EncodeCosRideState are the server-owned wire authority.
test("COS spawn, ride state and mount despawn preserve reliable rider lifecycle", () => {
	const owner = createEntities();
	owner.bootstrap( {
		...bootstrap,
		refObjSnapshot: [ { refObjId: 2023, kind: "npc" }, { refObjId: 2183, kind: "cos", tidWord: 0x11c6 } ]
	} );
	flush( owner );
	/*
================
spawn
================
	*/
	function spawn( ref, gid, cos = false ) {
		const p = Buffer.alloc( cos ? 57 : 49 );
		p.writeUInt32LE( ref );
		p.writeUInt32LE( gid, 4 );
		p.writeUInt16LE( 257, 8 );
		p[25] = 1;
		p.writeFloatLE( 10, 32 );
		p.writeFloatLE( 20, 36 );
		p.writeFloatLE( 1, 40 );
		p[45] = 1;
		return { opcode: 0x30d7, payload: p };
	}
	owner.receive( spawn( 2023, 1 ) );
	const cosSpawn = spawn( 2183, 2, true );
	cosSpawn.payload[31] = 4;
	owner.receive( cosSpawn );
	const initial = flush( owner ).events.find( event => event.kind === "spawn" && event.entity.gid === 2 );
	assert.equal(
		initial.entity.appearanceState[2],
		4,
		"COS spawn initializes the body setter before a late-viewer ride"
	);
	const ride = Buffer.alloc( 10 );
	ride[0] = 1;
	ride.writeUInt32LE( 1, 1 );
	ride[5] = 1;
	ride.writeUInt32LE( 2, 6 );
	owner.receive( { opcode: 0xb4b5, payload: ride } );
	assert.equal( flush( owner ).events[0].entity.mountedOn, 2 );
	owner.receive( { opcode: 0xb4b5, payload: Uint8Array.of( 2, 4 ) } );
	assert.equal( owner.read( 1 ).mountedOn, 2, "a refused dismount preserves the ride binding" );
	for ( const payload of [ Uint8Array.of( 2 ), Uint8Array.of( 2, 4, 0 ), Uint8Array.of( 3, 4 ) ] ) {
		assert.throws( () => owner.receive( { opcode: 0xb4b5, payload } ), /ride state/ );
	}
	const invalid = Buffer.from( ride );
	invalid.writeUInt32LE( 1, 6 );
	assert.throws( () => owner.receive( { opcode: 0xb4b5, payload: invalid } ), /references absent/ );
	const despawn = Buffer.alloc( 4 );
	despawn.writeUInt32LE( 2 );
	owner.receive( { opcode: 0x36ab, payload: despawn } );
	const events = flush( owner ).events;
	assert.equal( events.find( event => event.kind === "state" ).entity.mountedOn, undefined );
	assert.equal( events.at( -1 ).kind, "despawn" );
	// 777F60 stores the mount gid unresolved: a ride ahead of its vehicle's
	// spawn binds the rider instead of failing the viewer's world session.
	owner.receive( { opcode: 0xb4b5, payload: ride } );
	assert.equal( owner.read( 1 ).mountedOn, 2, "a ride may precede its vehicle" );
	owner.receive( cosSpawn );
	assert.equal( owner.read( 1 ).mountedOn, 2 );
	owner.dispose();
});

test("motion preserves authoritative gait and mount metadata across ticks and stops", () => {
	const owner = createEntities();
	owner.bootstrap( {
		...bootstrap,
		refObjSnapshot: [ { refObjId: 1, kind: "npc" }, { refObjId: 2, kind: "cos", tidWord: 0x11c6 } ]
	} );
	flush( owner );
	/*
================
spawn
================
	*/
	function spawn( ref, gid, cos = false ) {
		const p = Buffer.alloc( cos ? 57 : 49 );
		p.writeUInt32LE( ref );
		p.writeUInt32LE( gid, 4 );
		p.writeUInt16LE( 257, 8 );
		p[25] = 3;
		p.writeFloatLE( 8, 32 );
		p.writeFloatLE( 22, 36 );
		p[45] = 1;
		owner.receive( { opcode: 0x30d7, payload: p }, 0 );
	}
	spawn( 1, 7 );
	spawn( 2, 8, true );
	const move = Buffer.alloc( 14 );
	move.writeUInt32LE( 7 );
	move[4] = 1;
	move.writeUInt16LE( 257, 5 );
	move.writeInt16LE( 100, 7 );
	owner.receive( { opcode: 0xb738, payload: move }, 0 );
	owner.step( 100 );
	owner.receive( { opcode: 0x3122, payload: Uint8Array.of( 7, 0, 0, 0, 1, 2 ) }, 100 );
	owner.step( 200 );
	assert.equal( owner.read( 7 ).movementMode, 2 );
	assert.ok( Math.abs( owner.read( 7 ).x - 3 ) < 1e-8, "22 units/sec for 100ms, then 8 units/sec for 100ms" );
	const ride = Buffer.alloc( 10 );
	ride[0] = 1;
	ride.writeUInt32LE( 7, 1 );
	ride[5] = 1;
	ride.writeUInt32LE( 8, 6 );
	owner.receive( { opcode: 0xb4b5, payload: ride }, 200 );
	owner.step( 250 );
	assert.equal( owner.read( 7 ).mountedOn, 8 );
	owner.receive( { opcode: 0x3122, payload: Uint8Array.of( 7, 0, 0, 0, 1, 0 ) }, 300 );
	const stopped = owner.read( 7 );
	assert.ok( Math.abs( stopped.x - 3.8 ) < 1e-8 );
	owner.step( 5000 );
	assert.deepEqual( owner.read( 7 ), stopped );
	const despawn = Buffer.alloc( 4 );
	despawn.writeUInt32LE( 7 );
	owner.receive( { opcode: 0x36ab, payload: despawn }, 5000 );
	owner.step( 6000 );
	assert.equal( owner.read( 7 ), undefined );
	owner.dispose();
});

const peerFixture = JSON.parse(
	fs.readFileSync(
		path.join( root, "../../apps/server/internal/game/world/simulation/testdata/peer_spawn_row_fixture.json" ),
		"utf8"
	)
);
for ( const row of peerFixture.scenarios ) {
	test(`native peer fixture ${row.name}`, () => {
		const owner = createEntities();
		owner.bootstrap( {
			...bootstrap,
			refObjSnapshot: [ { refObjId: row.modelRefObjId, kind: "player" } ],
			refItemSnapshot: row.equipment ?? []
		} );
		flush( owner );
		const payload = Uint8Array.from( Buffer.from( row.payloadHex, "hex" ) );
		owner.receive( { opcode: 0x30d7, payload } );
		const entity = owner.read( row.gid );
		assert.equal( entity.name, row.characterName );
		assert.equal( entity.x, row.x );
		assert.equal( entity.heading, row.heading );
		assert.equal( entity.bodyShape, row.bodyShapeByte );
		assert.deepEqual(
			entity.equipment.map( item => item.refObjId ),
			(row.equipment ?? []).map( item => item.refObjId )
		);
		for ( let length = 0; length < payload.length; length++ ) {
			const bad = createEntities();
			bad.bootstrap( {
				...bootstrap,
				refObjSnapshot: [ { refObjId: row.modelRefObjId, kind: "player" } ],
				refItemSnapshot: row.equipment ?? []
			} );
			assert.throws( () => bad.receive( { opcode: 0x30d7, payload: payload.slice( 0, length ) } ) );
			assert.equal( bad.count(), 0 );
		}
		owner.receive( {
			opcode: 0x36ab,
			payload: Uint8Array.of( row.gid & 255, (row.gid >>> 8) & 255, (row.gid >>> 16) & 255, row.gid >>> 24 )
		} );
		assert.equal( owner.count(), 0 );
	});
}
test("retail ordered equipment replacement and clear-by-slot accept zero and absent references", () => {
	const row = peerFixture.scenarios[1], owner = createEntities();
	owner.bootstrap( {
		...bootstrap,
		refObjSnapshot: [ { refObjId: row.modelRefObjId, kind: "player" } ],
		refItemSnapshot: [ ...row.equipment, { refObjId: 999, typeFlags: row.equipment[3].typeFlags } ]
	} );
	flush( owner );
	owner.receive( { opcode: 0x30d7, payload: Uint8Array.from( Buffer.from( row.payloadHex, "hex" ) ) } );
	const visual = ( opcode, ref, slot = 0 ) => {
		const p = new Uint8Array( opcode === 0x3314 ? 10 : 9 ), v = new DataView( p.buffer );
		v.setUint32( 0, row.gid, true );
		p[4] = slot;
		v.setUint32( 5, ref, true );
		owner.receive( { opcode, payload: p } );
	};
	for ( const ref of [ 0, 107, 999, 123456 ] ) {
		visual( 0x3314, 999 );
		assert.equal( owner.read( row.gid ).equipment.find( i => i.slot === 6 ).refObjId, 999 );
		visual( 0x377c, ref, 6 );
		assert.equal( owner.read( row.gid ).equipment.some( i => i.slot === 6 ), false );
	}
	assert.throws( () => visual( 0x3314, 123456 ), /Unknown peer equipment/ );
	assert.throws( () => visual( 0x377c, 0, 255 ), /slot/ );
	owner.clear();
	assert.equal( owner.count(), 0 );
});

test("jewelry equip notifications leave the visual model unchanged without refusing valid packets", () => {
	const row = peerFixture.scenarios[1], owner = createEntities();
	const jewelry = [ 5, 12 ].flatMap( group =>
		[ 1, 2, 3 ].map( sub => ({ refObjId: 10000 + group * 10 + sub, typeFlags: 0x2c | (group << 7) | (sub << 11) }) )
	);
	owner.bootstrap( {
		...bootstrap,
		refObjSnapshot: [ { refObjId: row.modelRefObjId, kind: "player" } ],
		refItemSnapshot: [ ...row.equipment, ...jewelry ]
	} );
	flush( owner );
	owner.receive( { opcode: 0x30d7, payload: Uint8Array.from( Buffer.from( row.payloadHex, "hex" ) ) } );
	flush( owner );
	const before = owner.read( row.gid );
	for ( const item of jewelry ) {
		const payload = Buffer.alloc( 10 );
		payload.writeUInt32LE( row.gid );
		payload.writeUInt32LE( item.refObjId, 5 );
		payload[9] = 3;
		assert.doesNotThrow( () => owner.receive( { opcode: 0x3314, payload } ) );
		assert.deepEqual( owner.read( row.gid ), before );
		assert.throws(
			() => owner.receive( { opcode: 0x3314, payload: payload.subarray( 0, 9 ) } ),
			/Invalid equipment visual/
		);
		payload[4] = 11;
		assert.throws( () => owner.receive( { opcode: 0x3314, payload } ), /Invalid equipment visual/ );
	}
	owner.dispose();
});

test("versioned shop controls cross the admitted world boundary only after EnterWorld", async t => {
	const sockets = socketHarness( t ), world = createWorldSession( async () => "ticket" );
	world.enter( "fixture", "shard", "http://localhost:9000" );
	await settle();
	world.step( 1 );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( 2, welcome() );
	world.step( 2 );
	await settle();
	world.step( 3 );
	socket.receive( 7, entered() );
	for ( const row of rows ) socket.receive( row.opcode, row.payload );
	world.step( 4 );
	flush( world );
	world.step( 5 );
	assert.equal( world.status().phase, "world" );
	assert.throws( () => world.command( { kind: "select", gid: 17 } ), /not ready/ );
	assert.doesNotThrow( () =>
		world.command( {
			kind: "navigation",
			regionId: 25256,
			bundle: { navmesh: { regionSize: 1920, tileSize: 20, tilesPerAxis: 96, regions: [] } }
		} )
	);
	assert.equal( world.status().ready, false );
	socket.receive( 11, Buffer.from( JSON.stringify( { version: 1, npc: 17, name: "Merchant", offers: [] } ) ) );
	world.step( 6 );
	assert.equal( world.status().phase, "world" );
	world.dispose();
});

test("bootstrap reference tables remain worker-owned and cannot exhaust presentation journal", () => {
	const owner = createEntities();
	owner.bootstrap( { ...bootstrap, workerOnlyTable: "x".repeat( 5 << 20 ) } );
	const batch = flush( owner ), value = batch.events.find( e => e.kind === "bootstrap" ).value;
	assert.equal( value.workerOnlyTable, undefined );
	assert.equal( value.refObjSnapshot, undefined );
	assert.deepEqual( value.localPlayerEntry, bootstrap.localPlayerEntry );
	for ( const row of rows ) owner.receive( row );
	assert.equal( owner.count(), 1 );
});
test("commerce references precede a newly bought peer equipment visual", () => {
	const owner = createEntities();
	owner.bootstrap( bootstrap );
	flush( owner );
	owner.receive( { opcode: 0x32a6, payload: Uint8Array.of( 7, 0, 0, 0, 0, 0, 0, 0 ) } );
	flush( owner );
	owner.references( [ { refObjId: 12345, typeFlags: 4908, name: "Purchased sword" } ] );
	const p = Buffer.alloc( 10 );
	p.writeUInt32LE( 7 );
	p.writeUInt32LE( 12345, 5 );
	p[9] = 5;
	owner.receive( { opcode: 0x3314, payload: p } );
	const changed = flush( owner ).events.find( e => e.kind === "state" );
	assert.equal( changed.entity.equipment[0].refObjId, 12345 );
	assert.equal( changed.entity.equipment[0].plus, 5 );
	assert.throws( () => owner.references( [ { refObjId: 12345, typeFlags: 0x6c, name: "Conflict" } ] ) );
});

test("presentation retains omitted shop projection but honors an explicit close", () => {
	const p = createPresentation(), shop = { npc: 17, name: "Merchant", offers: [] };
	p.apply( { sequence: 1, events: [ { kind: "gameplay", state: { localGid: 1, shop } } ] } );
	p.apply( { sequence: 2, events: [ { kind: "gameplay", state: { localGid: 1, inventoryPending: false } } ] } );
	assert.equal( p.gameplay().shop, shop );
	p.apply( { sequence: 3, events: [ { kind: "gameplay", state: { localGid: 1, shop: undefined } } ] } );
	assert.equal( p.gameplay().shop, undefined );
});

test("reset acknowledgement and readiness are per travel generation for both native reset opcodes", async t => {
	const sockets = socketHarness( t ), world = createWorldSession( async () => "ticket" );
	world.enter( "fixture", "shard", "http://localhost:9000" );
	await settle();
	world.step( 1 );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( 2, welcome() );
	world.step( 2 );
	await settle();
	world.step( 3 );
	socket.receive( 7, entered() );
	for ( const row of rows ) socket.receive( row.opcode, row.payload );
	world.step( 4 );
	flush( world );
	world.step( 5 );
	world.ready();
	const count = opcode => socket.sent.filter( p => (p[0] | p[1] << 8) === opcode ).length;
	for ( const opcode of [ 0x3369, 0x366a ] ) {
		const acks = count( 0x36dd ), ready = count( 0x3012 );
		socket.receive( opcode, Uint8Array.of( 0x4f, 0x69 ) );
		world.step( 6 );
		assert.equal( world.status().ready, false );
		world.ready( 0 );
		assert.equal( world.status().ready, false, "stale ready cannot release reset" );
		assert.equal( world.status().phase, "entering-world" );
		assert.equal( count( 0x36dd ), acks + (opcode === 0x3369 ? 1 : 0) );
		assert.throws( () => world.command( { kind: "select", gid: 1 } ), /not ready/ );
		socket.receive( 7, entered() );
		for ( const row of rows ) socket.receive( row.opcode, row.payload );
		world.step( 7 );
		let batch;
		while ( (batch = world.take()) ) world.ack( batch.sequence );
		world.step( 8 );
		assert.equal( world.status().phase, "world" );
		world.ready();
		world.ready();
		assert.equal( count( 0x3012 ), ready + 1 );
	}
	world.dispose();
});

test("native life channel publishes death and revival without waiting for HP packets", () => {
	const owner = createEntities();
	owner.bootstrap( bootstrap );
	flush( owner );
	for ( const row of rows ) owner.receive( row );
	flush( owner );
	const gid = fixture.expect.gid, original = owner.read( gid ).appearanceState;
	for ( const life of [ 2, 1 ] ) {
		const payload = Buffer.alloc( 6 );
		payload.writeUInt32LE( gid );
		payload[5] = life;
		owner.receive( { opcode: 0x3122, payload } );
		const next = flush( owner ).events.find( e => e.kind === "state" ).entity;
		assert.equal( next.appearanceState[0], life );
		assert.deepEqual( next.appearanceState.slice( 1 ), original.slice( 1 ) );
	}
});

test("native status channel updates berserk independently of LIFE and movement", () => {
	const owner = createEntities();
	owner.bootstrap( bootstrap );
	flush( owner );
	for ( const row of rows ) owner.receive( row );
	flush( owner );
	const gid = fixture.expect.gid, original = owner.read( gid ).appearanceState;
	for ( const status of [ 1, 4, 0 ] ) {
		const payload = Buffer.alloc( 6 );
		payload.writeUInt32LE( gid );
		payload[4] = 4;
		payload[5] = status;
		owner.receive( { opcode: 0x3122, payload } );
		const next = flush( owner ).events.find( e => e.kind === "state" ).entity;
		assert.equal( next.appearanceState[2], status );
		assert.deepEqual( next.appearanceState.slice( 0, 2 ), original.slice( 0, 2 ) );
	}
});

test("emote broadcasts publish ordered identity and reject truncated packets atomically", () => {
	const owner = createEntities();
	owner.bootstrap( bootstrap );
	flush( owner );
	for ( const row of rows ) owner.receive( row );
	flush( owner );
	const gid = fixture.expect.gid, p = Buffer.alloc( 5 );
	p.writeUInt32LE( gid );
	p[4] = 6;
	for ( let length = 0; length < 5; length++ ) {
		assert.throws( () => owner.receive( { opcode: 0x324b, payload: p.subarray( 0, length ) }, 100 ) );
	}
	assert.equal( owner.take(), null );
	for ( const revision of [ 1, 2 ] ) {
		owner.receive( { opcode: 0x324b, payload: p }, 100 * revision );
		assert.deepEqual( flush( owner ).events[0].entity.emote, { action: 6, revision, atMs: 100 * revision } );
	}
});
test("7641D0: 0x323A skins a known entity, each application a new revision, and skin 0 clears it", () => {
	const owner = createEntities();
	owner.bootstrap( bootstrap );
	flush( owner );
	for ( const row of rows ) owner.receive( row );
	flush( owner );
	const gid = fixture.expect.gid, p = Buffer.alloc( 8 );
	p.writeUInt32LE( gid );
	p.writeUInt32LE( 1933, 4 );
	for ( let length = 0; length < 8; length++ ) {
		assert.throws( () => owner.receive( { opcode: 0x323a, payload: p.subarray( 0, length ) }, 100 ) );
	}
	for ( const revision of [ 1, 2 ] ) {
		owner.receive( { opcode: 0x323a, payload: p }, 100 );
		assert.deepEqual( flush( owner ).events[0].entity.transformSkin, {
			refObjId: 1933,
			player: false,
			equipment: [],
			revision
		} );
	}
	p.writeUInt32LE( 0, 4 );
	owner.receive( { opcode: 0x323a, payload: p }, 100 );
	assert.equal( flush( owner ).events[0].entity.transformSkin, undefined );
	p.writeUInt32LE( gid + 999, 0 );
	owner.receive( { opcode: 0x323a, payload: p }, 100 );
	assert.equal( owner.take(), null, "an unknown gid changes nothing" );
});
test("fatal cast queues a pending result; only native LIFE changes durable life and revival clears pending death", async () => {
	const { createWorldCore } = await load( "src/engine/runtime/simulation/worker/session/world/core.ts" );
	const core = createWorldCore( () => {} );
	core.bootstrap( bootstrap );
	flush( core );
	for ( const row of rows ) core.receive( row, 0 );
	flush( core );
	const gid = fixture.expect.gid, p = Buffer.alloc( 34 );
	p[0] = 1;
	p.writeUInt32LE( 7, 2 );
	p.writeUInt32LE( gid, 6 );
	p.writeUInt32LE( 99, 10 );
	p.writeUInt32LE( gid, 14 );
	p[18] = 1;
	p[19] = 1;
	p[20] = 1;
	p.writeUInt32LE( gid, 21 );
	p[25] = 128;
	p.writeUInt32LE( 54 << 8, 26 );
	core.receive( { opcode: 0xb245, payload: p }, 1 );
	const pending = flush( core ).events;
	assert.equal( pending.some( e => e.kind === "state" ), false );
	assert.equal( pending.find( e => e.kind === "hp-result" ).fatal, true );
	core.receive( { opcode: 0xb245, payload: p }, 2 );
	assert.equal( core.take(), null, "duplicate cast cannot repeat death" );
	const revive = Buffer.alloc( 6 );
	revive.writeUInt32LE( gid );
	revive[5] = 2;
	core.receive( { opcode: 0x3122, payload: revive }, 2 );
	assert.equal( flush( core ).events.find( e => e.kind === "state" ).entity.appearanceState[0], 2 );
	revive[5] = 1;
	core.receive( { opcode: 0x3122, payload: revive }, 3 );
	assert.equal( flush( core ).events.find( e => e.kind === "state" ).entity.appearanceState[0], 1 );
	core.dispose();
});

test("movement activity clears on death, revival, teleport and explicit stop", () => {
	const owner = createEntities();
	owner.bootstrap( { ...bootstrap, refObjSnapshot: [ { refObjId: 1, kind: "npc" } ] } );
	flush( owner );
	const spawn = Buffer.alloc( 49 );
	spawn.writeUInt32LE( 1 );
	spawn.writeUInt32LE( 7, 4 );
	spawn.writeUInt16LE( 257, 8 );
	spawn[25] = 3;
	spawn.writeFloatLE( 8, 32 );
	spawn.writeFloatLE( 22, 36 );
	spawn[45] = 1;
	owner.receive( { opcode: 0x30d7, payload: spawn }, 0 );
	flush( owner );
	const move = Buffer.alloc( 14 );
	move.writeUInt32LE( 7 );
	move[4] = 1;
	move.writeUInt16LE( 257, 5 );
	move.writeInt16LE( 1000, 7 );
	const start = now => {
		owner.receive( { opcode: 0xb738, payload: move }, now );
		owner.step( now + 16 );
		assert.equal( owner.read( 7 ).moving, true );
		flush( owner );
	};
	start( 0 );
	owner.die( 7 );
	assert.equal( owner.read( 7 ).moving, false );
	const life = Buffer.from( [ 7, 0, 0, 0, 0, 1 ] );
	owner.receive( { opcode: 0x3122, payload: life }, 32 );
	owner.step( 48 );
	assert.equal( owner.read( 7 ).moving, false, "revival cannot resurrect a cancelled run" );
	flush( owner );
	start( 100 );
	life[5] = 2;
	owner.receive( { opcode: 0x3122, payload: life }, 132 );
	assert.equal( owner.read( 7 ).moving, false );
	life[5] = 1;
	owner.receive( { opcode: 0x3122, payload: life }, 148 );
	flush( owner );
	start( 200 );
	const position = Buffer.alloc( 20 );
	position.writeUInt32LE( 7 );
	position.writeUInt16LE( 257, 4 );
	position.writeFloatLE( 10, 6 );
	owner.receive( { opcode: 0xb2f5, payload: position }, 232 );
	assert.equal( owner.read( 7 ).moving, false );
	flush( owner );
	start( 300 );
	owner.receive( { opcode: 0x3122, payload: Buffer.from( [ 7, 0, 0, 0, 1, 0 ] ) }, 332 );
	assert.equal( owner.read( 7 ).moving, false );
	owner.dispose();
});

test("transport loss retains the admitted inventory and entities until explicit logout", async t => {
	const sockets = socketHarness( t ),
		world = createWorldSession( async () => "ticket" ),
		presentation = createPresentation();
	const accept = () => {
		let batch;
		while ( (batch = world.take()) ) {
			presentation.apply( batch );
			world.ack( batch.sequence );
		}
	};
	world.enter( "fixture", "shard", "http://localhost:9000" );
	await settle();
	world.step( 1 );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( 2, welcome() );
	world.step( 2 );
	await settle();
	world.step( 3 );
	socket.receive(
		7,
		entered( {
			...bootstrap,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c, icon: "item/etc/hp_potion_01.ddj" } ],
			equipItems: [ { slot: 13, refObjId: 1, body: [ 1, 0, 0, 0, 3, 0 ] } ]
		} )
	);
	for ( const row of rows ) socket.receive( row.opcode, row.payload );
	world.step( 4 );
	accept();
	world.step( 5 );
	accept();
	const prior = structuredClone( presentation.gameplay() ), count = presentation.count();
	assert.equal( prior.inventory.length, 1 );
	socket.receive( 5, new Uint8Array() );
	world.step( 6 );
	accept();
	world.step( 10000 );
	accept();
	assert.equal( world.status().phase, "disconnected" );
	assert.deepEqual( presentation.gameplay(), prior );
	assert.equal( presentation.count(), count );
	assert.throws(
		() => world.command( { kind: "inventory-move", source: 13, destination: 14, quantity: 3 } ),
		/not ready/
	);
	assert.equal( sockets.length, 1 );
	world.disconnect( true );
	world.step( 10001 );
	accept();
	assert.equal( presentation.count(), 0 );
	assert.equal( presentation.gameplay()?.inventory.length ?? 0, 0 );
	world.dispose();
	presentation.dispose();
});

test("reference admission holds native packets until verified data, and cancellation retires the fetch", async t => {
	const sockets = socketHarness( t );
	let release, signal;
	const data = JSON.stringify( {
			referencesVersion: 2,
			skillLifecycleVersion: 1,
			refSkillSnapshot: [],
			refItemSnapshot: []
		} ),
		bytes = new TextEncoder().encode( data ),
		digest = Buffer.from( await crypto.subtle.digest( "SHA-256", bytes ) ).toString( "hex" );
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		assert.equal( options.cache, "force-cache" );
		assert.equal( new URL( url ).pathname, `/transport/references/${digest}.json` );
		signal = options.signal;
		return new Promise( r => release = r );
	} );
	const http = createSessionHttp();
	let referenceCompletion = Promise.resolve();
	const world = createWorldSession( async () => "ticket", ( ...args ) => {
		const request = http.references( ...args );
		referenceCompletion = request.then( () => {}, () => {} );
		return request;
	} );
	/*
================
begin
================
	*/
	async function begin() {
		world.enter( "fixture", "shard", "http://localhost:9000" );
		await settle();
		world.step( 1 );
		const socket = sockets.at( -1 );
		socket.onopen();
		socket.receive( 2, welcome() );
		world.step( 2 );
		await settle();
		world.step( 3 );
		const blob = Buffer.from(
				JSON.stringify( {
					v: 2,
					bootstrap,
					references: { path: `/transport/references/${digest}.json`, sha256: digest, bytes: bytes.length }
				} )
			),
			p = Buffer.alloc( 9 + blob.length );
		p[0] = 1;
		p.writeUInt32LE( blob.length, 5 );
		blob.copy( p, 9 );
		socket.receive( 7, p );
		for ( const row of rows ) socket.receive( row.opcode, row.payload );
		world.step( 4 );
		return socket;
	}
	await begin();
	assert.equal( world.status().phase, "entering-world" );
	let batch = world.take();
	if ( batch ) {
		assert.ok( !batch.events.some( e => e.kind === "spawn" ) );
		world.ack( batch.sequence );
	}
	defined( release )( new Response( data ) );
	// Await the real digest/read operation before advancing the synthetic clock.
	// A fixed number of event-loop turns cannot bound a native crypto worker.
	await referenceCompletion;
	for ( let tick = 5; tick < 55 && world.status().phase !== "world"; tick++ ) {
		await settle();
		world.step( tick );
		flush( world );
	}
	assert.equal( world.status().phase, "world" );
	world.disconnect( true );
	await begin();
	const late = release;
	world.disconnect( true );
	assert.equal( defined( signal ).aborted, true );
	defined( late )( new Response( data ) );
	await settle();
	world.step( 60 );
	assert.equal( world.status().phase, "disconnected" );
	world.dispose();
});

test("published static item rows join the login's own rows before the world is admitted", async t => {
	const sockets = socketHarness( t ), presentation = createPresentation();
	const data = JSON.stringify( {
			referencesVersion: 2,
			skillLifecycleVersion: 1,
			refSkillSnapshot: [],
			refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c, icon: "item/etc/hp_potion_01.ddj" } ]
		} ),
		bytes = new TextEncoder().encode( data ),
		digest = Buffer.from( await crypto.subtle.digest( "SHA-256", bytes ) ).toString( "hex" );
	t.mock.method( globalThis, "fetch", async () => new Response( data ) );
	const http = createSessionHttp();
	let referenceCompletion = Promise.resolve();
	const world = createWorldSession( async () => "ticket", ( ...args ) => {
		const request = http.references( ...args );
		referenceCompletion = request.then( () => {}, () => {} );
		return request;
	} );
	world.enter( "fixture", "shard", "http://localhost:9000" );
	await settle();
	world.step( 1 );
	const socket = sockets.at( -1 );
	socket.onopen();
	socket.receive( 2, welcome() );
	world.step( 2 );
	await settle();
	world.step( 3 );
	const blob = Buffer.from(
			JSON.stringify( {
				v: 2,
				bootstrap: {
					...bootstrap,
					inventorySlotCount: 45,
					equipmentSlotCount: 13,
					refItemSnapshot: [ { refObjId: 2, typeFlags: 0x6c, icon: "item/etc/mp_potion_01.ddj" } ],
					equipItems: [ { slot: 13, refObjId: 1, body: [ 1, 0, 0, 0, 3, 0 ] }, {
						slot: 14,
						refObjId: 2,
						body: [ 2, 0, 0, 0, 3, 0 ]
					} ]
				},
				references: { path: `/transport/references/${digest}.json`, sha256: digest, bytes: bytes.length }
			} )
		),
		p = Buffer.alloc( 9 + blob.length );
	p[0] = 1;
	p.writeUInt32LE( blob.length, 5 );
	blob.copy( p, 9 );
	socket.receive( 7, p );
	for ( const row of rows ) socket.receive( row.opcode, row.payload );
	await referenceCompletion;
	// The references digest runs on the crypto thread pool, so admission
	// waits on its completion, not on a count of event-loop turns: a loaded
	// machine needs more turns. Simulated time stays within the old range.
	const deadline = Date.now() + WORLD_ADMISSION_BUDGET_MS;
	for ( let tick = 4; world.status().phase !== "world" && Date.now() < deadline; tick = Math.min( tick + 1, 54 ) ) {
		await settle();
		world.step( tick );
		let batch;
		while ( (batch = world.take()) ) {
			presentation.apply( batch );
			world.ack( batch.sequence );
		}
	}
	assert.equal( world.status().phase, "world" );
	assert.deepEqual( presentation.gameplay()?.inventory.map( item => item.refObjId ).sort(), [ 1, 2 ] );
	world.dispose();
	presentation.dispose();
});

test("an edge-routed transport base carries its route to the socket and the reference fetch", async t => {
	const sockets = socketHarness( t ), requested = [];
	const data = JSON.stringify( {
			referencesVersion: 2,
			skillLifecycleVersion: 1,
			refSkillSnapshot: [],
			refItemSnapshot: []
		} ),
		bytes = new TextEncoder().encode( data ),
		digest = Buffer.from( await crypto.subtle.digest( "SHA-256", bytes ) ).toString( "hex" );
	t.mock.method( globalThis, "fetch", async url => {
		requested.push( String( url ) );
		return new Response( data );
	} );
	const world = createWorldSession( async () => "ticket", createSessionHttp().references );
	world.enter( "fixture", "shard", "https://edge.invalid/shards/a" );
	await settle();
	world.step( 1 );
	const socket = sockets.at( -1 );
	assert.equal( socket.url.toString(), "wss://edge.invalid/shards/a/transport/ws" );
	socket.onopen();
	socket.receive( 2, welcome() );
	world.step( 2 );
	await settle();
	world.step( 3 );
	const blob = Buffer.from(
			JSON.stringify( {
				v: 2,
				bootstrap,
				references: { path: `/transport/references/${digest}.json`, sha256: digest, bytes: bytes.length }
			} )
		),
		p = Buffer.alloc( 9 + blob.length );
	p[0] = 1;
	p.writeUInt32LE( blob.length, 5 );
	blob.copy( p, 9 );
	socket.receive( 7, p );
	world.step( 4 );
	assert.deepEqual( requested, [ `https://edge.invalid/shards/a/transport/references/${digest}.json` ] );
	world.dispose();
});

test("local entry preserves authoritative movement channels", () => {
	const owner = createEntities();
	owner.bootstrap( {
		...bootstrap,
		localPlayerEntry: { ...bootstrap.localPlayerEntry, walkSpeed: 40, runSpeed: 100 }
	} );
	flush( owner );
	owner.receive( { opcode: 0x32a6, payload: Uint8Array.of( 7, 0, 0, 0, 0, 0, 0, 0 ) } );
	const local = flush( owner ).events.find( e => e.kind === "spawn" ).entity;
	assert.equal( local.walkSpeed, 40 );
	assert.equal( local.runSpeed, 100 );
	owner.dispose();
});

test("town re-entry replaces corpse vitals and login coordinates before the local latch", async t => {
	const sockets = socketHarness( t ), world = createWorldSession( async () => "ticket" ), view = createPresentation();
	let resets = 0;
	const accept = () => {
		let batch;
		while ( batch = world.take() ) {
			resets += batch.events.filter( e => e.kind === "reset" ).length;
			view.apply( batch );
			world.ack( batch.sequence );
		}
	};
	const latch = Buffer.alloc( 8 );
	latch.writeUInt32LE( 7 );
	const finish = socket => {
		socket.receive( 0x32a6, latch );
		socket.receive( 0x30cb, Uint8Array.of( 1, 0, 0 ) );
		socket.receive( 0x330a, new Uint8Array() );
	};
	try {
		world.enter( "fixture", "shard", "http://localhost:9000" );
		await settle();
		world.step( 1 );
		const socket = sockets[0];
		socket.onopen();
		socket.receive( 2, welcome() );
		world.step( 2 );
		await settle();
		world.step( 3 );
		socket.receive( 7, entered( { ...bootstrap, character: { hp: 0, mp: 0, maxHp: 339, maxMp: 244 } } ) );
		finish( socket );
		world.step( 4 );
		accept();
		world.step( 5 );
		accept();
		world.ready();
		assert.equal( view.gameplay().vitals.find( v => v.gid === 7 ).hp, 0 );
		const destination = { regionId: 27471, x: 1205, y: 80, z: 396, angle: 0 };
		const beforeReset = resets;
		socket.receive( 0x3369, Uint8Array.of( 0x4f, 0x6b ) );
		// Deliver and acknowledge reset before the delayed bootstrap, as a real
		// reference fetch does. Completion must not restart renderer admission.
		world.step( 6 );
		accept();
		assert.equal( resets, beforeReset + 1 );
		assert.equal( world.status().phase, "entering-world" );
		socket.receive(
			7,
			entered( {
				...bootstrap,
				character: { hp: 339, mp: 244, maxHp: 339, maxMp: 244 },
				localPlayerEntry: { ...bootstrap.localPlayerEntry, startProfile: destination }
			} )
		);
		finish( socket );
		socket.receive( 0x3122, Uint8Array.of( 7, 0, 0, 0, 0, 1 ) );
		world.step( 6 );
		accept();
		world.step( 7 );
		accept();
		assert.equal( world.status().phase, "world" );
		assert.equal( resets, beforeReset + 1, "one reset for the entire town re-entry" );
		const actor = view.read( 7 ), game = view.gameplay();
		for ( const key of [ "regionId", "x", "y", "z" ] ) assert.equal( actor[key], destination[key], key );
		assert.equal( game.vitals.find( v => v.gid === 7 ).hp, 339 );
		assert.equal( game.vitals.find( v => v.gid === 7 ).mp, 244 );
		assert.notEqual( actor.appearanceState?.[0], 2 );
	} finally {
		world.dispose();
		view.dispose();
	}
});

test("local entity preserves the authoritative native country for item cooldown timing", () => {
	for ( const countryByte9c of [ 0, 1 ] ) {
		const owner = createEntities();
		owner.bootstrap( { ...bootstrap, localPlayerEntry: { ...bootstrap.localPlayerEntry, countryByte9c } } );
		flush( owner );
		owner.receive( { opcode: 0x32a6, payload: Uint8Array.of( 7, 0, 0, 0, 0, 0, 0, 0 ) } );
		assert.equal( flush( owner ).events.find( e => e.kind === "spawn" ).entity.countryByte9c, countryByte9c );
		owner.dispose();
	}
	for ( const countryByte9c of [ -1, 2, "0", null ] ) {
		assert.throws( () =>
			createEntities().bootstrap( {
				...bootstrap,
				localPlayerEntry: { ...bootstrap.localPlayerEntry, countryByte9c }
			} ), /country/ );
	}
});

test("production core LIFE stops a nonlocal monster and rejects late advisory travel without premature fatal-receipt death", async () => {
	const { createWorldCore } = await load( "src/engine/runtime/simulation/worker/session/world/core.ts" );
	const core = createWorldCore( () => {} );
	core.bootstrap( bootstrap );
	flush( core );
	for ( const row of rows ) core.receive( row, 0 );
	flush( core );
	const gid = fixture.expect.gid, move = Buffer.alloc( 14 );
	move.writeUInt32LE( gid );
	move[4] = 1;
	move.writeUInt16LE( 25256, 5 );
	move.writeInt16LE( 1000, 7 );
	move.writeInt16LE( 1000, 11 );
	core.receive( { opcode: 0xb738, payload: move }, 1 );
	core.step( 16 );
	assert.ok( flush( core ).events.some( e => e.kind === "state" && e.entity.gid === gid && e.entity.moving ) );
	const life = Buffer.alloc( 6 );
	life.writeUInt32LE( gid );
	life[5] = 2;
	core.receive( { opcode: 0x3122, payload: life }, 20 );
	const dead = flush( core ).events.find( e => e.kind === "state" && e.entity.gid === gid ).entity;
	assert.equal( dead.moving, false );
	assert.equal( dead.appearanceState[0], 2 );
	for ( const now of [ 21, 100, 1000 ] ) {
		core.receive( { opcode: 0xb738, payload: move }, now );
		core.step( now );
		const events = flush( core )?.events ?? [];
		assert.equal(
			events.some( e => e.kind === "state" && e.entity.gid === gid ),
			false,
			"late movement restarted dead mob"
		);
	}
	life[5] = 1;
	core.receive( { opcode: 0x3122, payload: life }, 1001 );
	flush( core );
	core.receive( { opcode: 0xb738, payload: move }, 1002 );
	core.step( 1018 );
	assert.ok( flush( core ).events.some( e => e.kind === "state" && e.entity.gid === gid && e.entity.moving ) );
	core.dispose();
});

test("monster LIFE preserves native impact displacement but stops cast-owned rush", () => {
	for ( const kind of [ 4, 5, 8 ] ) {
		const owner = createEntities();
		owner.bootstrap( bootstrap );
		flush( owner );
		for ( const row of rows ) owner.receive( row, 0 );
		flush( owner );
		const gid = fixture.expect.gid,
			e = owner.read( gid ),
			from = { regionId: e.regionId, x: e.x, y: e.y, z: e.z, angle: e.heading };
		owner.displace( { gid, token: 19, kind, destination: { ...from, x: from.x + 100 }, source: from }, 0 );
		owner.step( 16 );
		flush( owner );
		const life = Buffer.alloc( 6 );
		life.writeUInt32LE( gid );
		life[5] = 2;
		owner.receive( { opcode: 0x3122, payload: life }, 20 );
		const dead = owner.read( gid );
		assert.equal( dead.moving, kind !== 8 );
		owner.step( 40 );
		if ( kind === 8 ) assert.equal( owner.read( gid ).x, dead.x );
		else assert.ok( owner.read( gid ).x > dead.x, "native displacement exception was frozen" );
		owner.dispose();
	}
});

/*
================
admitWaitingWorld

Keep the presentation acknowledgement pending after the complete native
object bracket arrives. Network progress and rendering progress are distinct.
================
*/
async function admitWaitingWorld( t, complete = true ) {
	const sockets = socketHarness( t ), mints = [];
	const world = createWorldSession( async kind => {
		mints.push( kind );
		return "ticket";
	} );
	t.after( () => world.dispose() );
	world.enter( "fixture", "shard", "http://localhost:9000" );
	await settle();
	world.step( 1 );
	const socket = sockets[0];
	socket.onopen();
	socket.receive( 2, welcome() );
	world.step( 2 );
	await settle();
	world.step( 3 );
	socket.receive( 7, entered() );
	for ( const row of complete ? rows : rows.slice( 0, -1 ) ) socket.receive( row.opcode, row.payload );
	world.step( 4 );
	return { world, sockets, socket, mints };
}

test("complete entry waits for slow presentation without reconnecting or republishing bootstrap", async t => {
	const { world, sockets, mints } = await admitWaitingWorld( t );
	const pending = world.take(), original = JSON.stringify( pending );
	for ( const time of [ 10005, 30005, 60005 ] ) {
		world.step( time );
		await settle();
		assert.equal( world.status().phase, "entering-world" );
		assert.equal( world.status().error, undefined );
		assert.equal( world.take(), null );
	}
	assert.equal( sockets.length, 1 );
	assert.deepEqual( mints, [ "transport", "enterworld" ] );
	assert.equal( JSON.stringify( pending ), original );
	world.ack( pending.sequence );
	world.step( 60006 );
	assert.equal( world.status().phase, "world" );
	world.ready();
	assert.equal( world.status().ready, true );
});

test("incomplete native entry still times out while presentation is pending", async t => {
	const { world } = await admitWaitingWorld( t, false );
	world.take();
	world.step( 10005 );
	assert.equal( world.status().phase, "reconnecting" );
	assert.equal( world.status().error, "World connection timed out" );
});

test("travel waits for presentation but a later incomplete travel gets a fresh network deadline", async t => {
	const { world, socket, sockets } = await admitWaitingWorld( t );
	flush( world );
	world.step( 5 );
	world.ready();
	socket.receive( 0x3369, Uint8Array.of( 0x4f, 0x6b ) );
	socket.receive( 7, entered() );
	for ( const row of rows ) socket.receive( row.opcode, row.payload );
	world.step( 6 );
	const pending = world.take();
	world.step( 30006 );
	assert.equal( world.status().phase, "entering-world" );
	assert.equal( world.status().error, undefined );
	assert.equal( sockets.length, 1 );
	world.ack( pending.sequence );
	world.step( 30007 );
	world.ready();
	socket.receive( 0x3369, Uint8Array.of( 0x4f, 0x6b ) );
	world.step( 30008 );
	world.step( 40009 );
	assert.equal( world.status().error, "World connection timed out" );
});
