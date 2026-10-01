/*
===========================================================================

action-movement.test.mjs - movement supersedes the shared action session

Exercise real gameplay commands and native cast packets, including delayed
replies and a close/open pair delivered before the next simulation frame.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createActionSession } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/action-session.ts"
);

const capture = JSON.parse(
	readFileSync(
		new URL( "../../../server/internal/game/item/wire/testdata/skill_action_result_fixture.json", import.meta.url ),
		"utf8"
	)
);
const OP_PREDICTED_MOVE = 9;

/*
================
movementFixture
================
*/
function movementFixture() {
	const sent = [];
	const game = createGameplay( frame => sent.push( frame ) );
	const local = {
		gid: capture.expect.casterGid,
		refObjId: 1907,
		name: "actor",
		kind: /** @type {const} */ ("player"),
		regionId: 0x62a8,
		x: 100,
		y: 20,
		z: 100,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	return { game, local, sent };
}

/*
================
openCast
================
*/
function openCast( game, token, now ) {
	const row = capture.scenarios[0];
	const payload = Buffer.from( row.payloadHex, "hex" );
	payload.writeUInt32LE( token, 10 );
	game.receive( { opcode: row.opcode, payload }, now );
}

/*
================
closeCast
================
*/
function closeCast( game, token, now ) {
	const payload = Buffer.alloc( 6 );
	payload[0] = 2;
	payload.writeUInt32LE( token, 2 );
	game.receive( { opcode: 0xb505, payload }, now );
}

test("native ground cancellation selects exactly one command and coalesces pending replies", () => {
	for ( const count of [ 0, 1, 2, 255 ] ) {
		const owner = createActionSession();
		owner.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, count ) } );
		assert.equal( owner.cancelForMovement()?.opcode ?? null, count === 1 ? 0x72cd : null );
		owner.sentCancellation();
		assert.equal( owner.cancelForMovement(), null );
		owner.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 3, count, 4 ) } );
		assert.equal( owner.cancelForMovement(), null, "committed refusal must not generate a cancel every frame" );
		owner.clear();
		assert.equal( owner.cancelForMovement(), null );
	}
});

test("movement cancels repetition before waiting for batched cast closure, then walks to the latest click", () => {
	const { game, local, sent } = movementFixture();
	openCast( game, 1, 10 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 10 );
	for ( const x of [ 120, 140 ] ) {
		game.command( { kind: "move", destination: { ...local, x, angle: 0 } }, 11, undefined, local );
	}
	assert.deepEqual( sent.map( frame => [ frame.opcode, ...frame.payload ] ), [ [ 0x72cd, 2 ] ] );
	closeCast( game, 1, 12 );
	openCast( game, 2, 12 );
	game.step( 13, local );
	assert.equal( sent.length, 1, "an in-flight second strike still owns presentation" );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 2, 0 ) }, 14 );
	closeCast( game, 2, 15 );
	for ( let now = 16; now < 3000 && sent.length < 2; now += 50 ) game.step( now, local );
	assert.deepEqual( sent.map( frame => frame.opcode ), [ 0x72cd, OP_PREDICTED_MOVE ] );
	const decal = game.take()?.selectionDecal;
	assert.equal( decal?.kind, "ground" );
	assert.equal( decal?.kind === "ground" ? decal.pose.x : null, 140 );
	game.dispose();
});

test("a queued pair draining to one cancels while movement is still held", () => {
	const { game, local, sent } = movementFixture();
	openCast( game, 1, 10 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 10 );
	game.command( { kind: "move", destination: { ...local, x: 140, angle: 0 } }, 11, undefined, local );
	assert.equal( sent.length, 0 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 2, 1 ) }, 12 );
	game.step( 13, local );
	assert.deepEqual( sent.map( frame => [ frame.opcode, ...frame.payload ] ), [ [ 0x72cd, 2 ] ] );
	game.dispose();
});

test("a refused skill cancellation retries on its handoff to basic attack", () => {
	const { game, local, sent } = movementFixture();
	openCast( game, 1, 10 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 10 );
	game.command( { kind: "move", destination: { ...local, x: 140, angle: 0 } }, 11, undefined, local );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 3, 1, 4 ) }, 12 );
	game.step( 13, local );
	assert.equal( sent.length, 1 );
	closeCast( game, 1, 14 );
	openCast( game, 2, 14 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 2, 1 ) }, 14 );
	game.step( 15, local );
	assert.deepEqual( sent.map( frame => [ frame.opcode, ...frame.payload ] ), [ [ 0x72cd, 2 ], [ 0x72cd, 2 ] ] );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 2, 0 ) }, 16 );
	closeCast( game, 2, 17 );
	for ( let now = 18; now < 3000 && sent.length < 3; now += 50 ) game.step( now, local );
	assert.deepEqual( sent.map( frame => frame.opcode ), [ 0x72cd, 0x72cd, OP_PREDICTED_MOVE ] );
	game.dispose();
});

test("explicit cancel and world transfer discard held movement", () => {
	for ( const outcome of [ "cancel", "travel", "death" ] ) {
		const { game, local, sent } = movementFixture();
		openCast( game, 1, 10 );
		// The server publishes the accepted command with every cast.
		game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 10 );
		game.command( { kind: "move", destination: { ...local, x: 140, angle: 0 } }, 11, undefined, local );
		if ( outcome === "cancel" ) game.command( { kind: "cancel" }, 12, undefined, local );
		if ( outcome === "travel" ) game.resetWorld();
		if ( outcome === "death" ) local.appearanceState[0] = 2;
		if ( outcome !== "travel" ) closeCast( game, 1, 13 );
		for ( let now = 14; now < 3000; now += 50 ) game.step( now, local );
		assert.ok( sent.every( frame => frame.opcode !== OP_PREDICTED_MOVE ), outcome );
		game.dispose();
	}
});

/*
================
basic attack cancel releases a held move before the swing closes

CGObjPC_IsMotionChangeLocked (server 4EF880) blocks movement only for a
committed front command. A cancelled basic attack reports count 0 at once,
so the held move goes without waiting for the swing's cast to close.
================
*/
test("basic attack cancel releases a held move before the swing closes", () => {
	const { game, local, sent } = movementFixture();
	openCast( game, 1, 10 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 10 );
	game.command( { kind: "move", destination: { ...local, x: 140, angle: 0 } }, 11, undefined, local );
	assert.deepEqual( sent.map( frame => frame.opcode ), [ 0x72cd ], "movement sends only the cancel first" );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 2, 0 ) }, 12 );
	game.step( 13, local );
	assert.deepEqual(
		sent.map( frame => frame.opcode ),
		[ 0x72cd, OP_PREDICTED_MOVE ],
		"the move goes with the cast still open"
	);
	game.dispose();
});

/*
================
committed skill keeps the move held until its cast closes
================
*/
test("committed skill keeps the move held until its cast closes", () => {
	const { game, local, sent } = movementFixture();
	openCast( game, 1, 10 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 10 );
	game.command( { kind: "move", destination: { ...local, x: 140, angle: 0 } }, 11, undefined, local );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 3, 1, 4 ) }, 12 );
	game.step( 13, local );
	assert.ok( sent.every( frame => frame.opcode !== OP_PREDICTED_MOVE ), "a refused cancel keeps holding" );
	game.dispose();
});
