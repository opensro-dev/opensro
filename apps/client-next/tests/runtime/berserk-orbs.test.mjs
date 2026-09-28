/*
===========================================================================

berserk-orbs.test.mjs - Berserk feedback, orb travel and HUD admission tests.

Checks authoritative gauge updates separately from delayed visual arrivals.
Animation tests pass the projected colors through the real renderer boundary
so a valid endpoint cannot hide an invalid frame during activation.

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { advanceOrb, launchOrb } = await import( "../../src/engine/foundation/animation/orb-mover.ts" );
const { createOrbs } = await import( "../../src/engine/runtime/characters/orbs/orbs.ts" );
const { createFeedback, feedbackCount } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/feedback/feedback.ts"
);
test("native pet EXP loss never demotes and both reward signs publish their notice", () => {
	const owner = createFeedback(), p = Buffer.alloc( 13 );
	p.writeUInt32LE( 9 );
	p[4] = 3;
	p.writeUInt32LE( 40, 9 );
	for ( const [xp, delta, want] of [ [ 5, -20, 0 ], [ 50, -20, 30 ], [ 50, 0, 50 ] ] ) {
		p.writeInt32LE( delta, 5 );
		const r = owner.receive( 0x3508, p, 1, { gid: 9, level: 2, experience: [ xp, 0 ] } );
		assert.equal( defined( defined( r ).cos ).level, 2 );
		assert.deepEqual( defined( defined( r ).cos ).experience, [ want, 0 ] );
		assert.deepEqual( defined( r ).events, [] );
		assert.deepEqual( defined( r ).messages, delta ? [ { key: "UIIT_MSG_COSPET_LOST_EXP", value: 20 } ] : [] );
	}
	p.writeInt32LE( 24, 5 );
	const r = owner.receive( 0x3508, p, 1, { gid: 9, level: 1, experience: [ 110, 0 ] } );
	assert.equal( defined( defined( r ).cos ).level, 2 );
	assert.deepEqual( defined( defined( r ).cos ).experience, [ 16, 0 ] );
	assert.deepEqual( defined( r ).messages, [ { key: "UIIT_MSG_COSPET_GAIN_EXP", value: 24 } ] );
	assert.deepEqual( defined( r ).events, [ {
		kind: "orb-feedback",
		source: 40,
		target: 9,
		color: 3,
		count: feedbackCount( 24 / 47 )
	} ] );
	assert.throws( () => owner.receive( 0x3508, p.subarray( 0, 12 ), 1, { gid: 9, level: 1, experience: [ 0, 0 ] } ) );
});
/*
================
mover

Build an independent orb in its draw-in phase. Individual cases override only
the motion property whose behavior they exercise.
================
*/
function mover( patch = {} ) {
	return {
		position: [ 0, 0, 0 ],
		target: [ 1, 0, 0 ],
		velocity: [ 0, 1000, 0 ],
		phase: 3,
		elapsed: 0,
		delay: 0,
		speed: 1,
		...patch
	};
}
test("native oblique draw-in keeps its bend and skips overshoot admission", () => {
	const m = mover();
	assert.equal( advanceOrb( m, 100 ), true );
	assert.equal( m.phase, 3 );
	assert.ok( m.position[1] > 1 );
});
test("native timed draw-in checks overshoot on the first timed frame; done stays terminal", () => {
	const m = mover( { elapsed: 2200 } );
	assert.equal( advanceOrb( m, 100 ), true );
	assert.equal( m.phase, 0 );
	assert.deepEqual( m.position, [ 1, 0, 0 ] );
	m.target = [ 50, 0, 0 ];
	assert.equal( advanceOrb( m, 0 ), false );
	assert.deepEqual( m.position, [ 1, 0, 0 ] );
});
test("launch delay does not advance position, seeded launches replay deterministically", () => {
	const a = launchOrb( [ 100, 0, 0 ], [ 0, 10, 0 ], 1 ), b = launchOrb( [ 100, 0, 0 ], [ 0, 10, 0 ], 1 );
	assert.deepEqual( a, b );
	advanceOrb( a.mover, 1 );
	assert.deepEqual( a.mover.position, [ 100, 0, 0 ] );
});
const entities = [ { gid: 1, kind: "monster", regionId: 0, x: 100, y: 0, z: 0 }, {
	gid: 2,
	kind: "local-player",
	regionId: 0,
	x: 0,
	y: 0,
	z: 0
} ];
const event = { kind: "orb-feedback", source: 1, target: 2, color: 2, count: 1 };
test("gauge waits for arrivals, consumes missing targets and low-detail births, and resets atomically", () => {
	const o = createOrbs( () => {}, createPresentationRandom( 1 ) ), args = [ () => null, () => true, () => .1 ];
	o.receive( [ { kind: "orb-gauge", value: 2 }, { ...event, count: 2 } ], entities );
	assert.deepEqual( o.gauge(), { authoritative: 2, displayed: 0, pending: 2 } );
	for ( let n = 0; n < 70; n++ ) o.step( entities, n / 10, new Set( [ 1 ] ), ...args );
	assert.deepEqual( o.gauge(), { authoritative: 2, displayed: 2, pending: 0 } );
	o.receive( [ { kind: "orb-gauge", value: 3 }, { ...event, target: 999 } ], entities );
	o.step( entities, 8, new Set( [ 1 ] ), ...args );
	assert.equal( o.gauge().displayed, 3 );
	assert.equal( o.gauge().pending, 0 );
	o.receive( [ { kind: "orb-gauge", value: 4 }, event ], entities );
	o.step( entities, 9, new Set( [ 1 ] ), ...args, 1 );
	assert.equal( o.gauge().displayed, 4 );
	o.receive( [ { kind: "orb-gauge", value: 5 }, event ], entities );
	o.step( entities.filter( e => e.gid !== 1 ), 10, new Set(), ...args );
	assert.equal( o.gauge().displayed, 5 );
	o.receive( [ { kind: "orb-gauge", value: 0 }, { kind: "orb-clear" } ], entities );
	assert.deepEqual( o.gauge(), { authoritative: 0, displayed: 0, pending: 0 } );
	o.reset();
	assert.deepEqual( o.gauge(), { authoritative: 0, displayed: 0, pending: 0 } );
});
test("corpse settle owns launch; only arrival emits the direct spatial WAV once; reset cancels", () => {
	/** @type {any[]} */ const sounds = [];
	const o = createOrbs( s => sounds.push( s ), createPresentationRandom( 1 ) ),
		args = [ () => null, () => true, () => 0.1 ];
	o.receive( [ event ], entities );
	assert.deepEqual( o.step( entities, 0, new Set(), ...args ), [] );
	assert.deepEqual( sounds, [] );
	assert.equal( o.step( entities, 1, new Set( [ 1 ] ), ...args ).length, 1 );
	assert.deepEqual( sounds, [] );
	for ( let n = 1; n <= 60; n++ ) o.step( entities, 1 + n / 10, new Set(), ...args );
	assert.equal( sounds.length, 1 );
	assert.equal( sounds[0].gain, 1 );
	assert.equal( sounds[0].spatial, true );
	assert.match( sounds[0].path, /hyanget.wav$/ );
	o.receive( [ event ], entities );
	o.reset();
	o.step( entities, 10, new Set( [ 1 ] ), ...args );
	assert.equal( sounds.length, 1 );
});
test("violet clear and low detail discard launches without an arrival cue", () => {
	const sounds = [], o = createOrbs( s => sounds.push( s ), createPresentationRandom( 1 ) );
	o.receive( [ event, { kind: "orb-clear" } ], entities );
	assert.deepEqual( o.step( entities, 0, new Set( [ 1 ] ), () => null, () => true, () => 1 ), [] );
	o.receive( [ event ], entities );
	assert.deepEqual( o.step( entities, 1, new Set( [ 1 ] ), () => null, () => true, () => 1, 1 ), [] );
	assert.deepEqual( sounds, [] );
});
test("EXP feedback uses the post-level divisor and malformed packets cannot advance authority", () => {
	const o = createFeedback();
	o.bootstrap( { character: { level: 1, experience: 110 } } );
	const p = Buffer.alloc( 15 );
	p.writeUInt32LE( 1 );
	p.writeInt32LE( 24, 4 );
	p.writeInt32LE( 100, 8 );
	p.writeUInt16LE( 3, 13 );
	assert.throws( () => o.receive( 0x30d2, p.subarray( 0, 14 ), 2 ) );
	const r = o.receive( 0x30d2, p, 2 );
	assert.equal( defined( r ).level, 2 );
	assert.equal( defined( r ).experience, "16" );
	assert.equal( defined( r ).statPoints, 3 );
	assert.deepEqual( defined( r ).events.map( e => [ e.color, e.count ] ), [ [ 0, feedbackCount( 24 / 47 ) ], [
		1,
		4
	] ] );
	const g = Uint8Array.of( 4, 2, 1, 0, 0, 0 );
	assert.equal( defined( o.receive( 0x30b3, g, 2 ) ).events[1].count, 2 );
	assert.deepEqual( defined( o.receive( 0x30b3, g, 2 ) ).events, [ { kind: "orb-gauge", value: 2 }, {
		kind: "orb-clear"
	} ] );
});

