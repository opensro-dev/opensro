/*
===========================================================================

debug-camera.test.mjs - the GM console's camera rows (/zoom, /camera, /ms, /colon)

690C40 cases 0x10, 0x14 and 0x1E, ids 118/119: /zoom flips the
interface's zoom byte (+0x3CF) behind the wheel's 150-unit rail,
/camera flips the free-camera byte (+0x3CE) and reports it, /ms flips
the cursor manager's visibility (+0x20), /colon and /coloff write the
camera-collision byte (+0x3CC) that gates the clamp in
ApplyCameraOrbit (68FAD0); the follow never reads it. Each byte
lives with its owner; the console only flips and forwards.
Nothing reaches the server.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const { cameraWheelDelta, zoomCamera } = await import( "../../src/engine/foundation/rendering/camera-wheel.ts" );
// One wheel detent each way through the platform adapter (120 units).
const WHEEL_OUT = cameraWheelDelta( { deltaY: 100, deltaMode: 0 } );
const WHEEL_IN = cameraWheelDelta( { deltaY: -100, deltaMode: 0 } );
const { createInput } = await import( "../../src/engine/runtime/input/input.ts" );
const { createWorldStream } = await import( "../../src/engine/runtime/world/world.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );

/*
================
test("the wheel rail is the zoom byte: ceiling only, floor always")
================
*/
test("the wheel rail is the zoom byte: ceiling only, floor always", () => {
	// +0x3CF stands (constructor default): the 150 ceiling applies.
	assert.equal( zoomCamera( 149, WHEEL_OUT ), 150 );
	assert.equal( zoomCamera( 80, WHEEL_OUT ), 86 );
	// The byte clear: the wheel pulls past 150, the 10 floor still holds.
	assert.equal( zoomCamera( 149, WHEEL_OUT, false ), 155 );
	assert.equal( zoomCamera( 11, WHEEL_IN, false ), 10 );
	assert.equal( zoomCamera( 11, WHEEL_IN ), 10 );
});

/*
================
test("the input owner zooms to the rail until /zoom clears it")
================
*/
test("the input owner zooms to the rail until /zoom clears it", () => {
	const input = createInput();
	let now = 0;
	const wheel = delta => input.accept( { kind: "wheel", delta, timeMs: ++now } );
	assert.equal( input.camera().distance, 80 );
	for ( let i = 0; i < 12; i++ ) wheel( 120 );
	assert.equal( input.camera().distance, 150, "the constructor's rail caps the wheel" );
	input.zoomRail( false );
	for ( let i = 0; i < 20; i++ ) wheel( 120 );
	assert.equal( input.camera().distance, 270, "with the byte clear the wheel pulls past the rail" );
	input.zoomRail( true );
	for ( let i = 0; i < 60; i++ ) wheel( -120 );
	assert.equal( input.camera().distance, 10, "the floor holds in both states" );
});

/*
================
cameraUiFixture

The production HUD with the three camera sinks recorded.
================
*/
function cameraUiFixture( t, gm = true ) {
	/** @type {any[]} */
	const sent = [];
	const rail = [], free = [], cursor = [], modes = [], collision = [], speeds = [];
	const f = uiFixture(
		message => {
			if ( message.kind === "gameplay" ) sent.push( message.command );
		},
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		{
			setCameraZoomRail: value => rail.push( value ),
			setFreeCamera: value => free.push( value ),
			setFreeCameraSpeed: value => speeds.push( value ),
			setCameraCollision: value => collision.push( value ),
			setCursorVisible: value => cursor.push( value ),
			setRenderMode: ( mode, enabled ) => modes.push( [ mode, enabled ] )
		}
	);
	t.after( () => f.dispose() );
	f.state.gameplay.eligibility = { gm };
	let now = 0;
	/*
	================
	draw
	================
	*/
	function draw() {
		for ( let frame = 0; frame < 16; frame++ ) f.ui.step( f.state, now += 100 );
	}
	/*
	================
	typeLine
	================
	*/
	function typeLine( line ) {
		f.ui.event( {
			kind: "edit",
			id: "gm-input",
			value: line,
			start: line.length,
			end: line.length,
			composing: false
		} );
		f.ui.event( { kind: "activate", id: "submit" } );
		draw();
	}
	draw();
	f.ui.event( { kind: "key", code: "Backquote", shift: true } );
	draw();
	return { ...f, sent, rail, free, cursor, modes, collision, speeds, typeLine };
}

