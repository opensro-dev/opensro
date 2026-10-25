/*
===========================================================================

movement-skill-run-up.test.mjs - a skill press's predicted run-up

An admitted press on a target beyond reach walks the caster into reach on
the server (58D8F0). The client starts that walk at the press
(predictApproach with approach) so it is not one delivery behind the
server when the cast begins. The server's walk stops at its own combat
spacing, short of the predicted goal, and is adopted by heading; a refused
press walks back to the start the server kept.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { product } from "../helpers/navigation-fixture.mjs";
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const start = { regionId: 257, x: 100, y: 0, z: 100, angle: 0 };
const GID = 7;
// The run speed (units/s), the worker step and the one-way delivery.
const RUN_SPEED = 50;
const STEP_MS = 16;
const LATENCY_MS = 150;

/*
================
walkTo

The server's unsourced walk of the local player toward x along z = 100.
================
*/
function walkTo( x ) {
	const p = Buffer.alloc( 14 );
	p.writeUInt32LE( GID );
	p[4] = 1;
	p.writeUInt16LE( 257, 5 );
	p.writeInt16LE( x, 7 );
	p.writeInt16LE( 0, 9 );
	p.writeInt16LE( 100, 11 );
	return p;
}

/*
================
pressed

A caster at rest that pressed a skill on a target 300 units east at time 0
and ran its predicted run-up until the server's answer, one round trip on.
================
*/
function pressed() {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.speeds( 20, RUN_SPEED, 0 );
	assert.ok( m.predictApproach( { ...start, x: 400 }, 0, true ) );
	for ( let now = STEP_MS; now <= 2 * LATENCY_MS; now += STEP_MS ) m.step( now );
	return m;
}

/*
================
x
================
*/
function x( m ) {
	return defined( m.state().pose ).x;
}

test("the server's shorter run-up is adopted without stepping back", () => {
	const m = pressed(), progressed = x( m );
	assert.ok( progressed > start.x + RUN_SPEED * 0.25, `run-up began at the press (x ${progressed})` );
	assert.equal( m.approachPredictedSince(), 0 );
	// The server stops at its combat spacing, short of the predicted goal.
	m.native( walkTo( 380 ), 2 * LATENCY_MS, GID );
	assert.equal( m.approachPredictedSince(), undefined );
	assert.ok( Math.abs( x( m ) - progressed ) < 1, "adoption kept the progress made in flight" );
	for ( let now = 2 * LATENCY_MS + STEP_MS; now <= 8000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - 380 ) < 1, `ends at the server's goal (x ${x( m )})` );
});

test("a server-led run-up stands under the cast's settle: no jump", () => {
	// The native default. The server walks from the start on reading the
	// press and settles where it enters its combat spacing (x 300 here),
	// sending the correction with the cast. Its walk reached the client one
	// delivery after it began, and so does the correction.
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.speeds( 20, RUN_SPEED, 0 );
	const acknowledged = 2 * LATENCY_MS, settle = { ...start, x: 300 };
	for ( let now = STEP_MS; now < acknowledged; now += STEP_MS ) m.step( now );
	m.native( walkTo( 380 ), acknowledged, GID );
	// The server reached x 300 after (300 - 100) / RUN_SPEED seconds of its
	// walk; its correction lands that long after the acknowledgement.
	const corrected = acknowledged + (settle.x - start.x) / RUN_SPEED * 1000;
	let now = acknowledged + STEP_MS;
	for ( ; now < corrected; now += STEP_MS ) m.step( now );
	m.step( corrected );
	const before = x( m );
	m.correct( settle, corrected );
	assert.ok( Math.abs( x( m ) - before ) < 1, `the settle lands under the caster (${before} -> ${x( m )})` );
});

test("a server walk the other way is not the run-up", () => {
	const m = pressed();
	m.native( walkTo( 20 ), 2 * LATENCY_MS, GID );
	assert.equal( m.approachPredictedSince(), undefined );
	for ( let now = 2 * LATENCY_MS + STEP_MS; now <= 8000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - 20 ) < 1, `follows the server's walk (x ${x( m )})` );
});

