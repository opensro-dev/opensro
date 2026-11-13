/*
===========================================================================
shield-state.test.mjs - port-only absorption wire admission and expiry
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { shieldGauge, shieldGaugeFill } = await import( "../../src/engine/foundation/ui/shield-gauge.ts" );
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );

test("Bond feedback reaches the entity animation through the world dispatcher", () => {
	const core = createWorldCore( () => {} );
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		character: { name: "fixture", skills: [] },
		localPlayerEntry: { modelRef: 1907, startProfile: { regionId: 257, x: 0, y: 0, z: 0, angle: 0 } }
	} );
	core.receive( { opcode: 0x32a6, payload: Uint8Array.of( 7, 0, 0, 0, 0, 0, 0, 0 ) }, 0 );
	for ( const kind of [ 1, 2 ] ) {
		const payload = Buffer.alloc( kind === 1 ? 13 : 9 );
		payload.writeUInt32LE( 7, 0 );
		payload.writeUInt32LE( 8, 4 );
		payload[8] = kind;
		if ( kind === 1 ) payload.writeUInt32LE( 20, 9 );
		core.receive( { opcode: 0x3fb4, payload }, kind * 100 );
		core.step( kind * 100, false );
		const batch = defined( core.take() );
		assert.ok( batch.events.some( event => event.entity?.petBondPulse?.kind === kind ) );
		core.ack( batch.sequence );
	}
	core.dispose();
});

/*
================
shieldPacket
================
*/
function shieldPacket( amount = 300, duration = 5000, flags = 0 ) {
	const payload = Buffer.alloc( 21 );
	payload.writeUInt32LE( 7, 0 );
	payload.writeUInt32LE( amount, 4 );
	payload.writeUInt32LE( 1000, 8 );
	payload.writeUInt32LE( duration, 12 );
	payload.writeUInt32LE( 8, 16 );
	payload[20] = flags;
	return { opcode: 0x3fb2, payload };
}

test("shield packets retain separate absorption and retire the segment at exact expiry", () => {
	const entities = createEntities();
	entities.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	entities.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	entities.receive( shieldPacket(), 100 );
	assert.equal( defined( defined( entities.read( 7 ) ).shield ).amount, 300 );
	entities.receive( shieldPacket( 1500 ), 100 );
	assert.equal( defined( defined( entities.read( 7 ) ).shield ).amount, 1500 );
	assert.equal( shieldGauge( entities.read( 7 ), 1, 100 ).shield, .6 );
	entities.receive( shieldPacket(), 100 );
	assert.equal( shieldGauge( entities.read( 7 ), 1, 5099 ).amount, 300 );
	assert.equal( shieldGauge( entities.read( 7 ), 1, 5100 ).amount, 0 );
	entities.receive( shieldPacket( 120, 4000 ), 1100 );
	assert.equal( defined( defined( entities.read( 7 ) ).shield ).untilMs, 5100 );
	entities.receive( shieldPacket( 0, 0, 1 ), 5100 );
	assert.equal( defined( defined( entities.read( 7 ) ).shield ).burst, true );
	assert.equal( shieldGauge( entities.read( 7 ), .5, 5100 ).health, .5 );
	entities.dispose();
});

test("shield decoder rejects oversized, truncated and inconsistent updates", () => {
	const entities = createEntities();
	for (
		const packet of [
			shieldPacket( 1501 ),
			shieldPacket( 300, 5001 ),
			shieldPacket( 300, 5000, 1 ),
			shieldPacket( 0, 0, 2 )
		]
	) {
		assert.throws( () => entities.receive( packet, 0 ), /Invalid shield/ );
	}
	const packet = shieldPacket();
	for ( const length of [ 0, 20, 22 ] ) {
		assert.throws( () => entities.receive( { ...packet, payload: Buffer.alloc( length ) }, 0 ), /Invalid shield/ );
	}
	entities.dispose();
});

