/*
===========================================================================

local-mover.test.mjs - tests for local-mover.ts (GH #1440)

The main-thread mover walks a ground click from the click frame on, every
frame, with the worker's step against the worker's navigation. The flat
view below accepts every move and picks the point the query names, so the
timing rules are checked alone; the last test clicks on the published
Jangan navigation.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync as assetBytes } from "../../../../scripts/lib/publishedAsset.mjs";
const { createLocalMover } = await import( "../../src/engine/runtime/movement/local-mover.ts" );
const { createNavigation } = await import( "../../src/engine/foundation/navigation/clipper.ts" );
const { createNavigationResources } = await import( "../../src/engine/runtime/assets/worker/navigation/navigation.ts" );

const REGION = 0x61a8, SPEED = 50;
const START = { regionId: REGION, x: 1000, y: 0, z: 1000, angle: 0 };
// The camera-shake recording's frame intervals (ms): uneven on purpose.
const FRAMES = [ 16.7, 16.6, 33.4, 8.1, 16.7, 25.0, 16.6, 12.2, 16.7, 41.6, 16.7, 16.6 ];

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

A ground pick query aimed at a point (the flat view's pick reads it).
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
poseOf

The pose a non-idle frame draws.
================
*/
function poseOf( frame ) {
	assert.ok( frame.kind !== "idle", "the mover draws this frame" );
	return frame.pose;
}

/*
================
world
================
*/
function world( overrides = {} ) {
	return {
		pose: START,
		poseAtMs: undefined,
		revision: 0,
		moving: false,
		aim: undefined,
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
		view: { requestId: 1, navigation: flatView() },
		...overrides
	};
}

test("the body leaves rest on the click frame and never outruns walking speed at uneven frames", () => {
	const mover = createLocalMover(), w = world();
	assert.equal( mover.click( { seq: 1, query: query( 1300, 1000 ), drawn: START, now: 0 }, w ), true );
	let now = 0, last = START;
	for ( const dt of FRAMES ) {
		now += dt;
		const frame = mover.frame( now, w );
		assert.equal( frame.kind, "walking" );
		const step = Math.hypot( frame.pose.x - last.x, frame.pose.z - last.z );
		// float32 positions: allow a hundredth of a unit.
		assert.ok( step <= SPEED * dt / 1000 + .01, `step ${step} in ${dt} ms` );
		assert.ok( step > 0, "it moves every frame" );
		last = frame.pose;
	}
	// The first frame already moved: no frame of rest after the click.
	assert.ok( last.x > START.x + SPEED * now / 1000 - .1 );
});

test("a move prediction starts on its frame and walks its goal", () => {
	const mover = createLocalMover(), w = world();
	const destination = { ...START, x: 1300 };
	assert.equal(
		mover.predict( { seq: 1, aim: { kind: "goal", destination }, speed: SPEED, drawn: START, now: 0 }, w ),
		true
	);
	let now = 0, last = START;
	for ( const dt of FRAMES ) {
		now += dt;
		const frame = mover.frame( now, w );
		assert.equal( frame.kind, "walking" );
		const step = Math.hypot( frame.pose.x - last.x, frame.pose.z - last.z );
		assert.ok( step <= SPEED * dt / 1000 + .01, `step ${step} in ${dt} ms` );
		assert.ok( step > 0, "it moves every frame" );
		last = frame.pose;
	}
	assert.ok( last.x > START.x + SPEED * now / 1000 - .1, "no frame of rest after the move" );
	// No speed to walk at, or no navigation view: nothing is predicted and
	// the presenter keeps the walk (the command still went to the worker).
	assert.equal(
		createLocalMover().predict(
			{ seq: 1, aim: { kind: "goal", destination }, speed: 0, drawn: START, now: 0 },
			w
		),
		false
	);
	assert.equal(
		createLocalMover().predict(
			{ seq: 1, aim: { kind: "goal", destination }, speed: SPEED, drawn: START, now: 0 },
			world( { view: null } )
		),
		false
	);
});

