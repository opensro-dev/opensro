/*
===========================================================================

local-mover-approach.test.mjs - the main-thread mover predicts entity
approach walks (GH #1567)

A far select or pickup is a walk before it is an interaction: the worker
approaches, then interacts on arrival. Through the #1563 seam the mover
predicts that leg from the click frame (aim: the approach goal), the
worker's own aim replaces the prediction once it takes the input, and
the arrival hands the body back - the mover settles and ends, and the
worker's interaction owns what happens there. A select the worker took
but refused (in reach, gone) reconciles away with no trace.

A small worker stands in for the simulation worker: it takes each input
after a latency, walks the goal it computed (not necessarily the
predicted one) in 16 ms ticks, and publishes after a latency.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createLocalMover } = await import( "../../src/engine/runtime/movement/local-mover.ts" );

const REGION = 0x61a8, SPEED = 50, TICK_MS = 16, PUBLISH_MS = 32;
const IN_LATENCY_MS = 20, OUT_LATENCY_MS = 25;
/** @typedef {import("../../src/engine/contracts/gameplay").Pose} Pose */
const START = { regionId: REGION, x: 1000, y: 0, z: 1000, angle: 0 };
// The camera-shake recording's frame intervals (ms): uneven on purpose.
const FRAMES = [ 16.7, 16.6, 33.4, 8.1, 16.7, 25.0, 16.6, 12.2, 16.7, 41.6, 16.7, 16.6 ];
const CAP = 1.04;

/*
================
flatView

A navigation view that never blocks and never re-picks.
================
*/
function flatView() {
	return {
		available: () => true,
		block: () => 0,
		floor: () => undefined,
		pick: query => ({ ...query.ray.target }),
		clip: ( _from, to ) => to,
		surface: pose => pose
	};
}

/*
================
createWorker

The simulation worker, reduced to its approach walk: takes an input's
sequence after a latency, then walks its own goal (or refuses: takes the
sequence and walks nothing), publishing pose, aim and sequence.
================
*/
function createWorker() {
	const state = {
		pose: /** @type {Pose} */ ({ ...START }),
		goal: /** @type {Pose | undefined} */ (undefined),
		inputSeq: 0,
		revision: 0,
		atMs: 0
	};
	const inbox = [], published = [];
	return {
		state,
		send( seq, input, mainMs ) {
			inbox.push( { seq, input, atMs: mainMs + IN_LATENCY_MS } );
		},
		run( ms ) {
			while ( state.atMs + TICK_MS <= ms ) {
				state.atMs += TICK_MS;
				for ( const input of inbox.filter( i => i.atMs <= state.atMs ) ) {
					inbox.splice( inbox.indexOf( input ), 1 );
					state.inputSeq = Math.max( state.inputSeq, input.seq );
					if ( !input.input.refuse ) {
						state.goal = {
							...input.input.to,
							x: Math.trunc( input.input.to.x ),
							z: Math.trunc( input.input.to.z )
						};
					}
					state.revision++;
				}
				if ( state.goal ) {
					const dx = state.goal.x - state.pose.x,
						dz = state.goal.z - state.pose.z,
						left = Math.hypot( dx, dz ),
						step = SPEED * TICK_MS / 1000;
					if ( left <= step ) {
						state.pose = { ...state.pose, ...state.goal };
						state.goal = undefined;
						state.revision++;
					} else {
						state.pose = {
							...state.pose,
							x: state.pose.x + dx / left * step,
							z: state.pose.z + dz / left * step
						};
					}
				}
				if ( state.atMs % PUBLISH_MS === 0 ) {
					published.push( {
						visibleAtMain: state.atMs + OUT_LATENCY_MS,
						world: {
							pose: { ...state.pose },
							poseAtMs: state.atMs,
							revision: state.revision,
							moving: !!state.goal,
							aim: state.goal && { kind: "goal", destination: { ...state.goal } },
							inputSeq: state.inputSeq
						}
					} );
				}
			}
		},
		visible( mainMs ) {
			let newest;
			for ( const p of published ) if ( p.visibleAtMain <= mainMs ) newest = p;
			return newest?.world;
		}
	};
}

