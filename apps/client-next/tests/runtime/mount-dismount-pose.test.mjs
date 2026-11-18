/*
===========================================================================

mount-dismount-pose.test.mjs - a vehicle left stands where the ride ended

A trader rode its transport out of a town; on dismount the
transport was drawn back at the shop where the ride began. While
the local player rides, its walk belongs to the gameplay owner and the
mount's own row never moves; the dismount (0xB4B5 state 0) now puts that
row at the rider's live pose, as native's one mover leaves it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );
const REGION = 0x5ca0;
const PLAYER_GID = 7;
const MOUNT_GID = 123;
const START = { regionId: REGION, x: 100, y: 0, z: 100, angle: 0 };
const END = { regionId: REGION, x: 300, y: 0, z: 140, angle: 0 };

/*
================
flush
================
*/
function flush( core, now ) {
	core.step( now, false );
	const batch = core.take();
	if ( batch ) core.ack( batch.sequence );
	return batch?.events ?? [];
}

/*
================
mountSpawn

The 0x30D7 COS spawn of the transport where the ride begins.
================
*/
function mountSpawn() {
	const p = Buffer.alloc( 57 );
	p.writeUInt32LE( 2183, 0 );
	p.writeUInt32LE( MOUNT_GID, 4 );
	p.writeUInt16LE( REGION, 8 );
	p.writeFloatLE( START.x, 10 );
	p.writeFloatLE( START.y, 14 );
	p.writeFloatLE( START.z, 18 );
	p[25] = 1;
	p[31] = 4;
	p.writeFloatLE( 20, 32 );
	p.writeFloatLE( 50, 36 );
	p.writeFloatLE( 1, 40 );
	p[45] = 1;
	return p;
}

/*
================
transportRecord

The owner's 0x3158 record of the transport (band 2: identity, HP, an
empty bag, alive), which makes it the ridden COS a click moves.
================
*/
function transportRecord() {
	const p = Buffer.alloc( 21 );
	p.writeUInt32LE( MOUNT_GID, 0 );
	p.writeUInt32LE( 2183, 4 );
	p.writeUInt32LE( 100, 8 );
	p.writeUInt32LE( 50, 12 );
	return p;
}

/*
================
ride

The 0xB4B5 ride result: [1][u32 rider][u8 state][u32 mount].
================
*/
function ride( rider, mounted ) {
	const p = Buffer.alloc( 10 );
	p[0] = 1;
	p.writeUInt32LE( rider, 1 );
	p[5] = mounted ? 1 : 0;
	p.writeUInt32LE( MOUNT_GID, 6 );
	return { opcode: 0xb4b5, payload: p };
}

/*
================
correction

A 0xB2F5 stop of the local player at a pose: [u32 gid][u16 region]
[f32 x][f32 y][f32 z][u16 heading].
================
*/
function correction( pose ) {
	const p = Buffer.alloc( 20 );
	p.writeUInt32LE( PLAYER_GID, 0 );
	p.writeUInt16LE( pose.regionId, 4 );
	p.writeFloatLE( pose.x, 6 );
	p.writeFloatLE( pose.y, 10 );
	p.writeFloatLE( pose.z, 14 );
	return { opcode: 0xb2f5, payload: p };
}

/*
================
riddenCore

A world with the local player riding the transport: the transport's row
stands at START and the rider's own mover at END. While the local player
rides, only its own mover walks (the gameplay owner drives the mount in
presentation), so a ride that ended at END leaves exactly this state.
================
*/
function riddenCore() {
	const core = createWorldCore( () => {} );
	core.bootstrap( {
		protocolVersion: 2,
		simulationProtocolVersion: 1,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 2183, kind: "cos", tidWord: 0x11c6 } ],
		refItemSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: START }
	} );
	core.receive( { opcode: 0x32a6, payload: Buffer.from( [ PLAYER_GID, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	flush( core, 0 );
	core.receive( { opcode: 0x30d7, payload: mountSpawn() }, 0 );
	core.receive( { opcode: 0x3158, payload: transportRecord() }, 0 );
	core.receive( correction( END ), 10 );
	core.receive( ride( PLAYER_GID, true ), 20 );
	const events = flush( core, 20 );
	assert.ok(
		Math.abs( mountState( events )?.x ?? START.x ) - START.x < .5,
		"the ride leaves the mount's row where it stood"
	);
	return core;
}

/*
================
mountState

The last published row of the mount in a batch's events.
================
*/
function mountState( events ) {
	return events.filter( e => e.kind === "state" && e.entity.gid === MOUNT_GID ).at( -1 )?.entity;
}

test("the local player's dismount leaves the vehicle where the ride ended", () => {
	const core = riddenCore();
	core.receive( ride( PLAYER_GID, false ), 2000 );
	const mount = mountState( flush( core, 2000 ) );
	assert.ok( mount, "the dismount publishes the vehicle's row" );
	assert.equal( mount.regionId, END.regionId );
	assert.ok(
		Math.abs( mount.x - END.x ) < .5 && Math.abs( mount.z - END.z ) < .5,
		`vehicle at ${mount.x},${mount.z}`
	);
	assert.equal( mount.movementPath, undefined );
	core.dispose();
});

test("a refused ride request leaves the ridden vehicle's row alone", () => {
	const core = riddenCore();
	core.receive( { opcode: 0xb4b5, payload: Buffer.from( [ 2, 5 ] ) }, 2000 );
	assert.equal( mountState( flush( core, 2000 ) ), undefined );
	core.dispose();
});
