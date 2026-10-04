/*
===========================================================================

skill-press.test.mjs - tests for skill-queue.ts, skill-cooldowns.ts and the
gameplay press path: a cooling-down press is held or denied on the client,
never sent to be refused

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const queue = await import( "../../src/engine/foundation/gameplay/skill-queue.ts" );
const cooldowns = await import( "../../src/engine/foundation/gameplay/skill-cooldowns.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const { QUEUE_WINDOW_MS, ARRIVAL_MARGIN_MS, decidePress, createSkillPressQueue } = queue;

// ============================================================================
// decidePress

test("a ready skill is sent", () => {
	assert.deepEqual( decidePress( undefined, 50, 1000 ), { kind: "send" } );
	assert.deepEqual( decidePress( 0, 50, 1000 ), { kind: "send" } );
});

test("a skill ready by the time the press arrives is sent", () => {
	// 40 ms left, the press takes 75 ms to arrive.
	assert.deepEqual( decidePress( 40, 75, 1000 ), { kind: "send" } );
});

test("a nearly ready skill is held to arrive just after the server's cooldown ends", () => {
	const d = decidePress( 300, 75, 1000 );
	assert.deepEqual( d, { kind: "queue", fireAtMs: 1000 + 300 - 75 + ARRIVAL_MARGIN_MS } );
	// It arrives one delivery later: just after readiness.
	assert.equal( d.fireAtMs + 75 - (1000 + 300), ARRIVAL_MARGIN_MS );
});

test("a skill further off than the queue window is denied", () => {
	assert.deepEqual( decidePress( QUEUE_WINDOW_MS + 1, 75, 1000 ), {
		kind: "deny",
		remainingMs: QUEUE_WINDOW_MS + 1
	} );
});

// ============================================================================
// createSkillPressQueue

test("one held press: a newer press replaces it, and it fires once when due", () => {
	const q = createSkillPressQueue();
	q.queue( { skill: 1, command: "a", fireAtMs: 500 } );
	q.queue( { skill: 2, command: "b", fireAtMs: 600 } );
	assert.equal( q.due( 599 ), null );
	assert.equal( q.due( 600 )?.command, "b" );
	assert.equal( q.due( 700 ), null );
});

test("cancel drops the held press", () => {
	const q = createSkillPressQueue();
	assert.equal( q.cancel(), false );
	q.queue( { skill: 1, command: "a", fireAtMs: 500 } );
	assert.equal( q.cancel(), true );
	assert.equal( q.due( 1000 ), null );
});

test("the round trip is the smoothed time to each press's first answer", () => {
	const q = createSkillPressQueue();
	assert.equal( q.oneWayMs(), 0 );
	q.sent( 0 );
	q.answered( 200 );
	assert.equal( q.oneWayMs(), 100 );
	// A second answer to the same press is not a sample.
	q.answered( 900 );
	assert.equal( q.oneWayMs(), 100 );
	q.sent( 1000 );
	q.answered( 1360 );
	assert.equal( q.oneWayMs(), (200 + (360 - 200) / 8) / 2 );
	// A stall is not a sample.
	q.sent( 2000 );
	q.answered( 9000 );
	assert.equal( q.oneWayMs(), (200 + (360 - 200) / 8) / 2 );
});

test("a denial sounds at most every 400 ms", () => {
	const q = createSkillPressQueue();
	assert.equal( q.deny( { skill: 1, atMs: 0, remainingMs: 2000 } ), true );
	assert.equal( q.deny( { skill: 1, atMs: 100, remainingMs: 1900 } ), false );
	assert.equal( q.deny( { skill: 1, atMs: 400, remainingMs: 1600 } ), true );
	assert.equal( q.state().skillDenied?.atMs, 400 );
});

// ============================================================================
// skill-cooldowns.ts stand-ins

/** @type {any} Only the cooldown fields take part. */
const SKILL = { id: 7, cooldownGroup: 0, cooldownMs: 2000 };

test("an accepted cooldown starts when the server started it", () => {
	const c = cooldowns.createSkillCooldowns();
	// Answered at 1000, one delivery (100 ms) after the server started it.
	c.accepted( SKILL, 900, 1000 );
	assert.equal( cooldowns.skillCooldown( c.state(), 7, 0, 1000 )?.remainingMs, 1900 );
});

test("a press stands in for its cooldown until the answer replaces it", () => {
	const c = cooldowns.createSkillCooldowns();
	c.pressed( SKILL, 1100, 1700, 1000 );
	assert.equal( cooldowns.skillCooldown( c.state(), 7, 0, 1050 )?.remainingMs, 2050 );
	c.accepted( SKILL, 1100, 1200 );
	assert.equal( c.state().length, 1 );
	assert.equal( c.state()[0].provisionalUntilMs, undefined );
	// An accepted row outlives the stand-in's deadline.
	c.step( 1800 );
	assert.ok( cooldowns.skillCooldown( c.state(), 7, 0, 1800 ) );
});

