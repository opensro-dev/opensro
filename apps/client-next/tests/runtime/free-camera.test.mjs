/*
===========================================================================

free-camera.test.mjs - the GM free camera's native fly (#1787)

While the free-camera byte (CGInterface +0x3CE) stands,
CGInterface_UpdatePointerTracking (68FE50) holds the look-at target
and flies it with five held-key axes, each eased by 68F5D0; the CApp
camera build (A1BB00) orbits the eye around the held target with the
live yaw, pitch and distance. One test per branch of the trace's table:
the fade ramp's arms, W/S and D/A translation scaled linearly by the
/cs speed (+0x7B0), the Left/Right and Up/Down nudges written back into
the input owner and kept after the byte clears, the Q/E roll the off
path zeroes, the chat-focus skip, the 3 s delta cap, and the held
target with its live orbit.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createWorldStream } = await import( "../../src/engine/runtime/world/world.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createInput } = await import( "../../src/engine/runtime/input/input.ts" );
const { followCameraQuery } = await import( "../../src/engine/foundation/rendering/follow-camera.ts" );
const freeCamera = await import( "../../src/engine/foundation/rendering/free-camera.ts" );

const VK = { W: 0x57, S: 0x53, A: 0x41, D: 0x44, Q: 0x51, E: 0x45, LEFT: 0x25, UP: 0x26, RIGHT: 0x27, DOWN: 0x28 };
const POSE = { regionId: 257, x: 100, y: 10, z: 200, angle: 0 };
const LEVEL = { yaw: 0, pitch: 0, distance: 80 };
const EPSILON = 1e-4;

/*
================
unexpectedFlyWork
================
*/
/** @returns {never} */
function unexpectedFlyWork() {
	throw new Error( "the free camera reached the asset owner" );
}

/*
================
flyFixture

A world stream whose renderer records the cameras it is given, with a
held-key set, a chat-focus switch and the yaw/pitch write-backs.
================
*/
function flyFixture( install = true ) {
	const cameras = [], turns = [], keys = new Set();
	const focus = { typing: false };
	/** @type {import("../../src/engine/contracts/assets.ts").AssetOwner} */
	const assets = {
		available: () => 0,
		take: () => null,
		progress: unexpectedFlyWork,
		health: unexpectedFlyWork,
		textureFeatures: unexpectedFlyWork,
		install: unexpectedFlyWork,
		dispose: unexpectedFlyWork,
		request: unexpectedFlyWork,
		cancel() {}
	};
	const world = createWorldStream(
		assets,
		/** @type {any} The stream only publishes cameras in this fixture. */ ({
			setWorldCamera: camera => cameras.push( camera ),
			cancelWorldUpdate() {},
			setWorld() {}
		}),
		"http://localhost",
		createPresentationRandom( 1 )
	);
	if ( install ) {
		world.setFreeCameraInput( {
			held: vk => keys.has( vk ),
			typing: () => focus.typing,
			turn: ( yaw, pitch ) => turns.push( [ yaw, pitch ] )
		} );
	}
	const last = () => cameras[cameras.length - 1];
	return { world, cameras, turns, keys, focus, last };
}

/*
================
liftedTarget

The follow's lifted target the fly seeds +0x42C from (68FA7A).
================
*/
function liftedTarget( pose, camera ) {
	return followCameraQuery( {
		eye: [ 0, 0, 0 ],
		target: [ pose.x, pose.y, pose.z ],
		follow: { yaw: camera.yaw, pitch: camera.pitch, distance: camera.distance, offset: [ 0, 0 ] },
		fov: 1,
		near: 1,
		far: 2
	}, null ).target;
}

/*
================
near
================
*/
function near( actual, expected, message ) {
	for ( let i = 0; i < expected.length; i++ ) {
		assert.ok(
			Math.abs( actual[i] - expected[i] ) < EPSILON,
			`${message}: [${actual}] is not [${expected}]`
		);
	}
}

/*
================
flyOneFrame

A follow frame at 0 ms, the byte set, then one fly frame 100 ms later
with the given keys held. Returns the fly frame's camera.
================
*/
function flyOneFrame( f, held, camera = LEVEL, speed = undefined ) {
	f.world.step( POSE, camera, undefined, undefined, 0 );
	f.world.setFreeCamera( true );
	if ( speed !== undefined ) f.world.setFreeCameraSpeed( speed );
	for ( const vk of held ) f.keys.add( vk );
	f.world.step( POSE, camera, undefined, undefined, 100 );
	return f.last();
}

