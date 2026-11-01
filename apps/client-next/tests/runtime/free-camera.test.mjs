/*
===========================================================================

free-camera.test.mjs - the free-fly arm of the native free camera (#1787)

CGInterface_UpdatePointerTracking's free-fly arm (690146): the fade
stepper (68F5D0) as its assembly reads, translations that scale with the
/cs speed (+0x7B0) and turns that never read it, the native right vector
(68F7E0) and the eye the composer (A1BB00) rebuilds from the rig.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	createFreeCameraFades,
	freeCameraEye,
	seedFreeCamera,
	stepFreeCamera,
	stepFreeCameraFade
} = await import( "../../src/engine/foundation/rendering/free-camera.ts" );

const NO_KEYS = {
	forward: false,
	back: false,
	strafeRight: false,
	strafeLeft: false,
	turnLeft: false,
	turnRight: false,
	pitchUp: false,
	pitchDown: false,
	rollPositive: false,
	rollNegative: false
};
/** @type {{ eye: [number, number, number], target: [number, number, number] }} */
const CAMERA = { eye: [ 40, 30, 90 ], target: [ 10, 0, 20 ] };
const FRAME_SECONDS = .05;

/*
================
fly

Runs frames of the arm with the given keys held and returns the rig.
================
*/
function fly( keys, frames, speed = 100, camera = CAMERA ) {
	const rig = seedFreeCamera( camera ), fades = createFreeCameraFades();
	for ( let i = 0; i < frames; i++ ) stepFreeCamera( rig, fades, { ...NO_KEYS, ...keys }, speed, FRAME_SECONDS );
	return { rig, fades };
}

/*
================
near
================
*/
function near( actual, expected, message ) {
	for ( let i = 0; i < expected.length; i++ ) {
		assert.ok( Math.abs( actual[i] - expected[i] ) < 1e-4, `${message}: ${actual} vs ${expected}` );
	}
}

/*
================
test("the fade stepper ramps, rails, decays and holds as 68F5D0 does")
================
*/
test("the fade stepper ramps, rails, decays and holds as 68F5D0 does", () => {
	// step = 2 * dt: a quarter second moves the fade by half.
	assert.equal( stepFreeCameraFade( true, false, 0, .25 ), .5 );
	assert.equal( stepFreeCameraFade( true, false, .75, .25 ), 1, "the positive rail is 1" );
	assert.equal( stepFreeCameraFade( false, true, -.75, .25 ), -1, "the negative rail is -1" );
	assert.equal( stepFreeCameraFade( false, false, .25, .25 ), 0, "decay stops at 0, not past it" );
	assert.equal( stepFreeCameraFade( false, false, -.25, .25 ), 0 );
	assert.equal( stepFreeCameraFade( false, false, .75, .25 ), .25 );
	assert.equal( stepFreeCameraFade( true, true, .3, .25 ), .3, "both keys leave the fade alone" );
});

/*
================
test("the first free frame draws the camera it was seeded from")
================
*/
test("the first free frame draws the camera it was seeded from", () => {
	const rig = seedFreeCamera( CAMERA );
	near( freeCameraEye( rig ).eye, CAMERA.eye, "seeded eye" );
	// Level: a unit up, perpendicular to the view, with no sideways lean.
	const up = freeCameraEye( rig ).up, view = CAMERA.target.map( ( v, i ) => v - CAMERA.eye[i] );
	assert.ok( Math.abs( Math.hypot( ...up ) - 1 ) < 1e-6 && up[1] > 0 );
	assert.ok( Math.abs( up[0] * view[0] + up[1] * view[1] + up[2] * view[2] ) < 1e-4 );
	assert.ok( Math.abs( up[0] * view[2] - up[2] * view[0] ) < 1e-4, "up leans only along the view" );
	const { rig: idle } = fly( {}, 10 );
	assert.equal( idle.moved, false );
	near( idle.target, CAMERA.target, "no key, no move" );
});

