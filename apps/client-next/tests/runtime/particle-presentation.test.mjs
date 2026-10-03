/*
===========================================================================

particle-presentation.test.mjs - 20 Hz particle ticks drawn between ticks

The simulation keeps native 50 ms ticks; presentation carries the drawn
particle by the fraction of the next tick. These tests pin that drawn
values move between ticks and that the tick state itself is untouched.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const load = file => import( sourceFileUrl( file ).href );
const { createPresentedParticle, presentParticle, snapshotParticleTick, continueRotation } = await load(
	"src/engine/foundation/animation/particle-presentation.ts"
);
const { createParticleGraph, advanceParticleGraph, particleElementMatrix } = await load(
	"src/engine/foundation/animation/particle-graph.ts"
);
const { createZoomEase, ZOOM_EASE_TIME_MS } = await load( "src/engine/foundation/rendering/camera-zoom-ease.ts" );

const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
const spinZ = angle => {
	const m = identity(), c = Math.cos( angle ), s = Math.sin( angle );
	m[0] = c;
	m[1] = s;
	m[4] = -s;
	m[5] = c;
	return m;
};
const angleZ = ( m, offset = 0 ) => Math.atan2( m[offset + 1], m[offset] );
const particle = overrides => ({
	position: [ 0, 0, 0 ],
	velocity: [ 0, 0, 0 ],
	scale: [ 1, 1, 1 ],
	rotation: identity(),
	frame: 0,
	...overrides
});

test("a drawn particle continues its last tick between ticks", () => {
	const state = particle( { position: [ 0, 0, 0 ], scale: [ 1, 1, 1 ], rotation: spinZ( .2 ) } );
	snapshotParticleTick( state );
	state.position = [ 4, 0, -2 ];
	state.scale = [ 2, 2, 2 ];
	state.rotation = spinZ( .4 );
	const out = createPresentedParticle();
	presentParticle( state, .5, out );
	assert.deepEqual( out.position, [ 6, 0, -3 ] );
	assert.deepEqual( out.scale, [ 2.5, 2.5, 2.5 ] );
	assert.ok( Math.abs( angleZ( out.rotation ) - .5 ) < 1e-5, "spin continues by half a tick" );
	// The tick state is presentation input only.
	assert.deepEqual( state.position, [ 4, 0, -2 ] );
	assert.ok( Math.abs( angleZ( state.rotation ) - .4 ) < 1e-6 );
	assert.notEqual( out.rotation, state.rotation, "the scratch never aliases the particle" );
});

test("fraction zero and a fresh particle draw the tick state", () => {
	const state = particle( { position: [ 1, 2, 3 ], rotation: spinZ( .3 ) } ), out = createPresentedParticle();
	presentParticle( state, .7, out );
	assert.deepEqual( out.position, [ 1, 2, 3 ] );
	snapshotParticleTick( state );
	state.position = [ 5, 2, 3 ];
	presentParticle( state, 0, out );
	assert.deepEqual( out.position, [ 5, 2, 3 ] );
	assert.ok( Math.abs( angleZ( out.rotation ) - .3 ) < 1e-6 );
});

test("a rotation reset is drawn as is, not swept", () => {
	const out = new Float32Array( 16 );
	continueRotation( spinZ( 0 ), spinZ( 2.5 ), .5, out, 0 );
	assert.ok( Math.abs( angleZ( out ) - 2.5 ) < 1e-6 );
});

test("rotation continues on top of a scaled frame and keeps translation", () => {
	const previous = spinZ( .1 ), current = spinZ( .2 );
	for ( const m of [ previous, current ] ) {
		for ( const i of [ 0, 1, 2, 4, 5, 6, 8, 9, 10 ] ) m[i] *= 3;
		m[12] = 7;
	}
	const out = new Float32Array( 32 );
	continueRotation( previous, current, .5, out, 16 );
	assert.ok( Math.abs( angleZ( out, 16 ) - .25 ) < 1e-5 );
	assert.ok( Math.abs( Math.hypot( out[16], out[17], out[18] ) - 3 ) < 1e-5, "the column scale survives" );
	assert.equal( out[16 + 12], 7 );
});

test("graph elements record their previous tick for the drawn frame", () => {
	const graph = [ {
		parent: -1,
		parents: [ 0 ],
		births: [ 0 ],
		frames: 40,
		keepMatrix: false,
		keepOrigin: true,
		positionDepth: 0,
		matrixDepth: 0,
		velocityDepth: 0,
		followDepth: 0,
		scales: [],
		positions: [],
		rotations: [],
		program: { vectors: [ { name: "SetVelocity", value: [ 2, 0, 0 ], flags: 0, frames: [ 0 ] } ] }
	} ];
	const state = createParticleGraph( graph, 0 ), table = new Float32Array( [ .5 ] );
	advanceParticleGraph( state, graph, .2, identity(), table );
	const element = state.elements[0][0];
	assert.deepEqual( element.state.previous.position, [ 6, 0, 0 ] );
	assert.deepEqual( element.state.position, [ 8, 0, 0 ] );
	const drawn = new Float32Array( 16 );
	particleElementMatrix( element, drawn, 0, .25 );
	assert.equal( drawn[12], 8.5 );
});

test("the drawn zoom glides to the wheel distance and lands", () => {
	const ease = createZoomEase();
	assert.equal( ease.step( 80, 0 ), 80, "the first frame draws the target" );
	const first = ease.step( 74, 8 );
	assert.ok( first < 80 && first > 74, "one frame after a notch the camera is between" );
	let drawn = first, previous = first;
	for ( let t = 16; t <= 1000; t += 8 ) {
		drawn = ease.step( 74, t );
		assert.ok( drawn <= previous, "the glide never reverses" );
		previous = drawn;
	}
	assert.equal( drawn, 74, "it lands exactly on the native distance" );
	// Frame-rate independent: one long frame covers the same ground as many short ones.
	const coarse = createZoomEase(), fine = createZoomEase();
	coarse.step( 80, 0 );
	fine.step( 80, 0 );
	const a = coarse.step( 70, ZOOM_EASE_TIME_MS );
	let b = 0;
	for ( let t = 1; t <= ZOOM_EASE_TIME_MS; t++ ) b = fine.step( 70, t );
	assert.ok( Math.abs( a - b ) < 1e-9 );
	ease.reset();
	assert.equal( ease.step( 150, 2000 ), 150, "a reset draws the target at once" );
});
