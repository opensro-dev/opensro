/*
===========================================================================

own-walk-interpolation.test.mjs - the local mover drawn between its ticks

The shape of the owner's camera-shake recording (2026-10-24): a 100 u/s
walk simulated on 16 ms worker ticks, seen by the main thread 8-40 ms
late at uneven frame intervals. A start from rest and a redirect must
never draw the body faster than it walks; the previous dead-reckoned
presenter overshot the redirect and caught up at 1.5 times walking speed.
Loads the shipped modules through the shared native loader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createPosePresentation } = await import( "../../src/engine/runtime/characters/pose-presentation.ts" );

const GID = 7, REGION = 24998, SPEED = 100, STEP_MS = 16;
// Frame intervals from the recording (ms), cycled.
const FRAMES = [ 16.8, 24.9, 33.4, 29.1, 12.6, 29.2, 8.4, 25.1, 33.4, 20.9, 29.3, 12.5 ];
// Publication latency per frame (ms), cycled: the worker's newest tick is
// this much older than the frame (the recording jitters 0-45 ms).
const LATENCY = [ 8, 24, 40, 16, 32, 12, 28 ];
// The worker publishes every second tick (the recording's 32 ms steps).
const PUBLISH_MS = 32;
// movement.ts PRESENTATION_LOOKAHEAD_MS: the window a publication's path spans.
const LOOKAHEAD_MS = 100;
// How long a new leg's publications stay unadmitted in the windowed run.
const UNADMITTED_MS = 48;
const START_MS = 1000, REDIRECT_MS = 1512, ARRIVE_MS = REDIRECT_MS + 1200, SECOND_MS = 4300, END_MS = 6600;
const P0 = { regionId: REGION, x: 800, y: 60, z: 900, angle: 0 };
const LEG_A = { from: P0, to: { ...P0, x: P0.x + 300 } };
const CORNER = legAt( LEG_A, START_MS, REDIRECT_MS );
const LEG_B = { from: CORNER, to: { ...CORNER, z: CORNER.z + 120 } };
const LEG_C = { from: LEG_B.to, to: { ...LEG_B.to, x: LEG_B.to.x - 200 } };

/*
================
legAt
================
*/
function legAt( leg, startMs, ms ) {
	const length = Math.hypot( leg.to.x - leg.from.x, leg.to.z - leg.from.z );
	const t = Math.max( 0, Math.min( 1, (ms - startMs) * SPEED / 1000 / length ) );
	return {
		...leg.from,
		x: leg.from.x + (leg.to.x - leg.from.x) * t,
		z: leg.from.z + (leg.to.z - leg.from.z) * t
	};
}

/*
================
worker

The worker's publication for a tick: standing at P0 (its tick frozen, as
the recording's is), leg A from START_MS, leg B from the corner at
REDIRECT_MS, standing at B's end (frozen at the arrival tick) through a
long rest, then leg C from SECOND_MS.
================
*/
function worker( tickMs ) {
	if ( tickMs < START_MS ) return { atMs: 0, pose: P0, moving: false, revision: 1 };
	if ( tickMs < REDIRECT_MS ) {
		return {
			atMs: tickMs,
			pose: legAt( LEG_A, START_MS, tickMs ),
			moving: true,
			revision: 2,
			from: LEG_A.from,
			to: LEG_A.to,
			durationMs: 3000
		};
	}
	if ( tickMs < ARRIVE_MS ) {
		return {
			atMs: tickMs,
			pose: legAt( LEG_B, REDIRECT_MS, tickMs ),
			moving: true,
			revision: 3,
			from: LEG_B.from,
			to: LEG_B.to,
			durationMs: 1200
		};
	}
	if ( tickMs < SECOND_MS ) return { atMs: ARRIVE_MS, pose: LEG_B.to, moving: false, revision: 4 };
	return {
		atMs: tickMs,
		pose: legAt( LEG_C, SECOND_MS, tickMs ),
		moving: true,
		revision: 5,
		from: LEG_C.from,
		to: LEG_C.to,
		durationMs: 2000
	};
}

