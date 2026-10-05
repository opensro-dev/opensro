/*
===========================================================================

storage-room.test.mjs - the NPC warehouse wire and room state

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";
import { defined } from "../helpers/defined.mjs";

const room = await import( "../../src/engine/foundation/gameplay/storage-room.ts" );

// ITEM_ETC_HP_POTION_01: expendable stack class (typeFlags & 0x7E) === 0x6C.
const POTION = 3630, POTION_FLAGS = 0x6c | 0x80;
const refs = new Map( [ [ POTION, POTION_FLAGS ] ] ), caps = new Map( [ [ POTION, 50 ] ] );

/*
================
potionRow

[u8 slot][u32 ref][u16 quantity].
================
*/
function potionRow( slot, quantity ) {
	const p = new Uint8Array( 7 ), v = new DataView( p.buffer );
	p[0] = slot;
	v.setUint32( 1, POTION, true );
	v.setUint16( 5, quantity, true );
	return [ ...p ];
}

test("requests carry the native bodies", () => {
	assert.deepEqual( [ ...room.storageListRequest( 17 ).payload ], [ 17, 0, 0, 0, 0 ] );
	assert.equal( room.storageListRequest( 17 ).opcode, 0x72c3 );
	assert.deepEqual( [ ...room.storageOpenRequest( 17 ).payload ], [ 17, 0, 0, 0, 4, 0, 0, 0 ] );
	const deposit = room.storageMoveRequest( 17, { type: 2, source: 14, destination: 3, quantity: 0, gold: 0 } );
	assert.deepEqual( [ ...deposit.payload ], [ 2, 14, 3, 17, 0, 0, 0 ] );
	const inner = room.storageMoveRequest( 17, { type: 1, source: 3, destination: 4, quantity: 5, gold: 0 } );
	assert.deepEqual( [ ...inner.payload ], [ 1, 3, 4, 5, 0, 17, 0, 0, 0 ] );
	const gold = room.storageMoveRequest( 17, { type: 0x0c, source: 0, destination: 0, quantity: 0, gold: 300 } );
	assert.deepEqual( [ ...gold.payload ], [ 0x0c, 44, 1, 0, 0 ] );
});

test("the list decodes rows inside the capacity and refuses a stray byte", () => {
	const list = Uint8Array.from( [ 150, 2, ...potionRow( 4, 20 ), ...potionRow( 0, 7 ) ] );
	const decoded = room.decodeStorageList( list, refs );
	assert.equal( decoded.capacity, 150 );
	assert.deepEqual( decoded.items.map( row => [ row.slot, row.quantity ] ), [ [ 0, 7 ], [ 4, 20 ] ] );
	assert.throws( () => room.decodeStorageList( Uint8Array.from( [ ...list, 0 ] ), refs ), /length/ );
	assert.throws( () => room.decodeStorageList( Uint8Array.from( [ 2, 1, ...potionRow( 5, 1 ) ] ), refs ), /row/ );
});

test("the owner lists once, opens, and applies acknowledged moves", () => {
	const sent = [], owner = room.createStorageRoom( frame => sent.push( frame ) );
	owner.open( 17 );
	assert.equal( sent.at( -1 ).opcode, 0x72c3 );
	const gold = new Uint8Array( 8 );
	new DataView( gold.buffer ).setBigUint64( 0, 500n, true );
	owner.receive( { opcode: 0x3126, payload: gold }, refs );
	owner.receive( { opcode: 0x321a, payload: Uint8Array.from( [ 150, 1, ...potionRow( 0, 30 ) ] ) }, refs );
	assert.equal( sent.at( -1 ).opcode, 0x7338, "the list is followed by the storage function" );
	owner.receive( { opcode: 0xb338, payload: Uint8Array.from( [ 1, 4, 0, 0, 0 ] ) }, refs );
	const open = defined( owner.state() );
	assert.equal( open.phase, "open" );
	assert.equal( open.gold, "500" );

	// The bag rows share the decoded CSOItem shape.
	const bag = room.decodeStorageList( Uint8Array.from( [ 109, 1, ...potionRow( 14, 30 ) ] ), refs ).items;
	const withdraw = { type: 3, source: 0, destination: 15, quantity: 0, gold: 0 };
	const result = defined( room.storageMoveResult(
		{ opcode: 0xb06d, payload: Uint8Array.from( [ 1, 3, 0, 15 ] ) },
		open,
		bag,
		withdraw,
		caps
	) );
	assert.equal( result.room.items.length, 0 );
	assert.deepEqual( result.bag.map( row => row.slot ), [ 14, 15 ] );
	const refused = defined( room.storageMoveResult(
		{ opcode: 0xb06d, payload: Uint8Array.from( [ 2, 3 ] ) },
		open,
		bag,
		withdraw,
		caps
	) );
	assert.equal( refused.room, open, "a refusal changes nothing" );

	owner.close();
	owner.open( 17 );
	assert.equal( sent.at( -1 ).opcode, 0x7338, "a loaded room asks for the function directly" );
});

test("a refused open releases the room and leaves the notice to the caller", () => {
	const owner = room.createStorageRoom( () => {} );
	owner.open( 17 );
	const handled = owner.receive( { opcode: 0xb338, payload: Uint8Array.from( [ 2, 4 ] ) }, refs );
	assert.equal( handled, false, "the gameplay owner still shows the too-far notice" );
	assert.equal( owner.state(), null );
});

test("only the 3/3/13/10 type word is the warehouse ticket", () => {
	const word = ( t1, t2, t3, t4 ) => (t1 & 7) << 2 | (t2 & 3) << 5 | (t3 & 15) << 7 | (t4 & 31) << 11;
	assert.equal( room.isWarehouseTicket( word( 3, 3, 13, 10 ) ), true );
	assert.equal( room.isWarehouseTicket( word( 3, 3, 13, 7 ) ), false ); // the repair hammer
	assert.equal( room.isWarehouseTicket( word( 3, 3, 12, 10 ) ), false );
	assert.equal( room.isWarehouseTicket( word( 3, 3, 13, 10 ) | 2 ), false );
});