/*
================
test("68F5D0: the fade ramp's five arms")
================
*/
test("68F5D0: the fade ramp's five arms", () => {
	const fade = freeCamera.stepFreeCameraFade, f = Math.fround;
	// s = 2 * dt. Only the first key: up by s, clamped at +1.
	assert.equal( fade( true, false, 0, .1 ), f( .2 ) );
	assert.equal( fade( true, false, .9, .1 ), 1 );
	// Only the second key: down by s, clamped at -1.
	assert.equal( fade( false, true, 0, .1 ), f( -.2 ) );
	assert.equal( fade( false, true, -.9, .1 ), -1 );
	// Both keys: the fade holds.
	assert.equal( fade( true, true, .5, .1 ), .5 );
	// Neither, positive: decays by s and stops at 0.
	assert.equal( fade( false, false, .5, .1 ), f( f( .5 ) - f( .2 ) ) );
	assert.equal( fade( false, false, .1, .1 ), 0 );
	// Neither, negative: rises by s and is clamped only at +1, so it
	// may cross 0 (68F685 faddp, then 68F5FB); the next frame's
	// positive arm brings it back to 0.
	assert.equal( fade( false, false, -.5, .1 ), f( -.5 + f( .2 ) ) );
	const crossed = fade( false, false, -.1, .1 );
	assert.ok( crossed > 0, `a small negative fade crossed to ${crossed}` );
	assert.equal( fade( false, false, crossed, .1 ), 0 );
	// Neither, at rest: unchanged.
	assert.equal( fade( false, false, 0, .1 ), 0 );
});

/*
================
test("68FE74/690146: with the byte clear the follow drives the camera")
================
*/
test("68FE74/690146: with the byte clear the follow drives the camera", () => {
	const f = flyFixture();
	f.keys.add( VK.W );
	f.world.step( POSE, LEVEL, undefined, undefined, 0 );
	f.world.step( POSE, LEVEL, undefined, undefined, 100 );
	assert.deepEqual( f.last().target, [ POSE.x, POSE.y, POSE.z ] );
	assert.ok( f.last().follow, "the follow spec rides along" );
	assert.equal( f.world.freeCamera(), false );
	assert.equal( f.turns.length, 0, "no fly runs with the byte clear" );
});

/*
================
test("690177..69020B/A1BB00: the target is held and the eye orbits it live")
================
*/
test("690177..69020B/A1BB00: the target is held and the eye orbits it live", () => {
	const f = flyFixture( false );
	f.world.step( POSE, LEVEL, undefined, undefined, 0 );
	f.world.setFreeCamera( true );
	assert.equal( f.world.freeCamera(), true );
	const held = liftedTarget( POSE, LEVEL );
	// The player walks away: the target stays where the follow left it.
	const away = { ...POSE, x: 900, z: -400 };
	f.world.step( away, LEVEL, undefined, undefined, 100 );
	near( f.last().target, held, "held target" );
	assert.equal( f.last().follow, undefined, "the follow is off while the byte stands" );
	// Yaw 0, pitch 0: the eye sits the distance behind on +Z (the port's
	// yaw is the native one minus pi), up is +Y.
	near( f.last().eye, [ held[0], held[1], held[2] + 80 ], "level eye" );
	near( f.last().up, [ 0, 1, 0 ], "level up" );
	// A live drag: the eye swings around the same target.
	const dragged = { yaw: Math.PI / 2, pitch: .5, distance: 80 };
	f.world.step( away, dragged, undefined, undefined, 200 );
	near( f.last().target, held, "held target after the drag" );
	near( f.last().eye, [
		held[0] + Math.cos( .5 ) * 80,
		held[1] + Math.sin( .5 ) * 80,
		held[2]
	], "orbited eye" );
	// A live wheel: the drawn distance (eased toward the wheel's, as the
	// follow's is) pulls the eye out along the same ray.
	f.world.step( away, { ...dragged, distance: 120 }, undefined, undefined, 300 );
	const eye = f.last().eye, out = Math.hypot( eye[0] - held[0], eye[1] - held[1], eye[2] - held[2] );
	assert.ok( out > 80 && out <= 120, `the wheel left the eye ${out} from the target` );
	near( [ (eye[0] - held[0]) / out, (eye[1] - held[1]) / out ], [ Math.cos( .5 ), Math.sin( .5 ) ], "same ray" );
	// The byte clears: the follow re-aims at the player.
	f.world.setFreeCamera( false );
	f.world.step( away, LEVEL, undefined, undefined, 300 );
	assert.deepEqual( f.last().target, [ away.x, away.y, away.z ] );
});