test("shield segments use a light-gray HP-style highlight without changing their bounds", () => {
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 0, 0, 200, 100 ];
	const rows = shieldGaugeFill( [ 40, 20, 50, 7.5 ], clip );
	assert.equal( rows.length, 8 );
	assert.ok( rows[3].color[0] > rows[0].color[0] );
	assert.ok( rows[3].color[0] > rows[7].color[0] );
	for ( const row of rows ) {
		assert.equal( row.color[0], row.color[1] );
		assert.equal( row.color[1], row.color[2] );
		assert.ok( row.color[0] >= .55 && row.color[0] < .9 );
		assert.ok( row.rect[1] + row.rect[3] <= 27.5 );
		assert.deepEqual( row.clip, clip );
	}
	assert.deepEqual( shieldGaugeFill( [ 0, 0, 0, 8 ], clip ), [] );
});

test("retaliation pulse validates its recipient and leaves emotes independent", () => {
	const entities = createEntities();
	entities.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	entities.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	entities.receive( { opcode: 0x324b, payload: Buffer.from( [ 7, 0, 0, 0, 18 ] ) }, 10 );
	const emote = defined( entities.read( 7 ) ).emote;
	const packet = { opcode: 0x3fb3, payload: Buffer.from( [ 7, 0, 0, 0 ] ) };
	entities.receive( packet, 100 );
	entities.receive( packet, 200 );
	assert.deepEqual( defined( entities.read( 7 ) ).petCounterPulse, { revision: 2, atMs: 200 } );
	assert.deepEqual( defined( entities.read( 7 ) ).emote, emote );
	for ( const payload of [ Buffer.alloc( 0 ), Buffer.alloc( 3 ), Buffer.alloc( 5 ), Buffer.alloc( 4 ) ] ) {
		assert.throws( () => entities.receive( { opcode: 0x3fb3, payload }, 0 ), /Invalid pet counter pulse/ );
	}
	entities.dispose();
});

/*
================
Guardian's Bond wire boundaries
================
*/
test("Guardian pulses admit only exact recipient/bear/kind packets", () => {
	const entities = createEntities();
	entities.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	entities.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	const payload = Buffer.from( [ 7, 0, 0, 0, 8, 0, 0, 0, 2 ] );
	entities.receive( { opcode: 0x3fb4, payload }, 100 );
	assert.deepEqual( defined( entities.read( 7 ) ).petBondPulse, { revision: 1, atMs: 100, bear: 8, kind: 2 } );
	payload[8] = 1;
	entities.receive( { opcode: 0x3fb4, payload }, 200 );
	assert.equal( defined( defined( entities.read( 7 ) ).petBondPulse ).revision, 2 );
	for (
		const bad of [
			Buffer.alloc( 8 ),
			Buffer.alloc( 10 ),
			Buffer.alloc( 9 ),
			Buffer.from( [ 7, 0, 0, 0, 8, 0, 0, 0, 3 ] )
		]
	) {
		assert.throws( () => entities.receive( { opcode: 0x3fb4, payload: bad }, 0 ), /Invalid pet bond/ );
	}
	entities.dispose();
});

/*
================
Unbroken Spirit protocol
================
*/
test("Resolve validates stacks, tiers and lifetime without touching emotes", () => {
	const entities = createEntities();
	entities.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	entities.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	const payload = Buffer.from( [ 7, 0, 0, 0, 10, 3, 0x98, 0x3a, 0, 0 ] );
	entities.receive( { opcode: 0x3fb5, payload }, 100 );
	assert.deepEqual( defined( entities.read( 7 ) ).petResolve, { stacks: 10, tier: 3, atMs: 100, untilMs: 15100 } );
	for (
		const bad of [
			Buffer.alloc( 9 ),
			Buffer.alloc( 11 ),
			Buffer.alloc( 10 ),
			Buffer.from( [ 7, 0, 0, 0, 11, 3, 0, 0, 0, 0 ] ),
			Buffer.from( [ 7, 0, 0, 0, 10, 0, 0, 0, 0, 0 ] ),
			Buffer.from( [ 7, 0, 0, 0, 1, 1, 255, 255, 255, 255 ] )
		]
	) {
		assert.throws( () => entities.receive( { opcode: 0x3fb5, payload: bad }, 0 ), /Invalid pet Resolve/ );
	}
	entities.receive( { opcode: 0x3fb5, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0, 0, 0 ] ) }, 200 );
	assert.equal( defined( defined( entities.read( 7 ) ).petResolve ).stacks, 0 );
	entities.dispose();
});
