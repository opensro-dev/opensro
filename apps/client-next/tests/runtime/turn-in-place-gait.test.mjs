/*
===========================================================================

turn-in-place-gait.test.mjs - a turn in place plays no walk gait (#4155)

End to end: the real gameplay owner stands in as the worker (arrow keys,
movement owner, wire) and the real pose presentation answers the gait
question actor-motion asks it (moving or standing). The worker's own
snapshot calls a standing turn not moving from first to last; the
presentation must agree. It once did not: a turn keeps publishing fresh
standing samples, and with the render delay a walk had calibrated, the
buffered walk's end stayed ahead of the render clock for the whole turn,
so the local player played its walk gait while only turning. Native
v1.150 turns the facing only (CNavigationController_UpdateSteeringTurn
0x86D480 changes yaw; CNavigationController_OnTick 0x86D6D0 takes no step
without a move target), so the drawn body turns in its stand motion.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product } from "../helpers/navigation-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createLocalMover } = await import( "../../src/engine/runtime/movement/local-mover.ts" );
const { createPosePresentation } = await import( "../../src/engine/runtime/characters/pose-presentation.ts" );
const { createOwnWalkInterpolation } = await import( "../../src/engine/runtime/characters/own-walk-interpolation.ts" );
const { ARROW_TURN_WORDS_PER_MS } = await import( "../../src/engine/foundation/gameplay/direction-movement.ts" );

const REGION = 257, TICK_MS = 16, PUBLISH_MS = 32, IN_LATENCY_MS = 20, OUT_LATENCY_MS = 25;
const FRAMES = [ 16.7, 16.6, 33.4, 8.1, 16.7, 25.0, 16.6, 12.2, 16.7, 41.6, 16.7, 16.6 ];
const START = { regionId: REGION, x: 600, y: 0, z: 600, angle: 0 };
const GID = 7;

/*
================
flatView

The main thread's navigation view of the open fixture region.
================
*/
function flatView() {
	return {
		available: () => true,
		block: () => 0,
		floor: () => undefined,
		room: () => undefined,
		pick: () => null,
		clip: ( _from, to, query ) => {
			if ( query ) query.status = 0;
			return to;
		},
		surface: pose => pose
	};
}

/*
================
play

Runs keys (main-clock times) through a real gameplay worker and the
mover, feeding the pose presentation the local player's samples exactly
as presentation-samples builds them. Returns one row per frame.
================
*/
function play( keys, endMs ) {
	const game = createGameplay( () => {} );
	const local = { ...START, gid: GID, refObjId: 1907, kind: "local-player", name: "walker", heading: 0 };
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	const bundle = product( REGION );
	bundle.objects = [];
	game.command( { kind: "navigation", regionId: REGION, bundle }, 0, undefined, local );
	const mover = createLocalMover(), view = flatView(), inbox = [], published = [];
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	let state = defined( game.take() ), atMs = 0, now = 0, frame = 0, seq = 0, last = { ...START };
	const drawn = [], pending = keys.slice();
	const worldAt = ms => {
		let p;
		for ( const row of published ) if ( row.at <= ms ) p = row.state;
		const s = p ?? state;
		return {
			pose: s.pose,
			poseAtMs: s.poseAtMs,
			revision: s.movementRevision ?? 0,
			moving: s.moving ?? false,
			aim: s.movementAim,
			speedFactor: s.movementSpeedFactor ?? 1,
			turnRate: s.movementTurnRate ?? 0,
			relocation: 0,
			walkPredictable: s.walkPredictable === true,
			walkHeld: s.walkHeld === true,
			holding: s.moveHeld === true,
			speed: s.movementSpeed,
			inputSeq: s.movementInputSeq ?? 0,
			navigationRequestId: s.navigationRequestId,
			navigationOwner: s.navigationOwner,
			originMs: 0,
			view: { requestId: s.navigationRequestId, navigation: view },
			raw: s
		};
	};
	while ( now < endMs ) {
		now += FRAMES[frame++ % FRAMES.length];
		while ( atMs + TICK_MS <= now ) {
			atMs += TICK_MS;
			for ( const input of inbox.filter( i => i.at <= atMs ) ) {
				inbox.splice( inbox.indexOf( input ), 1 );
				game.noteInput( input.seq );
				game.command( { kind: "arrow-key", key: input.key, down: input.down }, atMs, undefined, local );
			}
			game.step( atMs, local );
			state = { ...state, ...(game.take() ?? {}) };
			if ( atMs % PUBLISH_MS === 0 ) published.push( { at: atMs + OUT_LATENCY_MS, state: { ...state } } );
		}
		while ( pending.length && pending[0].at <= now ) {
			const key = defined( pending.shift() );
			seq++;
			inbox.push( { ...key, seq, at: now + IN_LATENCY_MS } );
			const world = worldAt( now );
			if ( key.key === "up" ) mover.arrow( seq, key.down, last, now, world );
			else mover.turn( seq, key.down ? key.rate : 0, last, now, world );
		}
		const result = mover.frame( now, worldAt( now ) );
		if ( result.kind !== "idle" ) last = result.pose;
		const s = worldAt( now ).raw;
		presentation.samples(
			new Map( [ [ GID, {
				ownLeg: true,
				atMs: s.poseAtMs,
				revision: s.movementRevision ?? 0,
				moving: s.moving ?? false,
				...(s.movementPath ?
					{ from: s.movementPath.from, to: s.movementPath.to, durationMs: s.movementPath.durationMs } :
					{})
			} ] ] )
		);
		const pose = presentation.pose( GID, { ...s.pose }, now / 1000 );
		drawn.push( {
			now,
			workerMoving: s.moving ?? false,
			presentedMoving: presentation.moving( GID ),
			angle: pose.angle
		} );
	}
	game.dispose();
	return drawn;
}

