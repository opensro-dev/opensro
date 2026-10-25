/*
===========================================================================

presentation-clock.test.mjs - admitted leg timing across coalesced publications

The captured 32-peer first turn has no displayed input revision between idle
and its receipt. Replay the real clocks and positions, then isolate the main
frame gap from a worker stall that the user actually saw.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { product } from "../helpers/navigation-fixture.mjs";
const { createPosePresentation } = await import( "../../src/engine/runtime/characters/pose-presentation.ts" );
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const { createEntityMotion } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/motion/motion.ts"
);
const { poseDistance, sampleMovement } = await import( "../../src/engine/foundation/gameplay/native-movement.ts" );
const capture = JSON.parse(
	readFileSync( new URL( "../fixtures/coalesced-first-turn.json", import.meta.url ), "utf8" )
);
const START = { regionId: 257, x: 100, y: 0, z: 100, angle: 0 };
const END = { ...START, x: 200 };
const SPEED = 50;

/*
================
publish
================
*/
function publish( presentation, state ) {
	presentation.samples(
		new Map( [ [ 1, {
			atMs: state.poseAtMs,
			revision: state.movementRevision,
			moving: state.moving,
			...state.movementPath,
			transition: state.movementTransition
		} ] ] )
	);
}

test("the recorded first turn publishes its actual reconciled duration and avoids a short-frame catch-up", () => {
	const { receipt, path, stationary } = capture;
	const movement = createMovement( () => {} ), navigation = product( path.from.regionId );
	navigation.objects = [];
	movement.seed( stationary.logical );
	movement.navigation( path.from.regionId, navigation );
	movement.request( receipt.serverGoal, receipt.simulationAtMs - receipt.simulationRoundTripMs );
	movement.step( receipt.simulationAtMs );
	movement.receive(
		new TextEncoder().encode( JSON.stringify( {
			v: 1,
			id: 1,
			gid: 1,
			accepted: true,
			serverTimeMs: receipt.serverTimeMs,
			world: {
				spawn: receipt.serverGoal,
				moveSegment: {
					from: receipt.serverFrom,
					startedAtMs: receipt.serverDepartAtMs,
					arrivesAtMs: receipt.serverArriveAtMs
				}
			}
		} ) ),
		receipt.simulationAtMs,
		1
	);
	const state = movement.state();
	assert.ok( state.movementPath?.durationMs );
	assert.ok( poseDistance( state.movementPath.from, path.from ) < .05 );
	const expectedDuration = (receipt.serverArriveAtMs - receipt.serverTimeMs) *
		poseDistance( path.from, path.to ) / poseDistance( receipt.serverFrom, receipt.serverGoal );
	assert.equal( state.movementPath.durationMs, 100 );
	const publishedSpeed = poseDistance( state.movementPath.from, state.movementPath.to ) /
		state.movementPath.durationMs;
	assert.ok( Math.abs( publishedSpeed - poseDistance( path.from, path.to ) / expectedDuration ) < .0001 );
	const segment = { ...path, start: receipt.simulationAtMs, duration: expectedDuration };
	const presentation = createPosePresentation();
	presentation.origin( capture.originMs );
	presentation.samples(
		new Map( [ [ 1, {
			atMs: stationary.workerAtMs,
			revision: stationary.revision,
			moving: false,
			transition: stationary.transition
		} ] ] )
	);
	let previous = presentation.pose( 1, stationary.logical, stationary.atMs / 1000 );
	let previousAt = stationary.atMs, shortFrameExcess = 0, recordedExcess = 0;
	let recordedPrevious = stationary.displayed;
	for ( const [atMs, workerAtMs, x, y, z, angle, displayedX, displayedY, displayedZ] of capture.frames ) {
		const logical = { ...path.from, x, y, z, angle };
		assert.ok( poseDistance( sampleMovement( segment, workerAtMs ), logical ) < 1e-7 );
		presentation.samples(
			new Map( [ [ 1, {
				atMs: workerAtMs,
				revision: 10,
				moving: true,
				...path,
				durationMs: expectedDuration,
				transition: capture.transition
			} ] ] )
		);
		const drawn = presentation.pose( 1, logical, atMs / 1000 );
		assert.deepEqual( presentation.pose( 1, logical, atMs / 1000 ), drawn, "body and camera share one result" );
		const dtMs = atMs - previousAt;
		const recorded = { ...logical, x: displayedX, y: displayedY, z: displayedZ };
		if ( dtMs <= 1000 / 60 ) {
			shortFrameExcess = Math.max( shortFrameExcess, poseDistance( previous, drawn ) - dtMs * SPEED / 1000 );
			recordedExcess = Math.max(
				recordedExcess,
				poseDistance( recordedPrevious, recorded ) - dtMs * SPEED / 1000
			);
		}
		assert.equal( drawn.y, logical.y, "timing must not invent a terrain height" );
		previous = drawn;
		previousAt = atMs;
		recordedPrevious = recorded;
	}
	assert.ok( recordedExcess > .8, "the fixture retains the reported visible burst" );
	// Bound short-frame recovery to half one 4 ms walking step, rather than
	// accepting the captured burst of more than four such steps.
	assert.ok( shortFrameExcess < .1, `short-frame excess ${shortFrameExcess}` );
});