test("a second move prediction redirects the walk from where the body is drawn", () => {
	const mover = createLocalMover(), w = world();
	mover.predict(
		{ seq: 1, aim: { kind: "goal", destination: { ...START, x: 1300 } }, speed: SPEED, drawn: START, now: 0 },
		w
	);
	const before = mover.frame( 200, w );
	assert.equal( before.kind, "walking" );
	assert.equal(
		mover.predict(
			{
				seq: 2,
				aim: { kind: "goal", destination: { ...START, x: 1000, z: 1300 } },
				speed: SPEED,
				drawn: before.pose,
				now: 200
			},
			w
		),
		true
	);
	const after = mover.frame( 300, w );
	assert.equal( after.kind, "walking" );
	assert.ok( Math.abs( after.pose.x - before.pose.x ) < 1, "no further travel along the old leg" );
	assert.ok( after.pose.z > before.pose.z, "the new leg runs from the turn" );
});

test("a right-angle re-click turns at the click frame from where the body is drawn", () => {
	const mover = createLocalMover(), w = world();
	mover.click( { seq: 1, query: query( 1300, 1000 ), drawn: START, now: 0 }, w );
	const before = mover.frame( 200, w );
	assert.equal( before.kind, "walking" );
	// A whole-unit x: the 0x7021 destination is truncated to whole units.
	const turnX = Math.trunc( before.pose.x );
	mover.click( { seq: 2, query: query( turnX, 1300 ), drawn: before.pose, now: 200 }, w );
	const after = mover.frame( 300, w );
	assert.equal( after.kind, "walking" );
	assert.ok( Math.abs( after.pose.x - before.pose.x ) < 1, "no further travel along the old leg" );
	assert.ok( Math.abs( after.pose.z - before.pose.z - SPEED * .1 ) < .05, "the new leg runs from the turn" );
});

test("arrival lands on the truncated destination and holds until the worker has walked the click", () => {
	const mover = createLocalMover();
	mover.click( { seq: 1, query: query( 1010.9, 1000 ), drawn: START, now: 0 }, world() );
	// The worker has not answered yet: the body stays where the leg ended.
	let frame = mover.frame( 1000, world() );
	assert.equal( frame.kind, "walking" );
	assert.equal( frame.pose.x, 1010 );
	// The worker took the click and is still walking its own leg.
	frame = mover.frame( 1100, world( { inputSeq: 1, moving: true } ) );
	assert.equal( frame.kind, "walking" );
	// The worker's leg is over too: hand the pose back.
	frame = mover.frame( 1200, world( { inputSeq: 1, moving: false } ) );
	assert.equal( frame.kind, "ended" );
	assert.equal( frame.pose.x, 1010 );
	assert.equal( mover.active(), false );
});

test("a click the worker refused glides back to where the worker stands, within the cap, then ends", () => {
	const mover = createLocalMover();
	mover.click( { seq: 4, query: query( 1300, 1000 ), drawn: START, now: 0 }, world() );
	const first = mover.frame( 50, world() );
	assert.equal( first.kind, "walking" );
	let previous = poseOf( first ), now = 50, ended;
	// The worker took the click and refused it: it publishes its standing pose.
	for ( let i = 0; i < 60 && !ended; i++ ) {
		now += 16.7;
		const frame = mover.frame( now, world( { inputSeq: 4, poseAtMs: 40 + i, revision: 1 } ) );
		const pose = poseOf( frame );
		const step = Math.hypot( pose.x - previous.x, pose.z - previous.z );
		assert.ok( step <= SPEED * 1.04 * .0167 + 1e-6, `step ${step}` );
		if ( frame.kind === "ended" ) ended = pose;
		previous = pose;
	}
	assert.ok( ended, "the prediction ends once it is back" );
	assert.ok( Math.abs( ended.x - START.x ) < 1e-2, `ended at ${ended.x}` );
});

