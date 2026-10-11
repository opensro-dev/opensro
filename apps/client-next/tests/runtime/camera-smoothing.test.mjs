/*
===========================================================================

camera-smoothing.test.mjs - the Experimental smooth camera (port-only)

The owner's report (F4, local): the camera height "snaps, super minimal
but can be felt", on a click or a stop. Native 68F830 copies the drawn
height straight into the camera target, so the native camera shows every
presented height step. These tests pin the optional easing: a step
becomes continuous and settles, a slope is followed without lag, an
obstruction still pulls in at once, and every discontinuity snaps.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	CAMERA_HEIGHT_SNAP_UNITS,
	CAMERA_SMOOTH_MAX_STEP_SECONDS,
	beginCameraSmoothingFrame,
	createCameraSmoothing,
	smoothCameraDistance,
	smoothCameraHeight,
	smoothDamp
} = await import( "../../src/engine/foundation/rendering/camera-smoothing.ts" );

const FRAME = 1 / 60;

/*
================
run

Steps one smoothing record through frames of the given inputs; returns
the eased heights.
================
*/
function run( heights, { frame = FRAME, space = 0x6a62, mounted = false } = {} ) {
	const state = createCameraSmoothing(), out = [];
	heights.forEach( ( height, i ) => {
		const dt = beginCameraSmoothingFrame( state, i * frame, space, mounted );
		out.push( smoothCameraHeight( state, height, dt ) );
	} );
	return out;
}

/*
================
step
================
*/
test("a small height step becomes continuous: no frame moves more than a third of it, and it settles", () => {
	const input = [ ...Array( 30 ).fill( 100 ), ...Array( 60 ).fill( 100.6 ) ];
	const out = run( input );
	for ( let i = 1; i < out.length; i++ ) {
		assert.ok( Math.abs( out[i] - out[i - 1] ) <= 0.6 / 3 + 1e-9, `frame ${i} moved ${out[i] - out[i - 1]}` );
		assert.ok( out[i] >= out[i - 1] - 1e-12, "no backward motion, no overshoot" );
		assert.ok( out[i] <= 100.6 + 1e-12, "never past the target" );
	}
	assert.ok( Math.abs( out.at( -1 ) - 100.6 ) < 1e-3, `settled at ${out.at( -1 )}` );
	// Within a third of a second (20 frames) the step is 95% done.
	assert.ok( out[30 + 20] > 100 + 0.6 * 0.95, `${out[50]}` );
});

/*
================
slope
================
*/
test("walking up a slope is followed with a bounded, constant lag and no stair steps", () => {
	// 0.5 units of climb a frame: a steep slope at run speed.
	const input = Array.from( { length: 120 }, ( _, i ) => 50 + i * 0.5 );
	const out = run( input );
	const lag = input[input.length - 1] - out[out.length - 1];
	assert.ok( lag > 0 && lag < 4, `steady lag ${lag}` );
	for ( let i = 60; i < out.length; i++ ) {
		assert.ok( Math.abs( out[i] - out[i - 1] - 0.5 ) < 0.01, `frame ${i} step ${out[i] - out[i - 1]}` );
	}
});

/*
================
flat
================
*/
test("standing still on flat ground is the native camera exactly", () => {
	const out = run( Array( 50 ).fill( 37.25 ) );
	assert.ok( out.every( y => y === 37.25 ) );
});

/*
================
snap
================
*/
test("a teleport, a region or dungeon change, a mount and a long frame gap snap", () => {
	const jump = run( [ 10, 10, 10 + CAMERA_HEIGHT_SNAP_UNITS + 1 ] );
	assert.equal( jump[2], 10 + CAMERA_HEIGHT_SNAP_UNITS + 1, "a large jump snaps" );

	const state = createCameraSmoothing();
	smoothCameraHeight( state, 10, beginCameraSmoothingFrame( state, 0, 1, false ) );
	smoothCameraHeight( state, 10, beginCameraSmoothingFrame( state, FRAME, 1, false ) );
	assert.equal(
		smoothCameraHeight( state, 12, beginCameraSmoothingFrame( state, 2 * FRAME, 2, false ) ),
		12,
		"new space"
	);
	assert.equal(
		smoothCameraHeight( state, 14, beginCameraSmoothingFrame( state, 3 * FRAME, 2, true ) ),
		14,
		"mounted"
	);
	assert.equal(
		smoothCameraHeight(
			state,
			16,
			beginCameraSmoothingFrame( state, 3 * FRAME + CAMERA_SMOOTH_MAX_STEP_SECONDS + 0.01, 2, true )
		),
		16,
		"a long gap"
	);
	assert.equal(
		smoothCameraHeight( state, 18, beginCameraSmoothingFrame( state, 0, 2, true ) ),
		18,
		"time went back"
	);
});

/*
================
frameRate
================
*/
test("the easing is frame-rate independent: 30, 60 and 144 Hz land within a hair after the same time", () => {
	const at = hz => {
		const frames = Math.round( 0.2 * hz ) + 1;
		return run( [ 0, ...Array( frames ).fill( 1 ) ], { frame: 1 / hz } ).at( -1 );
	};
	const a = at( 30 ), b = at( 60 ), c = at( 144 );
	assert.ok( Math.abs( a - b ) < 0.03 && Math.abs( b - c ) < 0.03, `${a} ${b} ${c}` );
});

/*
================
distance
================
*/
test("an obstruction pulls the camera in at once; its release eases back out without overshoot", () => {
	const state = createCameraSmoothing();
	const frame = i => beginCameraSmoothingFrame( state, i * FRAME, 0, false );
	assert.equal( smoothCameraDistance( state, 150, frame( 0 ) ), 150 );
	assert.equal( smoothCameraDistance( state, 150, frame( 1 ) ), 150 );
	assert.equal( smoothCameraDistance( state, 60, frame( 2 ) ), 60, "pull-in is immediate" );
	const out = [];
	for ( let i = 3; i < 63; i++ ) out.push( smoothCameraDistance( state, 150, frame( i ) ) );
	assert.ok( out[0] < 80, `first release frame ${out[0]}` );
	for ( let i = 1; i < out.length; i++ ) assert.ok( out[i] >= out[i - 1] && out[i] <= 150 );
	assert.ok( 150 - out[out.length - 1] < 1, `released to ${out[out.length - 1]}` );
	assert.equal(
		smoothCameraDistance( state, 40, frame( 63 ) ),
		40,
		"a new obstruction mid-release pulls in at once"
	);
});

/*
================
damp
================
*/
test("smoothDamp never overshoots and holds on a zero step", () => {
	assert.deepEqual( smoothDamp( 3, 7, 0, 0.1, 0 ), [ 3, 0 ] );
	let x = 0, v = 0;
	for ( let i = 0; i < 200; i++ ) {
		[x, v] = smoothDamp( x, 10, v, 0.1, 0.05 );
		assert.ok( x <= 10 );
	}
	let y = 10, w = 0;
	for ( let i = 0; i < 200; i++ ) {
		[y, w] = smoothDamp( y, 0, w, 0.1, 0.05 );
		assert.ok( y >= 0 );
	}
	assert.equal( smoothCameraHeight( createCameraSmoothing(), Number.NaN, FRAME ), Number.NaN );
});
