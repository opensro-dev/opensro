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
import fs from "node:fs";
import path from "node:path";
import { root } from "../../tools/project.mjs";
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
	const owner = createEntities( undefined, undefined, undefined, ( _from, to ) => to );
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
	payload.writeFloatLE( 100, 40 );
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

/*
================
figureSpawn

The one figure spawn (0x30D7) for the correction-death tests: a
character spawn of the owner's ref kind at x 20 (57 bytes for a COS,
whose spawn carries the owner tail; 49 otherwise).
================
*/
function figureSpawn( refObjId, gid, cos ) {
	// decodeCharacterSpawn's wire order: ids, position (x at 10), the
	// life byte at 29, the speed scalars at 32/36/40, then the class
	// tail (a monster's grade byte; a band-2 COS's owner tail), so a
	// monster packet is 48 bytes and a COS packet 57.
	const payload = Buffer.alloc( cos ? 57 : 48 );
	payload.writeUInt32LE( refObjId );
	payload.writeUInt32LE( gid, 4 );
	payload.writeUInt16LE( REGION, 8 );
	payload.writeFloatLE( 20, 10 );
	payload.writeFloatLE( 100, 18 );
	payload[29] = 1;
	payload.writeFloatLE( 20, 32 );
	payload.writeFloatLE( SPEED, 36 );
	payload.writeFloatLE( 1, 40 );
	if ( cos ) payload[45] = 1;
	return { opcode: 0x30d7, payload };
}

/*
================
deathState

777B60 channel 0: the life state (2 is dead) for one gid.
================
*/
function deathState( gid ) {
	const payload = Buffer.alloc( 6 );
	payload.writeUInt32LE( gid );
	payload[5] = 2;
	return { opcode: 0x3122, payload };
}

/*
================
correctionFor

A 0xB2F5 position correction naming one gid at the given x.
================
*/
function correctionFor( gid, x ) {
	const payload = Buffer.alloc( 20 );
	payload.writeUInt32LE( gid );
	payload.writeUInt16LE( REGION, 4 );
	payload.writeFloatLE( x, 6 );
	return { opcode: 0xb2f5, payload };
}

/*
================
figureOwner

An entities owner bootstrapped with the monster and COS references the
correction-death tests spawn from.
================
*/
function figureOwner() {
	const owner = createEntities( undefined, undefined, undefined, ( _from, to ) => to );
	owner.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [
			{ refObjId: 2, kind: "monster", tidWord: 0xc6 },
			{ refObjId: 3, kind: "cos", tidWord: 0x11c6 }
		],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: REGION, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush( owner );
	return owner;
}

test("a dead monster's or COS's position correction moves nothing (775B50)", () => {
	// CPSMission_OnEntityMoveUpdate0xB2F5 (775B50): after the mover is
	// resolved (85E000), a monster or COS mover whose life state (+0x263)
	// is 2 (dead) returns without correcting; players are not checked.
	for ( const [refObjId, cos] of [ [ 2, false ], [ 3, true ] ] ) {
		const owner = figureOwner();
		owner.receive( figureSpawn( refObjId, GID, cos ), 0 );
		flush( owner );
		owner.receive( deathState( GID ), 100 );
		flush( owner );
		const corpse = owner.read( GID );
		assert.equal( corpse?.appearanceState?.[0], 2, "the figure is dead before the correction" );
		owner.receive( correctionFor( GID, 30 ), 200 );
		const events = flush( owner ).filter( event => event.kind === "state" );
		assert.deepEqual( events, [], "a dead monster or COS ignores its correction" );
		assert.deepEqual( owner.read( GID ), corpse, "the dead row is unchanged" );
		owner.dispose();
	}
});

test("a living monster's position correction still applies (775B50)", () => {
	const owner = figureOwner();
	owner.receive( figureSpawn( 2, GID, false ), 0 );
	flush( owner );
	owner.receive( correctionFor( GID, 30 ), 100 );
	const events = flush( owner ).filter( event => event.kind === "state" );
	assert.equal( events.length, 1, "the living monster takes its correction" );
	assert.equal( owner.read( GID )?.x, 30 );
	owner.dispose();
});

test("a dead player's position correction still applies (775B50 checks monsters and COS only)", () => {
	const fixture = JSON.parse(
		fs.readFileSync(
			path.join( root, "../../apps/server/internal/game/world/simulation/testdata/peer_spawn_row_fixture.json" ),
			"utf8"
		)
	);
	const row = fixture.scenarios[0];
	const owner = createEntities( undefined, undefined, undefined, ( _from, to ) => to );
	owner.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: row.modelRefObjId, kind: "player" } ],
		refItemSnapshot: row.equipment ?? [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: REGION, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush( owner );
	owner.receive( { opcode: 0x30d7, payload: Uint8Array.from( Buffer.from( row.payloadHex, "hex" ) ) }, 0 );
	flush( owner );
	owner.receive( deathState( row.gid ), 100 );
	flush( owner );
	assert.equal( owner.read( row.gid )?.appearanceState?.[0], 2, "the player is dead before the correction" );
	owner.receive( correctionFor( row.gid, row.x + 25 ), 200 );
	const events = flush( owner ).filter( event => event.kind === "state" );
	assert.equal( events.length, 1, "a dead player still takes its correction (present rebirth)" );
	assert.equal( owner.read( row.gid )?.x, row.x + 25 );
	owner.dispose();
});

test("a rider's movement moves its mount (CCharactor_GetActiveMoverEntity 0x85E000)", () => {
	const owner = createPeer();
	const MOUNT = 8;
	const spawn = Buffer.alloc( 49 );
	spawn.writeUInt32LE( 1 );
	spawn.writeUInt32LE( MOUNT, 4 );
	spawn.writeUInt16LE( REGION, 8 );
	spawn[25] = 3;
	spawn.writeFloatLE( 20, 32 );
	spawn.writeFloatLE( SPEED, 36 );
	spawn.writeFloatLE( 100, 40 );
	spawn[45] = 1;
	owner.receive( { opcode: 0x30d7, payload: spawn }, 0 );
	owner.receive( { opcode: 0xb4b5, payload: Buffer.from( [ 1, GID, 0, 0, 0, 1, MOUNT, 0, 0, 0 ] ) }, 0 );
	flush( owner );
	owner.receive( destinationPacket(), 0 );
	const moved = flush( owner ).filter( event => event.kind === "state" );
	assert.deepEqual( moved.map( event => event.entity.gid ), [ MOUNT ], "the mount takes the rider's travel" );
	assert.ok( moved[0].entity.movementPath, "the mount walks the rider's path" );
	owner.receive( sourcePacket( 0xb2f5, 30 ), 100 );
	const corrected = flush( owner ).filter( event => event.kind === "state" );
	assert.deepEqual( corrected.map( event => event.entity.gid ), [ MOUNT ], "a rider's correction places the mount" );
	owner.dispose();
});
