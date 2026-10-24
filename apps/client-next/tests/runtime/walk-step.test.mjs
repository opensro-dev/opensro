/*
===========================================================================

walk-step.test.mjs - tests for walk-step.ts

The pure walking step the worker's movement owner applies on its fixed
tick and the render-thread mover (epic #1437) will apply every frame.
Loads the TypeScript sources through the shared native loader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { stepWalk, startWalking, WALK_NAVIGATION_REJECT, WALK_NAVIGATION_STOP } = await import(
	"../../src/engine/foundation/gameplay/walk-step.ts"
);

const REGION = 24998, SPEED = 100;
const FROM = { regionId: REGION, x: 800, y: 60, z: 900, angle: 0 };

/*
================
open

A navigation view with no obstacles: every move is accepted as asked.
================
*/
const open = {
	clip: ( _from, to, query ) => {
		query.status = 0;
		return to;
	},
	surface: pose => pose
};

/*
================
leg

A goal walk of length units along +x at SPEED.
================
*/
/** @returns {import("../../src/engine/foundation/gameplay/walk-step.ts").WalkStepSegment} */
function leg( length, start = 0 ) {
	const to = { ...FROM, x: FROM.x + length };
	return { from: FROM, to, start, duration: length / SPEED * 1000 };
}

/*
================
walk

Steps a segment from FROM to endMs in steps of stepMs; returns the final
pose and whether the walk ended.
================
*/
function walk( segment, stepMs, endMs, navigation = open ) {
	let pose = FROM, poseAtMs = segment.start, current = segment, ended = false;
	for ( let now = segment.start + stepMs; now <= endMs + 1e-9; now += stepMs ) {
		if ( !current ) break;
		const result = stepWalk( {
			pose,
			poseAtMs,
			segment: current,
			owner: undefined,
			cursor: {},
			now,
			...navigation
		} );
		if ( result.kind === "stepped" ) {
			pose = result.pose;
			current = result.segment;
			ended = !result.segment;
		} else if ( result.kind === "uncovered" ) current = result.segment;
		poseAtMs = now;
	}
	return { pose, ended, segment: current };
}

test("a goal walk lands on the same pose whether stepped once or every frame", () => {
	// 300 units at 100 u/s: after one second it stands 100 units along.
	const once = walk( leg( 300 ), 1000, 1000 );
	const frames = walk( leg( 300 ), 1000 / 60, 1000 );
	const ticks = walk( leg( 300 ), 16, 1008 );
	assert.ok( Math.abs( once.pose.x - (FROM.x + 100) ) < 1e-3, `once: ${once.pose.x}` );
	// float32 rounding per step is the native behaviour; it stays sub-millimetre.
	assert.ok( Math.abs( frames.pose.x - once.pose.x ) < 1e-2, `frames: ${frames.pose.x}` );
	assert.ok( Math.abs( ticks.pose.x - (FROM.x + 100.8) ) < 1e-2, `ticks: ${ticks.pose.x}` );
	assert.equal( frames.pose.z, FROM.z );
});

test("a goal walk ends on its destination and never overshoots it", () => {
	const done = walk( leg( 30 ), 1000 / 60, 1000 );
	assert.equal( done.ended, true );
	assert.ok( Math.abs( done.pose.x - Math.fround( FROM.x + 30 ) ) < 1e-3, `arrived at ${done.pose.x}` );
});

test("the leg's direction is stored when it starts and the destination is float32", () => {
	const segment = startWalking( { ...leg( 300 ), to: { ...leg( 300 ).to, x: 1100.0000001 } } );
	assert.equal( segment.to.x, Math.fround( 1100.0000001 ) );
	// Native derives the direction through a float32 yaw (clientWalkingDirection):
	// +x comes back as [1, cos(pi/2) in float32], not an exact 0.
	const walking = segment.walking;
	assert.ok( walking );
	assert.equal( walking.direction[0], 1 );
	assert.ok( Math.abs( walking.direction[1] ) < 1e-6 );
	assert.ok( Math.abs( walking.speed - SPEED ) < 1e-6 );
	// A segment already walking keeps its stored direction.
	assert.equal( startWalking( segment ), segment );
});

test("a blocked step ends the walk, a rejected one retires it, missing coverage holds the pose", () => {
	const blocked = stepWalk( {
		pose: FROM,
		poseAtMs: 0,
		segment: leg( 300 ),
		owner: undefined,
		cursor: {},
		now: 100,
		...open,
		...{ clip: ( _f, to, query ) => (query.status = WALK_NAVIGATION_STOP, to) }
	} );
	assert.equal( blocked.kind, "stepped" );
	assert.equal( blocked.segment, null );
	const rejected = stepWalk( {
		pose: FROM,
		poseAtMs: 0,
		segment: leg( 300 ),
		owner: undefined,
		cursor: {},
		now: 100,
		...open,
		...{ clip: ( _f, to, query ) => (query.status = WALK_NAVIGATION_REJECT, to) }
	} );
	assert.equal( rejected.kind, "rejected" );
	const uncovered = stepWalk( {
		pose: FROM,
		poseAtMs: 0,
		segment: leg( 300 ),
		owner: undefined,
		cursor: {},
		now: 100,
		...open,
		...{ clip: () => null }
	} );
	assert.equal( uncovered.kind, "uncovered" );
	// The elapsed time is consumed: the rest of the leg starts now from the held pose.
	assert.equal( uncovered.segment.start, 100 );
	assert.deepEqual( uncovered.segment.from, FROM );
	assert.equal( uncovered.segment.admitted, false );
	assert.ok( Math.abs( uncovered.segment.duration - 3000 ) < 1e-6 );
});

test("no elapsed time is idle, a fixed-timing leg samples its own clock", () => {
	assert.deepEqual(
		stepWalk( {
			pose: FROM,
			poseAtMs: 500,
			segment: leg( 300, 0 ),
			owner: undefined,
			cursor: {},
			now: 500,
			...open
		} ),
		{ kind: "idle" }
	);
	const dash = { ...leg( 300 ), fixedTiming: true };
	const sampled = stepWalk( {
		pose: FROM,
		poseAtMs: 0,
		segment: dash,
		owner: undefined,
		cursor: {},
		now: 1500,
		...open
	} );
	assert.equal( sampled.kind, "sampled" );
	assert.ok( Math.abs( sampled.pose.x - (FROM.x + 150) ) < 1e-6, `sampled ${sampled.pose.x}` );
});

test("a direction walk is not bounded by its leg end's plane", () => {
	const direction = { ...leg( 30 ), direction: { heading: 16384, blocked: false } };
	const result = walk( direction, 1000 / 60, 1000 );
	assert.equal( result.ended, false, "a direction walk renews its leg instead of arriving" );
});
