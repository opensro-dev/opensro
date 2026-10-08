/*
===========================================================================

touch-camera.test.mjs - touch gestures drive the mouse camera input

A one-finger drag must reach the camera owner as a drag with the camera
button held, a pinch as wheel deltas, and a pinch must never fall back into
an orbit when one finger lifts.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createTouchCamera } = await import( "../../src/engine/foundation/rendering/touch-camera.ts" );

test("only an unmoved single touch can become a world tap", () => {
	const touch = createTouchCamera();
	touch.down( 1, 100, 100, 2 );
	assert.equal( touch.tap( 1, 100, 100 ), true );
	touch.move( 1, 130, 100, 2 );
	touch.move( 1, 100, 100, 2 );
	assert.equal( touch.tap( 1, 100, 100 ), false, "returning to the start does not restore a tap" );
	touch.up( 1 );
	touch.down( 1, 100, 100, 2 );
	touch.down( 2, 150, 100, 2 );
	touch.up( 2 );
	assert.equal( touch.tap( 1, 100, 100 ), false );
	touch.reset();
	assert.equal( touch.owns( 1 ), false );
	assert.equal( touch.tap( 1, 100, 100 ), false );
	assert.deepEqual( touch.move( 1, 130, 100, 2 ), [] );
	touch.down( 3, 100, 100, 2 );
	assert.equal( touch.tap( 3, 100, 100 ), true, "fresh input works after blur" );
	assert.equal( touch.tap( 3, 150, 100 ), false, "release displacement also cancels" );
});

test("a UI interruption latches through a pinch until every canvas finger lifts", () => {
	const touch = createTouchCamera();
	touch.down( 1, 100, 100, 2 );
	touch.down( 2, 200, 100, 2 );
	assert.deepEqual( touch.move( 2, 210, 100, 2 ), [ { kind: "wheel", delta: -50 } ] );
	assert.deepEqual( touch.interrupt(), [ { kind: "release" } ] );
	assert.deepEqual( touch.move( 2, 250, 100, 2 ), [], "interrupted two-finger movement cannot zoom" );
	touch.up( 2 );
	assert.deepEqual( touch.move( 1, 120, 100, 2 ), [], "remaining finger cannot orbit" );
	assert.equal( touch.tap( 1, 100, 100 ), false );
	assert.deepEqual( touch.down( 3, 200, 100, 2 ), [] );
	assert.deepEqual( touch.move( 3, 250, 100, 2 ), [], "a replacement finger cannot restart zoom" );
	touch.up( 3 );
	touch.up( 1 );
	touch.down( 4, 100, 100, 2 );
	assert.equal( touch.tap( 4, 100, 100 ), true );
	touch.down( 5, 200, 100, 2 );
	assert.deepEqual( touch.move( 5, 210, 100, 2 ), [ { kind: "wheel", delta: -50 } ], "a fresh pinch works" );
});

test("removing a third finger rebases the surviving pinch without a jump", () => {
	const touch = createTouchCamera();
	touch.down( 1, 100, 100, 2 );
	touch.down( 2, 200, 100, 2 );
	touch.down( 3, 400, 100, 2 );
	assert.deepEqual( touch.move( 2, 250, 100, 2 ), [] );
	touch.up( 1 );
	assert.deepEqual( touch.move( 2, 250, 100, 2 ), [] );
	assert.deepEqual( touch.move( 3, 410, 100, 2 ), [ { kind: "wheel", delta: -50 } ] );
});

test("one finger drags with the camera button and lifts with a release", () => {
	const touch = createTouchCamera();
	assert.deepEqual( touch.down( 1, 100, 100, 2 ), [ { kind: "pointer", x: 100, y: 100, buttons: 2 } ] );
	assert.deepEqual( touch.move( 1, 130, 100, 2 ), [ { kind: "pointer", x: 130, y: 100, buttons: 2 } ] );
	assert.deepEqual( touch.up( 1 ), [ { kind: "release" } ] );
	// Mouse mode 1 orbits on the primary button.
	assert.deepEqual( touch.down( 4, 0, 0, 1 ), [ { kind: "pointer", x: 0, y: 0, buttons: 1 } ] );
	assert.deepEqual( touch.move( 9, 5, 5, 1 ), [], "an unknown pointer is ignored" );
});

test("two fingers pinch into wheel deltas; spreading zooms in", () => {
	const touch = createTouchCamera();
	touch.down( 1, 100, 100, 2 );
	assert.deepEqual( touch.down( 2, 200, 100, 2 ), [ { kind: "release" } ], "the orbit stops" );
	/*
	================
	wheel
	================
	*/
	const wheel = outputs => {
		assert.equal( outputs.length, 1 );
		const [output] = outputs;
		if ( output?.kind !== "wheel" ) throw Error( "expected a wheel delta" );
		return output.delta;
	};
	assert.ok( wheel( touch.move( 2, 300, 100, 2 ) ) < 0, "fingers apart zoom in (negative is closer)" );
	assert.ok( wheel( touch.move( 2, 150, 100, 2 ) ) > 0, "fingers together zoom out" );
	assert.deepEqual( touch.move( 2, 150, 100, 2 ), [], "no movement, no wheel" );
	// Lifting one finger never turns the other into an orbit.
	assert.deepEqual( touch.up( 2 ), [] );
	assert.deepEqual( touch.move( 1, 120, 100, 2 ), [] );
	assert.deepEqual( touch.up( 1 ), [] );
	assert.deepEqual(
		touch.down( 1, 10, 10, 2 ),
		[ { kind: "pointer", x: 10, y: 10, buttons: 2 } ],
		"a new gesture orbits"
	);
});

test("the camera owner orbits on a touch drag and zooms on a pinch", async () => {
	const { createInput } = await import( "../../src/engine/runtime/input/input.ts" );
	const camera = createInput(), touch = createTouchCamera();
	const feed = outputs => {
		for ( const output of outputs ) camera.accept( { ...output, timeMs: 1 } );
	};
	const start = camera.camera();
	feed( touch.down( 1, 100, 100, 2 ) );
	feed( touch.move( 1, 140, 100, 2 ) );
	const orbited = camera.camera();
	assert.ok( Math.abs( orbited.yaw - start.yaw - 40 * 0.005 ) < 1e-9, "40 px of drag turns the camera" );
	feed( touch.up( 1 ) );
	feed( touch.down( 1, 100, 100, 2 ) );
	feed( touch.down( 2, 120, 100, 2 ) );
	feed( touch.move( 2, 220, 100, 2 ) );
	const zoomed = camera.camera();
	assert.equal( zoomed.yaw, orbited.yaw, "a pinch does not orbit" );
	assert.ok( zoomed.distance < orbited.distance, "spreading the fingers zooms in" );
});