/*
================
windowed

The same publication as the worker publishes it (movement.ts
presentationPath): from the tick's own pose to where the walk will be
PRESENTATION_LOOKAHEAD_MS later, timed by that window, not by the whole
leg.
================
*/
function windowed( tickMs ) {
	const state = worker( tickMs );
	if ( !state.moving ) return state;
	const [leg, startMs] = tickMs < REDIRECT_MS ?
		[ LEG_A, START_MS ] :
		tickMs < ARRIVE_MS ?
		[ LEG_B, REDIRECT_MS ] :
		[ LEG_C, SECOND_MS ];
	const length = Math.hypot( leg.to.x - leg.from.x, leg.to.z - leg.from.z );
	const remainingMs = Math.max( 0, startMs + length / SPEED * 1000 - tickMs );
	const lookahead = Math.min( LOOKAHEAD_MS, remainingMs );
	// A new leg's first publications come before the worker admits its
	// segment (movement.ts presentationPath, pathEligible false):
	// presentation-samples sends them as not moving, with no path start,
	// although the pose already advances.
	if ( tickMs - startMs < UNADMITTED_MS ) {
		return { ...state, moving: false, to: legAt( leg, startMs, tickMs + lookahead ), durationMs: lookahead };
	}
	return {
		...state,
		from: state.pose,
		to: legAt( leg, startMs, tickMs + lookahead ),
		durationMs: lookahead
	};
}

/*
================
run

Draws the scenario frame by frame; returns [frame seconds, drawn pose].
skewMs is how far the frame clock runs ahead of the worker clock (the
recording's is about 2 s; a negative one runs behind).
================
*/
function run( skewMs = 0, publish = worker ) {
	const presentation = createPosePresentation();
	presentation.origin( skewMs );
	/** @type {[number, import("../../src/engine/contracts/gameplay").Pose][]} */
	const drawn = [];
	let frame = 0, workerMs = 500;
	while ( workerMs < END_MS ) {
		const visible = workerMs - LATENCY[frame % LATENCY.length];
		const state = publish( Math.floor( visible / PUBLISH_MS ) * PUBLISH_MS );
		presentation.samples(
			new Map( [ [ GID, {
				ownLeg: true,
				atMs: state.atMs,
				revision: state.revision,
				moving: state.moving,
				...(state.from ? { from: state.from, to: state.to, durationMs: state.durationMs } : {})
			} ] ] )
		);
		const now = (workerMs + skewMs) / 1000;
		drawn.push( [ now, presentation.pose( GID, { ...state.pose }, now ) ] );
		workerMs += FRAMES[frame % FRAMES.length];
		frame++;
	}
	return drawn;
}

/*
================
speeds

Drawn speed (units per second) of every frame.
================
*/
function speeds( drawn ) {
	return drawn.slice( 1 ).map( ( [t, p], i ) => {
		const [t0, q] = drawn[i];
		return Math.hypot( p.x - q.x, p.z - q.z ) / (t - t0);
	} );
}

test("the worker's own 100 ms path windows draw no faster than the walk either", () => {
	for ( const skew of [ 0, 2037, -30 ] ) {
		const peak = Math.max( ...speeds( run( skew, windowed ) ) );
		assert.ok( peak <= SPEED * 1.05, `skew ${skew}: peak ${peak.toFixed( 1 )} u/s against ${SPEED}` );
	}
});

test("starts, a redirect and a start after a long rest never draw the body faster than it walks", () => {
	// Clocks in step, the recording's 2 s frame-clock lead, and a worker
	// clock running ahead.
	for ( const skew of [ 0, 2037, -30 ] ) {
		const fast = speeds( run( skew ) ).filter( speed => speed > SPEED * (1 + 1e-6) );
		assert.deepEqual(
			fast,
			[],
			`skew ${skew}: frames above walking speed: ${fast.map( s => s.toFixed( 1 ) ).join( ", " )}`
		);
	}
});

