/*
===========================================================================

ui-sound-events.test.mjs - tests for inventory.ts, gameplay.ts,
presentation.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createPresentation } = await import( "../../src/engine/runtime/presentation/presentation.ts" );
function equipment( durability ) {
	const p = Buffer.alloc( 18 );
	p.writeUInt32LE( 1 );
	p.writeUInt32LE( durability, 13 );
	return [ ...p ];
}
function durability( slot, value ) {
	const p = Buffer.alloc( 5 );
	p[0] = slot;
	p.writeInt32LE( value, 1 );
	return p;
}
test("durability audio follows native signed comparisons, slot gates and exact thresholds", () => {
	const sounds = [], owner = createInventory( () => {}, handle => sounds.push( handle ) );
	owner.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x2c } ],
		equipItems: [ { slot: 0, refObjId: 1, body: equipment( 7 ) }, { slot: 13, refObjId: 1, body: equipment( 7 ) } ]
	} );
	for ( const n of [ 6, 5, 0, 10, 11 ] ) owner.receive( 0x31e8, durability( 0, n ) );
	assert.deepEqual( sounds, [ "SND_EQDANGER", "SND_EQBREAK", "SND_REVIVE", "SND_REPAIR" ] );
	sounds.length = 0;
	for ( const n of [ 6, 0, 10 ] ) owner.receive( 0x31e8, durability( 13, n ) );
	assert.deepEqual( sounds, [ "SND_REVIVE" ] );
	owner.receive( 0x31e8, durability( 61, 0 ) );
	assert.equal( sounds.length, 1 );
	const before = owner.state().inventory;
	assert.throws( () => owner.receive( 0x31e8, Buffer.alloc( 4 ) ) );
	assert.throws( () => owner.receive( 0x31e8, durability( 12, 0 ) ) );
	assert.deepEqual( owner.state().inventory, before );
	assert.equal( sounds.length, 1 );
});
test("a durability increase flashes the repaired slot; wear does not (77C300 -> 54FA20)", () => {
	const owner = createInventory( () => {}, () => {} );
	owner.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x2c } ],
		equipItems: [ { slot: 0, refObjId: 1, body: equipment( 7 ) }, { slot: 13, refObjId: 1, body: equipment( 7 ) } ]
	} );
	owner.receive( 0x31e8, durability( 0, 6 ), 100 );
	assert.deepEqual( owner.state().itemFlashes, [] );
	owner.receive( 0x31e8, durability( 0, 30 ), 200 );
	owner.receive( 0x31e8, durability( 13, 30 ), 300 );
	assert.deepEqual( owner.state().itemFlashes, [
		{ slot: 0, kind: "repair", atMs: 200 },
		{ slot: 13, kind: "repair", atMs: 300 }
	] );
});
test("only accepted potion-family use emits a potion cue", () => {
	const cues = [], owner = createInventory( () => {}, h => cues.push( h ) );
	const body = [ 1, 0, 0, 0, 3, 0 ];
	owner.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0xec } ],
		equipItems: [ { slot: 13, refObjId: 1, body } ]
	} );
	owner.use( 13, 0 );
	assert.deepEqual( cues, [] );
	owner.receive( 0xb5bd, Uint8Array.of( 2, 1 ) );
	assert.deepEqual( cues, [] );
	owner.use( 13, 1 );
	owner.receive( 0xb5bd, Uint8Array.of( 1, 13, 2, 0, 0xec, 0 ) );
	assert.deepEqual( cues, [ "SND_POTION" ] );
	assert.throws( () => owner.receive( 0xb5bd, Uint8Array.of( 1, 13, 2 ) ) );
	assert.equal( cues.length, 1 );
});
test("quest and local level-up cues retain occurrence time and reject malformed packets before emission", () => {
	const cues = [], game = createGameplay( () => {}, ( handle, at ) => cues.push( { handle, at } ) );
	game.receive( { opcode: 0x36b0, payload: Buffer.alloc( 4 ) }, 0 );
	assert.equal( cues.length, 0 );
	game.seed( { gid: 7, regionId: 257, x: 0, y: 0, z: 0, heading: 0 } );
	game.receive( { opcode: 0xb29a, payload: Uint8Array.of( 2, 1 ) }, 10 );
	assert.equal( cues.length, 0 );
	game.receive( { opcode: 0xb29a, payload: Uint8Array.of( 1, 9, 0, 0, 0 ) }, 20 );
	const key = Buffer.from( "SN_TALK_COMMON_END" );
	game.receive( { opcode: 0x36bf, payload: Buffer.concat( [ Buffer.from( [ key.length, 0 ] ), key ] ) }, 30 );
	game.receive( { opcode: 0x36b0, payload: Uint8Array.of( 8, 0, 0, 0 ) }, 40 );
	game.receive( { opcode: 0x36b0, payload: Uint8Array.of( 7, 0, 0, 0 ) }, 50 );
	assert.deepEqual( cues, [ { handle: "SND_QUEST", at: 20 }, { handle: "SND_QUEST_END", at: 30 }, {
		handle: "SND_LEVUP",
		at: 50
	} ] );
	for ( const opcode of [ 0xb29a, 0x36bf, 0x36b0 ] ) {
		assert.throws( () => game.receive( { opcode, payload: new Uint8Array() }, 60 ) );
	}
	assert.equal( cues.length, 3 );
});
test("sound journal is transactional, drains once and discards pre-reset cues", () => {
	const p = createPresentation(), sound = { kind: "ui-sound", handle: "SND_POTION", at: 1 };
	p.apply( { sequence: 1, events: [ sound, sound ] } );
	assert.deepEqual( p.takeSounds(), [ sound, sound ] );
	assert.deepEqual( p.takeSounds(), [] );
	assert.throws( () => p.apply( { sequence: 2, events: [ sound, { kind: "state", entity: { gid: 99 } } ] } ) );
	assert.deepEqual( p.takeSounds(), [] );
	p.apply( { sequence: 2, events: [ sound, { kind: "reset", epoch: 1 }, { ...sound, at: 2 } ] } );
	assert.deepEqual( p.takeSounds(), [ { ...sound, at: 2 } ] );
	p.dispose();
	assert.deepEqual( p.takeSounds(), [] );
});

test("gameplay routes quest refusals to pending state and native abort notice without success audio", () => {
	const sent = [],
		cues = [],
		game = createGameplay( f => sent.push( f ), ( handle, at ) => cues.push( { handle, at } ) );
	game.seed( { gid: 7, regionId: 257, x: 0, y: 0, z: 0, heading: 0 } );
	game.receive( { opcode: 0x31ed, payload: Buffer.from( "010700000000000802", "hex" ) }, 0 );
	game.take();
	for ( const reward of [ true, false ] ) {
		const command = { kind: reward ? "quest-reward" : "quest-abandon", refId: 7 };
		game.command( command, 1, undefined );
		assert.equal( defined( game.take() ).questPending, 7 );
		const opcode = reward ? 0xb29a : 0xb1eb;
		assert.throws( () => game.receive( { opcode, payload: Uint8Array.of( 2 ) }, 2 ) );
		assert.equal( game.take(), null );
		game.receive( { opcode, payload: Uint8Array.of( 2, 4 ) }, 3 );
		const state = game.take();
		assert.equal( defined( state ).questPending, 0 );
		assert.equal( defined( defined( state ).quests ).length, 1 );
		assert.equal( defined( defined( state ).notices ).length, reward ? 0 : 1 );
		if ( !reward ) {
			assert.equal( defined( defined( state ).notices )[0].key, "UIIT_MSG_SR_ABORT_QUEST_ERROR_NOT_ALLOWED" );
		}
		game.command( command, 4, undefined );
		game.receive( { opcode, payload: Uint8Array.of( 2, 0 ) }, 5 );
		assert.equal( defined( game.take() ).questPending, 0 );
	}
	assert.equal( sent.length, 4 );
	assert.deepEqual( cues, [] );
});