test("a click behind a holding gate stays drawn in place, then walks on the release frame (#1570)", () => {
	const mover = createLocalMover(), goal = { ...START, x: 1300 };
	const gated = world( { walkPredictable: false, walkHeld: true, holding: true } );
	assert.equal( mover.click( { seq: 7, query: query( 1300, 1000 ), drawn: START, now: 0 }, gated ), false );
	// The gate holds: no prediction, the body is not the mover's to draw.
	assert.equal( mover.active(), false );
	assert.equal( mover.frame( 100, gated ).kind, "idle" );
	// Production cadence: the worker notes the sequence before the gate,
	// so the click's own publication is taken-but-not-walking while still
	// held; it must not consume the record.
	const taken = world( {
		walkPredictable: false,
		walkHeld: true,
		holding: true,
		inputSeq: 7,
		poseAtMs: 95,
		revision: 1
	} );
	assert.equal( mover.frame( 110, taken ).kind, "idle" );
	// The worker stopped holding and walks it: adoption steps from where
	// the body was drawn at click time.
	const released = world( {
		inputSeq: 7,
		moving: true,
		aim: { kind: "goal", destination: goal },
		poseAtMs: 190,
		revision: 2
	} );
	const first = mover.frame( 200, released );
	assert.equal( first.kind, "walking" );
	assert.deepEqual( poseOf( first ), START );
	const later = mover.frame( 400, released );
	assert.equal( later.kind, "walking" );
	assert.ok( Math.abs( poseOf( later ).x - (START.x + SPEED * .2) ) < .05, `x ${poseOf( later ).x}` );
	assert.equal( poseOf( later ).z, START.z );
});

test("an adopted walk hands its pose back when the worker's leg ends (#1570)", () => {
	const mover = createLocalMover(), goal = { ...START, x: 1010 };
	assert.equal(
		mover.click(
			{ seq: 7, query: query( 1010, 1000 ), drawn: START, now: 0 },
			world( { walkPredictable: false, walkHeld: true, holding: true } )
		),
		false
	);
	const released = world( {
		inputSeq: 7,
		moving: true,
		aim: { kind: "goal", destination: goal },
		poseAtMs: 190,
		revision: 1
	} );
	assert.equal( mover.frame( 200, released ).kind, "walking" );
	assert.equal( mover.frame( 300, released ).kind, "walking" );
	// The worker's leg is over: the drawn pose hands back at the goal.
	const standing = world( { inputSeq: 7, moving: false, pose: goal, poseAtMs: 280, revision: 2 } );
	mover.frame( 320, standing );
	const ended = mover.frame( 500, standing );
	assert.equal( ended.kind, "ended" );
	assert.ok( Math.abs( poseOf( ended ).x - 1010 ) < .05, `x ${poseOf( ended ).x}` );
	assert.equal( mover.active(), false );
});

test("a held click the worker took but does not walk leaves no trace (#1570)", () => {
	const mover = createLocalMover(), goal = { ...START, x: 1300 };
	mover.click(
		{ seq: 9, query: query( 1300, 1000 ), drawn: START, now: 0 },
		world( { walkPredictable: false, walkHeld: true, holding: true } )
	);
	// The gate resolves without a walk (refused at release): the record
	// is consumed with no trace.
	mover.frame( 100, world( { inputSeq: 9, moving: false, holding: false } ) );
	// A later walk for another input must not adopt the stale record.
	const frame = mover.frame(
		200,
		world( { inputSeq: 10, moving: true, aim: { kind: "goal", destination: goal }, poseAtMs: 190, revision: 1 } )
	);
	assert.equal( frame.kind, "idle" );
	assert.equal( mover.active(), false );
});

test("a fresh admitted click supersedes a held one (#1570)", () => {
	const mover = createLocalMover(), goal = { ...START, z: 1300 };
	mover.click(
		{ seq: 3, query: query( 1300, 1000 ), drawn: START, now: 0 },
		world( { walkPredictable: false, walkHeld: true, holding: true } )
	);
	// The gate cleared before the worker ever walked seq 3: seq 4 predicts.
	assert.equal( mover.click( { seq: 4, query: query( 1000, 1300 ), drawn: START, now: 50 }, world() ), true );
	const frame = mover.frame(
		150,
		world( { inputSeq: 4, moving: true, aim: { kind: "goal", destination: goal }, poseAtMs: 140, revision: 1 } )
	);
	assert.equal( frame.kind, "walking" );
	assert.equal( poseOf( frame ).x, START.x );
	assert.ok( poseOf( frame ).z > START.z, "walks the fresh click, not the held one" );
});