test("the drawn walk turns the redirect's own corner and stops where the worker stopped", () => {
	const drawn = run();
	// A publication later than any the delay has yet seen can let the body
	// pass a corner before the turn exists. It never passes by more than one
	// publication step of walking, and the late-turn rule (speed test above)
	// brings it back within walking speed.
	const beyond = Math.max( ...drawn.filter( ( [t] ) => t * 1000 < SECOND_MS ).map( ( [, p] ) => p.x ) ) - CORNER.x;
	assert.ok( beyond <= SPEED * PUBLISH_MS / 1000, `ran ${beyond.toFixed( 3 )} units past the corner` );
	const [, rested] = defined( drawn.filter( ( [t] ) => t * 1000 < SECOND_MS ).at( -1 ) );
	assert.ok( Math.hypot( rested.x - LEG_B.to.x, rested.z - LEG_B.to.z ) < 1e-6, "rests at B's end" );
	const [, last] = defined( drawn.at( -1 ) );
	assert.ok( Math.hypot( last.x - LEG_C.to.x, last.z - LEG_C.to.z ) < 1e-6, "arrives at C's end" );
});

test("each walk leaves rest within one delay of its click", () => {
	const drawn = run();
	for ( const [startMs, rest] of [ [ START_MS, P0 ], [ SECOND_MS, LEG_C.from ] ] ) {
		const left = drawn.find( ( [t, p] ) => t * 1000 >= startMs && Math.hypot( p.x - rest.x, p.z - rest.z ) > 1e-6 );
		assert.ok( left, "the body left rest" );
		// The delay covers the largest latency (40 ms) and the 32 ms
		// publication step, plus one frame.
		const late = left[0] * 1000 - startMs;
		assert.ok( late <= 40 + PUBLISH_MS + 34, `left rest ${late.toFixed( 1 )} ms late` );
	}
});

/*
================
straight

A presenter on a straight 100 u/s leg from P0 sampled every 16 ms with a
fixed 20 ms latency; returns a frame function drawing at worker ms.
================
*/
function straight() {
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	const leg = { from: P0, to: { ...P0, x: P0.x + 1000 } };
	/*
	================
	frame
	================
	*/
	const frame = ( ms, change = {} ) => {
		const tick = Math.floor( (ms - 20) / STEP_MS ) * STEP_MS;
		const input = {
			ownLeg: true,
			atMs: tick,
			revision: 1,
			moving: true,
			from: leg.from,
			to: leg.to,
			durationMs: 10_000,
			...change.input
		};
		presentation.samples( new Map( [ [ GID, input ] ] ) );
		return presentation.pose( GID, change.pose ?? legAt( leg, 0, tick ), ms / 1000 );
	};
	return { presentation, frame, leg };
}

test("a small server correction glides back over about 0.1 s; a teleport snaps", () => {
	const { frame, leg } = straight();
	let ms = 36, drawn = frame( ms );
	for ( ms += 16; ms < 600; ms += 16 ) drawn = frame( ms );
	// The server stops the walk 3 units behind where the leg had it.
	const tick = Math.floor( (ms - 20) / STEP_MS ) * STEP_MS;
	const stopped = { ...legAt( leg, 0, tick ), x: legAt( leg, 0, tick ).x - 3 };
	const correction = {
		input: { atMs: tick, revision: 2, moving: false, from: undefined, to: undefined },
		pose: stopped
	};
	const first = frame( ms, correction );
	assert.ok( Math.abs( first.x - drawn.x ) < 1.7, `jumped ${(first.x - drawn.x).toFixed( 2 )} in one frame` );
	for ( let i = 0; i < 20; i++ ) frame( ms += 16, correction );
	assert.ok( Math.abs( frame( ms += 16, correction ).x - stopped.x ) < 1e-6, "settles on the server pose" );
	// A relocation far beyond the smoothing range lands at once.
	const far = { ...stopped, x: stopped.x + 500 };
	const teleport = { input: { ...correction.input, atMs: tick + 400, revision: 3 }, pose: far };
	frame( ms += 16, teleport );
	assert.ok( Math.abs( frame( ms += 16, teleport ).x - far.x ) < 1e-6, "a teleport snaps" );
});

