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
import { readFileSync } from "node:fs";
const queue = await import( "../../src/engine/foundation/gameplay/skill-queue.ts" );
const cooldowns = await import( "../../src/engine/foundation/gameplay/skill-cooldowns.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const { QUEUE_WINDOW_MS, ARRIVAL_MARGIN_MS, decidePress, createSkillPressQueue } = queue;
const FIXTURE = new URL(
	"../../../server/internal/game/item/wire/testdata/skill_action_result_fixture.json",
	import.meta.url
);

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
	q.queue( { skill: 1, command: "a", fireAtMs: 500 }, 0 );
	q.queue( { skill: 2, command: "b", fireAtMs: 600 }, 0 );
	assert.equal( q.due( 599 ), null );
	assert.equal( q.due( 600 )?.command, "b" );
	assert.equal( q.due( 700 ), null );
});

test("cancel drops the held press", () => {
	const q = createSkillPressQueue();
	assert.equal( q.cancel(), false );
	q.queue( { skill: 1, command: "a", fireAtMs: 500 }, 0 );
	assert.equal( q.cancel(), true );
	assert.equal( q.due( 1000 ), null );
});

test("the round trip is the smoothed time to each press's first answer", () => {
	const q = createSkillPressQueue();
	assert.equal( q.oneWayMs(), 0 );
	q.sent( 0, 1 );
	q.answered( 200 );
	assert.equal( q.oneWayMs(), 100 );
	// A second answer to the same press is not a sample.
	q.answered( 900 );
	assert.equal( q.oneWayMs(), 100 );
	q.sent( 1000, 1 );
	q.answered( 1360 );
	assert.equal( q.oneWayMs(), (200 + (360 - 200) / 8) / 2 );
	// A stall is not a sample.
	q.sent( 2000, 1 );
	q.answered( 9000 );
	assert.equal( q.oneWayMs(), (200 + (360 - 200) / 8) / 2 );
});

test("a press the server queues is published until its count drains", () => {
	const q = createSkillPressQueue();
	q.sent( 1000, 5 );
	assert.equal( q.commandCount( 1, 2, 1010 ), true );
	assert.deepEqual( q.state().skillQueue, { skill: 5, sinceMs: 1010 } );
	// A refused replacement keeps the count and what waits.
	q.sent( 1100, 6 );
	assert.equal( q.commandCount( 3, 2, 1110 ), false );
	assert.equal( q.state().skillQueue?.skill, 5 );
	// A queued attack replaces the skill.
	q.commandSent();
	assert.equal( q.commandCount( 1, 2, 1200 ), true );
	assert.equal( q.state().skillQueue, undefined );
	q.sent( 1300, 7 );
	q.commandCount( 1, 2, 1310 );
	// Promotion releases the count to one.
	assert.equal( q.commandCount( 2, 1, 1500 ), true );
	assert.equal( q.state().skillQueue, undefined );
});

test("a client-held press outranks the server's and keeps its start across re-presses", () => {
	const q = createSkillPressQueue();
	q.sent( 0, 5 );
	q.commandCount( 1, 2, 10 );
	q.queue( { skill: 8, command: "a", fireAtMs: 400 }, 100 );
	q.queue( { skill: 8, command: "a", fireAtMs: 420 }, 200 );
	assert.deepEqual( q.state().skillQueue, { skill: 8, sinceMs: 100, fireAtMs: 420 } );
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

test("a press far from ready is denied silently and nothing sent", () => {
	const { game, presses, sounds } = presser();
	game.command( { kind: "skill", skillId: SLOW }, 1000, undefined, local );
	game.command( { kind: "skill", skillId: SLOW }, 1100, undefined, local );
	assert.equal( presses().length, 1 );
	assert.deepEqual( sounds, [] );
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

test("only the local player's own answers time the round trip", () => {
	const { game } = presser();
	const fixture = JSON.parse( readFileSync( FIXTURE, "utf8" ) ), peerCast = fixture.scenarios[0];
	assert.notEqual( fixture.expect.casterGid, LOCAL_GID );
	game.command( { kind: "skill", skillId: QUICK }, 1000, undefined, local );
	// A nearby caster's cast lands first; the press's own admission at 1060.
	game.receive( { opcode: peerCast.opcode, payload: Buffer.from( peerCast.payloadHex, "hex" ) }, 1020 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 1060 );
	assert.equal( game.command( { kind: "skill", skillId: QUICK }, 1100, undefined, local ), null );
	// One delivery is 30 ms: 200 ms left, sent 30 ms early, plus the margin.
	assert.equal( game.take()?.skillQueue?.fireAtMs, 1100 + 200 - 30 + ARRIVAL_MARGIN_MS );
	game.dispose();
});

/*
================
targetedPresser

A gameplay owner knowing TARGETED (300 ms cooldown, target required) and
two monsters it can read: 9 and 10, which dead makes a corpse.
================
*/
function targetedPresser( dead = false ) {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	/** @type {Map<number, import("../../src/engine/contracts/world.ts").EntityState>} */
	const entities = new Map();
	for ( const gid of [ 9, 10 ] ) {
		entities.set( gid, {
			...local,
			gid,
			kind: "monster",
			name: "mob",
			x: local.x + 10,
			...(gid === 10 && dead ? { appearanceState: [ 2, 0, 0 ] } : {})
		} );
	}
	const game = createGameplay( f => sent.push( f ), () => {}, () => {}, gid => entities.get( gid ) );
	const row = skillRef( TARGETED, 300 );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ TARGETED ] },
		// Range 60: the monsters stand within it, so the server casts at once.
		refSkillSnapshot: [ { ...row, ui: { ...row.ui, targetRequired: true, range: 60, targets: 6 } } ]
	} );
	game.seed( local );
	// The target gid of each skill press (SkillAction +7).
	const targets = () =>
		sent.filter( f => f.opcode === 0x72cd && f.payload[1] === 4 ).map( f =>
			Buffer.from( f.payload ).readUInt32LE( 7 )
		);
	// A select, granted at once (B45A [1, gid, 0, ...]).
	const select = ( gid, now ) => {
		game.command( { kind: "select", gid }, now, entities.get( gid ), local );
		const grant = Buffer.alloc( 11 );
		grant[0] = 1;
		grant.writeUInt32LE( gid, 1 );
		game.receive( { opcode: 0xb45a, payload: grant }, now );
	};
	const press = ( gid, now ) =>
		game.command( { kind: "skill", skillId: TARGETED, gid }, now, entities.get( gid ), local );
	return { game, targets, select, press };
}