for ( const frameMs of [ 4, 1000 / 60 ] ) {
	test(`an admitted first sample advances at its actual speed with ${frameMs} ms frames`, () => {
		const movement = createMovement( () => {} ), navigation = product(), presentation = createPosePresentation();
		navigation.objects = [];
		movement.seed( START );
		movement.navigation( START.regionId, navigation );
		movement.request( END, 0 );
		const state = movement.state();
		assert.equal( state.movementPath?.durationMs, 100 );
		presentation.origin( 0 );
		publish( presentation, state );
		presentation.pose( 1, START, 0 );
		const shown = presentation.pose( 1, START, frameMs / 1000 );
		assert.ok( Math.abs( shown.x - START.x - SPEED * frameMs / 1000 ) < 1e-9 );
	});
}

/*
================
sample
================
*/
function sample( presentation, atMs, nowMs, durationMs = 2000, ownLeg = true ) {
	presentation.samples(
		new Map( [ [ 1, {
			ownLeg,
			atMs,
			revision: 1,
			moving: true,
			from: START,
			to: END,
			durationMs
		} ] ] )
	);
	return presentation.pose( 1, { ...START, x: START.x + atMs * SPEED / 1000 }, nowMs / 1000 );
}

test("a fresh sample bridges an unseen main-frame gap without manufacturing a worker pause", () => {
	// A peer leg on the sampled presenter (the local mover's own legs are
	// drawn by the interpolated presenter since #1564).
	const p = createPosePresentation();
	p.origin( 0 );
	sample( p, 0, 0, 2000, false );
	sample( p, 16, 90, 2000, false );
	const drawn = sample( p, 112, 150, 2000, false );
	assert.ok( Math.abs( drawn.x - 107.5 ) < 1e-9 );
});

test("a worker pause parks a peer walker at the prediction bound and resumes without catch-up", () => {
	// The sampled presenter bounds every track MAX_EXTRAPOLATION_SECONDS
	// past its newest sample; only the local mover's own legs advance
	// through a pause, drawn by the interpolated presenter (#1564).
	const p = createPosePresentation();
	p.origin( 0 );
	sample( p, 0, 0, 2000, false );
	sample( p, 16, 90, 2000, false );
	const paused = sample( p, 16, 130, 2000, false );
	assert.ok( Math.abs( paused.x - 105.8 ) < 1e-9, `parked through the pause (x ${paused.x})` );
	const resumed = sample( p, 112, 150, 2000, false );
	assert.ok( Math.abs( resumed.x - paused.x ) < 1e-9, "the fresh sample does not snap the parked display" );
	const next = sample( p, 128, 166, 2000, false );
	assert.ok( next.x > resumed.x, "recovery moves again" );
	assert.ok(
		next.x - resumed.x <= 1.5 * 16 * SPEED / 1000 + 1e-9,
		`no catch-up beyond the recovery cap (${(next.x - resumed.x).toFixed( 2 )})`
	);
});

