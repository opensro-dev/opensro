/*
===========================================================================

server-event.test.mjs - the 0x34F2 server event gauge the worker keeps

The v1.696 client's CNIFServerEvent (native here) reads 0x34F2 (v1.696
0x00CFA790); the worker keeps the one mission it shows. Bodies as the
server's wire.ServerEvent* frames write them.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);
const { createServerEvent, OP_SERVER_EVENT } = await import(
	sourceFileUrl(
		"src/engine/runtime/simulation/worker/session/world/gameplay/server-event/server-event.ts"
	).href
);

function bytes( ...parts ) {
	const out = [];
	for ( const [kind, value] of parts ) {
		if ( kind === "u8" ) out.push( value & 0xff );
		else if ( kind === "u16" ) out.push( value & 0xff, (value >>> 8) & 0xff );
		else if ( kind === "u32" ) out.push( value & 0xff, (value >>> 8) & 0xff, (value >>> 16) & 0xff, value >>> 24 );
		else if ( kind === "f32" ) {
			const b = new Uint8Array( 4 );
			new DataView( b.buffer ).setFloat32( 0, value, true );
			out.push( ...b );
		} else if ( kind === "str" ) {
			out.push( value.length & 0xff, value.length >>> 8, ...Array.from( value, c => c.charCodeAt( 0 ) ) );
		}
	}
	return { opcode: OP_SERVER_EVENT, payload: Uint8Array.from( out ) };
}

const fullSync = ( ...rows ) =>
	bytes(
		[ "u8", 2 ],
		[ "u8", 3 ],
		[ "u32", rows.length ],
		...rows.flatMap( ( [id, count, goal] ) => [ [ "u32", id ], [ "u32", count ], [ "u32", goal ] ] )
	);
const countSync = ( ...rows ) =>
	bytes(
		[ "u8", 2 ],
		[ "u8", 1 ],
		[ "u32", rows.length ],
		...rows.flatMap( ( [id, count] ) => [ [ "u32", id ], [ "u32", count ] ] )
	);
const reward = [ [ "u32", 0x1ed ], [ "u8", 1 ], [ "u32", 3600 ], [ "f32", 0.2 ], [ "f32", 0.2 ] ];

test("the enter-game rows open the gauge; the last row wins", () => {
	const event = createServerEvent();
	assert.equal( event.state(), null );
	assert.equal( event.receive( fullSync( [ 30001, 7, 118 ] ) ), true );
	assert.deepEqual( event.state(), { id: 30001, count: 7, goal: 118 } );
	event.receive( fullSync( [ 30001, 1, 100 ], [ 30005, 4, 50 ] ) );
	assert.deepEqual( event.state(), { id: 30005, count: 4, goal: 50 } );
});

test("a count sync moves the open gauge and keeps its goal", () => {
	const event = createServerEvent();
	assert.equal( event.receive( countSync( [ 30001, 9 ] ) ), true );
	assert.equal( event.state(), null, "no window, nothing to move" );
	event.receive( fullSync( [ 30001, 7, 118 ] ) );
	event.receive( countSync( [ 30001, 9 ] ) );
	assert.deepEqual( event.state(), { id: 30001, count: 9, goal: 118 } );
});

test("a completion carries the next count and goal, past its reward", () => {
	const event = createServerEvent();
	event.receive( fullSync( [ 30001, 117, 118 ] ) );
	const all = bytes(
		[ "u8", 1 ],
		[ "u32", 30001 ],
		[ "u8", 1 ],
		[ "u8", 3 ],
		[ "u32", 2 ],
		[ "u32", 136 ],
		...reward
	);
	assert.equal( event.receive( all ), true );
	assert.deepEqual( event.state(), { id: 30001, count: 2, goal: 136 } );
	const winner = bytes(
		[ "u8", 1 ],
		[ "u32", 30001 ],
		[ "u8", 2 ],
		[ "str", "asd2" ],
		[ "u8", 3 ],
		[ "u32", 0 ],
		[ "u32", 140 ],
		[ "u32", 0x1ee ],
		[ "u8", 2 ],
		[ "u32", 31852 ]
	);
	event.receive( winner );
	assert.deepEqual( event.state(), { id: 30001, count: 0, goal: 140 } );
	const countOnly = bytes( [ "u8", 1 ], [ "u32", 30001 ], [ "u8", 1 ], [ "u8", 1 ], [ "u32", 5 ], ...reward );
	event.receive( countOnly );
	assert.deepEqual( event.state(), { id: 30001, count: 5, goal: 140 } );
});

test("a new goal, a start and an end", () => {
	const event = createServerEvent();
	event.receive( bytes( [ "u8", 5 ], [ "u32", 30001 ], [ "u8", 1 ], [ "u32", 118 ] ) );
	assert.deepEqual( event.state(), { id: 30001, count: 0, goal: 118 } );
	event.receive( countSync( [ 30001, 40 ] ) );
	event.receive( bytes( [ "u8", 3 ], [ "u32", 30001 ], [ "u8", 2 ], [ "u32", 200 ] ) );
	assert.deepEqual( event.state(), { id: 30001, count: 0, goal: 200 } );
	event.receive( bytes( [ "u8", 3 ], [ "u32", 30001 ], [ "u8", 0 ] ) );
	assert.deepEqual( event.state(), { id: 30001, count: 0, goal: 200 } );
	assert.equal( event.receive( bytes( [ "u8", 6 ], [ "u32", 30001 ] ) ), true );
	assert.equal( event.state(), null );
});

test("the reward-time messages are consumed and move nothing; other opcodes are not consumed", () => {
	const event = createServerEvent();
	event.receive( fullSync( [ 30001, 7, 118 ] ) );
	for ( const type of [ 4, 7, 8, 9 ] ) {
		assert.equal( event.receive( bytes( [ "u8", type ], [ "u32", 0 ] ) ), true );
	}
	assert.deepEqual( event.state(), { id: 30001, count: 7, goal: 118 } );
	assert.equal( event.receive( { opcode: 0x3fc0, payload: new Uint8Array( 5 ) } ), false );
});

test("a malformed body is a protocol error and keeps the last gauge", () => {
	const event = createServerEvent();
	event.receive( fullSync( [ 30001, 7, 118 ] ) );
	const truncated = fullSync( [ 30001, 1, 2 ] );
	const bad = [
		new Uint8Array( 0 ),
		truncated.payload.subarray( 0, truncated.payload.length - 1 ),
		Uint8Array.from( [ ...countSync( [ 30001, 9 ] ).payload, 0 ] ),
		bytes( [ "u8", 6 ] ).payload
	];
	for ( const payload of bad ) {
		assert.throws( () => event.receive( { opcode: OP_SERVER_EVENT, payload } ), /Invalid server event/ );
	}
	assert.deepEqual( event.state(), { id: 30001, count: 7, goal: 118 } );
});

test("clear drops the gauge until the next rows", () => {
	const event = createServerEvent();
	event.receive( fullSync( [ 30001, 7, 118 ] ) );
	event.clear();
	assert.equal( event.state(), null );
});

test("a teleport keeps the gauge; a new login drops it", () => {
	// The server sends the rows at login only (EntryEventFrames); a travel
	// bootstrap (continued) must not wipe what the window shows.
	const game = createGameplay( () => {} );
	game.bootstrap( {} );
	game.receive( fullSync( [ 30001, 7, 118 ] ), 0 );
	assert.deepEqual( game.take()?.serverEventGauge, { id: 30001, count: 7, goal: 118 } );
	game.resetWorld();
	game.bootstrap( {}, true );
	assert.deepEqual( game.take()?.serverEventGauge, { id: 30001, count: 7, goal: 118 } );
	game.bootstrap( {}, false );
	assert.equal( game.take()?.serverEventGauge, undefined );
	game.dispose();
});
