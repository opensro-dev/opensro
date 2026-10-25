/*
===========================================================================

local-mover-reconcile.test.mjs - the main-thread mover's reconciliation
(GH #1441b)

A small worker stands in for the simulation worker: it takes each click
after an inbound latency, walks it from its own pose in fixed 16 ms ticks,
and publishes its state every 32 ms after an outbound latency, on a worker
clock offset from the main frame clock. Scenarios add what the server
does mid-walk: a stop, a knockback, a teleport, a speed change, a refused
click, a drifting clock. Every drawn frame is held to the shared step cap
(1.04x walking speed times the frame time, drawn-step.ts), and the drawn
body must end where the worker ends.

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
// The heading word of a walk from a to b (native-movement movementHeading's
// convention does not matter here: the flat view ignores headings).
const HEADING = 0;

/*
================
flatView

A navigation view with no obstacles whose pick answers the point a test
query carries in its ray (query.ray.target).
================
*/
function flatView() {
	return {
		available: () => true,
		block: () => 0,
		floor: () => undefined,
		pick: query => ({ ...query.ray.target }),
		clip: ( _from, to, query ) => {
			if ( query ) query.status = 0;
			return to;
		},
		surface: pose => pose
	};
}

/*
================
query
================
*/
function query( x, z ) {
	return /** @type {any} */ ({
		originRegion: REGION,
		ray: { target: { regionId: REGION, x, y: 0, z } },
		terrainDepth: null
	});
}