/*
================
test("the GM console flips the camera bytes and reports only /camera")
================
*/
test("the GM console flips the camera bytes and reports only /camera", t => {
	const f = cameraUiFixture( t );
	f.typeLine( "/zoom" );
	assert.deepEqual( f.rail, [ false ] );
	assert.ok( !f.hasText( "Free Camera On" ) && !f.hasText( "Free Camera Off" ), "/zoom prints nothing" );
	f.typeLine( "/zoom" );
	assert.deepEqual( f.rail, [ false, true ] );
	f.typeLine( "/camera" );
	assert.deepEqual( f.free, [ true ] );
	assert.ok( f.hasText( "Free Camera On" ) );
	f.typeLine( "/camera" );
	assert.deepEqual( f.free, [ true, false ] );
	assert.ok( f.hasText( "Free Camera Off" ) );
	f.typeLine( "/ms" );
	assert.deepEqual( f.cursor, [ false ] );
	f.typeLine( "/ms" );
	assert.deepEqual( f.cursor, [ false, true ] );
	// /colon and /coloff are silent, like /zoom: the collision byte
	// flips and nothing is printed.
	f.typeLine( "/coloff" );
	assert.deepEqual( f.collision, [ false ] );
	f.typeLine( "/colon" );
	assert.deepEqual( f.collision, [ false, true ] );
	assert.deepEqual( f.sent, [], "a camera row reached the server" );
});

/*
================
test("the GM console negates the render family's graphic options")
================
*/
test("the GM console negates the render family's graphic options", t => {
	const f = cameraUiFixture( t );
	// SWorld_Construct seeds wireframe clear and the five others standing:
	// the first press turns /wire on and everything else off. The native
	// prints nothing for any of the six.
	f.typeLine( "/wire" );
	f.typeLine( "/ground" );
	f.typeLine( "/sky" );
	f.typeLine( "/mobj" );
	f.typeLine( "/char" );
	f.typeLine( "/lod" );
	assert.deepEqual( f.modes, [
		[ 0x12, true ],
		[ 0x13, false ],
		[ 0x14, false ],
		[ 0x15, false ],
		[ 0x16, false ],
		[ 0x17, false ]
	] );
	f.typeLine( "/wire" );
	f.typeLine( "/ground" );
	f.typeLine( "/lod" );
	assert.deepEqual( f.modes.slice( 6 ), [
		[ 0x12, false ],
		[ 0x13, true ],
		[ 0x17, true ]
	] );
	assert.deepEqual( f.sent, [], "a render row reached the server" );
});

/*
================
test("a player without GM eligibility flips no render option")
================
*/
test("a player without GM eligibility flips no render option", t => {
	const f = cameraUiFixture( t, false );
	f.typeLine( "/wire" );
	f.typeLine( "/ground" );
	f.typeLine( "/char" );
	assert.deepEqual( f.modes, [] );
	assert.deepEqual( f.sent, [] );
});

/*
================
test("a player without GM eligibility flips no camera byte")
================
*/
test("a player without GM eligibility flips no camera byte", t => {
	const f = cameraUiFixture( t, false );
	f.typeLine( "/zoom" );
	f.typeLine( "/camera" );
	f.typeLine( "/ms" );
	f.typeLine( "/coloff" );
	f.typeLine( "/colon" );
	assert.deepEqual( f.rail, [] );
	assert.deepEqual( f.free, [] );
	assert.deepEqual( f.cursor, [] );
	assert.deepEqual( f.collision, [] );
	assert.deepEqual( f.sent, [] );
});

/*
================
unexpectedCameraWork

Only camera publication and world retirement are expected in this fixture.
================
*/
/** @returns {never} */
function unexpectedCameraWork() {
	assert.fail( "the camera fixture must not perform unrelated asset or renderer work" );
}