test("a correction while walking never draws a frame faster than the walk (A5's 1.05 bar)", () => {
	const { frame, leg } = straight();
	let ms = 36, previous = frame( ms );
	for ( ms += 16; ms < 600; ms += 16 ) previous = frame( ms );
	// The server moves the walk 3 units sideways and walks on from there.
	const tick = Math.floor( (ms - 20) / STEP_MS ) * STEP_MS;
	const from = { ...legAt( leg, 0, tick ), z: legAt( leg, 0, tick ).z + 3 };
	const moved = { from, to: { ...from, x: from.x + 1000 } };
	let peak = 0, settledAt;
	for ( let i = 0; i < 120; i++, ms += FRAMES[i % FRAMES.length] ) {
		const now = Math.floor( (ms - 20) / STEP_MS ) * STEP_MS;
		const drawn = frame( ms, {
			input: { atMs: now, revision: 2, from: moved.from, to: moved.to, durationMs: 10_000 },
			pose: legAt( moved, tick, now )
		} );
		const dt = FRAMES[i % FRAMES.length] / 1000;
		if ( i ) peak = Math.max( peak, Math.hypot( drawn.x - previous.x, drawn.z - previous.z ) / dt );
		if ( settledAt === undefined && Math.abs( drawn.z - from.z ) < 1e-3 ) settledAt = i;
		previous = drawn;
	}
	assert.ok( peak <= SPEED * 1.05, `peak ${peak.toFixed( 1 )} u/s against ${SPEED}` );
	// The offset still drains: within the 120 frames (about 2.7 s).
	assert.ok( settledAt !== undefined, "the sideways offset settled" );
});

test("the main-thread mover's walk is drawn while it predicts and handed back without a jump (#1440)", () => {
	const { presentation, frame, leg } = straight();
	let ms = 36, previous = frame( ms );
	for ( ms += 16; ms < 400; ms += 16 ) previous = frame( ms );
	// The mover predicts a right-angle walk from the drawn pose.
	const start = presentation.drawn( GID );
	assert.deepEqual( start, previous );
	let mover = start, peakTurn = 0, shownMs = ms - 16;
	for ( let i = 0; i < 10; i++ ) {
		mover = { ...mover, z: mover.z + SPEED * .016, angle: 16384 };
		presentation.ownMover( GID, { kind: "walking", pose: mover } );
		const drawn = frame( ms += 16 );
		assert.equal( drawn.x, mover.x );
		assert.equal( drawn.z, mover.z );
		const turned = Math.abs( ((drawn.angle - previous.angle + 98304) % 65536) - 32768 );
		peakTurn = Math.max( peakTurn, turned / ((ms - shownMs) / 1000) );
		previous = drawn;
		shownMs = ms;
	}
	assert.equal( previous.angle, 16384, "the turn completes" );
	// The yaw turns toward the mover's heading at the native rate (86CBA0).
	assert.ok( peakTurn <= 98304 * 1.01, `turned ${peakTurn.toFixed( 0 )} words/s` );
	// The prediction ends: the presenter recovers from the mover's last pose.
	presentation.ownMover( GID, { kind: "ended", pose: mover } );
	let peak = 0;
	for ( let i = 0; i < 40; i++ ) {
		const drawn = frame( ms += 16 );
		peak = Math.max( peak, Math.hypot( drawn.x - previous.x, drawn.z - previous.z ) / .016 );
		previous = drawn;
		presentation.ownMover( GID, { kind: "idle" } );
	}
	assert.ok( peak <= SPEED * 1.05, `handback peak ${peak.toFixed( 1 )} u/s against ${SPEED}` );
	assert.ok( Math.abs( previous.x - legAt( leg, 0, Math.floor( (ms - 20) / STEP_MS ) * STEP_MS ).x ) < 30 );
});

