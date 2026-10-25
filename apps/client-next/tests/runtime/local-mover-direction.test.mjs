/*
===========================================================================

local-mover-direction.test.mjs - the main-thread mover predicts direction
walks (GH #1563, part 1)

A ground click that misses the ground (the sky, the horizon) walks the
ray's heading until something blocks it (CGInterface_MoveToWorldPoint
0x6932A0, direction-movement.ts). The mover predicts that walk from the
click frame and reconciles it as it does a goal walk: a small worker
stands in for the simulation worker, takes each click after a latency,
walks it in 16 ms ticks along the heading until a wall stops it, and
publishes its state, its live direction leg included, after a latency.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createLocalMover } = await import( "../../src/engine/runtime/movement/local-mover.ts" );
const { directionBearing, directionTurnSkipped, pickMissHeading } = await import(
	"../../src/engine/foundation/gameplay/direction-movement.ts"
);
const { WALK_NAVIGATION_STOP } = await import( "../../src/engine/foundation/gameplay/walk-step.ts" );
const { worldDelta } = await import( "../../src/engine/foundation/world/world-space.ts" );

const REGION = 0x61a8, SPEED = 50, TICK_MS = 16, PUBLISH_MS = 32;
const IN_LATENCY_MS = 20, OUT_LATENCY_MS = 25;
/** @typedef {import("../../src/engine/contracts/gameplay").Pose} Pose */
const START = { regionId: REGION, x: 1000, y: 0, z: 1000, angle: 0 };
// The camera-shake recording's frame intervals (ms): uneven on purpose.
const FRAMES = [ 16.7, 16.6, 33.4, 8.1, 16.7, 25.0, 16.6, 12.2, 16.7, 41.6, 16.7, 16.6 ];
const CAP = 1.04;
// A wall across the world at this x stops every walk that reaches it.
const WALL_X = 1150;
// No wall: a walk runs on past its first DIRECTION_LEG_UNITS leg.
const NO_WALL = Infinity;

/*
================
wallView

A navigation view whose pick misses for a miss query (query.ray.miss) and
answers the target otherwise, and whose clip stops any step at WALL_X.
================
*/
function wallView( wallX ) {
	return {
		available: () => true,
		block: () => 0,
		floor: () => undefined,
		pick: query => query.ray.miss ? null : { ...query.ray.target },
		clip: ( _from, to, query ) => {
			if ( query ) query.status = 0;
			if ( to.x < wallX ) return to;
			if ( query ) query.status = WALK_NAVIGATION_STOP;
			return { ...to, x: wallX };
		},
		surface: pose => pose
	};
}

/*
================
missQuery

A click into the sky along the horizontal direction (dx, dz).
================
*/
function missQuery( dx, dz ) {
	return /** @type {any} */ ({
		originRegion: REGION,
		ray: { miss: true, delta: [ dx, -1, dz ], target: null },
		terrainDepth: null
	});
}

/*
================
pointQuery
================
*/
function pointQuery( x, z ) {
	return /** @type {any} */ ({
		originRegion: REGION,
		ray: { target: { regionId: REGION, x, y: 0, z } },
		terrainDepth: null
	});
}