test("a refused press walks the run-up back to the start", () => {
	const m = pressed();
	m.castRefused( 2 * LATENCY_MS );
	assert.equal( m.approachPredictedSince(), undefined );
	for ( let now = 2 * LATENCY_MS + STEP_MS; now <= 4000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - start.x ) < 1, `back at the start (x ${x( m )})` );
});

test("a cast without a walk ends the run-up at the start", () => {
	const m = pressed();
	m.endPrediction( 2 * LATENCY_MS );
	for ( let now = 2 * LATENCY_MS + STEP_MS; now <= 4000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - start.x ) < 1, `back at the start (x ${x( m )})` );
});

test("a pickup prediction is not a skill run-up", () => {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	assert.ok( m.predictApproach( { ...start, x: 400 }, 0 ) );
	assert.equal( m.approachPredictedSince(), undefined );
});

// ============================================================================
// gameplay: the press that starts the run-up, and a repeated press

const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

/*
================
gameplayPress

A caster at rest with skill 30 (range 60, an enemy action) pressed at a
monster 300 units east at time 0, over flat navigation. predicted turns on
the Experimental window's Predicted run-up row.
================
*/
function gameplayPress( predicted = true ) {
	const local = {
		gid: GID,
		refObjId: 1907,
		kind: "local-player",
		name: "caster",
		regionId: 257,
		x: 100,
		y: 0,
		z: 100,
		heading: 0
	};
	const game = createGameplay( () => {} );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ 30 ] },
		refSkillSnapshot: [ {
			id: 30,
			group: 30,
			level: 1,
			status: false,
			effectRider: false,
			ui: {
				name: "SKILL_30",
				trainable: true,
				spCost: 1,
				targetRequired: true,
				targets: 6,
				cooldownMs: 0,
				actionMs: 1000,
				range: 60,
				admit: { weaponKinds: [ 255, 255 ] },
				masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
				prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
			}
		} ]
	} );
	game.seed( local );
	game.experimental( {
		predictedRunUp: predicted,
		webTransport: false,
		jobKillNotices: true,
		dropPlusNotices: true,
		plusSevenNotices: true
	} );
	const navigation = product();
	navigation.objects = [];
	game.command( { kind: "navigation", regionId: 257, bundle: navigation }, 0, undefined );
	const monster = { ...local, gid: 9, kind: "monster", name: "mob", x: local.x + 300 };
	game.command( { kind: "skill", skillId: 30, gid: 9 }, 0, monster, local );
	return { game, local, monster };
}

test("natively a press beyond reach waits for the server's walk", () => {
	// 6FCD50 sends the command and reads no range; the server's movement
	// acknowledgement walks the caster, so the press itself never moves it.
	const { game, local } = gameplayPress( false );
	game.step( 200, local );
	const pose = defined( game.take()?.pose );
	game.dispose();
	assert.ok( Math.abs( pose.x - local.x ) < 0.01, `stands at the press (x ${pose.x})` );
});

test("with the Predicted run-up row a press beyond reach starts the run-up at once", () => {
	const { game, local } = gameplayPress();
	game.step( 200, local );
	const pose = defined( game.take()?.pose );
	game.dispose();
	assert.ok( pose.x > local.x + 1, `walking at the press (x ${pose.x})` );
});

test("a repeated press during the run-up keeps it walking", () => {
	const { game, local, monster } = gameplayPress();
	game.step( 200, local );
	const before = defined( game.take()?.pose ).x;
	game.command( { kind: "skill", skillId: 30, gid: 9 }, 200, monster, local );
	game.step( 400, local );
	const after = defined( game.take()?.pose ).x;
	game.dispose();
	assert.ok( after > before + 1, `still walking after the second press (${before} -> ${after})` );
});

// ============================================================================
// gameplay: the predicted stop is the server's stop (its combat spacing)

