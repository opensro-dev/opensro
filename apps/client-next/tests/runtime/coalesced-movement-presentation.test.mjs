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

test("a same-time speed change replaces the cached validated lookahead", () => {
	const movement = createMovement( () => {} ), navigation = product();
	navigation.objects = [];
	movement.seed( FROM );
	movement.navigation( 257, navigation );
	movement.request( TO, 0 );
	movement.step( 16 );
	const before = movement.state();
	assert.ok( before.movementPath );
	const oldHistory = before.movementTransition.walkingPath;
	assert.ok( oldHistory );
	movement.speeds( 20, 100, 16 );
	const after = movement.state();
	assert.ok( after.movementPath );
	const history = after.movementTransition.walkingPath;
	assert.ok( history );
	assert.equal( after.pose?.x, before.pose?.x, "changing gait does not advance the authoritative pose" );
	assert.ok( after.movementPath.to.x > before.movementPath.to.x );
	assert.notEqual( history, oldHistory );
	assert.deepEqual( history.at( -1 ), after.movementPath.to, "the sampled proof reaches the new checked endpoint" );
	movement.clear();
});

test("a native 150-speed mount retains its terrain proof through a 1000 ms worker step", () => {
	const movement = createMovement( () => {} ), navigation = product(), presentation = createPosePresentation();
	navigation.objects = [];
	const heights = Buffer.alloc( 97 * 97 * 4 );
	for ( let z = 0; z < 97; z++ ) heights.writeFloatLE( 8, (z * 97 + 10) * 4 );
	navigation.navmesh.regions[0].heightMap = heights.toString( "base64" );
	movement.seed( FROM );
	movement.navigation( 257, navigation );
	movement.speeds( 45, 150, 0 );
	movement.request( { ...TO, x: 700 }, 0 );
	presentation.origin( 0 );
	let state = movement.state();
	assert.ok( state.pose );
	publish( presentation, state );
	presentation.pose( 7, state.pose, 0 );
	for ( let now = 16; now <= 320; now += 16 ) {
		movement.step( now );
		state = movement.state();
		assert.ok( state.pose );
		publish( presentation, state );
		presentation.pose( 7, state.pose, now / 1000 );
	}
	const before = state.pose;
	assert.ok( before );
	presentation.pose( 7, before, 1.32 );
	const continued = presentation.pose( 7, before, 1.3325 );
	movement.step( 1320 );
	state = movement.state();
	assert.ok( state.pose );
	assert.ok( Math.abs( state.pose.x - before.x - 150 ) < .001, "the native step is not subdivided or capped" );
	assert.ok( state.movementTransition.walkingPath );
	assert.ok( state.movementTransition.walkingPath.length > 75, "all accepted mounted progress has sampled proof" );
	assert.ok(
		state.movementTransition.walkingPath?.some( point => point.y > 7.5 ),
		"the skipped frame retains the intervening hill"
	);
	publish( presentation, state );
	let previous = presentation.pose( 7, state.pose, 1.3325 );
	assert.ok(
		Math.abs( previous.x - continued.x ) < .01,
		"the mounted publication introduces no instantaneous displacement"
	);
	for ( let now = 1344; now <= 2000; now += 16 ) {
		movement.step( now );
		state = movement.state();
		assert.ok( state.pose );
		publish( presentation, state );
		const shown = presentation.pose( 7, state.pose, now / 1000 );
		assert.ok(
			shown.x >= previous.x && shown.x - previous.x < 35,
			"mounted recovery remains a controlled trajectory"
		);
		assert.ok( state.movementPath );
		assert.ok( shown.x <= state.movementPath.to.x, "mounted recovery stays on the certified path" );
		previous = shown;
	}
	assert.ok( state.pose );
	assert.ok( Math.abs( previous.x - state.pose.x ) < 3 );
	movement.clear();
});

