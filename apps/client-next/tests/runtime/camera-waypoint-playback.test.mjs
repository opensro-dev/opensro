/*
===========================================================================

camera-waypoint-playback.test.mjs - the /cw* track's playback (#1716 PR2)

TimedValue_Advance (4DF160) clamps the track clock to the last record's
time and samples the vector track and the four orientation channels at
it; while the play byte (+0x3CD) stands, 68FE50 applies the sampled
pose and 68F830 returns at once, so the follow stays frozen, and the
6900F8 tail clears the byte once the clock reaches the end, the final
pose staying applied. Nothing native ever loads the track, so the
curve basis is the port's recorded inference (camera-waypoints.ts).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const waypoints = await import( "../../src/engine/foundation/ui/camera-waypoints.ts" );
const { createWorldStream } = await import( "../../src/engine/runtime/world/world.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );

const IDENTITY = { x: 0, y: 0, z: 0, w: 1 };

/*
================
keyframe
================
*/
function keyframe( timeSeconds, x, y = 0, z = 0, orientation = IDENTITY ) {
	return { timeSeconds, position: { x, y, z }, orientation };
}

// Three records on the X axis, one second apart, identity aim (-Z).
const TRACK = [ keyframe( 0, 0 ), keyframe( 1, 10 ), keyframe( 2, 20 ) ];

/*
================
sampleAt

The sample at time on a non-empty track, which always has one; the empty
track's null is asserted by its own test.
================
*/
function sampleAt( track, time ) {
	const sample = waypoints.sampleCameraWaypoints( track, time );
	assert.ok( sample );
	return sample;
}

/*
================
test("an empty or single-record track samples its only pose or null")
================
*/
test("an empty or single-record track samples its only pose or null", () => {
	assert.equal( waypoints.sampleCameraWaypoints( [], 0 ), null );
	assert.equal( waypoints.sampleCameraWaypoints( [], 5 ), null );
	const single = [ keyframe( 3, 7, 8, 9 ) ];
	assert.deepEqual( sampleAt( single, 0 ).position, { x: 7, y: 8, z: 9 } );
	assert.deepEqual( sampleAt( single, 99 ).position, { x: 7, y: 8, z: 9 } );
});

/*
================
test("sampling clamps outside the ends and lands on records exactly")
================
*/
test("sampling clamps outside the ends and lands on records exactly", () => {
	assert.deepEqual( sampleAt( TRACK, -2 ).position, { x: 0, y: 0, z: 0 } );
	assert.deepEqual( sampleAt( TRACK, 0 ).position, { x: 0, y: 0, z: 0 } );
	assert.deepEqual( sampleAt( TRACK, 1 ).position, { x: 10, y: 0, z: 0 } );
	assert.deepEqual( sampleAt( TRACK, 2 ).position, { x: 20, y: 0, z: 0 } );
	assert.deepEqual( sampleAt( TRACK, 42 ).position, { x: 20, y: 0, z: 0 } );
	assert.deepEqual( sampleAt( TRACK, 1 ).orientation, IDENTITY );
});

/*
================
test("a mid-segment sample runs the four-point curve, not a lerp")
================
*/
test("a mid-segment sample runs the four-point curve, not a lerp", () => {
	// Segment 0 clamps P0 to the first record: P0..P3 = 0, 0, 10, 20 at
	// u = 0.5 gives 0.5 * (0 + 10*0.5 + 20*0.25 - 10*0.125) = 4.375,
	// where a lerp would give 5.
	const mid = sampleAt( TRACK, 0.5 );
	assert.equal( mid.position.x, 4.375 );
	assert.equal( mid.position.y, 0 );
	assert.equal( mid.position.z, 0 );
	// Segment 1 has all four records: 10, 20 with neighbours 0 and 20
	// clamped only at the far end - P0..P3 = 0, 10, 20, 20 at u = 0.5.
	assert.equal( sampleAt( TRACK, 1.5 ).position.x, 15.625 );
});

/*
================
test("the sampled orientation stays a unit quaternion")
================
*/
test("the sampled orientation stays a unit quaternion", () => {
	const turned = [
		keyframe( 0, 0, 0, 0, IDENTITY ),
		keyframe( 1, 0, 0, 0, { x: 0, y: 1, z: 0, w: 0 } ),
		keyframe( 2, 0, 0, 0, { x: 0, y: 0, z: 0, w: -1 } )
	];
	for ( const t of [ 0.25, 0.5, 0.75, 1.25, 1.5 ] ) {
		const o = sampleAt( turned, t ).orientation;
		const length = Math.hypot( o.x, o.y, o.z, o.w );
		assert.ok( Math.abs( length - 1 ) < 1e-9, `t=${t} length ${length}` );
	}
});