/*
================
cameraWorldFixture

The world camera with a recorded renderer (the debug-field-of-view
fixture's shape): projection continues while assets are unavailable.
================
*/
function cameraWorldFixture( t ) {
	let camera;
	const collisionFlags = [];
	/** @type {import("../../src/engine/contracts/assets.ts").AssetOwner} */
	const assets = {
		available: () => 0,
		take: () => null,
		progress: unexpectedCameraWork,
		health: unexpectedCameraWork,
		textureFeatures: unexpectedCameraWork,
		install: unexpectedCameraWork,
		dispose: unexpectedCameraWork,
		request: unexpectedCameraWork,
		/*
		================
		cancel
		================
		*/
		cancel() {}
	};
	/** @type {import("../../src/engine/contracts/runtime.ts").Renderer} */
	const renderer = {
		dispose: unexpectedCameraWork,
		scenery: unexpectedCameraWork,
		gpuTiming: unexpectedCameraWork,
		adapterFacts: unexpectedCameraWork,
		videoOptions: unexpectedCameraWork,
		experimentalVideo: unexpectedCameraWork,
		setSelectionDecal: unexpectedCameraWork,
		setWorldRenderMode: unexpectedCameraWork,
		setFootprints: unexpectedCameraWork,
		setSunDropMarkers: unexpectedCameraWork,
		pickGround: unexpectedCameraWork,
		presentationCamera: unexpectedCameraWork,
		audioListener: unexpectedCameraWork,
		pickDestination: unexpectedCameraWork,
		pickFrontendCharacter: unexpectedCameraWork,
		pickFrontendRace: unexpectedCameraWork,
		frontendRaceCenters: unexpectedCameraWork,
		setCharacterPreview: unexpectedCameraWork,
		characterParticleSnapshot: unexpectedCameraWork,
		characterParticleTime: unexpectedCameraWork,
		presentationNight: unexpectedCameraWork,
		characterLocalMatrix: unexpectedCameraWork,
		characterMatrix: unexpectedCameraWork,
		characterSocket: unexpectedCameraWork,
		pickEntity: unexpectedCameraWork,
		setWeather: unexpectedCameraWork,
		setWorldClock: unexpectedCameraWork,
		setUi: unexpectedCameraWork,
		setDamageText: unexpectedCameraWork,
		setUiTexture: unexpectedCameraWork,
		setUiAtlas: unexpectedCameraWork,
		retainCharacterModels: unexpectedCameraWork,
		setCharacterAssembly: unexpectedCameraWork,
		characterStats: unexpectedCameraWork,
		setCharacterModel: unexpectedCameraWork,
		setCharacterAnimation: unexpectedCameraWork,
		setTeleportGates: unexpectedCameraWork,
		setCharacterActors: unexpectedCameraWork,
		characterActors: unexpectedCameraWork,
		worldView: unexpectedCameraWork,
		adoptWorld: unexpectedCameraWork,
		textureFeatures: unexpectedCameraWork,
		settleWorld: unexpectedCameraWork,
		setWorldTexture: unexpectedCameraWork,
		neededWorldTextures: unexpectedCameraWork,
		worldStats: unexpectedCameraWork,
		setGeometryInstances: unexpectedCameraWork,
		setGeometryTransform: unexpectedCameraWork,
		setGeometry: unexpectedCameraWork,
		setImage: unexpectedCameraWork,
		frame: unexpectedCameraWork,
		phase: unexpectedCameraWork,
		error: unexpectedCameraWork,
		/*
		================
		cancelWorldUpdate
		================
		*/
		cancelWorldUpdate() {},
		/*
		================
		setWorldCamera
		================
		*/
		setWorldCamera( value ) {
			camera = value;
		},
		/*
		================
		setWorldCameraCollision
		================
		*/
		setWorldCameraCollision( value ) {
			collisionFlags.push( value );
		},
		/*
		================
		setWorld
		================
		*/
		setWorld() {}
	};
	const stream = createWorldStream(
		assets,
		renderer,
		"https://fixture.invalid/",
		createPresentationRandom( 1 )
	);
	t.after( () => stream.dispose() );
	/*
	================
	draw
	================
	*/
	function draw( pose, nowMs = 0 ) {
		stream.step( pose, undefined, undefined, undefined, nowMs );
		assert.equal( stream.error(), null );
		assert.ok( camera );
		return camera;
	}
	return { stream, draw, collisionFlags };
}

/*
================
test("the free camera holds its last eye and target until it re-aims")
================
*/
test("the free camera holds its last eye and target until it re-aims", t => {
	const f = cameraWorldFixture( t );
	const home = { regionId: 1, x: 10, y: 0, z: 20, angle: 0 };
	const away = { regionId: 1, x: 110, y: 5, z: 220, angle: 0 };
	const standing = f.draw( home );
	assert.deepEqual( standing.target, [ 10, 0, 20 ] );
	f.stream.setFreeCamera( true );
	// 68FAD0 applies nothing while +0x3CE stands: walking away moves
	// neither the eye nor the target.
	const frozen = f.draw( away );
	assert.deepEqual( frozen.eye, standing.eye );
	assert.deepEqual( frozen.target, standing.target );
	assert.deepEqual( frozen.follow, standing.follow );
	// Clearing the byte re-aims: the next step follows the player again.
	f.stream.setFreeCamera( false );
	const reaimed = f.draw( away );
	assert.deepEqual( reaimed.target, [ 110, 5, 220 ] );
});