for ( const stalledOwner of [ "main", "worker" ] ) {
	test(`accepted walking history recovers after a ${stalledOwner} stall without a new receipt`, () => {
		const movement = createMovement( () => {} ), navigation = product(), presentation = createPosePresentation();
		navigation.objects = [];
		const heights = Buffer.alloc( 97 * 97 * 4 );
		for ( let z = 0; z < 97; z++ ) {
			for ( let x = 0; x < 97; x++ ) heights.writeFloatLE( Math.sin( x * .4 ), (z * 97 + x) * 4 );
		}
		navigation.navmesh.regions[0].heightMap = heights.toString( "base64" );
		movement.seed( FROM );
		movement.navigation( 257, navigation );
		movement.request( { ...TO, x: 500 }, 0 );
		presentation.origin( 0 );
		let state = movement.state();
		assert.ok( state.pose );
		publish( presentation, state );
		presentation.pose( 7, state.pose, 0 );
		for ( let now = 16; now <= 320; now += 16 ) {
			movement.step( now );
			state = movement.state();
			assert.ok( state.pose );
			publish( presentation, state );
			presentation.pose( 7, state.pose, now / 1000 );
		}
		// A main-thread stall resumes with the old publication before the
		// worker's delayed accepted pose arrives on the following frame.
		if ( stalledOwner === "worker" ) {
			for ( let now = 336; now <= 1312; now += 16 ) presentation.pose( 7, state.pose, now / 1000 );
		}
		const parked = presentation.pose( 7, state.pose, 1.32 );
		const continued = presentation.pose( 7, state.pose, 1.3325 );
		assert.ok( continued.x - parked.x < .625, "only the existing recovery spends this displayed frame" );
		movement.step( 1320 );
		state = movement.state();
		assert.ok( state.pose );
		publish( presentation, state );
		let previous = presentation.pose( 7, state.pose, 1.3325 );
		assert.ok(
			Math.abs( previous.x - continued.x ) < .01,
			"the delayed publication adds no instantaneous displacement"
		);
		for ( let now = 1344; now <= 2000; now += 16 ) {
			movement.step( now );
			state = movement.state();
			assert.ok( state.pose );
			publish( presentation, state );
			const shown = presentation.pose( 7, state.pose, now / 1000 );
			assert.ok(
				shown.x >= previous.x && shown.x - previous.x < 11,
				"recovery cannot reset to the logical pose"
			);
			previous = shown;
		}
		assert.ok( Math.abs( previous.x - state.pose.x ) < 1 );
		movement.clear();
	});
}

test("walking history remains admitted as the short lookahead reaches a wall", () => {
	const movement = createMovement( () => {} ), navigation = product(), presentation = createPosePresentation();
	navigation.objects = [];
	const blocked = Buffer.alloc( 9216 );
	for ( let z = 0; z < 96; z++ ) blocked[z * 96 + 8] = 1;
	navigation.navmesh.regions[0].blockedTiles = blocked.toString( "base64" );
	movement.seed( FROM );
	movement.navigation( 257, navigation );
	movement.request( TO, 0 );
	presentation.origin( 0 );
	let previous = FROM;
	let sawBlockedLookahead = false;
	for ( let now = 0; now <= 1600; now += 16 ) {
		movement.step( now );
		const state = movement.state();
		assert.ok( state.pose );
		publish( presentation, state );
		const shown = presentation.pose( 7, state.pose, (now + 32) / 1000 );
		sawBlockedLookahead ||= state.moving && state.movementTransition.pathEligible === false;
		assert.ok( shown.x - previous.x < 1.61, `blocked lookahead snapped ${shown.x - previous.x}` );
		assert.ok( shown.x < 160, "presentation never crosses the blocked tile" );
		previous = shown;
	}
	assert.ok( sawBlockedLookahead );
	assert.ok( previous.x > 159 );
	movement.clear();
});

test("terrain walking stays continuous through repeated worker catch-up publications", () => {
	const movement = createMovement( () => {} ), navigation = product(), presentation = createPosePresentation();
	navigation.objects = [];
	const heights = Buffer.alloc( 97 * 97 * 4 );
	for ( let z = 0; z < 97; z++ ) {
		for ( let x = 0; x < 97; x++ ) heights.writeFloatLE( Math.sin( x * .4 ), (z * 97 + x) * 4 );
	}
	navigation.navmesh.regions[0].heightMap = heights.toString( "base64" );
	movement.seed( FROM );
	movement.navigation( 257, navigation );
	movement.request( { ...TO, x: 500 }, 0 );
	presentation.origin( 0 );
	let sampled = 0, previous = FROM, previousStep = 0;
	for ( let now = 0; now <= 2208; now += 16 ) {
		if ( now <= 320 ) sampled = now;
		else if ( now >= 1328 ) sampled = Math.min( now, sampled + 96 );
		movement.step( sampled );
		const state = movement.state();
		assert.ok( state.pose );
		publish( presentation, state );
		const shown = presentation.pose( 7, state.pose, now / 1000 );
		const step = shown.x - previous.x;
		if ( now >= 1328 ) {
			assert.ok( step >= -.01 && step < 5.8, `catch-up publication reset recovery: ${step}` );
			assert.ok( Math.abs( step - previousStep ) < 1.5, "catch-up cannot alternate parked and jumping frames" );
		}
		previousStep = step;
		previous = shown;
	}
	const final = movement.state().pose;
	assert.ok( final );
	assert.ok( Math.abs( previous.x - final.x ) < 1 );
	movement.clear();
});

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

