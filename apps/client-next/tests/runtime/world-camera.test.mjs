/*
===========================================================================

world-camera.test.mjs - tests for world-math.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fc from "fast-check";

const { viewProjection, visibleSphere, prepareViewFrustum, visibleFrustumSphere, mapPlacement, placement } =
	await import( sourceFileUrl( "src/engine/foundation/rendering/world-math.ts" ).href );
function project( m, p ) {
	const v = [ ...p, 1 ], q = [ 0, 0, 0, 0 ];
	for ( let i = 0; i < 4; i++ ) for ( let j = 0; j < 4; j++ ) q[i] += m[j * 4 + i] * v[j];
	return q.slice( 0, 3 ).map( x => x / q[3] );
}

test("prepared frustum matches exact legacy decisions including touching clip planes", () => {
	fc.assert(
		fc.property(
			fc.array( fc.integer( { min: -10000, max: 10000 } ), { minLength: 16, maxLength: 16 } ),
			fc.array(
				fc.tuple(
					fc.integer( { min: -100000, max: 100000 } ),
					fc.integer( { min: -100000, max: 100000 } ),
					fc.integer( { min: -100000, max: 100000 } ),
					fc.integer( { min: 0, max: 10000 } )
				),
				{ minLength: 1, maxLength: 100 }
			),
			( values, spheres ) => {
				const m = Float32Array.from( values.map( x => x / 37 ) ), planes = prepareViewFrustum( m );
				for ( const [x, y, z, r] of spheres ) {
					assert.equal( visibleFrustumSphere( planes, x, y, z, r ), visibleSphere( m, [ x, y, z ], r ) );
				}
			}
		),
		{ seed: 909100, numRuns: 300 }
	);
	const m = viewProjection( { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], fov: Math.PI / 2, near: 1, far: 100 }, 1 ),
		planes = prepareViewFrustum( m );
	for ( const r of [ 0, .1, 1, 20 ] ) {
		for ( const z of [ 1 - r, 1 + r, 100 - r, 100 + r ] ) {
			for ( const epsilon of [ -1e-8, 0, 1e-8 ] ) {
				for ( const p of [ [ 0, 0, z + epsilon ], [ z + r + epsilon, 0, z ], [ 0, -z - r - epsilon, z ] ] ) {
					assert.equal( visibleFrustumSphere( planes, ...p, r ), visibleSphere( m, p, r ) );
				}
			}
		}
	}
});
test("map yaw follows native 0x451a10 independently of character pose yaw", () => {
	const map = mapPlacement( 0x694f, 0x694e, 30, 4, 50, Math.PI / 2 );
	const point = project( map, [ 10, 0, 0 ] );
	assert.ok( Math.abs( point[0] - 1950 ) < 1e-5 );
	assert.equal( point[1], 4 );
	assert.ok( Math.abs( point[2] - 60 ) < 1e-5 );
	assert.ok( project( placement( 0x694f, 0x694e, 30, 4, 50, Math.PI / 2 ), [ 10, 0, 0 ] )[2] < 50 );
	for ( const yaw of [ -2.4, -Math.PI / 2, 0, .73, Math.PI ] ) {
		const m = mapPlacement( 0x684e, 0x694e, 30, 4, 50, yaw ), p = [ 17, 8, -29 ], actual = project( m, p );
		const expected = [
			30 + p[0] * Math.cos( yaw ) - p[2] * Math.sin( yaw ),
			12,
			50 - 1920 + p[0] * Math.sin( yaw ) + p[2] * Math.cos( yaw )
		];
		for ( let i = 0; i < 3; i++ ) assert.ok( Math.abs( actual[i] - expected[i] ) < 1e-4 );
	}
});
test("native left-handed camera projects east right, up upward, and forward into WebGPU depth", () => {
	const camera = { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], fov: Math.PI / 2, near: 1, far: 100 };
	const m = viewProjection( camera, 1 );
	assert.ok( project( m, [ 1, 0, 10 ] )[0] > 0 );
	assert.ok( project( m, [ -1, 0, 10 ] )[0] < 0 );
	assert.ok( project( m, [ 0, 1, 10 ] )[1] > 0 );
	assert.ok( Math.abs( project( m, [ 0, 0, 1 ] )[2] ) < 1e-6 );
	assert.ok( Math.abs( project( m, [ 0, 0, 100 ] )[2] - 1 ) < 1e-6 );
	assert.equal( visibleSphere( m, [ 0, 0, 10 ], .1 ), true );
	assert.equal( visibleSphere( m, [ 0, 0, -10 ], .1 ), false );
	// A triangle facing the camera in native space stays clockwise on screen.
	const [a, b, c] = [ [ -1, -1, 10 ], [ 0, 1, 10 ], [ 1, -1, 10 ] ].map( p => project( m, p ) );
	assert.ok( (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0]) < 0 );
});
test("native rolled camera retains its authored up direction", () => {
	const m = viewProjection( {
		eye: [ 0, 0, 0 ],
		target: [ 0, 0, 1 ],
		up: [ 1, 0, 0 ],
		fov: Math.PI / 2,
		near: 1,
		far: 100
	}, 1 );
	assert.ok( project( m, [ 1, 0, 10 ] )[1] > 0 );
	assert.ok( project( m, [ 0, -1, 10 ] )[0] > 0 );
});
/*
================
leftHandedProjection

The old port's camera convention, written out: a left-handed look-at view
(z toward the target, x = up × z) and a vertical-FOV perspective with depth in
[0, 1], z = far/(far-near) - far*near/((far-near)*viewZ). This is exactly what
Babylon's LookAtLH × PerspectiveFovLH(..., halfZRange) computed; the closed
form agrees with it to 7e-8 (Babylon's float32 storage).
================
*/
function leftHandedProjection( camera, aspect, point ) {
	const sub = ( a, b ) => [ a[0] - b[0], a[1] - b[1], a[2] - b[2] ];
	const dot = ( a, b ) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
	const cross = ( a, b ) => [ a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0] ];
	const normalize = ( a ) => {
		const length = Math.hypot( ...a );
		return [ a[0] / length, a[1] / length, a[2] / length ];
	};
	const zAxis = normalize( sub( camera.target, camera.eye ) );
	const xAxis = normalize( cross( camera.up, zAxis ) );
	const yAxis = normalize( cross( zAxis, xAxis ) );
	const view = sub( point, camera.eye );
	const x = dot( xAxis, view ), y = dot( yAxis, view ), z = dot( zAxis, view );
	const focal = 1 / Math.tan( camera.fov / 2 );
	const { near, far } = camera;
	return [ focal / aspect * x / z, focal * y / z, far / (far - near) - far * near / ((far - near) * z) ];
}

test("camera projection agrees with the old port's left-handed convention", () => {
	const cameras = [
		{ eye: [ 50, 40, -80 ], target: [ 10, 5, 20 ], up: [ 0, 1, 0 ], fov: Math.PI / 3, near: 1, far: 3500 },
		{ eye: [ 50, 40, -80 ], target: [ 10, 5, 20 ], up: [ .2, 1, .3 ], fov: Math.PI / 3, near: 1, far: 3500 }
	];
	for ( const camera of cameras ) {
		const actual = viewProjection( camera, 16 / 9 );
		for ( const point of [ [ 10, 5, 20 ], [ 30, 15, 50 ], [ -20, 10, 100 ] ] ) {
			const reference = leftHandedProjection( camera, 16 / 9, point );
			const projected = project( actual, point );
			for ( let i = 0; i < 3; i++ ) assert.ok( Math.abs( projected[i] - reference[i] ) < 1e-5 );
		}
	}
});
