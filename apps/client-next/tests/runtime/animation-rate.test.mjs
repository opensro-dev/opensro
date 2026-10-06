/*
===========================================================================

animation-rate.test.mjs - action speed across wire and entity lifecycles

Exercises production decoders and the worker owner, including mounted routing
and replacement spawns. Movement speeds must remain independent.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { animationRate, decodeAnimationSpeed } = await import(
	"../../src/engine/foundation/animation/animation-rate.ts"
);
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);

/*
================
speed
================
*/
function speed( gid, denominator ) {
	const payload = Buffer.alloc( 8 );
	payload.writeUInt32LE( gid );
	payload.writeFloatLE( denominator, 4 );
	return { opcode: 0x3453, payload };
}

/*
================
spawn
================
*/
function spawn( gid, denominator ) {
	const payload = Buffer.alloc( 57 );
	payload.writeUInt32LE( 3914 );
	payload.writeUInt32LE( gid, 4 );
	payload.writeUInt16LE( 257, 8 );
	payload[25] = 3;
	payload.writeFloatLE( 20, 32 );
	payload.writeFloatLE( 50, 36 );
	payload.writeFloatLE( denominator, 40 );
	payload[45] = 1;
	payload.writeUInt32LE( 7, 52 );
	return { opcode: 0x30d7, payload };
}

test("action-speed wire converts the denominator once at float32 precision", () => {
	for ( const denominator of [ 100, 125, 200, 135 ] ) {
		assert.deepEqual( decodeAnimationSpeed( speed( 7, denominator ).payload ), {
			gid: 7,
			rate: Math.fround( 100 / denominator )
		} );
	}
	for ( const invalid of [ 0, -1, NaN, Infinity ] ) assert.throws( () => animationRate( invalid ) );
	for ( let length = 0; length < 8; length++ ) assert.throws( () => decodeAnimationSpeed( Buffer.alloc( length ) ) );
});

test("local bootstrap, mounted updates and replacement spawns retain the correct actor rate", () => {
	const owner = createEntities();
	owner.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 3914, tidWord: 0x11c6, kind: "cos" } ],
		localPlayerEntry: {
			modelRef: 1933,
			actionSpeed: 125,
			startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 }
		}
	} );
	owner.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	owner.receive( spawn( 8, 200 ), 0 );
	assert.equal( defined( owner.read( 7 ) ).animationRate, Math.fround( .8 ) );
	assert.equal( defined( owner.read( 8 ) ).animationRate, .5 );
	owner.receive( { opcode: 0xb4b5, payload: Buffer.from( [ 1, 7, 0, 0, 0, 1, 8, 0, 0, 0 ] ) }, 0 );
	owner.receive( speed( 7, 100 ), 1 );
	assert.equal( defined( owner.read( 8 ) ).animationRate, 1 );
	assert.equal( defined( owner.read( 7 ) ).animationRate, Math.fround( .8 ) );
	assert.equal( defined( owner.read( 8 ) ).runSpeed, 50 );
	assert.throws( () => owner.receive( speed( 8, 0 ), 2 ) );
	assert.equal( defined( owner.read( 8 ) ).animationRate, 1 );
	owner.receive( { opcode: 0x36ab, payload: Buffer.from( [ 8, 0, 0, 0 ] ) }, 3 );
	owner.receive( speed( 8, 125 ), 4 );
	assert.equal( owner.read( 8 ), undefined );
	owner.receive( spawn( 8, 100 ), 5 );
	assert.equal( defined( owner.read( 8 ) ).animationRate, 1 );
	assert.equal( defined( owner.read( 7 ) ).mountedOn, undefined );
	owner.dispose();
});