test("rejected indraft assets retire once and cannot repeat or retain arrived voices", () => {
	const sounds = [], o = createOrbs( s => sounds.push( s ), createPresentationRandom( 1 ) );
	o.receive( [ event ], entities );
	for ( let n = 0; n < 100; n++ ) {
		o.step(
			entities,
			n / 10,
			new Set( [ 1 ] ),
			() => null,
			() => false,
			() => 0,
			3,
			() => true
		);
	}
	assert.equal( sounds.length, 0 );
});

test("frame deltas preserve accumulated milliseconds at fractional refresh intervals", () => {
	const o = createOrbs( () => {}, createPresentationRandom( 1 ) );
	o.receive( [ event ], entities );
	const reference = createPresentationRandom( 1 ).orb( [ 100, 0, 0 ], [ 0, 10, 0 ] );
	for ( let n = 0; n < 20; n++ ) {
		const now = n / 60, actors = o.step( entities, now, new Set( [ 1 ] ), () => null, () => true, () => 1 );
		advanceOrb( reference, n ? Math.trunc( now * 1000 ) - Math.trunc( (n - 1) / 60 * 1000 ) : 0 );
		assert.deepEqual( [ actors[0].pose.x, actors[0].pose.y, actors[0].pose.z ], reference.position );
	}
});