/*
================
createWorker

The simulation worker, reduced to its walk: a goal or a heading, a speed,
and the publications it makes. A heading walk runs until the wall; a miss
within 5 degrees of the heading already walked changes nothing (0x693519).
================
*/
function createWorker( factor, wallX ) {
	const state = {
		pose: /** @type {Pose} */ ({ ...START }),
		goal: /** @type {Pose | undefined} */ (undefined),
		heading: /** @type {number | undefined} */ (undefined),
		speed: SPEED,
		inputSeq: 0,
		revision: 0,
		atMs: 0
	};
	const inbox = [], published = [];
	return {
		state,
		send( seq, click, mainMs ) {
			inbox.push( { seq, click, atMs: mainMs + IN_LATENCY_MS } );
		},
		run( ms ) {
			while ( state.atMs + TICK_MS <= ms ) {
				state.atMs += TICK_MS;
				for ( const input of inbox.filter( i => i.atMs <= state.atMs ) ) {
					inbox.splice( inbox.indexOf( input ), 1 );
					state.inputSeq = Math.max( state.inputSeq, input.seq );
					if ( input.click.miss ) {
						const heading = pickMissHeading( input.click.query, state.pose );
						if ( directionTurnSkipped( state.heading !== undefined, state.pose.angle, heading ) ) continue;
						state.heading = heading;
						state.goal = undefined;
						state.pose = { ...state.pose, angle: heading };
					} else {
						state.goal = {
							...input.click.to,
							x: Math.trunc( input.click.to.x ),
							z: Math.trunc( input.click.to.z )
						};
						state.heading = undefined;
					}
					state.revision++;
				}
				const step = state.speed * factor * TICK_MS / 1000;
				if ( state.heading !== undefined ) {
					const bearing = directionBearing( state.heading );
					const x = state.pose.x + Math.cos( bearing ) * step, z = state.pose.z + Math.sin( bearing ) * step;
					if ( x >= wallX ) {
						state.pose = { ...state.pose, x: wallX, z };
						state.heading = undefined;
						state.revision++;
					} else state.pose = { ...state.pose, x, z };
				} else if ( state.goal ) {
					const dx = state.goal.x - state.pose.x,
						dz = state.goal.z - state.pose.z,
						left = Math.hypot( dx, dz );
					if ( left <= state.speed * TICK_MS / 1000 ) {
						state.pose = { ...state.goal };
						state.goal = undefined;
						state.revision++;
					} else {
						const s = state.speed * TICK_MS / 1000;
						state.pose = {
							...state.pose,
							x: state.pose.x + dx / left * s,
							z: state.pose.z + dz / left * s
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
							moving: state.heading !== undefined || !!state.goal,
							goal: state.goal && { ...state.goal },
							direction: state.heading !== undefined ? { heading: state.heading, factor } : undefined,
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

Runs a scenario of clicks at main-clock times. factor is the worker's
direction speed factor (the 500 ms drift toward the server's walk).
================
*/
function play( { clicks, factor = 1, endMs = 6000, wallX = WALL_X } ) {
	const worker = createWorker( factor, wallX ), mover = createLocalMover(), view = wallView( wallX );
	const drawn = [], answers = [];
	let now = 0, frame = 0, last = { ...START }, seq = 0;
	const pending = clicks.slice();
	const worldAt = ms => {
		const p = worker.visible( ms );
		return {
			pose: p ? p.pose : { ...START },
			poseAtMs: p?.poseAtMs,
			revision: p?.revision ?? 0,
			moving: p?.moving ?? false,
			aim: p?.goal ?
				{ kind: "goal", destination: p.goal } :
				p?.direction && { kind: "direction", heading: p.direction.heading },
			speedFactor: p?.direction?.factor ?? 1,
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
			const query = click.miss ? missQuery( click.dx, click.dz ) : pointQuery( click.to.x, click.to.z );
			worker.send( seq, { ...click, query }, now );
			answers.push( mover.click( { seq, query, drawn: last, now }, worldAt( now ) ) );
		}
		const result = mover.frame( now, worldAt( now ) );
		if ( result.kind !== "idle" ) last = result.pose;
		drawn.push( { now, pose: { ...last }, kind: result.kind, truth: { ...worker.state.pose } } );
	}
	return { drawn, worker: worker.state, answers };
}

/*
================
gap

The planar distance between two poses in world space: a long walk folds
the drawn pose into the next region (direction-movement.ts foldPose).
================
*/
function gap( a, b ) {
	const [dx, dz] = worldDelta( a, b );
	return Math.hypot( dx, dz );
}

/*
================
peakRatio
================
*/
function peakRatio( drawn ) {
	let peak = 0;
	for ( let i = 1; i < drawn.length; i++ ) {
		const a = drawn[i - 1], b = drawn[i], dt = (b.now - a.now) / 1000;
		peak = Math.max( peak, gap( a.pose, b.pose ) / dt / SPEED );
	}
	return peak;
}

/*
================
maxLead
================
*/
function maxLead( drawn ) {
	return Math.max( ...drawn.map( f => gap( f.pose, f.truth ) ) );
}

/*
================
steadyLead

How far the drawn body leads the worker once a straight walk is under
way: the click's trip to the worker, walked at the worker's speed.
================
*/
function steadyLead( drawn ) {
	return Math.max( ...drawn.filter( f => f.now > 1500 && f.now < 2500 ).map( f => gap( f.pose, f.truth ) ) );
}

/*
================
endsAt
================
*/
function endsAt( result ) {
	const last = defined( result.drawn.at( -1 ) ).pose;
	return gap( last, result.worker.pose );
}

test("a click into the sky walks its heading from the click frame and stops at the wall with the worker", () => {
	const result = play( { clicks: [ { atMs: 100, miss: true, dx: 1, dz: .25 } ] } );
	assert.deepEqual( result.answers, [ true ], "the mover predicts the direction walk" );
	// The body leaves rest on the click frame, before any answer comes back.
	const early = result.drawn.filter( f => f.now > 100 && f.now < 100 + IN_LATENCY_MS + OUT_LATENCY_MS );
	assert.ok( early.length && defined( early.at( -1 ) ).pose.x > START.x + .5, "walks before the worker answers" );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	assert.ok( maxLead( result.drawn ) < 3, `strays ${maxLead( result.drawn ).toFixed( 2 )} from the worker` );
	assert.ok( Math.abs( result.worker.pose.x - WALL_X ) < 1e-6, "the worker stopped at the wall" );
	assert.ok( endsAt( result ) < 1e-2, `ends ${endsAt( result )} from the worker` );
	assert.equal( defined( result.drawn.at( -1 ) ).kind, "idle", "the prediction handed back" );
});

test("a direction walk the worker drifts faster is followed, never outrun", () => {
	const result = play( { clicks: [ { atMs: 100, miss: true, dx: 1, dz: 0 } ], factor: 1.1 } );
	const plain = play( { clicks: [ { atMs: 100, miss: true, dx: 1, dz: 0 } ] } );
	// The rebuild walks the worker's drifted speed, so the prediction's lead
	// grows with it; walked at plain speed, it would fall behind its lead.
	const ratio = steadyLead( result.drawn ) / steadyLead( plain.drawn );
	assert.ok( ratio > 1.05 && ratio < 1.15, `lead ratio ${ratio.toFixed( 3 )}` );
	assert.ok( peakRatio( result.drawn ) <= 1.1 * CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
	assert.ok( maxLead( result.drawn ) < 4, `strays ${maxLead( result.drawn ).toFixed( 2 )} from the worker` );
	assert.ok( endsAt( result ) < 1e-2, `ends ${endsAt( result )} from the worker` );
});

test("a miss within 5 degrees of the walked heading changes nothing; a wider one turns", () => {
	const same = play( {
		clicks: [ { atMs: 100, miss: true, dx: 1, dz: 0 }, { atMs: 600, miss: true, dx: 1, dz: .02 } ]
	} );
	assert.ok( maxLead( same.drawn ) < 3, `strays ${maxLead( same.drawn ).toFixed( 2 )} from the worker` );
	// The skipped miss never turns the predicted walk, not even for a frame.
	const drift = Math.max( ...same.drawn.map( f => Math.abs( f.pose.z - START.z ) ) );
	assert.ok( drift < 1e-6, `turned by ${drift}` );
	assert.ok( endsAt( same ) < 1e-2 );
	const turned = play( {
		clicks: [ { atMs: 100, miss: true, dx: 1, dz: 0 }, { atMs: 600, miss: true, dx: 1, dz: 1 } ]
	} );
	assert.ok( turned.worker.pose.z > START.z + 20, "the worker turned" );
	assert.ok( maxLead( turned.drawn ) < 3, `strays ${maxLead( turned.drawn ).toFixed( 2 )} from the worker` );
	assert.ok( peakRatio( turned.drawn ) <= CAP + 1e-6, `peak ${peakRatio( turned.drawn )}` );
	assert.ok( endsAt( turned ) < 1e-2 );
});

test("a ground click during a direction walk takes over, and a miss during a goal walk takes over", () => {
	const toPoint = play( {
		clicks: [ { atMs: 100, miss: true, dx: 1, dz: 0 }, { atMs: 700, to: { ...START, x: 1010, z: 1060 } } ]
	} );
	assert.ok( maxLead( toPoint.drawn ) < 3, `strays ${maxLead( toPoint.drawn ).toFixed( 2 )} from the worker` );
	assert.ok( endsAt( toPoint ) < 1e-2 );
	const toSky = play( {
		clicks: [ { atMs: 100, to: { ...START, x: 1000, z: 1080 } }, { atMs: 700, miss: true, dx: 1, dz: 0 } ]
	} );
	assert.ok( maxLead( toSky.drawn ) < 3, `strays ${maxLead( toSky.drawn ).toFixed( 2 )} from the worker` );
	assert.ok( peakRatio( toSky.drawn ) <= CAP + 1e-6, `peak ${peakRatio( toSky.drawn )}` );
	assert.ok( endsAt( toSky ) < 1e-2 );
});

test("a direction walk runs on past its first leg until something stops it", () => {
	// 24 s at 50 units a second: past the first 1000-unit leg.
	const result = play( { clicks: [ { atMs: 100, miss: true, dx: 1, dz: 0 } ], wallX: NO_WALL, endMs: 24000 } );
	assert.ok( gap( START, defined( result.drawn.at( -1 ) ).pose ) > 1100, "walked past the first leg" );
	assert.ok( maxLead( result.drawn ) < 3, `strays ${maxLead( result.drawn ).toFixed( 2 )} from the worker` );
	assert.ok( peakRatio( result.drawn ) <= CAP + 1e-6, `peak ${peakRatio( result.drawn )}` );
});

test("a miss within 5 degrees of the worker's own direction walk starts no prediction from rest", () => {
	const mover = createLocalMover(), view = wallView( WALL_X );
	const world = /** @type {any} */ ({
		pose: { ...START },
		poseAtMs: 0,
		revision: 1,
		moving: true,
		aim: { kind: "direction", heading: 0 },
		speedFactor: 1,
		turnRate: 0,
		walkPredictable: true,
		walkHeld: false,
		holding: false,
		speed: SPEED,
		inputSeq: 0,
		navigationRequestId: 1,
		navigationOwner: undefined,
		originMs: 0,
		view: { requestId: 1, navigation: view }
	});
	// The worker drops this miss (0x693519): nothing to predict.
	assert.equal( mover.click( { seq: 1, query: missQuery( 1, .02 ), drawn: START, now: 10 }, world ), false );
	assert.equal( mover.frame( 20, world ).kind, "idle" );
	// A wider miss is a new direction.
	assert.equal( mover.click( { seq: 2, query: missQuery( 1, 1 ), drawn: START, now: 30 }, world ), true );
});
