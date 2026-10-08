/*
===========================================================================

audio-space.test.mjs - world sounds keep their sides in Web Audio

The renderer's camera basis is the native left-handed one; Web Audio
places a source on the listener's right when it lies along forward x up.
A sound on the camera's right must be heard on the right, and one ahead
must stay ahead, for every heading.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { audioSpace } = await import( "../../src/engine/foundation/audio/space.ts" );
const { cameraBasis } = await import( "../../src/engine/foundation/rendering/world-math.ts" );

/** @typedef {readonly [number, number, number]} Vec3 */
/** @type {( a: Vec3, b: Vec3 ) => [number, number, number]} */
const cross = ( a, b ) => [ a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0] ];
/** @type {( a: Vec3, b: Vec3 ) => number} */
const dot = ( a, b ) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
/** @type {( a: Vec3, b: Vec3, k: number ) => [number, number, number]} */
const plus = ( a, b, k ) => [ a[0] + b[0] * k, a[1] + b[1] * k, a[2] + b[2] * k ];
/** @type {( a: Vec3, b: Vec3 ) => [number, number, number]} */
const minus = ( a, b ) => [ a[0] - b[0], a[1] - b[1], a[2] - b[2] ];

test("a sound on the camera's right is heard on the right, and ahead stays ahead", () => {
	for ( let heading = 0; heading < 8; heading++ ) {
		const yaw = heading * Math.PI / 4;
		/** @type {[number, number, number]} */
		const eye = [ 900, 40, 700 ];
		/** @type {[number, number, number]} */
		const target = [ eye[0] + Math.sin( yaw ) * 10, eye[1] - 3, eye[2] + Math.cos( yaw ) * 10 ];
		const camera = { eye, target, fov: 1, near: 1, far: 5000 };
		const basis = cameraBasis( camera );
		const listener = audioSpace( eye ), forward = audioSpace( basis.forward ), up = audioSpace( basis.up );
		// Web Audio's own right for this listener (PannerNode azimuth).
		const right = cross( forward, up );
		const rightSound = minus( audioSpace( plus( eye, basis.right, 50 ) ), listener );
		const aheadSound = minus( audioSpace( plus( eye, basis.forward, 50 ) ), listener );
		assert.ok( dot( rightSound, right ) > 0, `heading ${heading}: right sound panned left` );
		assert.ok( dot( aheadSound, forward ) > 0, `heading ${heading}: ahead sound placed behind` );
		assert.ok( dot( up, [ 0, 1, 0 ] ) > 0, `heading ${heading}: up flipped` );
	}
});