const TARGETED = 503;

test("a held press goes to the newest selection, as a fresh press would", () => {
	const { game, targets, select, press } = targetedPresser();
	select( 9, 900 );
	press( 9, 1000 );
	assert.equal( press( 9, 1100 ), null, "the re-press was not held" );
	select( 10, 1150 );
	for ( let now = 1166; now <= 1400; now += 16 ) game.step( now, local );
	assert.deepEqual( targets(), [ 9, 10 ] );
	game.dispose();
});

test("a held press at a monster that died lapses", () => {
	const { game, targets, select, press } = targetedPresser( true );
	select( 9, 900 );
	press( 9, 1000 );
	press( 9, 1100 );
	select( 10, 1150 );
	for ( let now = 1166; now <= 1400; now += 16 ) game.step( now, local );
	assert.deepEqual( targets(), [ 9 ] );
	game.dispose();
});

/*
================
cooldownAfterPress

Presses a targeted skill (range 60, 5 s cooldown) at a monster dx away and
returns the cooldown the client shows right after.
================
*/
function cooldownAfterPress( dx ) {
	const game = createGameplay( () => {} );
	const row = skillRef( TARGETED, 5000 );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ TARGETED ] },
		refSkillSnapshot: [ { ...row, ui: { ...row.ui, targetRequired: true, range: 60, targets: 6 } } ]
	} );
	game.seed( local );
	/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
	const monster = { ...local, gid: 9, kind: "monster", name: "mob", x: local.x + dx };
	game.command( { kind: "skill", skillId: TARGETED, gid: 9 }, 1000, monster, local );
	const shown = cooldowns.skillCooldown( game.take()?.skillCooldowns ?? [], TARGETED, 0, 1010 );
	game.dispose();
	return shown;
}

test("a press the server must run to first shows no cooldown until the cast starts", () => {
	assert.ok( cooldownAfterPress( 50 ), "an in-range press stands in for its cooldown" );
	assert.equal( cooldownAfterPress( 200 ), null, "an out-of-range press showed a cooldown that would vanish" );
});

/*
================
cooldownAfterPressWithMp

Presses an untargeted skill costing 50 MP (5 s cooldown) with mp of 100
maximum and returns the cooldown the client shows right after.
================
*/
function cooldownAfterPressWithMp( mp ) {
	const game = createGameplay( () => {} );
	const row = skillRef( SLOW, 5000 );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ SLOW ], mp, maxMp: 100 },
		refSkillSnapshot: [ { ...row, ui: { ...row.ui, mp: 50 } } ]
	} );
	game.seed( local );
	game.command( { kind: "skill", skillId: SLOW }, 1000, undefined, local );
	const shown = cooldowns.skillCooldown( game.take()?.skillCooldowns ?? [], SLOW, 0, 1010 );
	game.dispose();
	return shown;
}

test("a press the caster cannot pay MP for shows no cooldown", () => {
	assert.ok( cooldownAfterPressWithMp( 60 ), "a paid press stands in for its cooldown" );
	assert.equal( cooldownAfterPressWithMp( 10 ), null, "a press the server refuses for MP showed a cooldown" );
});

