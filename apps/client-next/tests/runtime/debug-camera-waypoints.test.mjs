/*
===========================================================================

debug-camera-waypoints.test.mjs - the GM console's camera-waypoint rows

690C40 ids 130-134: /cwclear resets the interface's keyframe track
(+0x3FC), /cwpop and /cwgo share one case (the free-camera byte +0x3CE
standing and a non-empty store, then the play byte +0x3CD is written and
the track clock zeroed), /cwstop clears the play byte, and /cwsave writes
the store's lines in the native text form. Nothing native ever loads the
track, so the port fills it from the published cameradata rows (#1716)
and the browser's c:\cw.txt is a download. Nothing reaches the server.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const waypoints = await import( "../../src/engine/foundation/ui/camera-waypoints.ts" );

// The two retail rows (config\cameradata.txt), as the build publishes them.
const ROWS = [
	{
		regionId: "0x6b4f",
		sectorX: 79,
		sectorY: 107,
		position: { x: 1205, y: 80, z: 396 },
		distanceGate: 30,
		rotationDegrees: { x: 10, y: 0, z: 0 },
		scalar: 50
	},
	{
		regionId: "0x694d",
		sectorX: 77,
		sectorY: 105,
		position: { x: 1466, y: 79, z: 1488 },
		distanceGate: 30,
		rotationDegrees: { x: 10, y: 270, z: 0 },
		scalar: 50
	}
];

/*
================
test("the published cameradata shape decodes, junk does not")
================
*/
test("the published cameradata shape decodes, junk does not", () => {
	const rows = waypoints.decodeCameraDataRows( { format: "sro-mission-camera-data", version: 1, rows: ROWS } );
	assert.equal( rows.length, 2 );
	assert.deepEqual( rows[0].position, { x: 1205, y: 80, z: 396 } );
	assert.deepEqual( rows[1].rotationDegrees, { x: 10, y: 270 } );
	assert.throws( () => waypoints.decodeCameraDataRows( null ) );
	assert.throws( () => waypoints.decodeCameraDataRows( {} ) );
	assert.throws( () => waypoints.decodeCameraDataRows( { rows: [ { position: { x: 1, y: 2 } } ] } ) );
	assert.throws(
		() =>
			waypoints.decodeCameraDataRows( {
				rows: [ { position: { x: 1, y: 2, z: 3 }, rotationDegrees: { x: "10", y: 0 } } ]
			} )
	);
	assert.throws(
		() =>
			waypoints.decodeCameraDataRows( {
				rows: [ { position: { x: 1, y: 2, z: 3 }, rotationDegrees: { x: 10, y: "0" } } ]
			} )
	);
});

/*
================
test("rows load as keyframes one second apart, yaw then pitch")
================
*/
test("rows load as keyframes one second apart, yaw then pitch", () => {
	const store = waypoints.createCameraWaypointStore();
	assert.deepEqual( store, { keyframes: [], clockSeconds: 0 } );
	waypoints.loadCameraDataKeyframes( store, waypoints.decodeCameraDataRows( { rows: ROWS } ) );
	assert.equal( store.keyframes.length, 2 );
	assert.equal( store.clockSeconds, 0 );
	const [first, second] = store.keyframes;
	assert.equal( first.timeSeconds, 0 );
	assert.equal( second.timeSeconds, 1 );
	assert.deepEqual( first.position, { x: 1205, y: 80, z: 396 } );
	// Pitch 10, yaw 0: the pure X-rotation quaternion (sin 5, 0, 0, cos 5).
	assert.ok( Math.abs( first.orientation.x - Math.sin( 5 * Math.PI / 180 ) ) < 1e-6 );
	assert.equal( first.orientation.y, 0 );
	assert.equal( first.orientation.z, 0 );
	assert.ok( Math.abs( first.orientation.w - Math.cos( 5 * Math.PI / 180 ) ) < 1e-6 );
	// Pitch 10, yaw 270: a unit quaternion carrying the yaw's sign pair.
	const o = second.orientation;
	assert.ok( Math.abs( o.x * o.x + o.y * o.y + o.z * o.z + o.w * o.w - 1 ) < 1e-6 );
	assert.ok( o.y > 0 && o.w < 0, "yaw 270 turns the pair (y, w) to (+, -)" );
});

