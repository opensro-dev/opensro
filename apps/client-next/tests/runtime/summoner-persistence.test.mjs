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
	game.receive( { opcode: 0x36ab, payload: Uint8Array.of( 41, 35, 0, 0 ) }, 2 );
	state = game.take();
	assert.equal( state?.cosRecords?.length, 0 );
	assert.equal( state?.inventory?.find( row => row.slot === 23 )?.summon?.state, 3 );
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
