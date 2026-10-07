/*
===========================================================================

coalesced-movement-presentation.test.mjs - navigation proof across a receipt

A click and its receipt can arrive before the next display frame. Exercise
the movement publisher and presentation together, without an intermediate
publication that the real main thread never received.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product } from "../helpers/navigation-fixture.mjs";
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const { createPosePresentation } = await import( "../../src/engine/runtime/characters/pose-presentation.ts" );
const FROM = { regionId: 257, x: 100, y: 0, z: 100, angle: 0 };
const TO = { ...FROM, x: 200 };

/*
================
publish
================
*/
function publish( presentation, state, timed = true ) {
	presentation.samples(
		new Map( [ [ 7, {
			atMs: state.poseAtMs,
			revision: state.movementRevision,
			moving: state.moving,
			from: state.movementPath?.from,
			to: state.movementPath?.to,
			durationMs: timed ? state.movementPath?.durationMs : undefined,
			transition: state.movementTransition
		} ] ] )
	);
}

test("an untimed coalesced click and accepted receipt retain the admitted path behind the new anchor", () => {
	const movement = createMovement( () => {} ), navigation = product(), presentation = createPosePresentation();
	navigation.objects = [];
	movement.seed( FROM );
	movement.navigation( 257, navigation );
	presentation.origin( 0 );
	publish( presentation, movement.state() );
	presentation.pose( 7, FROM, 0 );
	movement.request( TO, 0 );
	movement.step( 16 );
	const predicted = movement.state().pose;
	movement.receive(
		new TextEncoder().encode( JSON.stringify( {
			v: 1,
			id: 1,
			gid: 7,
			accepted: true,
			serverTimeMs: 16,
			world: { spawn: TO, moveSegment: { from: FROM, startedAtMs: 0, arrivesAtMs: 2000 } }
		} ) ),
		16,
		7
	);
	const state = movement.state();
	assert.ok( state.pose && state.movementPath );
	assert.deepEqual( state.pose, predicted, "the receipt preserves the logical pose" );
	assert.ok( Math.abs( state.movementPath.from.x - 100.8 ) < 1e-9, "the logical walk remains rebased" );
	publish( presentation, state, false );
	assert.equal(
		presentation.pose( 7, state.pose, .02 ).x,
		FROM.x,
		"the first visible receipt must not snap over the unpublished start of the admitted path"
	);
	let previous = FROM.x;
	for ( let frame = 1; frame <= 60; frame++ ) {
		const shown = presentation.pose( 7, state.pose, .02 + frame / 240 );
		assert.ok( shown.x >= previous && shown.x <= state.pose.x );
		previous = shown.x;
	}
	assert.ok( Math.abs( previous - state.pose.x ) < .01 );
});

test("retained path evidence does not authorize a diagonal across a turn", () => {
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	/** @type {import("../../src/engine/contracts/gameplay.ts").MovementTransition} */
	const transition = { relocation: 1, reason: "input", eligible: true };
	presentation.samples( new Map( [ [ 7, { atMs: 0, revision: 1, moving: false, transition } ] ] ) );
	presentation.pose( 7, FROM, 0 );
	const corner = { ...FROM, x: 101 }, target = { ...corner, z: 101 };
	presentation.samples(
		new Map( [ [ 7, {
			atMs: 16,
			revision: 2,
			moving: true,
			from: corner,
			to: { ...corner, z: 200 },
			transition: { ...transition, reason: "receipt", previousPath: { from: FROM, to: corner } }
		} ] ] )
	);
	const shown = presentation.pose( 7, target, .02 );
	assert.equal( shown.x, target.x );
	assert.equal( shown.z, target.z );
});

for ( const destination of [ { x: 102, z: 200 }, { x: 20, z: 260 }, { x: 260, z: 230 } ] ) {
	for ( const coalescedReceipt of [ false, true ] ) {
		test(`a proven turn to ${destination.x},${destination.z}, receipt=${coalescedReceipt}, follows its corner`, () => {
			const movement = createMovement( () => {} ),
				navigation = product(),
				presentation = createPosePresentation();
			navigation.objects = [];
			movement.seed( FROM );
			movement.navigation( 257, navigation );
			movement.request( TO, 0 );
			presentation.origin( 0 );
			for ( const time of [ 16, 32 ] ) {
				movement.step( time );
				const state = movement.state();
				assert.ok( state.pose );
				publish( presentation, state );
				presentation.pose( 7, state.pose, time / 1000 );
			}
			const last = movement.state();
			assert.ok( last.pose );
			let previous = presentation.pose( 7, last.pose, .048 );
			movement.request( { ...FROM, ...destination }, 40 );
			const turn = movement.state().movementTransition.turn;
			assert.ok( turn, "the movement owner supplies the connection, not a geometric guess" );
			assert.equal( turn.outgoing.from.x, 102 );
			let sampled = 40;
			if ( coalescedReceipt ) {
				movement.step( sampled = 56 );
				const { from, to } = turn.outgoing;
				const duration = Math.hypot( to.x - from.x, to.z - from.z ) / 50 * 1000;
				movement.receive(
					new TextEncoder().encode( JSON.stringify( {
						v: 1,
						id: 2,
						gid: 7,
						accepted: true,
						serverTimeMs: sampled,
						world: { spawn: to, moveSegment: { from, startedAtMs: 40, arrivesAtMs: 40 + duration } }
					} ) ),
					sampled,
					7
				);
				const transition = movement.state().movementTransition;
				assert.equal( transition.reason, "receipt" );
				assert.equal( transition.logicalDistance, 0 );
				assert.equal( transition.pathEligible, true );
				assert.deepEqual( transition.turn, turn, "receipt retains the coalesced turn proof" );
			}
			for ( let time = 60; time <= 400; time += 4 ) {
				while ( sampled + 16 <= time ) movement.step( sampled += 16 );
				const state = movement.state();
				assert.ok( state.pose );
				publish( presentation, state );
				const shown = presentation.pose( 7, state.pose, time / 1000 );
				const corner = turn.outgoing.from, end = turn.outgoing.to;
				const cross = (shown.x - corner.x) * (end.z - corner.z) - (shown.z - corner.z) * (end.x - corner.x);
				assert.ok(
					Math.abs( shown.z - FROM.z ) <= .01 ||
						Math.abs( cross ) / Math.hypot( end.x - corner.x, end.z - corner.z ) <= .01,
					"every drawn point stays on one of the admitted legs"
				);
				assert.ok(
					Math.hypot( shown.x - previous.x, shown.z - previous.z ) < .65,
					`turn snapped by ${Math.hypot( shown.x - previous.x, shown.z - previous.z )}`
				);
				previous = shown;
			}
			assert.ok(
				Math.hypot( previous.x - 102, previous.z - 100 ) > 12,
				"recovery cannot park the body at the corner"
			);
			assert.equal(
				previous.angle,
				movement.state().pose?.angle,
				"position recovery cannot leave the body facing back"
			);
		});
	}
}
