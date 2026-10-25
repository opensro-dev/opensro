/*
===========================================================================

movement-attack-chase.test.mjs - a far attack click's predicted chase

An attack click on a monster beyond reach sends the attack command and,
natively, the server walks the player to it: the chase is server-led and
the player stands still for a full round trip. The client predicts the
chase leg from the click to the server's combat spacing (both bodies plus
the weapon's reach, combat-spacing.ts), adopts the server's walk when it
arrives, and re-aims the leg while the target moves, as the server's
follow does. With the Predicted run-up row off, or without the spacing
data, the click stays server-led exactly as today.

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
// The run speed (units/s) and the worker step.
const RUN_SPEED = 50;
const STEP_MS = 16;

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
x
================
*/
function x( m ) {
	return defined( m.state().pose ).x;
}

test("a re-aimed chase leg rests at the moved target's boundary", () => {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.speeds( 20, RUN_SPEED, 0 );
	// The target stands at x 200; with an admission of 24 the leg stops at 176.
	assert.ok( m.predictApproach( { ...start, x: 176 }, 0, true ) );
	for ( let now = STEP_MS; now <= 160; now += STEP_MS ) m.step( now );
	// The target moved to x 260: the leg re-aims at its new boundary, 236.
	assert.ok( m.retargetPrediction( { ...start, x: 236 }, 160 ) );
	for ( let now = 160 + STEP_MS; now <= 5000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - 236 ) < 1, `rests at the moved boundary (x ${x( m )})` );
});

test("the server's chase walk is adopted after a re-aim without stepping back", () => {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.speeds( 20, RUN_SPEED, 0 );
	assert.ok( m.predictApproach( { ...start, x: 176 }, 0, true ) );
	for ( let now = STEP_MS; now <= 160; now += STEP_MS ) m.step( now );
	assert.ok( m.retargetPrediction( { ...start, x: 236 }, 160 ) );
	for ( let now = 160 + STEP_MS; now < 300; now += STEP_MS ) m.step( now );
	const progressed = x( m );
	// The server's own chase (its goal is the stand-off, past the boundary).
	m.native( walkTo( 246 ), 300, GID );
	assert.equal( m.approachPredictedSince(), undefined );
	assert.ok( Math.abs( x( m ) - progressed ) < 1, "adoption kept the progress made in flight" );
	for ( let now = 300 + STEP_MS; now <= 6000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - 246 ) < 1, `ends at the server's goal (x ${x( m )})` );
});

test("a target that closes inside the spacing ends the chase leg here", () => {
	const m = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	m.seed( start );
	m.navigation( 257, navigation );
	m.speeds( 20, RUN_SPEED, 0 );
	assert.ok( m.predictApproach( { ...start, x: 176 }, 0, true ) );
	for ( let now = STEP_MS; now <= 160; now += STEP_MS ) m.step( now );
	// The target charged to x 120: re-aimed at the live pose, the leg ends.
	const here = x( m );
	assert.ok( m.retargetPrediction( { ...start, x: Math.round( here ) }, 160 ) );
	for ( let now = 160 + STEP_MS; now <= 2000; now += STEP_MS ) m.step( now );
	assert.ok( Math.abs( x( m ) - here ) <= 1, `stands where the spacing was met (x ${x( m )})` );
});

// ============================================================================
// gameplay: the attack click that starts the chase

const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

/*
================
weaponBody

An equipped weapon's item body (id, durability 30 at byte 13).
================
*/
function weaponBody( id ) {
	const p = Buffer.alloc( 18 );
	p.writeUInt32LE( id, 0 );
	p.writeUInt32LE( 30, 13 );
	return [ ...p ];
}

/*
================
basicRow

One learned basic-attack catalogue row: a pinned base attack for the
given weapon kinds, with any reach marks the variant carries.
================
*/
function basicRow( id, weaponKinds, marks = {} ) {
	return {
		id,
		group: 1,
		level: 1,
		status: false,
		effectRider: false,
		ui: {
			name: "SKILL_TEST_BASE_" + id,
			trainable: true,
			targetRequired: true,
			spCost: 0,
			cooldownMs: 1000,
			masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			basicAttack: true,
			admit: { weaponKinds },
			...marks
		}
	};
}

