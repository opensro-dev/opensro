/*
===========================================================================

world-replay.test.mjs - tests for tools/world-replay.mjs

Replays the server's monster spawn fixture through the world core and the
presentation, and checks that checkpoints restore identical witnesses.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createWorldReplay } from "../../tools/world-replay.mjs";
import { root } from "../../tools/project.mjs";
const fixture = JSON.parse(
	fs.readFileSync(
		path.resolve( root, "../server/internal/game/enterworld/testdata/monster_spawn_fixture.json" ),
		"utf8"
	)
);
const pose = { regionId: 25256, x: 1, y: 2, z: 3, angle: 0 };
const bootstrap = {
	protocolVersion: 2,
	nativeResult: 1,
	simulationProtocolVersion: 1,
	refObjSnapshot: fixture.refObjSnapshot,
	localPlayerEntry: { modelRef: 1933, startProfile: pose }
};
const packets = fixture.packets.map( row => ({
	kind: "packet",
	atMs: 0,
	opcode: row.opcode,
	payload: [ ...Buffer.from( row.payloadHex, "hex" ) ]
}) );
/*
================
latch

The journal event that latches the bootstrap into the world.
================
*/
function latch() {
	const p = Buffer.alloc( 8 );
	p.writeUInt32LE( 7 );
	return { kind: "packet", atMs: 0, opcode: 0x32a6, payload: [ ...p ] };
}
test("checkpoint restores staged object list, unacknowledged journal and subsequent gameplay exactly", async () => {
	const a = await createWorldReplay();
	for (
		const event of [
			{ kind: "bootstrap", atMs: 0, value: bootstrap },
			latch(),
			{ kind: "tick", atMs: 0, advance: false },
			{ kind: "take", atMs: 0 },
			...packets.slice( 0, 2 )
		]
	) a.apply( event );
	const b = await createWorldReplay( JSON.parse( JSON.stringify( a.checkpoint() ) ) );
	const tail = [
		...packets.slice( 2 ),
		{ kind: "ack", atMs: 0, sequence: 1 },
		{ kind: "tick", atMs: 1, advance: true },
		{ kind: "take", atMs: 1 },
		{ kind: "ack", atMs: 1, sequence: 2 },
		{ kind: "command", atMs: 2, command: { kind: "move", destination: { ...pose, x: 30 } } },
		{ kind: "tick", atMs: 100, advance: true },
		{ kind: "take", atMs: 100 }
	];
	for ( const event of tail ) {
		a.apply( event );
		b.apply( event );
		assert.deepEqual( b.witness(), a.witness() );
	}
	assert.ok( a.witness().sent.some( frame => frame.opcode === 9 ) );
	const c = await createWorldReplay( a.checkpoint() );
	const receipt = {
		v: 1,
		id: 1,
		gid: 7,
		accepted: true,
		serverTimeMs: 100,
		world: { spawn: { ...pose, x: 20 }, moveSegment: { from: pose, startedAtMs: 100, arrivesAtMs: 1000 } }
	};
	for (
		const event of [
			{ kind: "packet", atMs: 100, opcode: 10, payload: [ ...Buffer.from( JSON.stringify( receipt ) ) ] },
			{ kind: "tick", atMs: 600, advance: true },
			{ kind: "ack", atMs: 600, sequence: 3 },
			{ kind: "take", atMs: 600 }
		]
	) {
		a.apply( event );
		c.apply( event );
	}
	assert.deepEqual( a.checkpoint(), c.checkpoint() );
	a.dispose();
	b.dispose();
	c.dispose();
});
test("checkpoint integrity, build identity, time ordering and replay event admission fail closed", async () => {
	const replay = await createWorldReplay();
	replay.apply( { kind: "bootstrap", atMs: 10, value: bootstrap } );
	const checkpoint = replay.checkpoint();
	checkpoint.events[0].value.localPlayerEntry.modelRef++;
	await assert.rejects( createWorldReplay( checkpoint ), /integrity/ );
	assert.throws( () => replay.apply( { kind: "tick", atMs: 9, advance: true } ), /clock/ );
	assert.throws( () => replay.apply( { kind: "packet", atMs: 10, opcode: 300, payload: [ 256 ] } ), /packet/ );
	replay.dispose();
	assert.throws( () => replay.apply( { kind: "clear", atMs: 10 } ), /disposed/ );
});

