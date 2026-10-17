/*
===========================================================================

vitals.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { vitalsUpdate } = await load( "foundation/gameplay/vitals.ts" );
const { createCombat } = await load( "runtime/simulation/worker/session/world/gameplay/combat/combat.ts" );
const { createGameplay } = await load( "runtime/simulation/worker/session/world/gameplay/gameplay.ts" );
function packet( flags, mask = 0x80000061 ) {
	const bytes = [ 1, 0, 0, 0, 0, 4, flags ];
	if ( flags & 1 ) bytes.push( 25, 0, 0, 0 );
	if ( flags & 2 ) bytes.push( 70, 0, 0, 0 );
	if ( flags & 8 ) bytes.push( 0x34, 0x12 );
	if ( flags & 4 ) {
		bytes.push( mask & 255, mask >>> 8 & 255, mask >>> 16 & 255, mask >>> 24 );
		for ( let bit = 0; bit < 32; bit++ ) {
			if ( (mask & (2 ** bit)) && (0x017fcfc0 & (2 ** bit)) ) bytes.push( bit + 1 );
		}
	}
	return Uint8Array.from( bytes );
}
test("33A6 covers all sixteen field combinations, including satiety before abnormal", () => {
	for ( let flags = 0; flags < 256; flags++ ) {
		const expected = { gid: 1, sourceFlags: 0x400 };
		if ( flags & 1 ) expected.hp = 25;
		if ( flags & 2 ) expected.mp = 70;
		if ( flags & 8 ) expected.satiety = 0x1234;
		if ( flags & 4 ) {
			expected.abnormal = 0x80000061;
			expected.abnormalLevels = [ { bit: 64, level: 7 } ];
		}
		assert.deepEqual( vitalsUpdate( packet( flags ) ), expected );
	}
});
test("every abnormal bit consumes exactly its native level-byte branch", () => {
	for ( let index = 0; index < 32; index++ ) {
		const bit = 2 ** index, p = packet( 4, bit ), r = vitalsUpdate( p );
		assert.equal( r.abnormal, bit );
		assert.deepEqual( r.abnormalLevels, bit & 0x017fcfc0 ? [ { bit, level: index + 1 } ] : [] );
	}
	assert.equal( vitalsUpdate( packet( 4, 0xffffffff ) ).abnormalLevels.length, 16 );
	assert.deepEqual( vitalsUpdate( packet( 4, 0 ) ).abnormalLevels, [] );
});
test("malformed packets never partially apply HP, MP or abnormal state", () => {
	const combat = createCombat();
	combat.seed( 1, { hp: 100, mp: 100, maxHp: 200 } );
	const p = packet( 15, 0xffffffff ), before = combat.state().vitals;
	for ( let end = 0; end < p.length; end++ ) {
		assert.throws( () => combat.receive( 0x33a6, p.subarray( 0, end ) ) );
		assert.deepEqual( combat.state().vitals, before );
	}
	for ( const malformed of [ Uint8Array.from( [ ...p, 0 ] ) ] ) {
		assert.throws( () => combat.receive( 0x33a6, malformed ) );
		assert.deepEqual( combat.state().vitals, before );
	}
	combat.receive( 0x33a6, p );
	combat.receive( 0x33a6, packet( 4, 0 ) );
	assert.deepEqual( combat.state().vitals[0], {
		gid: 1,
		hp: 25,
		mp: 70,
		maxHp: 200,
		satiety: 0x1234,
		abnormal: 0,
		abnormalLevels: []
	} );
});
test("reseeding and world travel preserve current vitals; a new login replaces them", () => {
	const g = createGameplay( () => {} ), local = { gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 };
	g.bootstrap( { character: { hp: 100, mp: 100, maxHp: 200, maxMp: 200 } } );
	g.seed( local );
	g.receive( { opcode: 0x33a6, payload: packet( 7 ) }, 0 );
	g.seed( local );
	assert.equal( g.take().vitals[0].hp, 25 );
	g.resetWorld();
	g.seed( local );
	assert.equal( g.take().vitals[0].hp, 25 );
	g.bootstrap( { character: { hp: 90, mp: 80, maxHp: 200, maxMp: 200 } } );
	g.seed( local );
	assert.equal( g.take().vitals[0].hp, 90 );
	g.dispose();
});
test("33A6 ignores unknown foreign GIDs while accepting spawned entities and owned companions", () => {
	const entities = new Map( [ [ 2, { gid: 2, kind: "monster", regionId: 1, x: 0, y: 0, z: 0, heading: 0 } ] ] );
	const g = createGameplay( () => {}, () => {}, () => {}, gid => entities.get( gid ) );
	const local = { gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 };
	g.bootstrap( {
		character: { hp: 100, mp: 100, maxHp: 200, maxMp: 200 },
		refObjSnapshot: [ { kind: "cos", refObjId: 9, tidWord: 0x09c6 } ]
	} );
	g.seed( local );
	const vitalFor = ( gid, hp ) => {
		const p = new Uint8Array( 11 ), v = new DataView( p.buffer );
		v.setUint32( 0, gid, true );
		v.setUint16( 4, 0x400, true );
		p[6] = 1;
		v.setUint32( 7, hp, true );
		return p;
	};
	assert.equal( g.receive( { opcode: 0x33a6, payload: vitalFor( 999, 50 ) }, 0 ), false );
	assert.deepEqual( g.take().vitals.map( row => row.gid ), [ 1 ] );
	assert.equal( g.receive( { opcode: 0x33a6, payload: vitalFor( 2, 75 ) }, 0 ), true );
	assert.deepEqual( g.take().vitals.map( row => [ row.gid, row.hp ] ), [ [ 1, 100 ], [ 2, 75 ] ] );
	entities.delete( 2 );
	g.entityLifecycle( { kind: "despawn", gid: 2 } );
	assert.equal( g.receive( { opcode: 0x33a6, payload: vitalFor( 2, 40 ) }, 0 ), false );
	assert.deepEqual( g.take().vitals.map( row => row.gid ), [ 1 ] );
	// Owned companion (0x3158) is accepted via cosRecords even while out of view,
	// and a type-1 0x3508 detach while out of view retires its vitals row so
	// later 0x33A6 packets for the detached GID are rejected.
	const cosRecord = new Uint8Array( 17 ), cosView = new DataView( cosRecord.buffer );
	cosView.setUint32( 0, 3, true );
	cosView.setUint32( 4, 9, true );
	cosView.setUint32( 8, 100, true );
	assert.equal( g.receive( { opcode: 0x3158, payload: cosRecord }, 0 ), true );
	assert.equal( g.receive( { opcode: 0x33a6, payload: vitalFor( 3, 80 ) }, 0 ), true );
	assert.deepEqual( g.take().vitals.map( row => [ row.gid, row.hp ] ), [ [ 1, 100 ], [ 3, 80 ] ] );
	assert.equal( g.receive( { opcode: 0x3508, payload: Uint8Array.of( 3, 0, 0, 0, 1 ) }, 0 ), true );
	assert.equal( g.receive( { opcode: 0x33a6, payload: vitalFor( 3, 60 ) }, 0 ), false );
	assert.deepEqual( g.take().vitals.map( row => row.gid ), [ 1 ] );
	g.dispose();
});