test("a rider's predicted walk moves the mount it rides, and both are handed back without a jump (#1566)", () => {
	// 0x85E000: while riding, the mount is the mover; both are sampled from
	// the local movement owner's walk (presentation-samples.ts).
	const MOUNT = 9;
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	const leg = { from: P0, to: { ...P0, x: P0.x + 1000 } };
	const both = ms => {
		const tick = Math.floor( (ms - 20) / STEP_MS ) * STEP_MS;
		const input = { ownLeg: true, atMs: tick, revision: 1, moving: true, ...leg, durationMs: 10_000 };
		presentation.samples( new Map( [ [ GID, input ], [ MOUNT, input ] ] ) );
		const target = legAt( leg, 0, tick );
		return {
			rider: presentation.pose( GID, target, ms / 1000 ),
			mount: presentation.pose( MOUNT, target, ms / 1000 )
		};
	};
	let ms = 36, previous = both( ms );
	for ( ms += 16; ms < 400; ms += 16 ) previous = both( ms );
	let mover = defined( presentation.drawn( GID ) );
	for ( let i = 0; i < 10; i++ ) {
		mover = { ...mover, z: mover.z + SPEED * .016 };
		presentation.ownMover( GID, { kind: "walking", pose: mover }, MOUNT );
		previous = both( ms += 16 );
		assert.equal( previous.mount.z, mover.z, "the mount walks the prediction" );
		assert.equal( previous.rider.z, mover.z );
	}
	presentation.ownMover( GID, { kind: "ended", pose: mover }, MOUNT );
	let peak = 0;
	for ( let i = 0; i < 40; i++ ) {
		const drawn = both( ms += 16 );
		for ( const key of [ "rider", "mount" ] ) {
			const a = previous[key], b = drawn[key];
			peak = Math.max( peak, Math.hypot( b.x - a.x, b.z - a.z ) / .016 );
		}
		previous = drawn;
		presentation.ownMover( GID, { kind: "idle" } );
	}
	assert.ok( peak <= SPEED * 1.05, `handback peak ${peak.toFixed( 1 )} u/s against ${SPEED}` );
});

test("a base walk pushed ahead (a frame-clock jump) is still drawn no faster than the walk", () => {
	const { presentation, frame } = straight();
	let ms = 36, previous = frame( ms ), peak = 0;
	for ( ms += 16; ms < 1200; ms += 16 ) {
		// The frame clock's origin moves 40 ms mid-walk (a clock resync): the
		// buffered walk's render time jumps ahead within one frame.
		if ( ms === 596 ) presentation.origin( -40 );
		const drawn = frame( ms );
		peak = Math.max( peak, Math.hypot( drawn.x - previous.x, drawn.z - previous.z ) / .016 );
		previous = drawn;
	}
	assert.ok( peak <= SPEED * 1.05, `peak ${peak.toFixed( 1 )} u/s against ${SPEED}` );
});

test("a worker stall keeps the walk on its leg at walking speed, with no catch-up after it", () => {
	const { presentation, frame, leg } = straight();
	let ms = 36, previous = frame( ms ), peak = 0;
	for ( ms = 52; ms < 1200; ms += 16 ) {
		// No new tick is published between 500 and 700 ms.
		const stalled = ms >= 500 && ms < 700;
		const drawn = stalled ?
			(presentation.samples(
				new Map( [ [ GID, {
					ownLeg: true,
					atMs: 464,
					revision: 1,
					moving: true,
					from: leg.from,
					to: leg.to,
					durationMs: 10_000
				} ] ] )
			),
				presentation.pose( GID, legAt( leg, 0, 464 ), ms / 1000 )) :
			frame( ms );
		peak = Math.max( peak, (drawn.x - previous.x) / .016 );
		previous = drawn;
	}
	assert.ok( peak <= SPEED * (1 + 1e-6), `peaked at ${peak.toFixed( 1 )} u/s` );
});