test("world clear retires gameplay before the next publication and restores the same presentation reset", async () => {
	const a = await createWorldReplay();
	for (
		const event of [
			{ kind: "bootstrap", atMs: 0, value: bootstrap },
			latch(),
			{ kind: "tick", atMs: 0, advance: false },
			{ kind: "take", atMs: 0 },
			{ kind: "present", atMs: 0, sequence: 1 }
		]
	) a.apply( event );
	assert.equal( a.witness().presentation.gameplay.localGid, 7 );
	const b = await createWorldReplay( a.checkpoint() );
	const tail = [
		{ kind: "ack", atMs: 1, sequence: 1 },
		{ kind: "clear", atMs: 1 },
		{ kind: "tick", atMs: 1, advance: false },
		{ kind: "take", atMs: 1 },
		{ kind: "present", atMs: 1, sequence: 2 }
	];
	for ( const event of tail ) {
		a.apply( event );
		b.apply( event );
	}
	assert.deepEqual( a.witness(), b.witness() );
	const state = a.witness().presentation;
	assert.deepEqual( state.entities, [] );
	assert.equal( state.gameplay.localGid, 0 );
	assert.equal( state.gameplay.pose, null );
	assert.deepEqual( state.gameplay.inventory, [] );
	assert.equal( state.bootstrap, null );
	assert.equal( state.presented.at( -1 ).reset, true );
	a.dispose();
	b.dispose();
});

test("checkpoint preserves queued input, delivery before acknowledgement, and undrained native packets", async () => {
	const a = await createWorldReplay();
	// Camera input moves the display camera; only the key reaches the worker.
	const events = [
		{ kind: "pointer", timeMs: 0, x: 10, y: 20, buttons: 2 },
		{ kind: "pointer", timeMs: 0, x: 110, y: 70, buttons: 2 },
		{ kind: "wheel", timeMs: 0, delta: -100 },
		{ kind: "key", timeMs: 0, code: "KeyW", down: true }
	];
	for (
		const event of [
			{ kind: "bootstrap", atMs: 0, value: bootstrap },
			latch(),
			{ kind: "tick", atMs: 0, advance: false },
			{ kind: "take", atMs: 0 },
			{ kind: "present", atMs: 0, sequence: 1 },
			{ kind: "input", atMs: 0, events }
		]
	) a.apply( event );
	const b = await createWorldReplay( a.checkpoint() );
	assert.equal( b.witness().input.accepted, 0 );
	// Display input precedes worker acknowledgement; the drag starts from the
	// native reset yaw (CApp_ResetCameraDefaults).
	assert.equal( b.witness().input.camera.yaw, Math.fround( 3.14 ) - Math.PI + 0.5 );
	const tail = [
		{ kind: "commit-input", atMs: 1 },
		{ kind: "ack", atMs: 1, sequence: 1 },
		{ kind: "packet", atMs: 2, opcode: 0x7ffe, payload: [ 1, 2, 3 ] },
		{ kind: "tick", atMs: 2, advance: false },
		{ kind: "take", atMs: 2 },
		{ kind: "present", atMs: 2, sequence: 2 }
	];
	for ( const event of tail ) {
		a.apply( event );
		b.apply( event );
	}
	assert.deepEqual( b.witness(), a.witness() );
	assert.equal( b.witness().input.accepted, 1 );
	assert.equal( b.witness().input.camera.yaw, Math.fround( 3.14 ) - Math.PI + 0.5 );
	const c = await createWorldReplay( b.checkpoint() );
	b.apply( { kind: "drain-native", atMs: 3 } );
	c.apply( { kind: "drain-native", atMs: 3 } );
	assert.deepEqual( b.witness(), c.witness() );
	assert.ok( c.witness().presentation.native.at( -1 ).events.some( e => e.opcode === 0x7ffe ) );
	assert.throws( () => c.apply( { kind: "present", atMs: 3, sequence: 2 } ), /journal gap/ );
	assert.throws( () => c.checkpoint(), /disposed/ );
	a.dispose();
	b.dispose();
	c.dispose();
});
