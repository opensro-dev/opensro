/*
===========================================================================

local-pose-presentation.test.mjs - sampled characters are drawn on the frame clock

A walk sampled every 16 ms by the worker but delivered in bursts must still
advance evenly per frame, and a small server correction must glide rather
than snap. Loads the shipped module through the shared native loader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createPosePresentation } = await import(
	sourceFileUrl( "src/engine/runtime/characters/pose-presentation.ts" ).href
);

const GID = 7, REGION = 24744, SPEED = 50, FRAME = 1 / 60, STEP_MS = 16;

/*
================
walkAt

The worker's logical pose at simulation time ms of a straight walk.
================
*/
function walkAt( ms ) {
	return { regionId: REGION, x: 100 + SPEED * Math.min( ms, 4000 ) / 1000, y: 10, z: 500, angle: 0 };
}

/*
================
drive

Renders frames from 0.5 s to endSeconds. Worker samples exist every 16 ms
of simulation time, but the frame only sees what has been delivered:
deliveredAt(ms) is when the sample of simulation time ms reaches it.
================
*/
function drive( presentation, deliveredAt, endSeconds ) {
	const drawn = [];
	for ( let now = .5; now < endSeconds; now += FRAME ) {
		let latest = 0;
		for ( let ms = 0; ms <= now * 1000; ms += STEP_MS ) if ( deliveredAt( ms ) <= now ) latest = ms;
		presentation.samples( new Map( [ [ GID, { atMs: latest, moving: true, to: walkAt( 4000 ) } ] ] ) );
		drawn.push( presentation.pose( GID, walkAt( latest ), now ).x );
	}
	return drawn;
}

/*
================
steps
================
*/
function steps( xs ) {
	return xs.slice( 1 ).map( ( x, i ) => x - xs[i] );
}

test("bursty delivery still advances the local player evenly every frame", () => {
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	// Samples arrive 5-50 ms late, bunched: a busy main thread.
	const late = ms => ms / 1000 + .005 + (Math.floor( ms / 48 ) % 3) * .015;
	// The first samples carry no velocity yet; judge the steady walk.
	const deltas = steps( drive( presentation, late, 2 ) ).slice( 40 );
	const expected = SPEED * FRAME;
	for ( const delta of deltas ) assert.ok( Math.abs( delta - expected ) < expected * .05, `frame step ${delta}` );
});

test("the walk never runs past the leg end while samples stall", () => {
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	// Nothing new is delivered after 1 s of simulation time.
	const stalled = ms => ms <= 1000 ? ms / 1000 : Infinity;
	const drawn = drive( presentation, stalled, 1.5 );
	const limit = walkAt( 1000 ).x + SPEED * .1 + 1e-6;
	assert.ok( Math.max( ...drawn ) <= limit, `ran on to ${Math.max( ...drawn )}` );
});

test("a small stop correction glides back instead of snapping", () => {
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	let now = 0;
	for ( let ms = 0; ms <= 1000; ms += STEP_MS ) {
		now = ms / 1000;
		presentation.samples( new Map( [ [ GID, { atMs: ms, moving: true, to: walkAt( 4000 ) } ] ] ) );
		presentation.pose( GID, walkAt( ms ), now );
	}
	const before = presentation.pose( GID, walkAt( 1000 ), now ).x;
	// The server stops the player 3 units behind where the client walked.
	const corrected = { ...walkAt( 1000 ), x: walkAt( 1000 ).x - 3 };
	now += FRAME;
	presentation.samples( new Map( [ [ GID, { atMs: 1000 + STEP_MS, moving: false } ] ] ) );
	const first = presentation.pose( GID, corrected, now ).x;
	assert.ok( Math.abs( first - before ) < 1, `jumped ${first - before} in one frame` );
	for ( let i = 0; i < 30; i++ ) {
		now += FRAME;
		presentation.pose( GID, corrected, now );
	}
	assert.ok(
		Math.abs( presentation.pose( GID, corrected, now ).x - corrected.x ) < .01,
		"settles on the server pose"
	);
	assert.equal( presentation.moving( GID ), false );
});

test("a re-anchor while walking never becomes velocity: no backward frames of many units", () => {
	const presentation = createPosePresentation(), RENDER = .004, FAST = 100;
	presentation.origin( 0 );
	// A 100 unit/s walk; a receipt re-anchors it 19 units back 8 ms after the
	// previous sample, and the walk goes on from there (production 22.7 s).
	const at = ( ms, back ) => ({ ...walkAt( 0 ), x: 100 + FAST * ms / 1000 - back });
	const drawn = [];
	let latest = 0, revision = 1, back = 0;
	for ( let now = .5; now < 1.4; now += RENDER ) {
		const ms = Math.floor( now * 1000 );
		if ( revision === 1 && ms >= 1008 ) {
			revision = 2;
			back = 19;
			latest = ms;
		} else if ( ms - latest >= STEP_MS ) latest = ms - (ms - latest) % STEP_MS;
		presentation.samples( new Map( [ [ GID, { atMs: latest, revision, moving: true, to: at( 4000, back ) } ] ] ) );
		drawn.push( presentation.pose( GID, at( latest, back ), now ).x );
	}
	for ( const delta of steps( drawn ).slice( 20 ) ) {
		assert.ok( Math.abs( delta ) < 1.5, `frame step ${delta.toFixed( 2 )} units` );
	}
	assert.ok( Math.abs( drawn.at( -1 ) - at( 1400, 19 ).x ) < 1, "settles on the re-anchored walk" );
});

test("a relocation beyond the smoothing range snaps at once", () => {
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	presentation.samples( new Map( [ [ GID, { atMs: 0, moving: false } ] ] ) );
	presentation.pose( GID, walkAt( 0 ), 0 );
	const moved = { ...walkAt( 0 ), x: walkAt( 0 ).x + 150 };
	presentation.samples( new Map( [ [ GID, { atMs: STEP_MS, moving: false } ] ] ) );
	assert.equal( presentation.pose( GID, moved, FRAME ).x, moved.x );
});

test("a remote walker with timed samples is drawn as evenly as the local player", () => {
	const presentation = createPosePresentation(), REMOTE = 99;
	presentation.origin( 0 );
	const late = ms => ms / 1000 + .005 + (Math.floor( ms / 48 ) % 3) * .015;
	const drawn = [];
	for ( let now = .5; now < 2; now += FRAME ) {
		let latest = 0;
		for ( let ms = 0; ms <= now * 1000; ms += STEP_MS ) if ( late( ms ) <= now ) latest = ms;
		presentation.samples( new Map( [ [ REMOTE, { atMs: latest, moving: true, to: walkAt( 4000 ) } ] ] ) );
		drawn.push( presentation.pose( REMOTE, walkAt( latest ), now ).x );
	}
	const expected = SPEED * FRAME;
	for ( const delta of steps( drawn ).slice( 40 ) ) {
		assert.ok( Math.abs( delta - expected ) < expected * .05, `frame step ${delta}` );
	}
});

test("a character without timed samples keeps delivery interpolation", () => {
	const presentation = createPosePresentation();
	presentation.origin( 0 );
	presentation.samples( new Map() );
	const first = presentation.pose( 5, walkAt( 0 ), 0 );
	assert.equal( first.x, walkAt( 0 ).x );
	// Without timed samples the pose bridges toward a new target, never past it.
	const next = presentation.pose( 5, walkAt( 100 ), .05 );
	assert.ok( next.x >= walkAt( 0 ).x && next.x <= walkAt( 100 ).x );
});