/*
================
gameplayPressSpaced

gameplayPress with the server's body radii on the bootstrap's
refObjSnapshot rows: the caster's body is 2 (refObjId 1907), the monster's
is 2 (refObjId 2001). The monster stands `distance` east (84 by default),
inside the walk-back an
unanswered press earns after 500 ms, so the predicted walk rests at its
goal before that. The server casts where both bodies plus the authored
range meet (60 + 2 + 2 = 64 from the target): its stop is x 120.
================
*/
function gameplayPressSpaced( rarity = 0, distance = 84, rangeBonus = false ) {
	const local = {
		gid: GID,
		refObjId: 1907,
		kind: "local-player",
		name: "caster",
		regionId: 257,
		x: 100,
		y: 0,
		z: 100,
		heading: 0
	};
	const game = createGameplay( () => {} );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ 30 ] },
		refObjSnapshot: [ {
			refObjId: 1907,
			kind: "player",
			tidWord: 0,
			bodyRadius: 2
		}, {
			refObjId: 2001,
			kind: "monster",
			tidWord: 0,
			nameStrId: "SN_MOB_TEST",
			name: "mob",
			bodyRadius: 2
		} ],
		refSkillSnapshot: [ {
			id: 30,
			group: 30,
			level: 1,
			status: false,
			effectRider: false,
			ui: {
				name: "SKILL_30",
				trainable: true,
				spCost: 1,
				targetRequired: true,
				targets: 6,
				cooldownMs: 0,
				actionMs: 1000,
				range: 60,
				...(rangeBonus ? { rangeBonus: true } : {}),
				admit: { weaponKinds: [ 255, 255 ] },
				masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
				prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
			}
		} ]
	} );
	game.seed( local );
	game.experimental( {
		predictedRunUp: true,
		jobKillNotices: true,
		dropPlusNotices: true,
		plusSevenNotices: true,
		webTransport: false
	} );
	const navigation = product();
	navigation.objects = [];
	game.command( { kind: "navigation", regionId: 257, bundle: navigation }, 0, undefined );
	const monster = { ...local, gid: 9, refObjId: 2001, kind: "monster", name: "mob", x: local.x + distance, rarity };
	game.command( { kind: "skill", skillId: 30, gid: 9 }, 0, monster, local );
	return { game, local, monster };
}

/*
================
restedPose

Step the spaced press until the predicted walk rests (well inside the
unanswered walk-back), and return the published pose.
================
*/
function restedPose( game, local ) {
	for ( let now = 16; now <= 480; now += 16 ) game.step( now, local );
	const pose = defined( game.take()?.pose );
	game.dispose();
	return pose;
}

test("the predicted run-up rests at the server's stop, not the authored range", () => {
	// The jump: aiming at the authored range rests the caster at x 124
	// while the server casts from x 120, so the cast pulls it back 4.
	const { game, local } = gameplayPressSpaced();
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - 120 ) <= 1, `rests at the server's stop (x ${pose.x})` );
});

test("a giant target's graded body moves the predicted stop out", () => {
	// Grade 4 (giant): the target's body is trunc(2 * 3) = 6, so the
	// server casts 60 + 2 + 6 = 68 from the target: its stop is x 116.
	const { game, local } = gameplayPressSpaced( 4 );
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - 116 ) <= 1, `rests at the giant's stop (x ${pose.x})` );
});

test("a target already inside the server's spacing starts no run-up", () => {
	// 63 east is beyond the authored range (60) but inside the spacing
	// (64): the server casts where the caster stands, so nothing walks.
	const { game, local } = gameplayPressSpaced( 0, 63 );
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - local.x ) < 0.01, `stands at the press (x ${pose.x})` );
});

test("a press inside the spacing leaves no prediction for the next press to hide behind", () => {
	// The in-spacing press must open nothing: a phantom walk to the
	// caster's own position would make this far press look like a repeat
	// press during a run-up and swallow the run-up it should start.
	const { game, local, monster } = gameplayPressSpaced( 0, 63 );
	const far = { ...monster, x: local.x + 300 };
	game.command( { kind: "skill", skillId: 30, gid: 9 }, 16, far, local );
	game.step( 200, local );
	const pose = defined( game.take()?.pose );
	game.dispose();
	assert.ok( pose.x > local.x + 1, `the far press starts its own run-up (x ${pose.x})` );
});

test("a row that asks for a caster range parameter starts no run-up", () => {
	// The server adds the caster's crossbow/wizard parameter to such a
	// row's reach, beyond every aim the client can compute from the
	// authored range (the spacing stop x 118 and the margin aim x 122,
	// for a target 82 east, are both short of its boundary): the press
	// stays server-led, as natively, and nothing walks.
	const { game, local } = gameplayPressSpaced( 0, 82, true );
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - local.x ) < 0.01, `stands at the press (x ${pose.x})` );
});