/*
================
test("W and S move along the view, and the distance scales with the /cs speed")
================
*/
test("W and S move along the view, and the distance scales with the /cs speed", () => {
	const view = CAMERA.target.map( ( v, i ) => v - CAMERA.eye[i] ), length = Math.hypot( ...view );
	const moved = speed => {
		const { rig } = fly( { forward: true }, 20, speed );
		return rig.target.map( ( v, i ) => v - CAMERA.target[i] );
	};
	const slow = moved( 100 ), fast = moved( 300 );
	// 20 frames of 50 ms: the fade climbs 0.1 a frame to its rail, so the
	// target travels sum(fade) * speed * dt along the unit view.
	let fade = 0, travelled = 0;
	for ( let i = 0; i < 20; i++ ) {
		fade = Math.min( fade + .1, 1 );
		travelled += fade * 100 * FRAME_SECONDS;
	}
	near( slow, view.map( v => v / length * travelled ), "W moves along the view" );
	near( fast, slow.map( v => v * 3 ), "three times the speed, three times the travel" );
	const { rig: back } = fly( { back: true }, 20 );
	near( back.target, CAMERA.target.map( ( v, i ) => v - slow[i] ), "S mirrors W" );
});

/*
================
test("A and D strafe along the native right vector, scaled by the speed")
================
*/
test("A and D strafe along the native right vector, scaled by the speed", () => {
	const seeded = seedFreeCamera( CAMERA );
	// 68F7E0: (1, 0, 0) * RotY(native yaw), native yaw = client yaw + pi.
	const nativeYaw = seeded.yaw + Math.PI;
	const right = [ Math.cos( nativeYaw ), 0, -Math.sin( nativeYaw ) ];
	const { rig } = fly( { strafeRight: true }, 20, 200 );
	const moved = rig.target.map( ( v, i ) => v - CAMERA.target[i] ), length = Math.hypot( ...moved );
	near( moved.map( v => v / length ), right, "D slides along the right vector" );
	const { rig: left } = fly( { strafeLeft: true }, 20, 200 );
	near( left.target, CAMERA.target.map( ( v, i ) => v - moved[i] ), "A mirrors D" );
	const { rig: slow } = fly( { strafeRight: true }, 20, 100 );
	near( slow.target.map( ( v, i ) => (v - CAMERA.target[i]) * 2 ), moved, "half the speed, half the slide" );
});

/*
================
test("the turns step 2 * fade * dt and never read the speed")
================
*/
test("the turns step 2 * fade * dt and never read the speed", () => {
	const seeded = seedFreeCamera( CAMERA );
	// One 50 ms frame from a zero fade: the fade becomes 0.1 and the
	// angle moves 2 * 0.1 * 0.05.
	const turn = 2 * .1 * FRAME_SECONDS;
	for ( const speed of [ 1, 500 ] ) {
		assert.ok(
			Math.abs( fly( { turnLeft: true }, 1, speed ).rig.yaw - (seeded.yaw - turn) ) < 1e-6,
			"Left turns yaw down"
		);
		assert.ok(
			Math.abs( fly( { pitchUp: true }, 1, speed ).rig.pitch - (seeded.pitch + turn) ) < 1e-6,
			"Up raises pitch"
		);
		assert.ok( Math.abs( fly( { rollPositive: true }, 1, speed ).rig.roll - turn ) < 1e-6, "Q rolls up" );
		near( fly( { turnLeft: true, pitchUp: true }, 1, speed ).rig.target, CAMERA.target, "turns never translate" );
	}
});

/*
================
test("roll turns the up vector about the view and leaves the eye")
================
*/
test("roll turns the up vector about the view and leaves the eye", () => {
	const level = seedFreeCamera( CAMERA ), rolled = { ...level, roll: .5 };
	const a = freeCameraEye( level ), b = freeCameraEye( rolled );
	near( b.eye, a.eye, "roll leaves the eye" );
	const view = level.target.map( ( v, i ) => v - a.eye[i] );
	const dot = ( u, w ) => u[0] * w[0] + u[1] * w[1] + u[2] * w[2];
	assert.ok( Math.abs( dot( b.up, view ) ) < 1e-6, "up stays perpendicular to the view" );
	assert.ok( Math.abs( dot( a.up, b.up ) - Math.cos( .5 ) ) < 1e-6, "up turns by the roll" );
});
