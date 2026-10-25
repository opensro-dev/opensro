/*
===========================================================================

arrow-turn-prediction.test.mjs - the main-thread mover predicts the arrow
keys' turn (GH #1574)

End to end: the real gameplay owner stands in as the worker (its arrow
keys, movement owner and wire), taking each key after a latency and
publishing every 32 ms after another; the real main-thread mover predicts
on the uneven frame cadence. Left and Right turn the facing from the frame
the key goes down, walking or standing, before any answer can come back;
the facing never steps back when the worker's answers take over, and the
body ends where the worker ends.

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
const { ARROW_TURN_WORDS_PER_MS } = await import( "../../src/engine/foundation/gameplay/direction-movement.ts" );
const { worldDelta } = await import( "../../src/engine/foundation/world/world-space.ts" );

// The main thread to worker trip and back (a frame or so each way; the
// network to the server is behind the worker and does not enter).
const REGION = 257, TICK_MS = 16, PUBLISH_MS = 32, IN_LATENCY_MS = 20, OUT_LATENCY_MS = 25;
// No worker answer can reach the main thread sooner than this after a key.
const BLIND_MS = IN_LATENCY_MS + TICK_MS + OUT_LATENCY_MS;
const FRAMES = [ 16.7, 16.6, 33.4, 8.1, 16.7, 25.0, 16.6, 12.2, 16.7, 41.6, 16.7, 16.6 ];
const START = { regionId: REGION, x: 600, y: 0, z: 600, angle: 0 };

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
headingGap

Signed short way from a to b, in heading words.
================
*/
function headingGap( a, b ) {
	return ((b - a) % 65535 + 65535 + 32767) % 65535 - 32767;
}

/*
================
gap
================
*/
function gap( a, b ) {
	const [dx, dz] = worldDelta( a, b );
	return Math.hypot( dx, dz );
}

/*
================
worstReverse

The largest step the drawn facing ever takes against the turn (positive
words; 0 when it only ever turns one way).
================
*/
function worstReverse( frames, sign ) {
	let back = 0;
	for ( let i = 1; i < frames.length; i++ ) {
		back = Math.max( back, -sign * headingGap( frames[i - 1].pose.angle, frames[i].pose.angle ) );
	}
	return back;
}

/*
================
play

Runs keys (main-clock times) through a real gameplay worker and the mover.
Each drawn frame carries the worker's true pose at that moment (truth).
================
*/
function play( keys, endMs = 3000 ) {
	const game = createGameplay( () => {} );
	const local = { ...START, gid: 7, refObjId: 1907, kind: "local-player", name: "walker", heading: 0 };
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	const bundle = product( REGION );
	bundle.objects = [];
	game.command( { kind: "navigation", regionId: REGION, bundle }, 0, undefined, local );
	const mover = createLocalMover(), view = flatView(), inbox = [], published = [];
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
			walkPredictable: s.walkPredictable === true,
			walkHeld: s.walkHeld === true,
			holding: s.moveHeld === true,
			speed: s.movementSpeed,
			inputSeq: s.movementInputSeq ?? 0,
			navigationRequestId: s.navigationRequestId,
			navigationOwner: s.navigationOwner,
			originMs: 0,
			view: { requestId: s.navigationRequestId, navigation: view }
		};
	};
	while ( now < endMs + IN_LATENCY_MS ) {
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
		if ( now <= endMs ) drawn.push( { now, pose: { ...last }, kind: result.kind, truth: { ...state.pose } } );
	}
	game.dispose();
	return drawn;
}

test("Left turns a standing body before any answer can come back, and never back", () => {
	const drawn = play( [ { at: 200, key: "left", down: true, rate: ARROW_TURN_WORDS_PER_MS }, {
		at: 700,
		key: "left",
		down: false
	} ] );
	const blind = drawn.filter( f => f.now > 200 && f.now < 200 + BLIND_MS );
	assert.ok( blind.length && defined( blind.at( -1 ) ).pose.angle > 0, "turning before the worker can answer" );
	const turning = drawn.filter( f => f.now > 200 && f.now < 760 );
	assert.ok( worstReverse( turning, 1 ) < 40, `steps back ${worstReverse( turning, 1 )} words` );
	// The worker publishes its standing turn as it goes: the drawn facing is
	// never more than the blind time's turn from the worker's.
	const apart = Math.max( ...turning.map( f => Math.abs( headingGap( f.pose.angle, f.truth.angle ) ) ) );
	assert.ok( apart < BLIND_MS * 2 * ARROW_TURN_WORDS_PER_MS, `facing ${apart} words from the worker's` );
	const end = defined( drawn.at( -1 ) );
	assert.ok(
		Math.abs( headingGap( end.pose.angle, end.truth.angle ) ) < 200,
		`${end.pose.angle} vs ${end.truth.angle}`
	);
});

test("turning while Up is held curves the drawn walk from the key's own frame, and never back", () => {
	const drawn = play( [
		{ at: 100, key: "up", down: true },
		{ at: 600, key: "left", down: true, rate: ARROW_TURN_WORDS_PER_MS },
		{ at: 1100, key: "left", down: false },
		{ at: 1800, key: "up", down: false }
	], 2600 );
	const blind = drawn.filter( f => f.now > 600 && f.now < 600 + BLIND_MS );
	assert.ok( blind.length && defined( blind.at( -1 ) ).pose.angle > 0, "the drawn facing turns at once" );
	const curve = drawn.filter( f => f.now > 600 && f.now < 1160 );
	assert.ok( worstReverse( curve, 1 ) < 40, `steps back ${worstReverse( curve, 1 )} words` );
	// Through the curve the body walks with the worker's, never more than a
	// key trip apart.
	const off = Math.max( ...curve.map( f => gap( f.pose, f.truth ) ) );
	assert.ok( off < 3, `strays ${off.toFixed( 2 )} from the worker` );
	const end = defined( drawn.at( -1 ) );
	assert.ok( gap( end.pose, end.truth ) < .5, `ends ${gap( end.pose, end.truth ).toFixed( 3 )} from the worker` );
});

test("between answers the predicted walk itself curves with the turn", () => {
	// No publication ever changes: only the mover's own stepping can curve it.
	const mover = createLocalMover();
	const world = /** @type {any} */ ({
		pose: { ...START },
		poseAtMs: 0,
		revision: 0,
		moving: false,
		aim: undefined,
		speedFactor: 1,
		turnRate: 0,
		walkPredictable: true,
		walkHeld: false,
		holding: false,
		speed: 50,
		inputSeq: 0,
		navigationRequestId: 1,
		navigationOwner: undefined,
		originMs: 0,
		view: { requestId: 1, navigation: flatView() }
	});
	mover.arrow( 1, true, START, 0, world );
	mover.turn( 2, ARROW_TURN_WORDS_PER_MS, START, 0, world );
	let last = START;
	for ( let now = 16; now <= 500; now += 16 ) {
		const frame = mover.frame( now, world );
		if ( frame.kind !== "idle" ) last = frame.pose;
	}
	// Half a second at pi rad/s from facing +x: a quarter turn, and the walk
	// has bent off the x axis.
	assert.ok( last.angle > 14000, `faces ${last.angle}` );
	assert.ok( Math.abs( last.z - START.z ) > 5, `curved ${Math.abs( last.z - START.z ).toFixed( 2 )}` );
});