test("a peer's pause still parks at the prediction bound", () => {
	// A peer's leg is the server's, which a stop or a knockback can cut.
	const p = createPosePresentation();
	p.origin( 0 );
	sample( p, 0, 0, 2000, false );
	sample( p, 16, 90, 2000, false );
	const parked = sample( p, 16, 130, 2000, false );
	assert.ok( Math.abs( parked.x - 105.8 ) < 1e-9, `parked (x ${parked.x})` );
});

test("a lag replay never draws the walker faster than it walks, backwards or past its leg", () => {
	// Frame hitches (a stalled main thread) and publication gaps (a stalled
	// worker, a round-trip spike), as the owner's 400 ms ping with CPU lag.
	const p = createPosePresentation();
	p.origin( 0 );
	const frames = [ 4, 16, 33, 61, 250, 9, 16, 48, 3, 25, 70, 400, 16, 16 ];
	const publishEvery = [ 16, 16, 16, 300, 16, 32, 48, 16, 450, 16 ];
	let now = 0, worker = 0, publish = 0, previous;
	for ( let frame = 0; now < 2400; frame++ ) {
		now += frames[frame % frames.length];
		while ( worker + publishEvery[publish % publishEvery.length] <= now ) {
			worker += publishEvery[publish++ % publishEvery.length];
		}
		const drawn = sample( p, Math.min( worker, 2000 ), now );
		if ( previous ) {
			const step = drawn.x - previous.x, budget = SPEED * (now - previous.at) / 1000;
			assert.ok( step >= -1e-9, `no backward step at ${now} ms (${step})` );
			assert.ok( step <= budget + 1e-6, `no faster than walking at ${now} ms (${step} > ${budget})` );
		}
		assert.ok( drawn.x <= END.x + 1e-9, `never past the leg's end (x ${drawn.x})` );
		previous = { x: drawn.x, at: now };
	}
	assert.ok( previous && Math.abs( previous.x - END.x ) < 1e-9, "arrives at the leg's end" );
});

for ( const durationMs of [ 1000, 4000, Infinity ] ) {
	test(`an unseen frame gap adds no recovery to a same-revision retime of ${durationMs} ms`, () => {
		const uninterrupted = createPosePresentation(), delayed = createPosePresentation();
		for ( const p of [ uninterrupted, delayed ] ) {
			p.origin( 0 );
			sample( p, 0, 0 );
			sample( p, 16, 90 );
		}
		// Both receive the same retime at 150 ms. Only the reference got a
		// display opportunity and its preceding publication at 112 ms.
		sample( uninterrupted, 112, 112 );
		for ( const nowMs of [ 150, 166, 182, 198 ] ) {
			assert.deepEqual(
				sample( delayed, 112, nowMs, durationMs ),
				sample( uninterrupted, 112, nowMs, durationMs )
			);
		}
	});
}

for ( const durationMs of [ 1000, 4000, Infinity ] ) {
	test(`a duration-only change to ${durationMs} ms retires stale sampled velocity`, () => {
		const p = createPosePresentation();
		p.origin( 0 );
		sample( p, 0, 0, 2000, false );
		sample( p, 16, 16, 2000, false );
		const before = sample( p, 16, 16, durationMs, false );
		assert.ok( Math.abs( before.x - 100.8 ) < 1e-9 );
		const after = sample( p, 16, 80, durationMs, false );
		assert.ok( Math.abs( after.x - before.x - 100 * 64 / durationMs ) < 1e-9 );
	});
}