/*
================
createWorker

The simulation worker, reduced to its walk: a pose, a goal, a speed, and
the publications it makes.
================
*/
function createWorker( origin, outLatencyMs = OUT_LATENCY_MS ) {
	/** @type {{ pose: Pose, goal: Pose | undefined, speed: number, inputSeq: number, revision: number, atMs: number }} */
	const state = {
		pose: { ...START },
		goal: undefined,
		speed: SPEED,
		inputSeq: 0,
		revision: 0,
		atMs: 0
	};
	const inbox = [], published = [];
	return {
		state,
		// A click arrives IN_LATENCY_MS after it was made on the main clock.
		send( seq, destination, mainMs, refuse = false ) {
			inbox.push( { seq, destination, atMs: mainMs - origin( mainMs ) + IN_LATENCY_MS, refuse } );
		},
		// Runs the worker to worker time ms in fixed ticks, publishing as it goes.
		run( ms, events = [] ) {
			while ( state.atMs + TICK_MS <= ms ) {
				state.atMs += TICK_MS;
				for ( const input of inbox.filter( i => i.atMs <= state.atMs ) ) {
					inbox.splice( inbox.indexOf( input ), 1 );
					state.inputSeq = Math.max( state.inputSeq, input.seq );
					if ( input.refuse ) continue;
					state.goal = {
						...input.destination,
						x: Math.trunc( input.destination.x ),
						z: Math.trunc( input.destination.z )
					};
					state.revision++;
				}
				for ( const event of events.filter( e => e.atMs === state.atMs ) ) event.apply( state );
				if ( state.goal ) {
					const dx = state.goal.x - state.pose.x, dz = state.goal.z - state.pose.z;
					const left = Math.hypot( dx, dz ), step = state.speed * TICK_MS / 1000;
					if ( left <= step ) {
						state.pose = { ...state.goal };
						state.goal = undefined;
						state.revision++;
					} else {state.pose = {
							...state.pose,
							x: state.pose.x + dx / left * step,
							z: state.pose.z + dz / left * step
						};}
				}
				if ( state.atMs % PUBLISH_MS === 0 ) {
					published.push( {
						visibleAtMain: state.atMs + origin( state.atMs ) + outLatencyMs,
						world: {
							pose: { ...state.pose, angle: HEADING },
							poseAtMs: state.atMs,
							revision: state.revision,
							moving: !!state.goal,
							goal: state.goal && { ...state.goal },
							speed: state.speed,
							inputSeq: state.inputSeq,
							originAtPublish: origin( state.atMs )
						}
					} );
				}
			}
		},
		// The newest publication visible on the main clock at mainMs.
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

Runs a scenario: clicks at main-clock times, worker events at worker
times, frames at the recording's intervals. Returns every drawn frame and
the worker's final state.
================
*/
/**
 * @param {{
 *   clicks: { atMs: number, to: Pose, refuse?: boolean }[],
 *   events?: { atMs: number, apply: ( state: any ) => unknown }[],
 *   origin?: ( ms: number ) => number,
 *   endMs?: number,
 *   outLatencyMs?: number
 * }} scenario
 */
function play( { clicks, events = [], origin = () => 0, endMs = 8000, outLatencyMs = OUT_LATENCY_MS } ) {
	const worker = createWorker( origin, outLatencyMs ), mover = createLocalMover(), view = flatView();
	const drawn = [];
	// The scenario starts when the worker's clock reads zero; click times are
	// main-clock times from then.
	const base = Math.max( 0, origin( 0 ) );
	let now = base, frame = 0, last = { ...START }, seq = 0;
	const pending = clicks.map( click => ({ ...click, atMs: base + click.atMs }) );
	const worldAt = ms => {
		const p = worker.visible( ms );
		return {
			pose: p ? p.pose : { ...START },
			poseAtMs: p?.poseAtMs,
			revision: p?.revision ?? 0,
			moving: p?.moving ?? false,
			aim: p?.goal && { kind: "goal", destination: p.goal },
			speedFactor: 1,
			turnRate: 0,
			walkPredictable: true,
			walkHeld: false,
			holding: false,
			speed: p?.speed ?? SPEED,
			inputSeq: p?.inputSeq ?? 0,
			navigationRequestId: 1,
			navigationOwner: undefined,
			originMs: p ? p.originAtPublish : origin( ms ),
			view: { requestId: 1, navigation: view }
		};
	};
	while ( now < base + endMs ) {
		now += FRAMES[frame++ % FRAMES.length];
		worker.run( now - origin( now ), events );
		while ( pending.length && pending[0].atMs <= now ) {
			const click = defined( pending.shift() );
			seq++;
			worker.send( seq, click.to, now, click.refuse );
			mover.click( { seq, query: query( click.to.x, click.to.z ), drawn: last, now }, worldAt( now ) );
		}
		const result = mover.frame( now, worldAt( now ) );
		if ( result.kind !== "idle" ) last = result.pose;
		drawn.push( {
			now,
			pose: { ...last },
			kind: result.kind,
			speed: worker.state.speed,
			truth: { ...worker.state.pose }
		} );
	}
	return { drawn, worker: worker.state };
}

/*
================
peakRatio

The fastest drawn frame against walking speed, skipping one snap frame.
================
*/
/**
 * @param {{ now: number, pose: Pose, speed: number }[]} drawn
 * @param {( frame: any ) => boolean} [skip]
 */
function peakRatio( drawn, skip = () => false ) {
	let peak = 0;
	for ( let i = 1; i < drawn.length; i++ ) {
		if ( skip( drawn[i] ) ) continue;
		const a = drawn[i - 1], b = drawn[i], dt = (b.now - a.now) / 1000;
		const speed = Math.hypot( b.pose.x - a.pose.x, b.pose.z - a.pose.z ) / dt;
		peak = Math.max( peak, speed / Math.max( a.speed, b.speed ) );
	}
	return peak;
}

/*
================
maxLead

How far the drawn body ever strays from where the worker actually is at
the same moment: the prediction's lead (the click's trip to the worker)
plus a tick, never the publication's age or a clock offset.
================
*/
function maxLead( drawn ) {
	return Math.max( ...drawn.map( f => Math.hypot( f.pose.x - f.truth.x, f.pose.z - f.truth.z ) ) );
}

/*
================
endsAt

The drawn body comes to rest at the worker's final pose.
================
*/
function endsAt( result ) {
	const last = defined( result.drawn.at( -1 ) ).pose;
	return Math.hypot( last.x - result.worker.pose.x, last.z - result.worker.pose.z );
}

test("a prediction the worker agrees with is drawn without residual and ends where the worker ends", () => {
	const result = play( { clicks: [ { atMs: 100, to: { ...START, x: 1060 } } ] } );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	// The body leaves rest on the click frame and walks every frame after.
	const walking = result.drawn.filter( f => f.now >= 100 && f.pose.x < 1059 );
	for ( let i = 1; i < walking.length; i++ ) {
		assert.ok( walking[i].pose.x > walking[i - 1].pose.x, `stalled at ${walking[i].now}` );
	}
	assert.ok( maxLead( result.drawn ) < 3, `strays ${maxLead( result.drawn ).toFixed( 2 )} from the worker` );
	// No residual: the worker's answers, which trail the prediction by the
	// round trip, never pull the drawn body back along its walk.
	for ( let i = 1; i < result.drawn.length; i++ ) {
		assert.ok(
			result.drawn[i].pose.x >= result.drawn[i - 1].pose.x - 1e-4,
			`pulled back at ${result.drawn[i].now}`
		);
	}
	assert.ok( endsAt( result ) < 1e-2, `ends ${endsAt( result )} from the worker` );
	assert.equal( defined( result.drawn.at( -1 ) ).kind, "idle", "the prediction handed back" );
});

test("two clicks the worker has not taken yet are replayed in order", () => {
	const result = play( {
		clicks: [ { atMs: 100, to: { ...START, x: 1100 } }, { atMs: 400, to: { ...START, x: 1013, z: 1080 } } ]
	} );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	assert.ok( endsAt( result ) < 1e-2 );
});

test("a server stop mid-leg glides the body back within the cap and ends at the stop", () => {
	const result = play( {
		clicks: [ { atMs: 100, to: { ...START, x: 1200 } } ],
		events: [ { atMs: 800, apply: s => (s.goal = undefined, s.revision++) } ]
	} );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	assert.ok( endsAt( result ) < 1e-2, `ends ${endsAt( result )} from the stop` );
});

test("a knockback and an obstacle shove mid-leg are given back within the cap", () => {
	const result = play( {
		clicks: [ { atMs: 100, to: { ...START, x: 1200 } } ],
		events: [
			{ atMs: 640, apply: s => (s.pose = { ...s.pose, x: s.pose.x - 8 }, s.revision++) },
			{ atMs: 1200, apply: s => (s.pose = { ...s.pose, z: s.pose.z + 2 }, s.revision++) }
		]
	} );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	assert.ok( endsAt( result ) < 1e-2 );
});

test("a teleport snaps on one frame and the rest stays within the cap", () => {
	const result = play( {
		clicks: [ { atMs: 100, to: { ...START, x: 1200 } } ],
		events: [ {
			atMs: 640,
			apply: s => (s.pose = { ...s.pose, x: s.pose.x + 500 }, s.goal = undefined, s.revision++)
		} ]
	} );
	const far = result.drawn.findIndex( f => f.pose.x > 1300 );
	assert.ok( far > 0, "the body lands at the teleport" );
	assert.ok( peakRatio( result.drawn, f => f === result.drawn[far] ) <= CAP + 1e-6 );
	assert.ok( endsAt( result ) < 1e-2 );
});

test("a speed change mid-leg is followed: the body slows with the worker", () => {
	const result = play( {
		clicks: [ { atMs: 100, to: { ...START, x: 1200 } } ],
		events: [ { atMs: 640, apply: s => (s.speed = 20, s.revision++) } ],
		endMs: 12000
	} );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	const late = result.drawn.filter( f => f.now > 1500 && f.now < 2500 );
	const first = defined( late[0] ), final = defined( late.at( -1 ) );
	const dt = (final.now - first.now) / 1000;
	const speed = (final.pose.x - first.pose.x) / dt;
	assert.ok( Math.abs( speed - 20 ) < 1, `walks at ${speed.toFixed( 2 )} u/s after the change` );
	assert.ok( endsAt( result ) < 1e-2 );
});

test("a refused click between two accepted ones leaves no trace", () => {
	const result = play( {
		clicks: [
			{ atMs: 100, to: { ...START, x: 1100 } },
			{ atMs: 300, to: { ...START, x: 1010, z: 1100 }, refuse: true },
			{ atMs: 600, to: { ...START, x: 1030, z: 960 } }
		]
	} );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	// The worker never walked the refused click; the body ends where it does.
	assert.ok( endsAt( result ) < 1e-2, `ends ${endsAt( result )} from the worker` );
	assert.equal( result.worker.pose.x, 1030 );
});

test("a skewed and drifting worker clock origin is followed", () => {
	// The camera-shake recording's origin was about 2 s and drifting.
	for ( const origin of [ () => 2037, () => -30, ms => 2037 + ms * .002 ] ) {
		const result = play( {
			clicks: [ { atMs: 100, to: { ...START, x: 1100 } }, { atMs: 900, to: { ...START, x: 1050, z: 1060 } } ],
			origin
		} );
		assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
		assert.ok( maxLead( result.drawn ) < 3, `strays ${maxLead( result.drawn ).toFixed( 2 )} from the worker` );
		assert.ok( endsAt( result ) < 1e-2 );
	}
});

test("a publication from well before a click, arriving after it, does not replay the click early", () => {
	// A slow link: every publication arrives 150 ms after its tick, so the
	// first ones the mover rebuilds from after the click predate it. The
	// click must be replayed from its own time, not the publication's.
	const result = play( { clicks: [ { atMs: 100, to: { ...START, x: 1100 } } ], outLatencyMs: 150 } );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	// The body is never further along than the walk could carry it since the
	// click: a click replayed from an older publication's time would be.
	// Clicks are made on the first frame at or after their time.
	const clickNow = defined( result.drawn.find( f => f.now >= 100 ) ).now;
	for ( const frame of result.drawn ) {
		const walked = frame.pose.x - START.x, allowed = SPEED * Math.max( 0, frame.now - clickNow ) / 1000;
		assert.ok( walked <= allowed + .05, `${walked.toFixed( 2 )} walked by ${frame.now.toFixed( 1 )} ms` );
	}
	assert.ok( maxLead( result.drawn ) < 3, `strays ${maxLead( result.drawn ).toFixed( 2 )} from the worker` );
	assert.ok( endsAt( result ) < 1e-2 );
});

test("after a slowdown the cap holds the drawn body to the new walking speed through a correction", () => {
	const result = play( {
		clicks: [ { atMs: 100, to: { ...START, x: 1300 } } ],
		events: [
			{ atMs: 640, apply: s => (s.speed = 20, s.revision++) },
			// Then a knockback under the new speed: the residual drains at 1.04x 20 u/s.
			{ atMs: 1600, apply: s => (s.pose = { ...s.pose, x: s.pose.x - 8 }, s.revision++) }
		],
		endMs: 20000
	} );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	assert.ok( endsAt( result ) < 1e-2 );
});

test("a teleport drops the clicks still in flight instead of walking them from the new place", () => {
	// A click made just before a teleport the worker has already done: the
	// rebuild that lands the body far away must not walk that click's leg.
	const result = play( {
		clicks: [ { atMs: 100, to: { ...START, x: 1200 } }, { atMs: 650, to: { ...START, x: 900 } } ],
		events: [ {
			atMs: 640,
			apply: s => (s.pose = { ...s.pose, x: s.pose.x + 500 }, s.goal = undefined, s.revision++)
		} ]
	} );
	const landed = result.drawn.findIndex( f => f.pose.x > 1300 );
	assert.ok( landed > 0, "the body lands at the teleport" );
	// From the landing frame until the worker answers the click, the body
	// stands where it landed rather than walking the stale leg.
	const after = result.drawn.slice( landed, landed + 3 );
	for ( const frame of after ) {
		assert.ok( Math.abs( frame.pose.x - after[0].pose.x ) < 1e-6, `walked ${frame.pose.x - after[0].pose.x}` );
	}
});

/*
================
playMoves

play, for a command that already carries its destination (a GM /Pos or
map auto-move): the runtime predicts a goal aim at the command's frame,
and the worker takes the same input after its inbound latency.
================
*/
/**
 * @param {{
 *   moves: { atMs: number, to: Pose, refuse?: boolean }[],
 *   endMs?: number
 * }} scenario
 */
function playMoves( { moves, endMs = 8000 } ) {
	const worker = createWorker( () => 0 ), mover = createLocalMover(), view = flatView();
	const drawn = [];
	const base = 0;
	let now = base, frame = 0, last = { ...START }, seq = 0;
	const pending = moves.map( move => ({ ...move, atMs: base + move.atMs }) );
	const worldAt = ms => {
		const p = worker.visible( ms );
		return {
			pose: p ? p.pose : { ...START },
			poseAtMs: p?.poseAtMs,
			revision: p?.revision ?? 0,
			moving: p?.moving ?? false,
			aim: p?.goal && { kind: "goal", destination: p.goal },
			speedFactor: 1,
			turnRate: 0,
			walkPredictable: true,
			walkHeld: false,
			holding: false,
			speed: p?.speed ?? SPEED,
			inputSeq: p?.inputSeq ?? 0,
			navigationRequestId: 1,
			navigationOwner: undefined,
			originMs: p ? p.originAtPublish : 0,
			view: { requestId: 1, navigation: view }
		};
	};
	while ( now < base + endMs ) {
		now += FRAMES[frame++ % FRAMES.length];
		worker.run( now, [] );
		while ( pending.length && pending[0].atMs <= now ) {
			const move = defined( pending.shift() );
			seq++;
			worker.send( seq, move.to, now, move.refuse );
			mover.predict(
				{ seq, aim: { kind: "goal", destination: move.to }, speed: SPEED, drawn: last, now },
				worldAt( now )
			);
		}
		const result = mover.frame( now, worldAt( now ) );
		if ( result.kind !== "idle" ) last = result.pose;
		drawn.push( {
			now,
			pose: { ...last },
			kind: result.kind,
			speed: worker.state.speed,
			truth: { ...worker.state.pose }
		} );
	}
	return { drawn, worker: worker.state };
}

test("a move the worker agrees with is drawn without residual and ends where the worker ends", () => {
	const result = playMoves( { moves: [ { atMs: 100, to: { ...START, x: 1060 } } ] } );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	const walking = result.drawn.filter( f => f.now >= 100 && f.pose.x < 1059 );
	for ( let i = 1; i < walking.length; i++ ) {
		assert.ok( walking[i].pose.x > walking[i - 1].pose.x, `stalled at ${walking[i].now}` );
	}
	for ( let i = 1; i < result.drawn.length; i++ ) {
		assert.ok(
			result.drawn[i].pose.x >= result.drawn[i - 1].pose.x - 1e-4,
			`pulled back at ${result.drawn[i].now}`
		);
	}
	assert.ok( endsAt( result ) < 1e-2, `ends ${endsAt( result )} from the worker` );
	assert.equal( defined( result.drawn.at( -1 ) ).kind, "idle", "the prediction handed back" );
});

test("a refused move hands the drawn pose back within the cap and ends where the worker stands", () => {
	const result = playMoves( { moves: [ { atMs: 100, to: { ...START, x: 1200 }, refuse: true } ] } );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	assert.ok(
		result.drawn.some( f => f.kind === "ended" ),
		"the prediction never handed back"
	);
	// The worker never walked: the body returns to its pose and rests there.
	assert.ok( endsAt( result ) < 1e-2, `ends ${endsAt( result )} from the worker` );
	assert.equal( defined( result.drawn.at( -1 ) ).kind, "idle" );
});

test("a second move redirects the prediction and both bodies end at the new goal", () => {
	const result = playMoves( {
		moves: [ { atMs: 100, to: { ...START, x: 1100 } }, { atMs: 400, to: { ...START, x: 1013, z: 1080 } } ]
	} );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	assert.ok( endsAt( result ) < 1e-2, `ends ${endsAt( result )} from the worker` );
	const last = defined( result.drawn.at( -1 ) ).pose;
	assert.ok( Math.hypot( last.x - 1013, last.z - 1080 ) < 1e-2, "the body did not end at the new goal" );
});