/*
================
test("the collision byte forwards to the renderer and the follow keeps tracking")
================
*/
test("the collision byte forwards to the renderer and the follow keeps tracking", t => {
	const f = cameraWorldFixture( t );
	const home = { regionId: 1, x: 10, y: 0, z: 20, angle: 0 };
	const away = { regionId: 1, x: 110, y: 5, z: 220, angle: 0 };
	const standing = f.draw( home );
	assert.deepEqual( standing.target, [ 10, 0, 20 ] );
	f.stream.setCameraCollision( false );
	assert.deepEqual( f.collisionFlags, [ false ] );
	// 68F830 never reads +0x3CC: walking away still moves the target.
	// Only the renderer's clamp is off (the follow-camera tests pin
	// its requested-distance arm against a wall).
	const moved = f.draw( away );
	assert.deepEqual( moved.target, [ 110, 5, 220 ] );
	assert.notDeepEqual( moved.eye, standing.eye );
	f.stream.setCameraCollision( true );
	assert.deepEqual( f.collisionFlags, [ false, true ] );
	const still = f.draw( away );
	assert.deepEqual( still.target, [ 110, 5, 220 ] );
});

/*
================
test("/cs forwards its clamped speed to the camera owner")
================
*/
test("/cs forwards its clamped speed to the camera owner", t => {
	const f = cameraUiFixture( t );
	f.typeLine( "/cs 250" );
	f.typeLine( "/cs 9999" );
	// One word only: a bare or two-word line writes nothing (690C40 case 136).
	f.typeLine( "/cs" );
	f.typeLine( "/cs 1 2" );
	assert.deepEqual( f.speeds, [ 250, 500 ] );
	assert.deepEqual( f.sent, [] );
});

/*
================
flyForward

Holds W for one second in 100 ms steps from a free camera seeded at the
standing follow, and returns the camera the last step drew.
================
*/
/**
 * @param {any} f
 * @param {any} pose
 * @param {object | null} [keys]
 */
function flyForward( f, pose, keys = { forward: true } ) {
	f.stream.setFreeCamera( true );
	let camera = f.draw( pose, 1000 );
	for ( let now = 1100; now <= 2000; now += 100 ) {
		f.stream.setFreeCameraKeys( keys === null ? null : { ...NO_FREE_KEYS, ...keys } );
		camera = f.draw( pose, now );
	}
	return camera;
}

const NO_FREE_KEYS = {
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

/*
================
test("the free camera flies along its view, scaled by the /cs speed")
================
*/
test("the free camera flies along its view, scaled by the /cs speed", t => {
	const home = { regionId: 1, x: 10, y: 0, z: 20, angle: 0 };
	const travel = speed => {
		const f = cameraWorldFixture( t );
		const standing = f.draw( home );
		if ( speed !== undefined ) f.stream.setFreeCameraSpeed( speed );
		const flown = flyForward( f, home );
		const view = standing.target.map( ( v, i ) => v - standing.eye[i] );
		const moved = flown.target.map( ( v, i ) => v - standing.target[i] );
		const length = Math.hypot( ...moved );
		// The target moves along the view (target - eye), and the eye with it.
		const cos = moved.reduce( ( sum, v, i ) => sum + v * view[i], 0 ) / (length * Math.hypot( ...view ));
		assert.ok( cos > .9999, `moved off the view: ${cos}` );
		assert.equal( flown.follow, undefined, "a moved free camera is not clamped" );
		return length;
	};
	const seeded = travel( undefined ), slow = travel( 100 ), fast = travel( 250 );
	assert.ok( slow > 0 );
	assert.ok( Math.abs( seeded - slow ) < 1e-3, "the constructor seeds +0x7B0 with 100" );
	assert.ok( Math.abs( fast / slow - 2.5 ) < 1e-3, `speed 250 flew ${fast / slow} times speed 100` );
});

/*
================
test("the free camera holds while a text field owns the keyboard")
================
*/
test("the free camera holds while a text field owns the keyboard", t => {
	const f = cameraWorldFixture( t );
	const home = { regionId: 1, x: 10, y: 0, z: 20, angle: 0 };
	const standing = f.draw( home );
	const held = flyForward( f, home, null );
	assert.deepEqual( held.eye, standing.eye );
	assert.deepEqual( held.target, standing.target );
	assert.deepEqual( held.follow, standing.follow );
});

/*
================
test("re-entering the free camera starts again from the follow")
================
*/
test("re-entering the free camera starts again from the follow", t => {
	const f = cameraWorldFixture( t );
	const home = { regionId: 1, x: 10, y: 0, z: 20, angle: 0 };
	const away = { regionId: 1, x: 110, y: 5, z: 220, angle: 0 };
	f.draw( home );
	const flown = flyForward( f, home );
	f.stream.setFreeCameraKeys( NO_FREE_KEYS );
	f.stream.setFreeCamera( false );
	const following = f.draw( away, 3000 );
	assert.deepEqual( following.target, [ 110, 5, 220 ] );
	assert.notDeepEqual( following.target, flown.target );
	// The fades persist on the interface: the next entry's first frames
	// still drift until they decay, but from the follow's camera.
	f.stream.setFreeCamera( true );
	const entered = f.draw( away, 3100 );
	assert.deepEqual( entered.target, following.target );
});