test("persisted Berserk gauge seeds feedback and rejects out-of-range wire values", () => {
	const f = createFeedback();
	f.bootstrap( { character: { berserkPoints: 4 } } );
	assert.equal( f.gauge(), 4 );
	const p = Buffer.from( [ 4, 5, 3, 0, 0, 0 ] );
	assert.equal( defined( f.receive( 0x30b3, p, 7 ) ).events[1].count, 1 );
	p[1] = 6;
	assert.throws( () => f.receive( 0x30b3, p, 7 ) );
	assert.equal( f.gauge(), 5 );
	assert.throws( () => f.bootstrap( { character: { berserkPoints: 9 } } ) );
});
const { berserkHud, berserkEntryFlash } = await import( "../../src/engine/foundation/ui/berserk-hud.ts" );
const { createUiPreparation } = await import( "../../src/engine/foundation/ui/ui.ts" );

// ============================================================================
// HUD animation timing and renderer admission

test("Berserk HUD drains from top every 12 seconds and clears at 60 seconds", () => {
	assert.deepEqual( berserkHud( 0 ).circles, [ 1, 1, 1, 1, 1 ] );
	assert.deepEqual( berserkHud( 12500 ).circles, [ .5, 1, 1, 1, 1 ] );
	assert.deepEqual( berserkHud( 25000 ).circles, [ 0, 0, 1, 1, 1 ] );
	assert.deepEqual( berserkHud( 60000 ).circles, [ 0, 0, 0, 0, 0 ] );
	assert.equal( berserkHud( 50 ).frame, 1 );
	assert.equal( berserkHud( 600 ).frame, 0 );
	assert.equal( berserkHud( 63000 ).glow, 0 );
});

test("Berserk entry flash peaks after200ms and retires after700ms", () => {
	assert.equal( berserkEntryFlash( 0 ), 0 );
	assert.equal( berserkEntryFlash( 200 ), 128 / 255 );
	assert.equal( berserkEntryFlash( 450 ), 64 / 255 );
	assert.equal( berserkEntryFlash( 700 ), 0 );
});

test("Berserk glow fades in, holds full opacity, then fades out", () => {
	const samples = [
		{ elapsed: 0, opacity: 0 },
		{ elapsed: 1500, opacity: 0.5 },
		{ elapsed: 3000, opacity: 1 },
		{ elapsed: 30000, opacity: 1 },
		{ elapsed: 60000, opacity: 1 },
		{ elapsed: 61500, opacity: 0.5 },
		{ elapsed: 63000, opacity: 0 }
	];

	for ( const sample of samples ) {
		assert.equal( berserkHud( sample.elapsed ).glow, sample.opacity, "glow at " + sample.elapsed );
	}
});

test("Every Berserk animation frame is admitted by the renderer", () => {
	const owner = createUiPreparation();
	const frameIntervalMs = 1000 / 144;
	const animationEndMs = 64000;

	// Fractional frame times exercise the fade between its exact endpoints.
	for ( let time = 0; time <= animationEndMs; time += frameIntervalMs ) {
		const hud = berserkHud( time );
		const alphas = [ hud.glow, ...hud.circles, ...hud.fire, berserkEntryFlash( time ) ];
		assert.doesNotThrow(
			() =>
				owner.prepare( {
					revision: Math.floor( time ),
					width: 100,
					height: 100,
					quads: alphas.map( alpha => ({
						rect: [ 0, 0, 10, 10 ],
						clip: [ 0, 0, 100, 100 ],
						uv: [ 0, 0, 1, 1 ],
						texture: "",
						color: [ 1, 1, 1, alpha ]
					}) )
				} ),
			"Berserk at " + time
		);
	}
});
