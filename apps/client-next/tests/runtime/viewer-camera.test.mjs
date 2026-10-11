/*
===========================================================================

viewer-camera.test.mjs - the 3D viewer frames a model from its bounds

Tall, wide and tiny models fill the field of view without leaving it,
seen from the same three-quarter angle, with the whole model between the
near and far planes.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { viewerCamera, refitCamera, VIEWER_YAW, VIEWER_PITCH } = await import(
	"../../src/engine/foundation/rendering/viewer-camera.ts"
);

/*
================
spread

The largest |offset| / depth of the box's corners along the camera's up
axis, and along its right axis divided by the aspect, or Infinity when a
corner is outside the near and far planes.
================
*/
function spread( camera, bounds, aspect = 1 ) {
	const f = camera.target.map( ( v, i ) => v - camera.eye[i] ), length = Math.hypot( ...f );
	const forward = f.map( v => v / length );
	const flat = Math.hypot( forward[0], forward[2] );
	const right = [ -forward[2] / flat, 0, forward[0] / flat ];
	const up = [
		right[1] * forward[2] - right[2] * forward[1],
		right[2] * forward[0] - right[0] * forward[2],
		right[0] * forward[1] - right[1] * forward[0]
	];
	const dot = ( a, b ) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
	let widest = 0;
	for ( let corner = 0; corner < 8; corner++ ) {
		const p = [ 0, 1, 2 ].map( axis =>
			((corner >> axis) & 1 ? bounds.max[axis] : bounds.min[axis]) - camera.eye[axis]
		);
		const depth = dot( p, forward );
		if ( depth <= camera.near || depth >= camera.far ) return Infinity;
		widest = Math.max( widest, Math.abs( dot( p, right ) ) / depth / aspect, Math.abs( dot( p, up ) ) / depth );
	}
	return widest;
}

/** @typedef {import("../../src/engine/foundation/rendering/viewer-camera.ts").ViewerBounds} ViewerBounds */

test("tall, wide and tiny models fit the view from the fixed angle", () => {
	for (
		const bounds of /** @type {ViewerBounds[]} */ ([
			{ min: [ -0.5, 0, -0.5 ], max: [ 0.5, 4, 0.5 ] },
			{ min: [ -6, 0, -3 ], max: [ 6, 2, 3 ] },
			{ min: [ -0.01, 0, -0.01 ], max: [ 0.01, 0.02, 0.01 ] }
		])
	) {
		const camera = viewerCamera( bounds, [ 0, 1 ] ), edge = Math.tan( camera.fov / 2 );
		// Every corner is in view, and the widest one nearly reaches the edge:
		// framed, not lost in margin.
		const widest = spread( camera, bounds );
		assert.ok( widest <= edge && widest > edge * 0.85, JSON.stringify( { bounds, widest, edge } ) );
		assert.deepEqual( camera.target, bounds.min.map( ( v, i ) => (v + bounds.max[i]) / 2 ) );
		const d = camera.eye.map( ( v, i ) => v - camera.target[i] );
		const distance = Math.hypot( ...d );
		// The pitch looks down from above; the yaw is measured from the front.
		assert.ok( Math.abs( Math.asin( d[1] / distance ) - VIEWER_PITCH ) < 1e-9 );
		assert.ok( Math.abs( Math.atan2( d[0], d[2] ) + VIEWER_YAW ) < 1e-9 );
	}
});

test("a portrait frame fits a tall model by its height, not its width", () => {
	const bounds = /** @type {ViewerBounds} */ ({ min: [ -0.5, 0, -0.5 ], max: [ 0.5, 2, 0.5 ] }), aspect = 0.75;
	const square = viewerCamera( bounds, [ 0, 1 ] ), portrait = viewerCamera( bounds, [ 0, 1 ], aspect );
	const edge = Math.tan( portrait.fov / 2 ), widest = spread( portrait, bounds, aspect );
	assert.ok( widest <= edge && widest > edge * 0.85, String( widest ) );
	// A narrower frame never brings the camera closer.
	const distance = c => Math.hypot( ...c.eye.map( ( v, i ) => v - c.target[i] ) );
	assert.ok( distance( portrait ) >= distance( square ) - 1e-9 );
	assert.throws( () => viewerCamera( bounds, [ 0, 1 ], 0 ) );
});

test("the front direction turns the view with it", () => {
	const bounds = /** @type {ViewerBounds} */ ({ min: [ -1, 0, -1 ], max: [ 1, 2, 1 ] });
	const a = viewerCamera( bounds, [ 0, 1 ] ), b = viewerCamera( bounds, [ 1, 0 ] );
	const da = Math.atan2( a.eye[0] - a.target[0], a.eye[2] - a.target[2] );
	const db = Math.atan2( b.eye[0] - b.target[0], b.eye[2] - b.target[2] );
	assert.ok( Math.abs( Math.abs( da - db ) - Math.PI / 2 ) < 1e-9 );
});

test("empty or broken bounds are refused", () => {
	assert.throws( () => viewerCamera( { min: [ 1, 0, 0 ], max: [ 0, 1, 1 ] }, [ 0, 1 ] ) );
	assert.throws( () => viewerCamera( { min: [ 0, 0, 0 ], max: [ NaN, 1, 1 ] }, [ 0, 1 ] ) );
});

test("refitting scales the drawn box to the fill and keeps a centred one centred", () => {
	const camera = viewerCamera( { min: [ -1, 0, -1 ], max: [ 1, 2, 1 ] }, [ 0, 1 ], 0.75 );
	const distance = c => Math.hypot( ...c.eye.map( ( v, i ) => v - c.target[i] ) );
	// A centred box already at the fill changes nothing.
	const same = refitCamera( camera, [ 60, 24, 240, 376 ], 300, 400, { kind: "height", share: 0.88 } );
	assert.ok( Math.abs( distance( same ) - distance( camera ) ) < 1e-9 );
	same.target.forEach( ( v, i ) => assert.ok( Math.abs( v - camera.target[i] ) < 1e-9 ) );
	// Half the fill's height: the camera comes halfway in, along the same line.
	const near = refitCamera( camera, [ 105, 112, 195, 288 ], 300, 400, { kind: "height", share: 0.88 } );
	assert.ok( Math.abs( distance( near ) - distance( camera ) * 0.5 ) < 1e-9 );
	// Contain lets the wider axis decide: a box as wide as the frame backs off.
	const wide = refitCamera( camera, [ 0, 112, 300, 288 ], 300, 400, { kind: "contain", share: 0.9 } );
	assert.ok( Math.abs( distance( wide ) - distance( camera ) / 0.9 ) < 1e-9 );
	// An off-centre box moves the target, never the view direction.
	const moved = refitCamera( camera, [ 0, 24, 180, 376 ], 300, 400, { kind: "height", share: 0.88 } );
	const dir = c => c.eye.map( ( v, i ) => (v - c.target[i]) / distance( c ) );
	dir( moved ).forEach( ( v, i ) => assert.ok( Math.abs( v - dir( camera )[i] ) < 1e-9 ) );
	assert.ok( Math.hypot( ...moved.target.map( ( v, i ) => v - camera.target[i] ) ) > 0.01 );
});