for ( const kind of [ "correction", "server receipt" ] ) {
	test(`a ${kind} retains already displayed navigation lookahead before advancing the worker`, () => {
		const movement = createMovement( () => {} ), navigation = product(), presentation = createPosePresentation();
		navigation.objects = [];
		movement.seed( FROM );
		movement.navigation( 257, navigation );
		movement.request( TO, 0 );
		if ( kind === "correction" ) {
			movement.receive(
				new TextEncoder().encode( JSON.stringify( {
					v: 1,
					id: 1,
					gid: 7,
					accepted: true,
					serverTimeMs: 0,
					world: { spawn: TO, moveSegment: { from: FROM, startedAtMs: 0, arrivesAtMs: 2000 } }
				} ) ),
				0,
				7
			);
		}
		presentation.origin( 0 );
		publish( presentation, movement.state() );
		presentation.pose( 7, FROM, 0 );
		movement.step( 16 );
		const published = movement.state();
		assert.ok( published.pose );
		publish( presentation, published );
		presentation.pose( 7, published.pose, .016 );
		const shown = presentation.pose( 7, published.pose, .05 );
		assert.ok( shown.x > FROM.x + 2.4, "display extrapolates within the admitted 100 ms future" );
		const corrected = { ...FROM, x: FROM.x + 1.6 - .01512 };
		if ( kind === "correction" ) movement.correct( corrected, 32 );
		else {movement.receive(
				new TextEncoder().encode( JSON.stringify( {
					v: 1,
					id: 1,
					gid: 7,
					accepted: true,
					serverTimeMs: 32,
					world: { spawn: corrected }
				} ) ),
				32,
				7
			);}
		const state = movement.state();
		assert.ok( state.pose );
		assert.equal( state.moving, false );
		assert.equal( state.movementTransition.eligible, true );
		publish( presentation, state );
		const received = presentation.pose( 7, state.pose, .05 );
		assert.ok(
			Math.abs( received.x - shown.x ) < .0001,
			"a delayed correction cannot discard displayed future proof"
		);
		let previous = received;
		for ( let frame = 1; frame <= 60; frame++ ) {
			const next = presentation.pose( 7, state.pose, .05 + frame / 120 );
			assert.ok( next.x <= previous.x && next.x >= state.pose.x );
			assert.ok( Math.abs( next.z - FROM.z ) < .01 && Math.abs( next.y - FROM.y ) < .01 );
			previous = next;
		}
		assert.ok( Math.abs( previous.x - state.pose.x ) < .01 );
		movement.clear();
	});
}

