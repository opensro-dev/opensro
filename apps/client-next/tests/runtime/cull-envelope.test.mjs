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
const { prepareViewFrustum, visibleFrustumSphere, viewProjection } = await import(
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
	return { eye, target: TARGET.map( ( v, i ) => v + drift[i] ), fov: 1, near: 1, far: 3500 };
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