/*
================
play

Predicts each click's approach leg at its main-clock time and steps the
mover frame by frame, recording the drawn body and the worker's truth.
================
*/
function play( clicks, endMs = 5000 ) {
	const worker = createWorker(), mover = createLocalMover(), view = flatView();
	const drawn = [], answers = [], clicksAt = [];
	let now = 0, frame = 0, last = { ...START }, seq = 0;
	const pending = clicks.slice();
	const worldAt = ms => {
		const p = worker.visible( ms );
		return {
			pose: p ? p.pose : { ...START },
			poseAtMs: p?.poseAtMs,
			revision: p?.revision ?? 0,
			moving: p?.moving ?? false,
			aim: p?.aim,
			speedFactor: 1,
			turnRate: 0,
			walkPredictable: true,
			walkHeld: false,
			holding: false,
			speed: SPEED,
			inputSeq: p?.inputSeq ?? 0,
			navigationRequestId: 1,
			navigationOwner: undefined,
			originMs: 0,
			view: { requestId: 1, navigation: view }
		};
	};
	while ( now < endMs ) {
		now += FRAMES[frame++ % FRAMES.length];
		worker.run( now );
		while ( pending.length && pending[0].atMs <= now ) {
			const click = defined( pending.shift() );
			seq++;
			clicksAt.push( now );
			worker.send( seq, click, now );
			answers.push( mover.predict(
				{ seq, aim: { kind: "goal", destination: click.aimTo }, speed: SPEED, drawn: last, now },
				worldAt( now )
			) );
		}
		const result = mover.frame( now, worldAt( now ) );
		if ( result.kind !== "idle" ) last = result.pose;
		drawn.push( { now, pose: { ...last }, kind: result.kind, truth: { ...worker.state.pose } } );
	}
	return { drawn, worker: worker.state, answers, clicksAt };
}

/*
================
distance

Planar distance between two poses.
================
*/
function distance( a, b ) {
	return Math.hypot( a.x - b.x, a.z - b.z );
}

/*
================
assertCapped

No drawn frame outruns the shared step cap (drawn-step.ts 1.04x).
================
*/
function assertCapped( drawn ) {
	for ( let i = 1; i < drawn.length; i++ ) {
		const dt = drawn[i].now - drawn[i - 1].now,
			step = distance( drawn[i].pose, drawn[i - 1].pose );
		assert.ok(
			step <= SPEED * CAP * dt / 1000 + 1e-6,
			`frame ${i} moved ${step} in ${dt} ms, past the cap`
		);
	}
}

test("an approach is predicted from the click frame and hands back on arrival", () => {
	// The mover predicts (1100, 1000); the worker walks its own (1103, 1000).
	const aimTo = { regionId: REGION, x: 1100, y: 0, z: 1000, angle: 0 };
	const { drawn, worker, answers, clicksAt } = play( [
		{ atMs: 100, aimTo, to: { regionId: REGION, x: 1103, y: 0, z: 1000, angle: 0 } }
	] );
	assert.deepEqual( answers, [ true ] );
	// The body left rest within a frame of the click, before the worker's
	// answer could possibly be visible (in 20 ms, publish 32, out 25).
	const first = drawn.find( d => d.now > clicksAt[0] && distance( d.pose, START ) > 0 );
	assert.ok( first, "no motion followed the click" );
	assert.ok( first.now - clicksAt[0] <= 42, "motion waited past the click frame" );
	assert.ok( distance( first.truth, START ) <= distance( first.pose, START ), "the worker led the body" );
	assertCapped( drawn );
	// Arrival hands back: an ended frame at the worker's own destination,
	// then idle, and the interaction owns the body from there.
	const end = drawn.findIndex( d => d.kind === "ended" );
	assert.ok( end > 0, "the prediction never handed back" );
	assert.ok( distance( drawn[end].pose, worker.pose ) < 0.05, "handed back away from the worker" );
	assert.ok( distance( worker.pose, { x: 1103, z: 1000 } ) < 1e-9, "the worker walked its own aim" );
	assert.ok( drawn.slice( end + 1 ).every( d => d.kind === "idle" ), "the mover kept the body after handback" );
});

test("a refused approach reconciles away and leaves no trace", () => {
	const aimTo = { regionId: REGION, x: 1100, y: 0, z: 1000, angle: 0 };
	const { drawn, answers } = play( [ { atMs: 100, aimTo, refuse: true } ], 2500 );
	assert.deepEqual( answers, [ true ] );
	assertCapped( drawn );
	const end = drawn.findIndex( d => d.kind === "ended" );
	assert.ok( end > 0, "the refusal never reconciled" );
	assert.ok( distance( drawn[end].pose, START ) < 0.05, "the body stayed displaced" );
	assert.ok( drawn.slice( end + 1 ).every( d => d.kind === "idle" ) );
	// It walked out and came back: the excursion was real but bounded by
	// the answer latency, never by the whole approach leg.
	const farthest = Math.max( ...drawn.map( d => distance( d.pose, START ) ) );
	assert.ok( farthest < 20, `the body walked ${farthest} toward a refused approach` );
});

test("an approach clicked mid-walk redirects the prediction without a jump", () => {
	const first = { regionId: REGION, x: 1200, y: 0, z: 1000, angle: 0 };
	const second = { regionId: REGION, x: 1050, y: 0, z: 900, angle: 0 };
	const { drawn, worker, answers } = play( [
		{ atMs: 100, aimTo: first, to: first },
		{ atMs: 700, aimTo: second, to: second }
	] );
	assert.deepEqual( answers, [ true, true ] );
	assertCapped( drawn );
	const ends = drawn.filter( d => d.kind === "ended" );
	assert.equal( ends.length, 1, "the prediction dropped between the two clicks" );
	assert.ok( distance( ends[0].pose, worker.pose ) < 0.05 );
	assert.ok( distance( worker.pose, second ) < 1e-9, "the worker ended at the second approach" );
});