/*
================
test("690272/6902AD: W moves the target along the view by fade * speed * dt")
================
*/
test("690272/6902AD: W moves the target along the view by fade * speed * dt", () => {
	const f = flyFixture();
	const held = liftedTarget( POSE, LEVEL );
	const camera = flyOneFrame( f, [ VK.W ] );
	// fade 0.2 after 100 ms, speed 100 (BC9A2C), dt 0.1: 2 units toward
	// the target from the eye, which is -Z at yaw 0.
	near( camera.target, [ held[0], held[1], held[2] - 2 ], "W" );
	const g = flyFixture();
	near( flyOneFrame( g, [ VK.S ] ).target, [ held[0], held[1], held[2] + 2 ], "S" );
});

/*
================
test("6902AD/69040B: the translation scales linearly with the /cs speed")
================
*/
test("6902AD/69040B: the translation scales linearly with the /cs speed", () => {
	const held = liftedTarget( POSE, LEVEL );
	const moved = ( vk, speed ) => {
		const target = flyOneFrame( flyFixture(), [ vk ], LEVEL, speed ).target;
		return Math.hypot( target[0] - held[0], target[1] - held[1], target[2] - held[2] );
	};
	for ( const vk of [ VK.W, VK.D ] ) {
		const base = moved( vk, 100 );
		assert.ok( Math.abs( base - 2 ) < EPSILON, `speed 100 moved ${base}` );
		for ( const speed of [ 1, 250, 500 ] ) {
			const ratio = moved( vk, speed ) / base;
			assert.ok( Math.abs( ratio - speed / 100 ) < 1e-5, `speed ${speed}: ratio ${ratio}` );
		}
	}
});

/*
================
test("6903A9/69040B: D moves the target along the yaw's right vector")
================
*/
test("6903A9/69040B: D moves the target along the yaw's right vector", () => {
	const held = liftedTarget( POSE, LEVEL );
	// Yaw 0 looks down -Z; (1,0,0)*RotY(0 + pi) is -X, the screen right
	// of a camera on +Z in the left-handed world.
	near( flyOneFrame( flyFixture(), [ VK.D ] ).target, [ held[0] - 2, held[1], held[2] ], "D" );
	near( flyOneFrame( flyFixture(), [ VK.A ] ).target, [ held[0] + 2, held[1], held[2] ], "A" );
	// Yaw pi/2 (eye on +X, looking down -X): right is +Z.
	const turned = { ...LEVEL, yaw: Math.PI / 2 };
	const lifted = liftedTarget( POSE, turned );
	near( flyOneFrame( flyFixture(), [ VK.D ], turned ).target, [ lifted[0], lifted[1], lifted[2] + 2 ], "D turned" );
});

/*
================
test("69034F: Left/Right nudge the yaw, written back and kept after the byte clears")
================
*/
test("69034F: Left/Right nudge the yaw, written back and kept after the byte clears", () => {
	const f = flyFixture();
	flyOneFrame( f, [ VK.LEFT ] );
	// yaw -= 2 * fade * dt = 2 * 0.2 * 0.1.
	assert.equal( f.turns.length, 1 );
	assert.ok( Math.abs( f.turns[0][0] - -.04 ) < EPSILON, `yaw ${f.turns[0][0]}` );
	assert.equal( f.turns[0][1], 0, "Left leaves the pitch" );
	const g = flyFixture();
	flyOneFrame( g, [ VK.RIGHT ] );
	assert.ok( Math.abs( g.turns[0][0] - .04 ) < EPSILON, `yaw ${g.turns[0][0]}` );
	// The real input owner keeps the written yaw after the byte clears,
	// as App+0x374 keeps it natively.
	const input = createInput();
	input.setCameraAngles( -.04, .3 );
	assert.deepEqual( input.camera(), { yaw: -.04, pitch: .3, distance: 80 } );
});