for ( const frameMs of [ 4, 1000 / 60, 37.6 ] ) {
	test(`a receipt that keeps the logical walk cannot park a ${frameMs} ms display interval`, () => {
		const p = createPosePresentation();
		p.origin( 0 );
		sample( p, 0, 0, 2000, false );
		sample( p, 16, 20, 2000, false );
		const receiptAt = 20 + frameMs;
		const from = { ...START, x: START.x + receiptAt * SPEED / 1000 };
		const durationMs = (END.x - from.x) / SPEED * 1000;
		for ( const elapsed of [ 0, 4, 16, 32, 64, 128 ] ) {
			const atMs = receiptAt + elapsed;
			const target = { ...START, x: START.x + atMs * SPEED / 1000 };
			p.samples(
				new Map( [ [ 1, {
					atMs,
					revision: 2,
					moving: true,
					from,
					to: END,
					durationMs,
					transition: {
						relocation: 0,
						reason: "receipt",
						eligible: true,
						pathEligible: true,
						logicalDistance: 0,
						previousPath: { from: START, to: END },
						corridor: { from, to: from }
					}
				} ] ] )
			);
			const shown = p.pose( 1, target, atMs / 1000 );
			assert.ok( Math.abs( shown.x - target.x ) < 1e-9, "a zero-distance receipt adds no visual debt" );
			assert.deepEqual( p.pose( 1, target, atMs / 1000 ), shown, "body and camera share the same frame" );
		}
	});
}

test("the recorded retained-walk receipt does not create a half-unit catch-up step", () => {
	const recorded = JSON.parse(
		readFileSync( new URL( "../fixtures/retained-walk-receipt.json", import.meta.url ), "utf8" )
	);
	const p = createPosePresentation();
	p.origin( recorded.originMs );
	let previous, recordedPrevious, previousAt;
	let maximumExcess = 0, recordedExcess = 0;
	for ( const [atMs, workerAtMs, revision, x, y, z, angle, shownX, shownY, shownZ] of recorded.frames ) {
		const state = recorded.states[revision];
		const logical = { regionId: recorded.regionId, x, y, z, angle };
		p.samples(
			new Map( [ [ 1, {
				atMs: workerAtMs,
				revision,
				moving: state.moving,
				transition: state.transition,
				...state.path
			} ] ] )
		);
		const drawn = p.pose( 1, logical, atMs / 1000 );
		const oldDrawn = { ...logical, x: shownX, y: shownY, z: shownZ };
		if ( previous && recordedPrevious && previousAt !== undefined ) {
			const budget = (atMs - previousAt) * SPEED / 1000;
			maximumExcess = Math.max( maximumExcess, poseDistance( previous, drawn ) - budget );
			recordedExcess = Math.max( recordedExcess, poseDistance( recordedPrevious, oldDrawn ) - budget );
		}
		assert.deepEqual( p.pose( 1, logical, atMs / 1000 ), drawn );
		assert.equal( drawn.y, logical.y );
		previous = drawn;
		recordedPrevious = oldDrawn;
		previousAt = atMs;
	}
	assert.ok( recordedExcess > .5, "the capture retains the live continuity failure" );
	assert.ok( maximumExcess < .5, `retained-walk catch-up ${maximumExcess}` );
});

test("first-sample timing caps the endpoint and never interpolates a distant terrain height", () => {
	const p = createPosePresentation();
	p.origin( 0 );
	p.samples(
		new Map( [ [ 1, {
			atMs: 0,
			revision: 1,
			moving: true,
			from: START,
			to: { ...END, x: 101, y: 100 },
			durationMs: 10
		} ] ] )
	);
	p.pose( 1, START, 0 );
	const shown = p.pose( 1, START, .05 );
	assert.equal( shown.x, 101 );
	assert.equal( shown.y, 0 );
});