test("a later click the mover does not predict hands the walk back instead of dropping it", () => {
	const mover = createLocalMover();
	mover.click( { seq: 1, query: query( 1300, 1000 ), drawn: START, now: 0 }, world() );
	const walking = mover.frame( 100, world() );
	assert.equal( walking.kind, "walking" );
	// A second click while the worker says a click would not walk now.
	assert.equal(
		mover.click(
			{ seq: 2, query: query( 1000, 1300 ), drawn: walking.pose, now: 110 },
			world( { walkPredictable: false } )
		),
		false
	);
	const ended = mover.frame( 116, world() );
	assert.equal( ended.kind, "ended" );
	assert.deepEqual( ended.pose, walking.kind === "walking" ? walking.pose : null );
	assert.equal( mover.frame( 133, world() ).kind, "idle" );
});

test("a later command keeps the prediction while the worker walks a goal, and ends it on a walk with no aim", () => {
	const mover = createLocalMover(), goal = { ...START, x: 1300 };
	mover.click( { seq: 3, query: query( 1300, 1000 ), drawn: START, now: 0 }, world() );
	// A potion press (seq 4): the worker still walks the clicked goal.
	assert.equal(
		mover.frame(
			100,
			world( { inputSeq: 4, moving: true, aim: { kind: "goal", destination: goal }, poseAtMs: 90, revision: 1 } )
		)
			.kind,
		"walking"
	);
	// The worker now walks something it publishes no aim for (a skill
	// displacement): the mover cannot rebuild it.
	assert.equal(
		mover.frame( 116, world( { inputSeq: 5, moving: true, aim: undefined, poseAtMs: 106, revision: 2 } ) ).kind,
		"ended"
	);
});

test("no prediction without the worker's go-ahead or the worker's navigation", () => {
	const click = { seq: 1, query: query( 1300, 1000 ), drawn: START, now: 0 };
	assert.equal( createLocalMover().click( click, world( { walkPredictable: false } ) ), false );
	assert.equal( createLocalMover().click( click, world( { navigationRequestId: 2 } ) ), false );
	assert.equal( createLocalMover().click( click, world( { view: null } ) ), false );
	assert.equal( createLocalMover().click( click, world( { speed: undefined } ) ), false );
	// A view replaced mid-walk ends the prediction.
	const mover = createLocalMover();
	mover.click( click, world() );
	assert.equal(
		mover.frame( 100, world( { navigationRequestId: 2, view: { requestId: 2, navigation: flatView() } } ) ).kind,
		"ended"
	);
});

// A dash's authored segment (cast-displacement.ts kind 5/8 travel): 100
// units at 250 units a second, on the frame clock (local-mover's fixed aim).
const DASH = { from: START, to: { ...START, x: 1100 }, start: 0, duration: 400, fixedTiming: true };
const DASH_RATE = 250;

test("a fixed segment is drawn on its own clock from the prediction frame", () => {
	const mover = createLocalMover(), w = world();
	assert.equal(
		mover.predict( { seq: 0, aim: { kind: "fixed", segment: DASH }, speed: DASH_RATE, drawn: START, now: 0 }, w ),
		true
	);
	let now = 0, last = START;
	for ( const dt of FRAMES ) {
		now += dt;
		const frame = mover.frame( now, w );
		assert.equal( frame.kind, "walking" );
		const step = Math.hypot( frame.pose.x - last.x, frame.pose.z - last.z );
		// The cap follows the segment's authored rate, not walking speed.
		assert.ok( step <= DASH_RATE * 1.04 * dt / 1000 + .01, `step ${step} in ${dt} ms` );
		assert.ok( step > 0, "it moves every frame" );
		last = frame.pose;
	}
	// On the segment's clock, not on publication arrivals.
	assert.ok( Math.abs( last.x - (START.x + DASH_RATE * now / 1000) ) < 2, `at ${last.x} after ${now} ms` );
});