/*
================
test("6904A8: Up/Down nudge the pitch, unclamped by the drag rails")
================
*/
test("6904A8: Up/Down nudge the pitch, unclamped by the drag rails", () => {
	const f = flyFixture();
	flyOneFrame( f, [ VK.UP ] );
	assert.ok( Math.abs( f.turns[0][1] - .04 ) < EPSILON, `pitch ${f.turns[0][1]}` );
	assert.equal( f.turns[0][0], 0, "Up leaves the yaw" );
	const g = flyFixture();
	flyOneFrame( g, [ VK.DOWN ] );
	assert.ok( Math.abs( g.turns[0][1] - -.04 ) < EPSILON, `pitch ${g.turns[0][1]}` );
	// Past the drag's 1.0708 rail: the fly writes it as it is (6904CB);
	// the next drag clamps it, as 67CDAF does.
	const steep = { ...LEVEL, pitch: 1.07 };
	const h = flyFixture();
	flyOneFrame( h, [ VK.UP ], steep );
	assert.ok( h.turns[0][1] > 1.0707963705062866, `pitch ${h.turns[0][1]} was clamped` );
	const input = createInput();
	input.setCameraAngles( 0, 1.5 );
	input.accept( { kind: "pointer", x: 0, y: 0, buttons: 2, timeMs: 1 } );
	input.accept( { kind: "pointer", x: 0, y: 1, buttons: 2, timeMs: 2 } );
	assert.equal( input.camera().pitch, 1.0707963705062866, "the next drag applies its rail" );
});

/*
================
test("6904FC/6913DB: Q/E roll the up vector; /camera off zeroes the roll")
================
*/
test("6904FC/6913DB: Q/E roll the up vector; /camera off zeroes the roll", () => {
	const f = flyFixture();
	const camera = flyOneFrame( f, [ VK.Q ] );
	const roll = Math.fround( .04 );
	// (0,1,0)*Rz(roll) at yaw pi (native), pitch 0: (-sin r, cos r, 0)
	// turned by RotY(pi) is (sin r, cos r, 0).
	near( camera.up, [ Math.sin( roll ), Math.cos( roll ), 0 ], "rolled up" );
	assert.equal( f.turns.length, 0, "the roll writes no yaw or pitch" );
	// Off and on again: the roll starts from 0. The fade is kept, so the
	// first frame back is a zero-delta one, which adds no roll.
	f.keys.clear();
	f.world.setFreeCamera( false );
	f.world.step( POSE, LEVEL, undefined, undefined, 200 );
	f.world.step( POSE, LEVEL, undefined, undefined, 300 );
	f.world.setFreeCamera( true );
	f.world.step( POSE, LEVEL, undefined, undefined, 300 );
	near( f.last().up, [ 0, 1, 0 ], "roll after /camera off" );
	const g = flyFixture();
	near( flyOneFrame( g, [ VK.E ] ).up, [ -Math.sin( roll ), Math.cos( roll ), 0 ], "E" );
});

/*
================
test("69016F: the fly is skipped while a text field has focus")
================
*/
test("69016F: the fly is skipped while a text field has focus", () => {
	const f = flyFixture();
	const held = liftedTarget( POSE, LEVEL );
	f.focus.typing = true;
	const camera = flyOneFrame( f, [ VK.W, VK.LEFT ] );
	near( camera.target, held, "typing holds the target" );
	assert.equal( f.turns.length, 0, "typing writes no yaw" );
	// Focus leaves: the fade starts from 0, not from frames spent typing.
	f.focus.typing = false;
	f.world.step( POSE, LEVEL, undefined, undefined, 200 );
	near( f.last().target, [ held[0], held[1], held[2] - 2 ], "first flown frame" );
});

/*
================
test("A0F533: the frame delta the fly reads is capped at 3 s")
================
*/
test("A0F533: the frame delta the fly reads is capped at 3 s", () => {
	const f = flyFixture();
	const held = liftedTarget( POSE, LEVEL );
	f.world.step( POSE, LEVEL, undefined, undefined, 0 );
	f.world.setFreeCamera( true );
	f.keys.add( VK.W );
	f.world.step( POSE, LEVEL, undefined, undefined, 10000 );
	// dt 3: the fade clamps at 1, so the move is 1 * 100 * 3.
	near( f.last().target, [ held[0], held[1], held[2] - 300 ], "capped delta" );
});

/*
================
test("A15AA0: the input owner reports held virtual keys until release")
================
*/
test("A15AA0: the input owner reports held virtual keys until release", () => {
	const input = createInput();
	input.accept( { kind: "key", code: "KeyW", down: true, timeMs: 1 } );
	input.accept( { kind: "key", code: "ArrowLeft", down: true, timeMs: 2 } );
	assert.equal( input.keyHeld( VK.W ), true );
	assert.equal( input.keyHeld( VK.LEFT ), true );
	assert.equal( input.keyHeld( VK.S ), false );
	input.accept( { kind: "key", code: "KeyW", down: false, timeMs: 3 } );
	assert.equal( input.keyHeld( VK.W ), false );
	// A focus release lets every key go.
	input.accept( { kind: "release", timeMs: 4 } );
	assert.equal( input.keyHeld( VK.LEFT ), false );
});