/*
================
unexpectedPlaybackWork

Playback never loads assets; reaching one of these is a test failure.
================
*/
/** @returns {never} */
function unexpectedPlaybackWork() {
	throw new Error( "camera playback reached the asset owner" );
}

/*
================
playbackFixture

A world stream whose renderer only records the cameras it is given.
================
*/
function playbackFixture() {
	const cameras = [];
	/** @type {import("../../src/engine/contracts/assets.ts").AssetOwner} */
	const assets = {
		available: () => 0,
		take: () => null,
		progress: unexpectedPlaybackWork,
		health: unexpectedPlaybackWork,
		textureFeatures: unexpectedPlaybackWork,
		install: unexpectedPlaybackWork,
		dispose: unexpectedPlaybackWork,
		request: unexpectedPlaybackWork,
		cancel() {}
	};
	const world = createWorldStream(
		assets,
		/** @type {any} The stream only publishes cameras and retires the world in this fixture. */ ({
			setWorldCamera: camera => cameras.push( camera ),
			cancelWorldUpdate() {},
			setWorld() {}
		}),
		"http://localhost",
		createPresentationRandom( 1 )
	);
	world.setCameraWaypoints( TRACK );
	return { world, cameras };
}

const POSE = { regionId: 257, x: 10, y: 20, z: 30, angle: 0 };

/*
================
test("playback drives the camera along the track and ends on the last pose")
================
*/
test("playback drives the camera along the track and ends on the last pose", () => {
	const { world, cameras } = playbackFixture();
	world.setCameraPlaying( true );
	// The first step applies the first record (zero delta, clock at 0):
	// identity aim looks along -Z from the record's position.
	world.step( POSE, undefined, undefined, undefined, 0 );
	assert.equal( cameras.length, 1 );
	assert.deepEqual( cameras[0].eye, [ 0, 0, 0 ] );
	assert.deepEqual( cameras[0].target, [ 0, 0, -1 ] );
	assert.deepEqual( cameras[0].up, [ 0, 1, 0 ] );
	assert.equal( cameras[0].follow, undefined, "the follow spec must not ride along during playback" );
	// Half a second in, the eye is the sampled curve point.
	world.step( POSE, undefined, undefined, undefined, 500 );
	assert.deepEqual( cameras[1].eye, [ 4.375, 0, 0 ] );
	// Past the end the clock clamps: the final pose is applied and the
	// play byte clears, so the next step is the follow camera again.
	world.step( POSE, undefined, undefined, undefined, 2500 );
	assert.deepEqual( cameras[2].eye, [ 20, 0, 0 ] );
	world.step( POSE, undefined, undefined, undefined, 3000 );
	assert.deepEqual( cameras[3].target, [ POSE.x, POSE.y, POSE.z ], "the follow resumed after the end clear" );
});

/*
================
test("stop freezes on the last applied pose while the free camera stands")
================
*/
test("stop freezes on the last applied pose while the free camera stands", () => {
	const { world, cameras } = playbackFixture();
	world.setFreeCamera( true );
	world.setCameraPlaying( true );
	world.step( POSE, undefined, undefined, undefined, 0 );
	world.step( POSE, undefined, undefined, undefined, 500 );
	assert.deepEqual( cameras[1].eye, [ 4.375, 0, 0 ] );
	// /cwstop: the byte clears and the camera keeps its last eye however
	// far the player walks (the free-camera hold re-applies lastCamera).
	world.setCameraPlaying( false );
	world.step( { ...POSE, x: 900 }, undefined, undefined, undefined, 1000 );
	assert.equal( cameras.length, 3 );
	assert.deepEqual( cameras[2].eye, [ 4.375, 0, 0 ] );
	// Playing again restarts the clock at the first record.
	world.setCameraPlaying( true );
	world.step( POSE, undefined, undefined, undefined, 1500 );
	assert.deepEqual( cameras[3].eye, [ 0, 0, 0 ] );
});

/*
================
test("clearing the track mid-play hands the camera back to the follow")
================
*/
test("clearing the track mid-play hands the camera back to the follow", () => {
	const { world, cameras } = playbackFixture();
	world.setCameraPlaying( true );
	world.step( POSE, undefined, undefined, undefined, 0 );
	assert.deepEqual( cameras[0].eye, [ 0, 0, 0 ] );
	world.setCameraWaypoints( [] );
	world.step( POSE, undefined, undefined, undefined, 500 );
	assert.deepEqual( cameras[1].target, [ POSE.x, POSE.y, POSE.z ] );
});
