/*
===========================================================================

viewer-camera.test.mjs - the 3D viewer frames a model from its bounds

Tall, wide and tiny models all fit the field of view, seen from the same
three-quarter angle, with the whole model between the near and far planes.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { viewerCamera, VIEWER_YAW, VIEWER_PITCH } = await import(
	"../../src/engine/foundation/rendering/viewer-camera.ts"
);

/*
================
fits

The bounding sphere fits inside the camera's vertical field of view.
================
*/
function fits( camera, bounds ) {
	const radius = Math.hypot( ...bounds.max.map( ( v, i ) => v - bounds.min[i] ) ) / 2;
	const distance = Math.hypot( ...camera.eye.map( ( v, i ) => v - camera.target[i] ) );
	return Math.asin( radius / distance ) <= camera.fov / 2 + 1e-9 && camera.near < distance - radius &&
		camera.far > distance + radius;
}

test("tall, wide and tiny models fit the view from the fixed angle", () => {
	for (
		const bounds of [
			{ min: [ -0.5, 0, -0.5 ], max: [ 0.5, 4, 0.5 ] },
			{ min: [ -6, 0, -3 ], max: [ 6, 2, 3 ] },
			{ min: [ -0.01, 0, -0.01 ], max: [ 0.01, 0.02, 0.01 ] }
		]
	) {
		const camera = viewerCamera( bounds, [ 0, 1 ] );
		assert.ok( fits( camera, bounds ), JSON.stringify( bounds ) );
		assert.deepEqual( camera.target, bounds.min.map( ( v, i ) => (v + bounds.max[i]) / 2 ) );
		const d = camera.eye.map( ( v, i ) => v - camera.target[i] );
		const distance = Math.hypot( ...d );
		// The pitch looks down from above; the yaw is measured from the front.
		assert.ok( Math.abs( Math.asin( d[1] / distance ) - VIEWER_PITCH ) < 1e-9 );
		assert.ok( Math.abs( Math.atan2( d[0], d[2] ) + VIEWER_YAW ) < 1e-9 );
	}
});

test("the front direction turns the view with it", () => {
	const bounds = { min: [ -1, 0, -1 ], max: [ 1, 2, 1 ] };
	const a = viewerCamera( bounds, [ 0, 1 ] ), b = viewerCamera( bounds, [ 1, 0 ] );
	const da = Math.atan2( a.eye[0] - a.target[0], a.eye[2] - a.target[2] );
	const db = Math.atan2( b.eye[0] - b.target[0], b.eye[2] - b.target[2] );
	assert.ok( Math.abs( Math.abs( da - db ) - Math.PI / 2 ) < 1e-9 );
});

test("empty or broken bounds are refused", () => {
	assert.throws( () => viewerCamera( { min: [ 1, 0, 0 ], max: [ 0, 1, 1 ] }, [ 0, 1 ] ) );
	assert.throws( () => viewerCamera( { min: [ 0, 0, 0 ], max: [ NaN, 1, 1 ] }, [ 0, 1 ] ) );
});
