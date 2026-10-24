/*
===========================================================================

summoner-persistence.test.mjs - native inventory and companion lifecycle

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { decodeInventoryItem } = await import( "../../src/engine/foundation/gameplay/inventory-item.ts" );

test("native companion creation, cancellation, death and revival preserve the summoner item", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( {
		refObjSnapshot: [ { kind: "cos", refObjId: 950, tidWord: 0x19c6 } ],
		refItemSnapshot: [ { refObjId: 900, typeFlags: 0x8cc } ],
		equipItems: [ { refObjId: 900, slot: 23, body: [ 132, 3, 0, 0, 1 ] } ]
	} );
	const payload = new Uint8Array( 43 ), view = new DataView( payload.buffer );
	view.setUint32( 0, 9001, true );
	view.setUint32( 4, 950, true );
	view.setUint32( 8, 42, true );
	payload[24] = 1;
	view.setUint16( 25, 7654, true );
	view.setUint16( 31, 4, true );
	payload.set( new TextEncoder().encode( "Wolf" ), 33 );
	payload[42] = 23;
	game.receive( { opcode: 0x3158, payload }, 0 );
	let state = game.take();
	assert.equal( state?.inventory?.find( row => row.slot === 23 )?.summon?.name, "Wolf" );
	for ( const value of [ 2, 3, 4, 3 ] ) {
		game.receive( { opcode: 0x3645, payload: Uint8Array.of( 23, 0x40, value ) }, 1 );
		state = game.take();
		const item = state?.inventory?.find( row => row.slot === 23 );
		assert.equal( item?.summon?.state, value );
		assert.equal( item?.summon?.refObjId, 950 );
		assert.equal( item?.summon?.name, "Wolf" );
		assert.equal( item?.quantity, 1 );
	}
	// The server's removal notice (0x3508 case 1, 4D2140) precedes the despawn.
	game.receive( { opcode: 0x3508, payload: Uint8Array.of( 41, 35, 0, 0, 1 ) }, 2 );
	game.receive( { opcode: 0x36ab, payload: Uint8Array.of( 41, 35, 0, 0 ) }, 2 );
	state = game.take();
	assert.equal( state?.cosRecords?.length, 0 );
	assert.equal( state?.inventory?.find( row => row.slot === 23 )?.summon?.state, 3 );
	game.dispose();
});

test("moving a summoned scroll in the bag preserves its companion GID for 0x3508 rename and evolution updates (59DAA0)", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refObjSnapshot: [
			{ kind: "cos", refObjId: 950, tidWord: 0x19c6 },
			{ kind: "cos", refObjId: 951, tidWord: 0x19c6 }
		],
		refItemSnapshot: [ { refObjId: 900, typeFlags: 0x8cc } ],
		equipItems: [ { refObjId: 900, slot: 23, body: [ 132, 3, 0, 0, 1 ] } ]
	} );
	const payload = new Uint8Array( 43 ), view = new DataView( payload.buffer );
	view.setUint32( 0, 9001, true );
	view.setUint32( 4, 950, true );
	view.setUint32( 8, 42, true );
	payload[24] = 1;
	view.setUint16( 25, 7654, true );
	view.setUint16( 31, 4, true );
	payload.set( new TextEncoder().encode( "Wolf" ), 33 );
	payload[42] = 23;
	game.receive( { opcode: 0x3158, payload }, 0 );
	assert.equal( game.take()?.inventory?.find( row => row.slot === 23 )?.summon?.gid, 9001 );

	// Move the summoned scroll from slot 23 to slot 24 (0xB06D type 0).
	game.receive( { opcode: 0xb06d, payload: Uint8Array.of( 1, 0, 23, 24, 1, 0, 0 ) }, 2 );
	let moved = game.take()?.inventory?.find( row => row.slot === 24 );
	assert.equal( moved?.summon?.gid, 9001 );
	assert.equal( moved?.summon?.name, "Wolf" );
	assert.equal( moved?.summon?.refObjId, 950 );

	// 0x3508 case 5 (rename, 77AADB -> 59DAA0) updates the moved scroll at slot 24.
	const rename = new Uint8Array( 11 ), rv = new DataView( rename.buffer );
	rv.setUint32( 0, 9001, true );
	rename[4] = 5;
	rv.setUint16( 5, 4, true );
	rename.set( new TextEncoder().encode( "Fang" ), 7 );
	game.receive( { opcode: 0x3508, payload: rename }, 3 );
	moved = game.take()?.inventory?.find( row => row.slot === 24 );
	assert.equal( moved?.summon?.name, "Fang" );

	// 0x3508 case 7 (evolution, 77AC91 -> 59DAA0) updates refObjId on the moved scroll at slot 24.
	const evolve = new Uint8Array( 9 ), ev = new DataView( evolve.buffer );
	ev.setUint32( 0, 9001, true );
	evolve[4] = 7;
	ev.setUint32( 5, 951, true );
	game.receive( { opcode: 0x3508, payload: evolve }, 4 );
	moved = game.take()?.inventory?.find( row => row.slot === 24 );
	assert.equal( moved?.summon?.refObjId, 951 );
	game.dispose();
});

test("unknown summoner states and incompatible character families cannot shift the next item", () => {
	const refs = new Map( [ [ 900, 0x8cc ] ] );
	for ( const state of [ 0, 5, 255 ] ) {
		assert.throws( () => decodeInventoryItem( Uint8Array.of( 132, 3, 0, 0, state ), 0, refs ) );
	}
	const payload = Uint8Array.of( 132, 3, 0, 0, 2, 182, 3, 0, 0 );
	assert.throws( () => decodeInventoryItem( payload, 0, refs, new Map( [ [ 950, 0x11c6 ] ] ) ) );
});

test("a summoned record naming a reference outside the snapshot still parses by the item subtype", () => {
	// No objRefs at all: the attack record (subtype 1) carries no rental
	// clock, the pickup record (subtype 2) carries it, exactly as 492D40
	// wrote them; the next byte is the record's end either way.
	const attack = new Uint8Array( 16 ), av = new DataView( attack.buffer );
	av.setUint32( 0, 900, true );
	attack[4] = 2;
	av.setUint32( 5, 4242, true );
	av.setUint16( 9, 4, true );
	attack.set( new TextEncoder().encode( "Wolf" ), 11 );
	const refs = new Map( [ [ 900, 0x8cc ], [ 901, 0x10cc ] ] );
	const wolf = decodeInventoryItem( attack, 0, refs );
	assert.equal( wolf.next, attack.length );
	assert.deepEqual( [ wolf.item?.summon?.refObjId, wolf.item?.summon?.name, wolf.item?.summon?.remainingSeconds ], [
		4242,
		"Wolf",
		undefined
	] );
	const pickup = new Uint8Array( 25 ), pv = new DataView( pickup.buffer );
	pv.setUint32( 0, 901, true );
	pickup[4] = 3;
	pv.setUint32( 5, 4243, true );
	pv.setUint16( 9, 0, true );
	pv.setInt32( 11, 2401324, true );
	pickup[15] = 1;
	pickup[16] = 0;
	pv.setUint32( 17, 7, true );
	pv.setInt32( 21, 30, true );
	const pig = decodeInventoryItem( pickup, 0, refs );
	assert.equal( pig.next, pickup.length );
	assert.deepEqual( [ pig.item?.summon?.refObjId, pig.item?.summon?.remainingSeconds ], [ 4243, 2401324 ] );
	assert.deepEqual( pig.item?.summon?.rentals, [ { kind: 0, id: 7, seconds: 30 } ] );
	// A known character keeps its own layout whatever the item subtype
	// says: band 4 carries the clock even inside an attack flute.
	const known = new Uint8Array( 16 ), kv = new DataView( known.buffer );
	kv.setUint32( 0, 900, true );
	known[4] = 2;
	kv.setUint32( 5, 950, true );
	kv.setUint16( 9, 0, true );
	kv.setInt32( 11, 99, true );
	const clocked = decodeInventoryItem( known, 0, refs, new Map( [ [ 950, 0x21c6 ] ] ) );
	assert.equal( clocked.next, known.length );
	assert.equal( clocked.item?.summon?.remainingSeconds, 99 );
	// A subtype the writer never emits is still a malformed record.
	assert.throws(
		() => decodeInventoryItem( Uint8Array.of( 132, 3, 0, 0, 2, 182, 3, 0, 0 ), 0, new Map( [ [ 900, 0x18cc ] ] ) ),
		/Unsupported summoner subtype/
	);
});