test("a sloped first leg acquires its vertical tangent from sampled ground without an endpoint-height jump", () => {
	const p = createPosePresentation(), to = { ...END, y: 100 };
	p.origin( 0 );
	p.samples( new Map( [ [ 1, { atMs: 0, revision: 1, moving: true, from: START, to, durationMs: 2000 } ] ] ) );
	p.pose( 1, START, 0 );
	assert.equal( p.pose( 1, START, .008 ).y, 0 );
	// The actual ground climbs .2 units over the first 16 ms. Its distant
	// destination is much higher; that height must not define this tangent.
	const ground = { ...START, x: 100.8, y: .2 };
	p.samples( new Map( [ [ 1, { atMs: 16, revision: 1, moving: true, from: START, to, durationMs: 2000 } ] ] ) );
	const first = p.pose( 1, ground, .024 );
	assert.ok( Math.abs( first.y ) < 1e-9, "the first received height preserves displayed continuity" );
	const next = p.pose( 1, ground, .032 );
	assert.ok( next.y > first.y && next.y <= .4, "height recovery stays inside the sampled tangent" );
	assert.ok( Math.abs( next.x - 101.6 ) < 1e-9, "vertical recovery does not change admitted horizontal speed" );
});

test("remote publications carry the active clock through speed changes and a zero-speed hold", () => {
	const motion = createEntityMotion( undefined, ( _from, to ) => to );
	/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
	const entity = {
		...START,
		gid: 1,
		refObjId: 1907,
		name: "peer",
		kind: "player",
		heading: 0,
		movementMode: 3,
		walkSpeed: 20,
		runSpeed: SPEED,
		spawnDestination: END
	};
	motion.spawn( entity, 0 );
	const first = motion.step( 0 )[0];
	assert.equal( first.movementPath?.durationMs, 100 );
	assert.equal( first.movementPath?.to.x, 105 );
	const faster = { ...entity, runSpeed: 100 };
	motion.speeds( entity, faster, 100 );
	const retimed = motion.step( 100 )[0];
	assert.equal( retimed.movementPath?.from.x, 105 );
	assert.equal( retimed.movementPath?.durationMs, 100 );
	assert.equal( retimed.movementPath?.to.x, 115 );
	const held = { ...faster, runSpeed: 0 };
	motion.speeds( faster, held, 200 );
	const stopped = motion.step( 200 )[0];
	assert.equal( stopped.movementPath?.from.x, 115 );
	assert.equal( stopped.movementPath?.durationMs, 100 );
	assert.equal( stopped.movementPath?.to.x, stopped.x );
	assert.equal( motion.step( 400 )[0].x, stopped.x );
});

test("sustaining worker clock debt past the 100 ms lookahead corridor does not freeze and sprint presentation between worker wakes", () => {
	const p = createPosePresentation();
	p.origin( 0 );
	const debtMs = 300;
	let previousX = START.x, previousAt = debtMs;
	for ( let tick = 0; tick < 10; tick++ ) {
		const workerAtMs = tick * 16;
		const publishWallMs = workerAtMs + debtMs;
		const from = { ...START, x: START.x + workerAtMs * SPEED / 1000 };
		const to = { ...START, x: from.x + 100 * SPEED / 1000 };
		p.samples(
			new Map( [ [ 1, {
				atMs: workerAtMs,
				revision: 1,
				moving: true,
				from,
				to,
				durationMs: 100
			} ] ] )
		);
		for ( const displayOffset of [ 0, 8 ] ) {
			const nowMs = publishWallMs + displayOffset;
			const shown = p.pose( 1, from, nowMs / 1000 );
			if ( nowMs > previousAt ) {
				const frameStep = shown.x - previousX;
				const expectedStep = (nowMs - previousAt) * SPEED / 1000;
				assert.ok(
					frameStep > 0.1,
					`frame at ${nowMs} ms must advance instead of clamping at 100 ms lookahead (${frameStep})`
				);
				assert.ok(
					frameStep <= expectedStep * 1.05 + 1e-6,
					`frame at ${nowMs} ms must not sprint faster than walking speed (${frameStep} vs ${expectedStep})`
				);
			}
			previousX = shown.x;
			previousAt = nowMs;
		}
	}
});