for ( const kind of [ "correction", "server receipt", "settled receipt" ] ) {
	test(`a terminal ${kind} retains navigation proof behind its corrected endpoint`, () => {
		const movement = createMovement( () => {} ), navigation = product();
		const presentation = createPosePresentation(), control = createPosePresentation();
		navigation.objects = [];
		movement.seed( FROM );
		movement.navigation( 257, navigation );
		movement.request( TO, 0 );
		if ( kind === "correction" ) {
			movement.receive(
				new TextEncoder().encode( JSON.stringify( {
					v: 1,
					id: 1,
					gid: 7,
					accepted: true,
					serverTimeMs: 0,
					world: { spawn: TO, moveSegment: { from: FROM, startedAtMs: 0, arrivesAtMs: 2000 } }
				} ) ),
				0,
				7
			);
		}
		for ( const owner of [ presentation, control ] ) {
			owner.origin( 0 );
			publish( owner, movement.state() );
			owner.pose( 7, FROM, 0 );
		}
		movement.step( 2000 );
		const terminal = movement.state();
		assert.ok( terminal.pose );
		assert.equal( terminal.moving, false );
		assert.ok( terminal.movementTransition.walkingPath );
		let now = 2, shown = FROM;
		for ( const owner of [ presentation, control ] ) {
			publish( owner, terminal );
			owner.pose( 7, terminal.pose, now );
		}
		for ( let frame = 0; frame < 120; frame++ ) {
			now += .008;
			shown = presentation.pose( 7, terminal.pose, now );
			control.pose( 7, terminal.pose, now );
			if ( terminal.pose.x - shown.x < 1.8 ) break;
		}
		assert.ok( terminal.pose.x - shown.x > 1.2, "the drawn body still trails the terminal logical pose" );
		const corrected = { ...terminal.pose, x: terminal.pose.x - .01512 };
		now += .0175;
		const expected = control.pose( 7, terminal.pose, now );
		if ( kind === "correction" ) movement.correct( corrected, now * 1000 );
		else {
			movement.receive(
				new TextEncoder().encode( JSON.stringify( {
					v: 1,
					id: 1,
					gid: 7,
					accepted: true,
					serverTimeMs: now * 1000,
					world: {
						spawn: corrected,
						...(kind === "settled receipt" ?
							{ moveSegment: { from: FROM, startedAtMs: 0, arrivesAtMs: 2000 } } :
							{})
					}
				} ) ),
				now * 1000,
				7
			);
		}
		const state = movement.state();
		assert.ok( state.pose );
		assert.equal( state.moving, false );
		assert.equal( state.movementTransition.eligible, true );
		assert.ok( state.movementTransition.walkingPath );
		assert.equal( state.movementTransition.walkingPath[0]?.x, FROM.x );
		assert.deepEqual( state.movementTransition.walkingPath.at( -1 ), state.pose );
		publish( presentation, state );
		const received = presentation.pose( 7, state.pose, now );
		assert.ok( Math.abs( received.x - expected.x ) < .001, "new terminal correction cannot spend its own force" );
		assert.ok( state.pose.x - received.x > .5, "the remaining glide is retained rather than snapped" );
		let previous = received;
		for ( let frame = 1; frame <= 60; frame++ ) {
			const next = presentation.pose( 7, state.pose, now + frame / 120 );
			assert.ok( next.x >= previous.x && next.x <= state.pose.x );
			assert.ok( Math.abs( next.y - FROM.y ) < .01 && Math.abs( next.z - FROM.z ) < .01 );
			previous = next;
		}
		assert.ok( Math.abs( previous.x - state.pose.x ) < .01 );
		movement.clear();
	});
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
	assert.equal( state.movementPath.from.x, Math.fround( 100.8 ), "the logical walk remains rebased" );
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

test("a delayed receipt preserves the displayed origin across an unpublished worker advance", () => {
	const movement = createMovement( () => {} ), navigation = product(), presentation = createPosePresentation();
	navigation.objects = [];
	movement.seed( FROM );
	movement.navigation( 257, navigation );
	presentation.origin( 0 );
	publish( presentation, movement.state() );
	presentation.pose( 7, FROM, 0 );
	movement.request( TO, 0 );
	movement.step( 16 );
	movement.state();
	movement.receive(
		new TextEncoder().encode( JSON.stringify( {
			v: 1,
			id: 1,
			gid: 7,
			accepted: true,
			serverTimeMs: 300,
			world: { spawn: TO, moveSegment: { from: FROM, startedAtMs: 0, arrivesAtMs: 2000 } }
		} ) ),
		300,
		7
	);
	const state = movement.state();
	assert.ok( state.pose );
	publish( presentation, state );
	assert.equal( presentation.pose( 7, state.pose, .3 ).x, FROM.x );
	let previous = FROM.x;
	for ( let now = 316; now <= 800; now += 16 ) {
		movement.step( now );
		const next = movement.state();
		assert.ok( next.pose );
		publish( presentation, next );
		const shown = presentation.pose( 7, next.pose, now / 1000 );
		assert.ok( shown.x >= previous && shown.x - previous < 3.5, `recovery jumped ${shown.x - previous}` );
		previous = shown.x;
	}
	movement.clear();
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
			// Native stores each accepted source as float32: two 16 ms steps
			// followed by the 8 ms command-time step round independently.
			assert.equal( turn.outgoing.from.x, Math.fround( Math.fround( Math.fround( 100 + .8 ) + .8 ) + .4 ) );
			let sampled = 40;
			if ( coalescedReceipt ) {
				movement.step( sampled = 56 );
				const from = turn.outgoing.from, to = { ...FROM, ...destination };
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
				assert.deepEqual(
					transition.turn?.incoming,
					turn.incoming,
					"receipt retains the admitted incoming walk"
				);
				assert.deepEqual(
					transition.turn?.outgoing.from,
					turn.outgoing.from,
					"receipt retains the actual corner"
				);
				assert.ok( transition.turn );
				assert.ok(
					Math.hypot(
						transition.turn.outgoing.to.x - turn.outgoing.from.x,
						transition.turn.outgoing.to.z - turn.outgoing.from.z
					) < 6,
					"the outgoing proof ends at the validated lookahead, not the intent goal"
				);
			}
			for ( let time = 60; time <= 400; time += 4 ) {
				while ( sampled + 16 <= time ) movement.step( sampled += 16 );
				const state = movement.state();
				assert.ok( state.pose );
				publish( presentation, state );
				const shown = presentation.pose( 7, state.pose, time / 1000 );
				const corner = turn.outgoing.from, end = { ...FROM, ...destination };
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
