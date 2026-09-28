/*
===========================================================================

remote-source-motion.test.mjs - source samples preserve remote navigation

Exercises the packet owner so a source update cannot silently become a halt
between the native destination and settlement packets.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);

const GID = 7;
const REGION = 257;
const SPEED = 50;

/*
================
flush
================
*/
function flush( owner ) {
	const batch = owner.take();
	if ( batch ) owner.ack( batch.sequence );
	return batch?.events ?? [];
}

/*
================
createPeer
================
*/
function createPeer() {
	const owner = createEntities();
	owner.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 1, kind: "npc" } ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: REGION, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush( owner );
	const payload = Buffer.alloc( 49 );
	payload.writeUInt32LE( 1 );
	payload.writeUInt32LE( GID, 4 );
	payload.writeUInt16LE( REGION, 8 );
	payload[25] = 3;
	payload.writeFloatLE( 20, 32 );
	payload.writeFloatLE( SPEED, 36 );
	payload[45] = 1;
	owner.receive( { opcode: 0x30d7, payload }, 0 );
	flush( owner );
	return owner;
}

/*
================
sourcePacket
================
*/
function sourcePacket( opcode, x ) {
	const payload = Buffer.alloc( 20 );
	const offset = opcode === 0x30e3 ? 0 : 4;
	payload.writeUInt32LE( GID, opcode === 0x30e3 ? 16 : 0 );
	payload.writeUInt16LE( REGION, offset );
	payload.writeFloatLE( x, offset + 2 );
	return { opcode, payload };
}

/*
================
destinationPacket
================
*/
function destinationPacket() {
	const payload = Buffer.alloc( 14 );
	payload.writeUInt32LE( GID );
	payload[4] = 1;
	payload.writeUInt16LE( REGION, 5 );
	payload.writeInt16LE( 100, 7 );
	return { opcode: 0xb738, payload };
}

test("source samples retain travel and advance between server ticks until explicit settlement", () => {
	const owner = createPeer();
	owner.receive( destinationPacket(), 0 );
	flush( owner );
	for ( let now = 100; now <= 500; now += 100 ) {
		owner.receive( sourcePacket( 0x30e3, SPEED * now / 1000 ), now );
		const source = flush( owner ).find( event => event.kind === "state" );
		assert.equal( source?.entity.moving, true, "source sample must not stop locomotion" );
		owner.step( now + 50 );
		const sampled = flush( owner ).find( event => event.kind === "state" );
		assert.ok( sampled, "destination remains active after source sample" );
		assert.equal( sampled.entity.moving, true );
		assert.equal( sampled.entity.x, SPEED * (now + 50) / 1000 );
	}
	owner.receive( sourcePacket( 0xb2f5, 30 ), 600 );
	const settled = flush( owner ).find( event => event.kind === "state" );
	assert.equal( settled?.entity.moving, false );
	owner.step( 700 );
	assert.equal( flush( owner ).length, 0, "settlement retires the path" );
	owner.dispose();
});

test("source synchronization does not start an idle entity walking", () => {
	const owner = createPeer();
	owner.receive( sourcePacket( 0x30e3, 10 ), 100 );
	const synchronized = flush( owner ).find( event => event.kind === "state" );
	assert.equal( synchronized?.entity.moving, false );
	owner.step( 200 );
	assert.equal( flush( owner ).length, 0 );
	owner.dispose();
});