test("a turn in place after a walk reads standing throughout, the walk reads moving", () => {
	const drawn = play( [
		{ at: 200, key: "up", down: true },
		{ at: 1200, key: "up", down: false },
		{ at: 1600, key: "left", down: true, rate: ARROW_TURN_WORDS_PER_MS },
		{ at: 2800, key: "left", down: false }
	], 3400 );
	const walking = drawn.filter( f => f.now > 500 && f.now < 1150 );
	assert.ok( walking.length > 0, "the walk produced frames" );
	assert.ok(
		walking.every( f => f.workerMoving && f.presentedMoving ),
		"the walk reads moving at both layers"
	);
	const standing = drawn.filter( f => f.now > 1400 && f.now < 1600 );
	assert.ok(
		standing.length > 0 && standing.every( f => !f.presentedMoving ),
		"the settled stand reads standing"
	);
	const turning = drawn.filter( f => f.now > 1900 && f.now < 2750 );
	assert.ok( turning.length > 0, "the turn produced frames" );
	assert.ok(
		turning.every( f => !f.workerMoving ),
		"the worker calls the turn not moving"
	);
	// The body really turned while it read standing: over half a turn of
	// drawn facing across the window.
	const turned = Math.abs( defined( turning.at( -1 ) ).angle - defined( turning[0] ).angle );
	assert.ok( turned > 16000, `the drawn body turned (${turned} words)` );
	assert.ok(
		turning.every( f => !f.presentedMoving ),
		"a turn in place plays no walk gait"
	);
});

test("a walk stopped short of its leg's arrival ends its gait at the stop", () => {
	// A stand published mid-leg ends the buffered walk at its own tick: the
	// gait must not run on to the arrival the leg had planned (1000 ms).
	const walk = createOwnWalkInterpolation();
	const at = x => ({ regionId: 1, x, y: 0, z: 0, angle: 0 });
	const from = at( 0 ), to = at( 100 );
	walk.push( { atMs: 0, pose: at( 0 ), moving: true, from, to, durationMs: 1000, arrivedAtMs: 0 } );
	walk.push( { atMs: 100, pose: at( 10 ), moving: true, from, to, durationMs: 1000, arrivedAtMs: 100 } );
	assert.equal( walk.moving( 150 ), true, "the walk reads moving while it walks" );
	walk.push( { atMs: 200, pose: at( 20 ), moving: false, arrivedAtMs: 200 } );
	assert.equal( walk.moving( 600 ), false, "the gait ends with the stop, not at the planned arrival" );
});
