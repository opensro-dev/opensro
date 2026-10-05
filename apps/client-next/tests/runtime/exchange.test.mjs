/*
===========================================================================

exchange.test.mjs - the exchange window's frames, requests and swap

0xB237 / 0x3219 opening (75B370, 75B420), the 0x3569 lists (75B690), the
partner's gold (75B580), the confirm button's lock-then-approve
(6B2280) and the 0x3272 swap's slot order (765260).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const exchange = await import( "../../src/engine/foundation/gameplay/exchange.ts" );

// ITEM_ETC_HP_POTION_01 (etc band, one u16 count).
const POTION = 3630;
const refs = new Map( [ [ POTION, 0x60 | 0x0c ] ] );

/**
 * @param {number} opcode
 * @param {number[]} bytes
 */
const frame = ( opcode, bytes ) => ({ opcode, payload: Uint8Array.from( bytes ) });
const u32 = ( /** @type {number} */ v ) => [ v & 255, v >> 8 & 255, v >> 16 & 255, v >>> 24 ];

test("the window opens, fills both sides and swaps in slot order", () => {
	let state = exchange.emptyExchange();
	state = defined( exchange.exchangeFrame( state, frame( 0xb237, [ 1, ...u32( 0x30d41 ) ] ), refs ) ).state;
	assert.equal( state.open, true );
	assert.equal( state.partner, 0x30d41 );
	// Own list: [owner][count][bag slot][exchange slot][CSOItem: ref, u16 count].
	const own = frame( 0x3569, [ ...u32( 0x30d40 ), 1, 20, 0, ...u32( POTION ), 5, 0 ] );
	state = defined( exchange.exchangeFrame( state, own, refs ) ).state;
	assert.deepEqual( state.own.map( r => [ r.slot, r.bagSlot, r.item.quantity ] ), [ [ 0, 20, 5 ] ] );
	const theirs = frame( 0x3569, [ ...u32( 0x30d41 ), 2, 3, ...u32( POTION ), 2, 0, 1, ...u32( POTION ), 7, 0 ] );
	state = defined( exchange.exchangeFrame( state, theirs, refs ) ).state;
	assert.deepEqual( state.theirs.map( r => r.slot ), [ 3, 1 ] );
	state = defined( exchange.exchangeFrame( state, frame( 0x30bb, [ 2, ...u32( 1000 ) ] ), refs ) ).state;
	assert.equal( state.theirGold, 1000 );

	assert.equal( exchange.exchangeRequest( state, { kind: "exchange-confirm" } ).opcode, 0x7095 );
	state = defined( exchange.exchangeFrame( state, frame( 0xb095, [ 1 ] ), refs ) ).state;
	assert.throws( () => exchange.exchangeRequest( state, { kind: "exchange-confirm" } ) );
	state = defined( exchange.exchangeFrame( state, frame( 0x37cf, [] ), refs ) ).state;
	assert.equal( exchange.exchangeRequest( state, { kind: "exchange-confirm" } ).opcode, 0x734a );
	assert.throws( () => exchange.exchangeRequest( state, { kind: "exchange-put", slot: 21 } ) );

	const swapped = defined( exchange.exchangeFrame( state, frame( 0x3272, [] ), refs ) );
	assert.equal( swapped.state.open, false );
	const bag = new Map( [ [ 13, { slot: 13 } ], [ 20, { slot: 20 } ] ] );
	const next = exchange.applyExchangeSwap( /** @type {any} */ (bag), defined( swapped.swap ), 13, 45 );
	// Slot 1's item lands first (slot 14), slot 3's next (15), then 20 empties.
	assert.deepEqual( [ ...next.keys() ].sort( ( a, b ) => a - b ), [ 13, 14, 15 ] );
	assert.equal( next.get( 14 )?.quantity, 7 );
	assert.equal( next.get( 15 )?.quantity, 2 );
});

test("refusals and failures carry their category-1 codes", () => {
	const closed = exchange.emptyExchange();
	assert.equal( exchange.exchangeFrame( closed, frame( 0xb237, [ 2, 0x28 ] ), refs )?.notice, 0x28 );
	const open = { ...closed, open: true, partner: 9 };
	const failed = defined( exchange.exchangeFrame( open, frame( 0x3457, [ 0x2c ] ), refs ) );
	assert.equal( failed.notice, 0x2c );
	assert.equal( failed.state.open, false );
	assert.equal( exchange.exchangeFrame( closed, frame( 0xb095, [ 1 ] ), refs ), null );
	const request = exchange.exchangeRequest( closed, { kind: "exchange-request", gid: 0x30d41 } );
	assert.deepEqual( [ request.opcode, ...request.payload ], [ 0x7237, ...u32( 0x30d41 ) ] );
	const gold = exchange.exchangeRequest( open, { kind: "exchange-gold", amount: 1000 } );
	assert.deepEqual( [ ...gold.payload ], [ 0x0d, ...u32( 1000 ) ] );
});