/*
================
attackPress

A caster at rest (bodies: caster 2, monster 2) with a sword whose reach
is 20 clicking attack on a monster `distance` east: the server's spacing
admits the attack at 2 + 2 + 20 = 24 from the monster. predicted turns on
the Experimental window's Predicted run-up row, which gates the chase.
The caster has learned the fist and sword base attacks, as a character
does; `basic` swaps the sword row's reach marks, `buff` stands a
range-up effect on the caster before the click.
================
*/
function attackPress(
	{ predicted = true, distance = 40, weapon = true, ranged = true, basic = {}, buff = false } = {}
) {
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
	const monster = { ...local, gid: 9, refObjId: 2001, kind: "monster", name: "mob", x: local.x + distance };
	const world = new Map( [ [ monster.gid, monster ] ] );
	const game = createGameplay( () => {}, () => {}, () => {}, gid => world.get( gid ) );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ 9901, 9902 ] },
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refSkillSnapshot: [
			basicRow( 9901, [ 1, 255 ] ),
			basicRow( 9902, [ 2, 255 ], basic ),
			{
				id: 9903,
				group: 1,
				level: 1,
				status: false,
				effectRider: false,
				ui: {
					name: "SKILL_TEST_RANGE_UP",
					trainable: true,
					targetRequired: false,
					spCost: 0,
					cooldownMs: 1000,
					masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
					prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
					rangeUp: true
				}
			}
		],
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
		refItemSnapshot: [ {
			refObjId: 3001,
			typeFlags: 0x132c,
			name: "Sword",
			nativeFields: ranged ? { actionRange23c: 20 } : {}
		} ],
		equipItems: weapon ? [ { slot: 6, refObjId: 3001, body: weaponBody( 3001 ) } ] : []
	} );
	game.seed( local );
	game.experimental( {
		predictedRunUp: predicted,
		jobKillNotices: true,
		dropPlusNotices: true,
		plusSevenNotices: true,
		webTransport: false
	} );
	const navigation = product();
	navigation.objects = [];
	game.command( { kind: "navigation", regionId: 257, bundle: navigation }, 0, undefined );
	if ( buff ) {
		const effect = Buffer.alloc( 12 );
		effect.writeUInt32LE( GID, 0 );
		effect.writeUInt32LE( 9903, 4 );
		effect.writeUInt32LE( 1, 8 );
		game.receive( { opcode: 0xb419, payload: new Uint8Array( effect ) }, 0 );
	}
	game.command( { kind: "attack", gid: 9 }, 0, monster, local );
	return { game, local, monster, world };
}

/*
================
restedPose

Step the press until the walk rests (well inside the unanswered
walk-back), and return the published pose.
================
*/
function restedPose( game, local ) {
	for ( let now = STEP_MS; now <= 480; now += STEP_MS ) game.step( now, local );
	const pose = defined( game.take()?.pose );
	game.dispose();
	return pose;
}

test("an attack click beyond reach starts the chase at once and rests at the spacing stop", () => {
	// Server-led today: the player stands at the press for a round trip.
	// The monster at x 140 admits the attack at 24: the stop is x 116.
	const { game, local } = attackPress();
	let early = local.x;
	let pose;
	for ( let now = STEP_MS; now <= 480; now += STEP_MS ) {
		game.step( now, local );
		const published = game.take()?.pose;
		if ( published ) pose = published;
		if ( now === 192 && pose ) early = pose.x;
	}
	game.dispose();
	pose = defined( pose );
	assert.ok( early > local.x + 1, `walking at the click (x ${early})` );
	assert.ok( Math.abs( pose.x - 116 ) <= 1, `rests at the spacing stop (x ${pose.x})` );
});

test("with the Predicted run-up row off the click stays server-led", () => {
	const { game, local } = attackPress( { predicted: false } );
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - local.x ) < 0.01, `stands at the click (x ${pose.x})` );
});

test("a target already inside the spacing starts no chase", () => {
	// 20 east is inside the admission (24): the server attacks in place.
	const { game, local } = attackPress( { distance: 20 } );
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - local.x ) < 0.01, `stands at the click (x ${pose.x})` );
});

test("a moving target re-aims the chase leg", () => {
	const { game, local, monster, world } = attackPress();
	for ( let now = STEP_MS; now <= 160; now += STEP_MS ) game.step( now, local );
	// The monster ran east to x 148: the stop moves from 116 to 124.
	world.set( monster.gid, { ...monster, x: 148 } );
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - 124 ) <= 1, `rests at the re-aimed stop (x ${pose.x})` );
});

test("without the weapon's reach on its row the click stays server-led", () => {
	const { game, local } = attackPress( { ranged: false } );
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - local.x ) < 0.01, `stands at the click (x ${pose.x})` );
});

test("a basic row that asks for a caster range parameter starts no chase", () => {
	// The server adds the caster's crossbow/wizard parameter to such a
	// row's reach, beyond the boundary the client can compute: the click
	// stays server-led, as the run-up's rangeBonus gate keeps a press.
	const { game, local } = attackPress( { basic: { rangeBonus: true } } );
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - local.x ) < 0.01, `stands at the click (x ${pose.x})` );
});

test("a range-up buff standing on the caster starts no chase", () => {
	// The buff feeds the attack-range keeper, which replaces the weapon's
	// range with an amount the client cannot see: no proven reach, no chase.
	const { game, local } = attackPress( { buff: true } );
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - local.x ) < 0.01, `stands at the click (x ${pose.x})` );
});

test("a basic row with an authored range bounds the chase by it, not the weapon", () => {
	// The row authors 30 where the weapon publishes 20: the server's
	// boundary is the row's (admission 2 + 2 + 30 = 34), so the chase
	// rests at 140 - 34 = 106, not the weapon's 116.
	const { game, local } = attackPress( { basic: { range: 30 } } );
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - 106 ) <= 1, `rests at the authored-range stop (x ${pose.x})` );
});

test("unarmed, the bare-hand reach bounds the chase", () => {
	// No weapon equipped: reach 6, admission 2 + 2 + 6 = 10. The monster
	// at x 130: the stop is x 120.
	const { game, local } = attackPress( { weapon: false, distance: 30 } );
	const pose = restedPose( game, local );
	assert.ok( Math.abs( pose.x - 120 ) <= 1, `rests at the unarmed stop (x ${pose.x})` );
});