/*
================
test("save text is the native line form, and reset empties it")
================
*/
test("save text is the native line form, and reset empties it", () => {
	const store = waypoints.createCameraWaypointStore();
	assert.equal( waypoints.cameraWaypointSaveText( store ), "", "an empty store saves empty" );
	store.keyframes = [
		{
			timeSeconds: 0,
			position: { x: 1205, y: 80, z: 396 },
			orientation: { x: 0, y: 0, z: 0, w: 1 }
		},
		{
			timeSeconds: 2.5,
			position: { x: -1.5, y: 0.25, z: 10000 },
			orientation: { x: 0.5, y: -0.5, z: 0.5, w: -0.5 }
		}
	];
	store.clockSeconds = 7;
	assert.equal(
		waypoints.cameraWaypointSaveText( store ),
		"{ 0.000000f, Vector(1205.000000f, 80.000000f, 396.000000f), " +
			"Vector4(0.000000f, 0.000000f, 0.000000f, 1.000000f) }\r\n" +
			"{ 2.500000f, Vector(-1.500000f, 0.250000f, 10000.000000f), " +
			"Vector4(0.500000f, -0.500000f, 0.500000f, -0.500000f) }\r\n"
	);
	waypoints.resetCameraWaypoints( store );
	assert.deepEqual( store, { keyframes: [], clockSeconds: 0 } );
	assert.equal( waypoints.cameraWaypointSaveText( store ), "" );
});

/*
================
waypointUiFixture

The production HUD with the waypoint sinks recorded.
================
*/
function waypointUiFixture( t, gm = true ) {
	/** @type {any[]} */
	const sent = [];
	const loaded = [], playing = [], saved = [];
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
			setFreeCamera: () => {},
			setCameraWaypoints: keyframes => loaded.push( keyframes ),
			setCameraPlaying: value => playing.push( value ),
			saveCameraWaypoints: text => saved.push( text )
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
	return { ...f, sent, loaded, playing, saved, typeLine };
}

/*
================
test("the HUD loads the store and the rows run their native gates")
================
*/
test("the HUD loads the store and the rows run their native gates", t => {
	const f = waypointUiFixture( t );
	// The cameradata asset settled with the HUD: the two retail rows are
	// the store, forwarded to the camera owner once.
	assert.equal( f.loaded.length, 1 );
	assert.equal( f.loaded[0].length, 2 );
	assert.deepEqual( f.loaded[0][0].position, { x: 1205, y: 80, z: 396 } );
	// /cwgo without the free-camera byte: nothing starts.
	f.typeLine( "/cwgo" );
	assert.deepEqual( f.playing, [] );
	f.typeLine( "/cwpop" );
	assert.deepEqual( f.playing, [] );
	// Free camera on: both names start playback, /cwstop clears it.
	f.typeLine( "/camera" );
	f.typeLine( "/cwgo" );
	assert.deepEqual( f.playing, [ true ] );
	f.typeLine( "/cwstop" );
	assert.deepEqual( f.playing, [ true, false ] );
	f.typeLine( "/cwpop" );
	assert.deepEqual( f.playing, [ true, false, true ] );
	// /cwsave downloads the native lines; nothing is printed.
	f.typeLine( "/cwsave" );
	assert.equal( f.saved.length, 1 );
	const lines = f.saved[0].split( "\r\n" );
	assert.equal( lines.length, 3, "two lines and their closing CRLF" );
	assert.equal(
		lines[0],
		"{ 0.000000f, Vector(1205.000000f, 80.000000f, 396.000000f), " +
			"Vector4(0.087156f, 0.000000f, 0.000000f, 0.996195f) }"
	);
	assert.ok( lines[1].startsWith( "{ 1.000000f, Vector(1466.000000f, 79.000000f, 1488.000000f), Vector4(" ) );
	// /cwclear resets the store: the forward is empty, the save is empty,
	// and the non-empty gate refuses playback even with the camera free.
	f.typeLine( "/cwclear" );
	assert.equal( f.loaded.length, 2 );
	assert.deepEqual( f.loaded[1], [] );
	f.typeLine( "/cwsave" );
	assert.equal( f.saved[1], "" );
	f.typeLine( "/cwstop" );
	f.typeLine( "/cwgo" );
	assert.deepEqual( f.playing, [ true, false, true, false ] );
	assert.deepEqual( f.sent, [], "a waypoint row reached the server" );
});

/*
================
test("a player without GM eligibility runs no waypoint row")
================
*/
test("a player without GM eligibility runs no waypoint row", t => {
	const f = waypointUiFixture( t, false );
	assert.equal( f.loaded.length, 1, "the store still loads; only the rows are gated" );
	f.typeLine( "/camera" );
	f.typeLine( "/cwgo" );
	f.typeLine( "/cwclear" );
	f.typeLine( "/cwsave" );
	assert.deepEqual( f.playing, [] );
	assert.equal( f.loaded.length, 1 );
	assert.deepEqual( f.saved, [] );
	assert.deepEqual( f.sent, [] );
});