test("handing over to a skill displacement and back never jumps", () => {
	const { presentation, frame, leg } = straight();
	let ms = 36, previous = frame( ms );
	for ( ms += 16; ms < 400; ms += 16 ) previous = frame( ms );
	const tick = Math.floor( (ms - 20) / STEP_MS ) * STEP_MS;
	// A dash: the generic presenter takes the frame and recovers from the
	// last interpolated pose rather than jumping to the dash's sample.
	const dash = {
		ownLeg: true,
		atMs: tick,
		revision: 2,
		moving: true,
		displacement: true,
		startedAtMs: tick,
		from: legAt( leg, 0, tick ),
		to: { ...legAt( leg, 0, tick ), x: legAt( leg, 0, tick ).x + 40 },
		durationMs: 400
	};
	presentation.samples( new Map( [ [ GID, dash ] ] ) );
	const handed = presentation.pose( GID, legAt( leg, 0, tick ), (ms += 16) / 1000 );
	assert.ok(
		Math.abs( handed.x - previous.x ) < SPEED * .016 + 1e-6,
		`jumped ${(handed.x - previous.x).toFixed( 2 )}`
	);
	// Back on the walk: the interpolated presenter picks up from the drawn pose.
	const before = presentation.pose( GID, legAt( leg, 0, tick ), (ms += 16) / 1000 );
	const back = frame( ms += 16 );
	assert.ok( Math.abs( back.x - before.x ) < 3, `jumped ${(back.x - before.x).toFixed( 2 )} on return` );
});

test("a second pose() in the frame the prediction ends hands rider and mount back only once (#1566)", () => {
	const MOUNT = 9;
	/*
	================
	handback

	Rider and mount across the end of a prediction, asking each pose once
	or twice in the ended frame.
	================
	*/
	const handback = twice => {
		const presentation = createPosePresentation();
		presentation.origin( 0 );
		const leg = { from: P0, to: { ...P0, x: P0.x + 1000 } };
		const both = ( ms, repeat = false ) => {
			const tick = Math.floor( (ms - 20) / STEP_MS ) * STEP_MS;
			const input = { ownLeg: true, atMs: tick, revision: 1, moving: true, ...leg, durationMs: 10_000 };
			presentation.samples( new Map( [ [ GID, input ], [ MOUNT, input ] ] ) );
			const target = legAt( leg, 0, tick );
			const poses = [
				presentation.pose( GID, target, ms / 1000 ),
				presentation.pose( MOUNT, target, ms / 1000 )
			];
			if ( repeat ) {
				presentation.pose( GID, target, ms / 1000 );
				presentation.pose( MOUNT, target, ms / 1000 );
			}
			return poses.map( pose => [ pose.x, pose.z ] );
		};
		let ms = 36;
		for ( ; ms < 400; ms += 16 ) both( ms );
		const mover = { ...defined( presentation.drawn( GID ) ), z: P0.z + 30 };
		presentation.ownMover( GID, { kind: "walking", pose: mover }, MOUNT );
		both( ms += 16 );
		presentation.ownMover( GID, { kind: "ended", pose: mover }, MOUNT );
		const frames = [ both( ms += 16, twice ) ];
		presentation.ownMover( GID, { kind: "idle" } );
		for ( let i = 0; i < 5; i++ ) frames.push( both( ms += 16 ) );
		return frames;
	};
	assert.deepEqual( handback( true ), handback( false ) );
});
