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

const { viewProjection } = await import( "../../src/engine/foundation/rendering/world-math.ts" );
const { viewerCamera, refitCamera, touchesEdge, widenCamera, framed, nextFraming, VIEWER_YAW, VIEWER_PITCH } =
	await import(
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

test("a picture whose model reaches any border is clipped; one inside it is whole", () => {
	assert.equal( touchesEdge( [ 10, 10, 90, 90 ], 100, 100 ), false );
	assert.equal( touchesEdge( [ 0, 10, 90, 90 ], 100, 100 ), true );
	assert.equal( touchesEdge( [ 10, 0, 90, 90 ], 100, 100 ), true );
	assert.equal( touchesEdge( [ 10, 10, 100, 90 ], 100, 100 ), true );
	assert.equal( touchesEdge( [ 10, 10, 90, 100 ], 100, 100 ), true );
});

test("widening keeps the view's direction and target and fits a box twice as large", () => {
	const bounds = /** @type {ViewerBounds} */ ({ min: [ -1, 0, -1 ], max: [ 1, 2, 1 ] }),
		camera = viewerCamera( bounds, [ 0, -1 ] );
	const wide = widenCamera( camera, 2 );
	assert.deepEqual( wide.target, camera.target );
	const back = camera.eye.map( ( v, i ) => v - camera.target[i] ),
		next = wide.eye.map( ( v, i ) => v - wide.target[i] );
	next.forEach( ( v, i ) => assert.ok( Math.abs( v - back[i] * 2 ) < 1e-9 ) );
	assert.ok( Math.abs( spread( wide, bounds ) - spread( camera, bounds ) / 2 ) < 0.05 * spread( camera, bounds ) );
	const doubled = /** @type {ViewerBounds} */ ({ min: [ -2, -1, -2 ], max: [ 2, 3, 2 ] });
	assert.ok( spread( wide, doubled ) <= spread( camera, bounds ) * 1.05 );
	assert.ok( wide.near < Math.hypot( ...next ) - Math.hypot( 2, 2, 2 ) );
	assert.ok( wide.far > Math.hypot( ...next ) + Math.hypot( 2, 2, 2 ) );
});

/*
================
pixelOf

Where a world point lands in a width x height picture drawn by camera,
through the projection the renderer draws with (y down).
================
*/
function pixelOf( camera, point, width, height ) {
	const m = viewProjection( camera, width / height );
	const clip = [ 0, 1, 2, 3 ].map( row =>
		m[row] * point[0] + m[4 + row] * point[1] + m[8 + row] * point[2] + m[12 + row]
	);
	return [ (clip[0] / clip[3] + 1) / 2 * width, (1 - clip[1] / clip[3]) / 2 * height ];
}

test("the refit centres an off-centre model in the drawn picture, both ways across", () => {
	const camera = viewerCamera( { min: [ -1, 0, -1 ], max: [ 1, 2, 1 ] }, [ 0, -1 ] );
	for ( const side of [ -1, 1 ] ) {
		// A point drawn a quarter of the picture off centre, boxed where it lands.
		const point = [ side * 0.5, 1, 0.3 ], [x, y] = pixelOf( camera, point, 400, 400 );
		const next = refitCamera( camera, [ x - 20, y - 20, x + 20, y + 20 ], 400, 400, {
			kind: "contain",
			share: 0.5
		} );
		const [cx, cy] = pixelOf( next, point, 400, 400 );
		assert.ok( Math.abs( cx - 200 ) < 12 && Math.abs( cy - 200 ) < 12, `side ${side}: ${cx}, ${cy}` );
	}
});

/*
================
drawnBox

The opaque box a picture of these world points would show: each point
projected through the renderer's projection, the box clipped to the
picture as a readback clips it.
================
*/
/** @returns {[number, number, number, number]} */
function drawnBox( camera, points, width, height ) {
	const pixels = points.map( point => pixelOf( camera, point, width, height ) );
	const clip = ( v, limit ) => Math.max( 0, Math.min( limit, v ) );
	return [
		clip( Math.min( ...pixels.map( p => p[0] ) ), width ),
		clip( Math.min( ...pixels.map( p => p[1] ) ), height ),
		clip( Math.max( ...pixels.map( p => p[0] ) ), width ),
		clip( Math.max( ...pixels.map( p => p[1] ) ), height )
	];
}

test("a model reaching far off the target's depth is framed by the loop, not by one refit", () => {
	// A body the first framing knows, and a wing toward the camera it does not.
	const body = [ 0, 1, 2, 3, 4, 5, 6, 7 ].map( c => [ c & 1 ? 1 : -1, c & 2 ? 2 : 0, c & 4 ? 1 : -1 ] );
	const points = [ ...body, [ 4, 3, -4 ], [ 5, 2.5, -2 ] ];
	const fill = /** @type {import("../../src/engine/foundation/rendering/viewer-camera.ts").ViewerFill} */ ({
		kind: "contain",
		share: 0.92
	});
	let camera = viewerCamera( { min: [ -1, 0, -1 ], max: [ 1, 2, 1 ] }, [ 0, -1 ] );
	let box = drawnBox( camera, points, 384, 384 ), fitting = { widened: 0, refits: 0 };
	assert.ok( touchesEdge( box, 384, 384 ), "the first framing clips the wing" );
	const once = drawnBox( refitCamera( camera, box, 384, 384, fill ), points, 384, 384 );
	assert.equal( framed( once, 384, 384, fill ), false, "one refit of a clipped picture misses" );
	for ( let step = 0; step < 20; step++ ) {
		const next = nextFraming( camera, box, 384, 384, fill, fitting );
		if ( !next ) break;
		camera = next.camera;
		fitting = next.fitting;
		box = drawnBox( camera, points, 384, 384 );
	}
	assert.ok( framed( box, 384, 384, fill ), `framed: ${box.map( Math.round )}` );
	assert.equal( nextFraming( camera, box, 384, 384, fill, fitting ), null );
});

test("a height fill may leave the sides; a contained still may not", () => {
	/** @typedef {import("../../src/engine/foundation/rendering/viewer-camera.ts").ViewerFill} Fill */
	const height = /** @type {Fill} */ ({ kind: "height", share: 0.88 }),
		contain = /** @type {Fill} */ ({ kind: "contain", share: 0.9 });
	assert.equal( touchesEdge( [ 0, 24, 300, 376 ], 300, 400, height ), false );
	assert.equal( touchesEdge( [ 0, 24, 300, 376 ], 300, 400, contain ), true );
	assert.equal( touchesEdge( [ 10, 0, 290, 376 ], 300, 400, height ), true );
	assert.equal( framed( [ 0, 24, 300, 376 ], 300, 400, height ), true );
	assert.equal( framed( [ 15, 20, 285, 380 ], 300, 400, contain ), true );
	assert.equal( framed( [ 60, 60, 240, 340 ], 300, 400, contain ), false );
	assert.equal( framed( [ 40, 20, 300, 380 ], 300, 400, contain ), false );
});
