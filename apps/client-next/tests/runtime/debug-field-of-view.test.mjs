/*
===========================================================================

debug-field-of-view.test.mjs - native /setfov parsing and camera publication

Runs the production GM input path with a recorded local callback, then the
world camera with a recorded renderer. No command packet or browser source
patch is needed to observe either boundary.

===========================================================================
*/

import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const { debugFieldOfView } = await import( "../../src/engine/foundation/ui/debug-commands.ts" );
const { createWorldStream } = await import( "../../src/engine/runtime/world/world.ts" );
const { viewProjection } = await import( "../../src/engine/foundation/rendering/world-math.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );

const FRAME_MS = 100;
const WARM_FRAMES = 16;
const NATIVE_THIRTY_DEGREE_RADIANS = 0.5235987901687622;
const NATIVE_SIXTY_DEGREE_RADIANS = 1.0471975803375244;

/*
================
fieldOfViewUiFixture
================
*/
function fieldOfViewUiFixture( t, gm = true ) {
	const sent = [], degrees = [];
	const f = uiFixture(
		message => sent.push( message ),
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		{ setWorldFieldOfView: value => degrees.push( value ) }
	);
	t.after( () => f.dispose() );
	f.state.gameplay.eligibility = { gm };
	let now = 0;
	let result;
	/*
	================
	draw
	================
	*/
	function draw() {
		for ( let frame = 0; frame < WARM_FRAMES; frame++ ) result = f.ui.step( f.state, now += FRAME_MS ) ?? result;
		return result;
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
	return { ...f, sent, degrees, draw, typeLine };
}

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
fieldOfViewWorldFixture

Projection continues while assets are unavailable. Recording that existing
renderer boundary keeps this test independent of licensed world bundles.
================
*/
function fieldOfViewWorldFixture( t ) {
	let camera;
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
		setWorldCameraCollision: unexpectedCameraWork,
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
	function draw() {
		stream.step( { regionId: 1, x: 10, y: 0, z: 20, angle: 0 } );
		assert.equal( stream.error(), null );
		assert.ok( camera );
		return camera;
	}
	return { stream, draw };
}

/*
================
test("/setfov uses signed decimal prefixes followed by unsigned bounds")
================
*/
test("/setfov uses signed decimal prefixes followed by unsigned bounds", () => {
	/** @type {[string, number][]} */
	const cases = [
		[ "9", 10 ],
		[ "10", 10 ],
		[ "11", 11 ],
		[ "149", 149 ],
		[ "150", 150 ],
		[ "151", 150 ],
		[ "0", 10 ],
		[ "-0", 10 ],
		[ "-1", 150 ],
		[ "+60", 60 ],
		[ "60junk", 60 ],
		[ "60.9", 60 ],
		[ "0x3c", 10 ],
		[ "bad60", 10 ],
		[ "+", 10 ],
		[ "-", 10 ],
		[ "2147483648", 150 ],
		[ "4294967296", 150 ],
		[ "-2147483649", 150 ],
		[ "9".repeat( 400 ), 150 ],
		[ "-" + "9".repeat( 400 ), 150 ],
		[ "０６０", 60 ],
		[ "٦٠suffix", 60 ],
		[ "۶۰", 60 ],
		[ "६०", 60 ],
		[ "－60", 10 ],
		[ "𝟞𝟘", 10 ],
		[ "\u07c6\u07c0", 10 ]
	];
	for ( const [word, expected] of cases ) {
		assert.equal( debugFieldOfView( [ word ] ), expected, JSON.stringify( word ) );
	}
	assert.equal( debugFieldOfView( [] ), null );
	assert.equal( debugFieldOfView( [ "30", "40" ] ), null );
});

/*
================
test("/setfov keeps native byte whitespace and the documented Unicode inference")
================
*/
test("/setfov keeps native byte whitespace and the documented Unicode inference", () => {
	for ( const prefix of [ "\t", "\n", "\v", "\f", "\r", " ", "\u00a0", "\u3000" ] ) {
		assert.equal( debugFieldOfView( [ prefix + "60" ] ), 60, JSON.stringify( prefix ) );
	}
	for ( const prefix of [ "\u0085", "\ufeff" ] ) {
		assert.equal( debugFieldOfView( [ prefix + "60" ] ), 10, JSON.stringify( prefix ) );
	}
});

/*
================
test("the GM console changes local FOV and consumes invalid argument counts")
================
*/
test("the GM console changes local FOV and consumes invalid argument counts", t => {
	const f = fieldOfViewUiFixture( t );
	f.typeLine( "/setfov" );
	f.typeLine( "/setfov 30 40" );
	assert.deepEqual( f.degrees, [], "invalid argument counts must leave the camera unchanged" );
	for ( const word of [ "30", "-1", "0", "bad", "150", "151", "4294967296", "６０" ] ) {
		f.typeLine( "/setfov " + word );
	}
	assert.deepEqual( f.degrees, [ 30, 150, 10, 10, 150, 150, 150, 60 ] );
	assert.deepEqual( f.sent, [], "local FOV commands must never reach the session or server" );
});

/*
================
test("a player without GM eligibility cannot change the local FOV")
================
*/
test("a player without GM eligibility cannot change the local FOV", t => {
	const f = fieldOfViewUiFixture( t, false );
	assert.equal( f.draw().controls.some( row => row.id === "gm-input" ), false );
	f.typeLine( "/setfov 30" );
	assert.deepEqual( f.degrees, [] );
	assert.deepEqual( f.sent, [] );
});

/*
================
test("committed FOV changes the actual world projection with native float conversion")
================
*/
test("committed FOV changes the actual world projection with native float conversion", t => {
	const f = fieldOfViewWorldFixture( t );
	const before = f.draw();
	assert.equal( before.fov, Math.PI / 3, "an untouched camera keeps the existing port projection" );
	f.stream.setFieldOfView( 30 );
	const after = f.draw();
	assert.equal( after.fov, NATIVE_THIRTY_DEGREE_RADIANS );
	assert.deepEqual( after.eye, before.eye );
	assert.deepEqual( after.target, before.target );
	assert.deepEqual( after.follow, before.follow );
	const beforeProjection = viewProjection( before, 1 );
	const afterProjection = viewProjection( after, 1 );
	assert.ok(
		Math.abs( afterProjection[0] ) > Math.abs( beforeProjection[0] ),
		"narrower FOV enlarges projected horizontal geometry"
	);
	assert.ok(
		Math.abs( afterProjection[5] ) > Math.abs( beforeProjection[5] ),
		"narrower FOV enlarges projected vertical geometry"
	);
	f.stream.setFieldOfView( 60 );
	assert.equal( f.draw().fov, NATIVE_SIXTY_DEGREE_RADIANS );
});

/*
================
test("world asset resets retain FOV while a new runtime camera starts at its default")
================
*/
test("world asset resets retain FOV while a new runtime camera starts at its default", t => {
	const f = fieldOfViewWorldFixture( t );
	f.stream.setFieldOfView( 30 );
	f.stream.reset();
	assert.equal( f.draw().fov, NATIVE_THIRTY_DEGREE_RADIANS );
	const next = fieldOfViewWorldFixture( t );
	assert.equal( next.draw().fov, Math.PI / 3 );
});