test("a press the server queues drops its cooldown stand-in", () => {
	const { game } = presser();
	game.command( { kind: "skill", skillId: SLOW }, 1000, undefined, local );
	assert.ok( cooldowns.skillCooldown( game.take()?.skillCooldowns ?? [], SLOW, 0, 1010 ) );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 1050 );
	assert.equal( cooldowns.skillCooldown( game.take()?.skillCooldowns ?? [], SLOW, 0, 1060 ), null );
	game.dispose();
});

test("a press the server runs to its target for shows as next until its cast starts", () => {
	const game = createGameplay( () => {} );
	const row = skillRef( TARGETED, 5000 );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ TARGETED ] },
		refSkillSnapshot: [ { ...row, ui: { ...row.ui, targetRequired: true, range: 60, targets: 6 } } ]
	} );
	game.seed( local );
	/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
	const monster = { ...local, gid: 9, kind: "monster", name: "mob", x: local.x + 200 };
	game.command( { kind: "skill", skillId: TARGETED, gid: 9 }, 1000, monster, local );
	assert.deepEqual( game.take()?.skillQueue, { skill: TARGETED, sinceMs: 1000 } );
	// The admission (B2CD arm, count 1) leaves the run-up in place.
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 1100 );
	assert.equal( game.take()?.skillQueue?.skill, TARGETED );
	// In range the cast is refused or opens: nothing waits any more.
	game.receive( { opcode: 0xb245, payload: Uint8Array.of( 2, 4 ) }, 3000 );
	assert.equal( game.take()?.skillQueue, undefined );
	game.dispose();
});

test("a skill at the target brings the selection ring back from the move marker", () => {
	const { game, select, press } = targetedPresser();
	select( 9, 900 );
	assert.equal( game.take()?.selectionDecal?.kind, "target" );
	game.command( { kind: "move", destination: { ...local, x: local.x - 30, angle: 0 } }, 950, undefined, local );
	assert.equal( game.take()?.selectionDecal?.kind, "ground", "the click marks its point" );
	press( 9, 1000 );
	const decal = game.take()?.selectionDecal;
	assert.equal( decal?.kind, "target", "the press left the move marker up" );
	assert.equal( decal?.gid, 9 );
	game.dispose();
});

/*
================
pressAt

Presses an enemy-only targeted skill (Cold Wave Arrest's groups: animal,
monster, player; range 60, 5 s cooldown) at entity and returns what the
client shows right after: a cooldown, a prediction, and the frames sent.
================
*/
function pressAt( entity ) {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const game = createGameplay( f => sent.push( f ) );
	const row = skillRef( TARGETED, 5000 );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ TARGETED ] },
		refSkillSnapshot: [ { ...row, ui: { ...row.ui, targetRequired: true, range: 60, targets: 14, actionMs: 800 } } ]
	} );
	game.seed( local );
	game.command( { kind: "skill", skillId: TARGETED, gid: entity.gid }, 1000, entity, local );
	const state = game.take();
	const shown = {
		cooldown: cooldowns.skillCooldown( state?.skillCooldowns ?? [], TARGETED, 0, 1010 ),
		predicted: !!state?.castPrediction,
		presses: sent.filter( f => f.opcode === 0x72cd ).length
	};
	game.dispose();
	return shown;
}

test("an enemy skill at the caster or an NPC is sent with no prediction and no cooldown", () => {
	for ( const entity of [ local, { ...local, gid: 77, kind: "npc", name: "npc", x: local.x + 10 } ] ) {
		const shown = pressAt( entity );
		assert.equal( shown.presses, 1, "the press is still the server's to answer (6FCD50)" );
		assert.equal( shown.cooldown, null, `a ${entity.kind} target stood a cooldown in` );
		assert.equal( shown.predicted, false, `a ${entity.kind} target predicted the cast` );
	}
	const monster = pressAt( { ...local, gid: 9, kind: "monster", name: "mob", x: local.x + 10 } );
	assert.ok( monster.cooldown && monster.predicted, "a monster in range still predicts and stands in" );
});

test("an enemy skill pressed with nothing selected never animates, cools down or sends", () => {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const game = createGameplay( f => sent.push( f ) );
	const row = skillRef( TARGETED, 5000 );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ TARGETED ] },
		refSkillSnapshot: [ { ...row, ui: { ...row.ui, targetRequired: true, range: 60, targets: 14, actionMs: 800 } } ]
	} );
	game.seed( local );
	assert.throws(
		() => game.command( { kind: "skill", skillId: TARGETED }, 1000, undefined, local ),
		/requires a target/
	);
	const state = game.take();
	assert.equal( cooldowns.skillCooldown( state?.skillCooldowns ?? [], TARGETED, 0, 1010 ), null );
	assert.ok( !state?.castPrediction, "a targetless press predicted a cast" );
	assert.equal( sent.filter( f => f.opcode === 0x72cd ).length, 0 );
	game.dispose();
});