test("a fixed segment hands its destination back when it ends", () => {
	const mover = createLocalMover(), w = world();
	mover.predict( { seq: 0, aim: { kind: "fixed", segment: DASH }, speed: DASH_RATE, drawn: START, now: 0 }, w );
	let now = 0, frame;
	do {
		now += 16.7;
		frame = mover.frame( now, w );
	} while ( frame.kind === "walking" && now < 1000 );
	assert.equal( frame.kind, "ended" );
	assert.equal( frame.pose.x, DASH.to.x );
	assert.equal( frame.pose.z, DASH.to.z );
	assert.equal( mover.active(), false );
	assert.equal( mover.frame( now + 16.7, w ).kind, "idle" );
});

test("a rebuild mid-dash replays the same absolute segment, and a knockback ends it", () => {
	const mover = createLocalMover();
	mover.predict( { seq: 0, aim: { kind: "fixed", segment: DASH }, speed: DASH_RATE, drawn: START, now: 0 }, world() );
	const first = mover.frame( 100, world() );
	assert.equal( first.kind, "walking" );
	assert.ok( Math.abs( first.pose.x - 1025 ) < .5, `at ${first.pose.x}` );
	// The worker's answer: its pose sampled on the same segment at 90 ms.
	const rebuilt = mover.frame(
		200,
		world( {
			moving: true,
			aim: { kind: "fixed", segment: DASH },
			pose: { ...START, x: 1022.5 },
			poseAtMs: 90,
			revision: 1
		} )
	);
	assert.equal( rebuilt.kind, "walking" );
	// Still the segment's clock (1050 at 200 ms), never re-based.
	assert.ok( Math.abs( rebuilt.pose.x - 1050 ) < .5, `at ${rebuilt.pose.x}` );
	// A knockback lands: the worker walks something it publishes no aim
	// for, so the prediction ends through the presenter's carry.
	assert.equal(
		mover.frame( 216, world( { moving: true, aim: undefined, poseAtMs: 210, revision: 2 } ) ).kind,
		"ended"
	);
});

test("on the published Jangan navigation the predicted walk is the worker's clip", async () => {
	const bytes = path => assetBytes( path, CLIENT_PUBLIC_ROOT );
	const product = await createNavigationResources().resolve(
		bytes( "/assets/world/outdoor/regions/region-61a8.json" ),
		REGION,
		async path => bytes( path )
	);
	const navigation = createNavigation();
	navigation.install( REGION, product );
	const from = navigation.surface( { regionId: REGION, x: 1640, y: 0, z: 1390, angle: 0 } );
	const w = world( { pose: from, view: { requestId: 1, navigation } } );
	const mover = createLocalMover();
	// A pick that lands on the ground 40 units west (the flat-view query shape
	// does not reach this clipper's pick, so aim through the flat view and
	// walk on the real one).
	const view = flatView();
	const target = navigation.surface( { ...from, x: from.x - 40 } );
	assert.equal(
		mover.click(
			{ seq: 1, query: query( target.x, target.z ), drawn: from, now: 0 },
			{ ...w, view: { requestId: 1, navigation: { ...navigation, pick: view.pick } } }
		),
		true
	);
	let now = 0, pose = from;
	for ( let i = 0; i < 120; i++ ) {
		now += 16.7;
		const frame = mover.frame( now, w );
		if ( frame.kind !== "walking" ) break;
		const query = { slide: false, status: 0 };
		// Every drawn step is one the clipper accepts from the previous pose.
		const clipped = navigation.clip( pose, frame.pose, query );
		assert.ok( clipped, "covered" );
		pose = frame.pose;
	}
	assert.ok(
		Math.hypot( pose.x - Math.trunc( target.x ), pose.z - Math.trunc( target.z ) ) < 1,
		`arrived ${pose.x},${pose.z}`
	);
});