test("a refusal or a missing answer removes the stand-in", () => {
	const c = cooldowns.createSkillCooldowns();
	c.pressed( SKILL, 1100, 1700, 1000 );
	assert.equal( c.refused(), true );
	assert.equal( cooldowns.skillCooldown( c.state(), 7, 0, 1100 ), null );
	c.pressed( SKILL, 1100, 1700, 1000 );
	c.step( 1699 );
	assert.ok( cooldowns.skillCooldown( c.state(), 7, 0, 1699 ) );
	c.step( 1700 );
	assert.equal( cooldowns.skillCooldown( c.state(), 7, 0, 1700 ), null );
});

// ============================================================================
// The gameplay press path

const LOCAL_GID = 7;
const QUICK = 501;
const SLOW = 502;
/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
const local = {
	gid: LOCAL_GID,
	refObjId: 1907,
	kind: "local-player",
	name: "presser",
	regionId: 257,
	x: 100,
	y: 0,
	z: 100,
	heading: 0
};

/*
================
skillRef

An untargeted skill with a cooldown.
================
*/
function skillRef( id, cooldownMs ) {
	return {
		id,
		group: id,
		level: 1,
		status: false,
		effectRider: false,
		ui: {
			name: `SKILL_${id}`,
			trainable: true,
			spCost: 1,
			targetRequired: false,
			cooldownMs,
			masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
		}
	};
}

/*
================
presser

A gameplay owner knowing QUICK (300 ms cooldown) and SLOW (5 s), the
skill presses it sends, and the sounds it plays.
================
*/
function presser() {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [], sounds = [];
	const game = createGameplay( f => sent.push( f ), handle => sounds.push( handle ) );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ QUICK, SLOW ] },
		refSkillSnapshot: [ skillRef( QUICK, 300 ), skillRef( SLOW, 5000 ) ]
	} );
	game.seed( local );
	const presses = () => sent.filter( f => f.opcode === 0x72cd && f.payload[1] === 4 );
	return { game, presses, sounds };
}

test("a re-press inside the cooldown is held and goes out once the skill is ready", () => {
	const { game, presses } = presser();
	game.command( { kind: "skill", skillId: QUICK }, 1000, undefined, local );
	assert.equal( presses().length, 1 );
	// 100 ms later: 200 ms of cooldown left, inside the queue window.
	assert.equal( game.command( { kind: "skill", skillId: QUICK }, 1100, undefined, local ), null );
	assert.equal( presses().length, 1, "a cooling-down press was sent" );
	assert.equal( game.take()?.skillQueue?.skill, QUICK );
	for ( let now = 1116; now < 1300; now += 16 ) game.step( now, local );
	assert.equal( presses().length, 1, "the held press left before the cooldown ended" );
	for ( let now = 1300; now <= 1400; now += 16 ) game.step( now, local );
	assert.equal( presses().length, 2, "the held press never left" );
	assert.equal( game.take()?.skillQueue, undefined );
	game.dispose();
});

test("a press far from ready is denied with a sound and nothing sent", () => {
	const { game, presses, sounds } = presser();
	game.command( { kind: "skill", skillId: SLOW }, 1000, undefined, local );
	game.command( { kind: "skill", skillId: SLOW }, 1100, undefined, local );
	assert.equal( presses().length, 1 );
	assert.deepEqual( sounds, [ "SND_WARNING" ] );
	const state = game.take();
	assert.equal( state?.skillDenied?.skill, SLOW );
	assert.equal( state?.skillQueue, undefined );
	game.dispose();
});

test("a ground click drops a held press", () => {
	const { game, presses } = presser();
	game.command( { kind: "skill", skillId: QUICK }, 1000, undefined, local );
	game.command( { kind: "skill", skillId: QUICK }, 1100, undefined, local );
	game.command( { kind: "move", destination: { ...local, x: 200, angle: 0 } }, 1150, undefined, local );
	for ( let now = 1166; now <= 1600; now += 16 ) game.step( now, local );
	assert.equal( presses().length, 1, "the dropped press went out" );
	game.dispose();
});

test("a refusal frees the skill for an immediate retry", () => {
	const { game, presses } = presser();
	game.command( { kind: "skill", skillId: SLOW }, 1000, undefined, local );
	game.receive( { opcode: 0xb245, payload: Uint8Array.of( 2, 4 ) }, 1100 );
	game.command( { kind: "skill", skillId: SLOW }, 1150, undefined, local );
	assert.equal( presses().length, 2 );
	game.dispose();
});
