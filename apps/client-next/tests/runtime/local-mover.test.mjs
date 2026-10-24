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
const { createLocalMover } = await import( "../../src/engine/runtime/characters/local-mover.ts" );
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
world
================
*/
function world( overrides = {} ) {
	return {
		pose: START,
		moving: false,
		walkPredictable: true,
		speed: SPEED,
		inputSeq: 0,
		navigationRequestId: 1,
		navigationOwner: undefined,
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

test("a right-angle re-click turns at the click frame from where the body is drawn", () => {
	const mover = createLocalMover(), w = world();
	mover.click( { seq: 1, query: query( 1300, 1000 ), drawn: START, now: 0 }, w );
	const before = mover.frame( 200, w );
	assert.equal( before.kind, "walking" );
	mover.click( { seq: 2, query: query( before.pose.x, 1300 ), drawn: before.pose, now: 200 }, w );
	const after = mover.frame( 300, w );
	assert.equal( after.kind, "walking" );
	assert.ok( Math.abs( after.pose.x - before.pose.x ) < .01, "no further travel along the old leg" );
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

test("a click the worker refused ends the prediction where the body is drawn", () => {
	const mover = createLocalMover();
	mover.click( { seq: 4, query: query( 1300, 1000 ), drawn: START, now: 0 }, world() );
	const walking = mover.frame( 50, world() );
	const ended = mover.frame( 66, world( { inputSeq: 4, moving: false } ) );
	assert.equal( ended.kind, "ended" );
	assert.deepEqual( ended.pose, walking.kind === "walking" ? walking.pose : null );
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
