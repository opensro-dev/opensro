/*
===========================================================================

cull-envelope.test.mjs - the widened terrain cull a moving camera stays inside

Whatever sphere the live frustum shows, the envelope must keep while the
camera stays within its margin and slack (else the terrain would miss a
range on screen); leaving the envelope rebuilds it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCullEnvelope, CULL_MARGIN_RAD, CULL_EYE_SLACK } = await import(
	"../../src/engine/foundation/rendering/cull-envelope.ts"
);
const { prepareViewFrustum, visibleFrustumSphere, viewProjection, projectionTangents, cameraBasis } = await import(
	"../../src/engine/foundation/rendering/world-math.ts"
);

const ASPECT = 16 / 9, DISTANCE = 80, TARGET = [ 1000, 40, 1000 ];

// A follow camera orbiting TARGET, as the drag turns it.
function orbit( yaw, pitch, drift = [ 0, 0, 0 ] ) {
	const eye = [
		TARGET[0] + Math.cos( pitch ) * Math.sin( yaw ) * DISTANCE + drift[0],
		TARGET[1] + Math.sin( pitch ) * DISTANCE + drift[1],
		TARGET[2] + Math.cos( pitch ) * Math.cos( yaw ) * DISTANCE + drift[2]
	];
	return /** @type {any} */ ({ eye, target: TARGET.map( ( v, i ) => v + drift[i] ), fov: 1, near: 1, far: 3500 });
}

// A seeded generator: the same spheres every run.
function random( seed ) {
	return () => (seed = (seed * 1664525 + 1013904223) >>> 0) / 2 ** 32;
}

test("every sphere the live view shows stays inside the envelope while the camera is within it", () => {
	const envelopes = createCullEnvelope(), next = random( 7 );
	const start = orbit( .3, .4 ), envelope = envelopes.envelope( start, ASPECT );
	const wide = prepareViewFrustum( envelope.matrix );
	let checked = 0;
	for ( let view = 0; view < 40; view++ ) {
		// Turns and drifts up to just inside the margin and the slack.
		const share = view / 40;
		const live = orbit(
			.3 + CULL_MARGIN_RAD * .45 * Math.cos( view ),
			.4 + CULL_MARGIN_RAD * .45 * Math.sin( view ),
			[ CULL_EYE_SLACK * .4 * share, 0, -CULL_EYE_SLACK * .3 * share ]
		);
		if ( envelopes.envelope( live, ASPECT ) !== envelope ) continue;
		const exact = prepareViewFrustum( viewProjection( live, ASPECT ) );
		for ( let i = 0; i < 400; i++ ) {
			const c = [
				TARGET[0] + (next() - .5) * 6000,
				TARGET[1] + (next() - .5) * 600,
				TARGET[2] + (next() - .5) * 6000
			];
			const radius = 20 + next() * 220;
			if ( !visibleFrustumSphere( exact, c[0], c[1], c[2], radius ) ) continue;
			checked++;
			assert.ok(
				visibleFrustumSphere( wide, c[0], c[1], c[2], radius + envelope.slack ),
				`view ${view}: a sphere on screen at ${c.map( v => v.toFixed( 0 ) )} is outside the envelope`
			);
		}
	}
	assert.ok( checked > 1000, `only ${checked} visible spheres were checked` );
});

test("the envelope holds through a small turn and is rebuilt past its margin or slack", () => {
	const envelopes = createCullEnvelope();
	const first = envelopes.envelope( orbit( 0, .4 ), ASPECT );
	assert.equal( envelopes.envelope( orbit( CULL_MARGIN_RAD / 4, .4 ), ASPECT ), first, "a small turn keeps it" );
	const turned = envelopes.envelope( orbit( CULL_MARGIN_RAD * 1.5, .4 ), ASPECT );
	assert.notEqual( turned.stamp, first.stamp, "a turn past the margin rebuilds it" );
	const moved = envelopes.envelope( orbit( CULL_MARGIN_RAD * 1.5, .4, [ CULL_EYE_SLACK * 2, 0, 0 ] ), ASPECT );
	assert.notEqual( moved.stamp, turned.stamp, "an eye past the slack rebuilds it" );
	assert.notEqual(
		envelopes.envelope( orbit( CULL_MARGIN_RAD * 1.5, .4, [ CULL_EYE_SLACK * 2, 0, 0 ] ), 4 / 3 ).stamp,
		moved.stamp,
		"a new aspect rebuilds it"
	);
	envelopes.invalidate();
	assert.notEqual( envelopes.envelope( orbit( 0, .4 ), ASPECT ).stamp, first.stamp );
});

/*
================
turned

The camera turned by angle about a unit axis (Rodrigues), its up turned
too, so the turn may roll the view: a drag's yaw and pitch never swing a
corner ray toward a side plane's normal, a free turn does.
================
*/
function turned( camera, axis, angle ) {
	const c = Math.cos( angle ), s = Math.sin( angle );
	const turn = v => {
		const d = axis[0] * v[0] + axis[1] * v[1] + axis[2] * v[2];
		const x = [ axis[1] * v[2] - axis[2] * v[1], axis[2] * v[0] - axis[0] * v[2], axis[0] * v[1] - axis[1] * v[0] ];
		return v.map( ( value, i ) => value * c + x[i] * s + axis[i] * d * (1 - c) );
	};
	const forward = turn( camera.target.map( ( v, i ) => v - camera.eye[i] ) );
	return /** @type {any} */ ({
		...camera,
		target: camera.eye.map( ( v, i ) => v + forward[i] ),
		up: turn( camera.up ?? [ 0, 1, 0 ] )
	});
}

test("a turn within the margin about any axis keeps the far corners of the view inside the envelope", () => {
	const envelopes = createCullEnvelope(), next = random( 11 );
	const start = { ...orbit( .3, .4 ), fov: 1.2 }, envelope = envelopes.envelope( start, ASPECT );
	const wide = prepareViewFrustum( envelope.matrix );
	let checked = 0;
	for ( let view = 0; view < 60; view++ ) {
		const z = next() * 2 - 1, phi = next() * 2 * Math.PI, r = Math.sqrt( 1 - z * z );
		const live = turned( start, [ r * Math.cos( phi ), r * Math.sin( phi ), z ], CULL_MARGIN_RAD * .99 );
		assert.equal( envelopes.envelope( live, ASPECT ), envelope, `view ${view} left the envelope` );
		const { tanX, tanY } = projectionTangents( live, ASPECT ), basis = cameraBasis( live );
		for ( const [sx, sy] of [ [ 1, 1 ], [ 1, -1 ], [ -1, 1 ], [ -1, -1 ] ] ) {
			for ( const depth of [ 500, 1500, 3000, 3400 ] ) {
				// A point just inside the live corner ray at this depth.
				const x = sx * tanX * .999 * depth, y = sy * tanY * .999 * depth;
				const p = [ 0, 1, 2 ].map( i =>
					live.eye[i] + basis.right[i] * x + basis.up[i] * y + basis.forward[i] * depth
				);
				checked++;
				assert.ok(
					visibleFrustumSphere( wide, p[0], p[1], p[2], 1 + envelope.slack ),
					`view ${view}: the corner (${sx}, ${sy}) at depth ${depth} is outside the envelope`
				);
			}
		}
	}
	assert.equal( checked, 60 * 16 );
});
