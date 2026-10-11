/*
===========================================================================

particle-holder-link.test.mjs - linked effect elements ride their holder

Native CEFEffect_Render (B153A0) carries elements linked to their holder
by the holder's motion on every draw (CEFEffect_UpdateElementMatrices
AF13D0), while the effect ticks at 20 Hz. A holder that walks and turns,
drawn at irregular frame times: an element with position, follow and
matrix links stays at its authored offset in the holder's frame, and a
follow-only element stays on the holder, on the CPU ribbon path and on the
GPU record path alike. Drawn by their last tick velocity instead, both
trailed and overshot the holder: the shaking aura of the owner's report.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const load = file => import( sourceFileUrl( file ).href );
const { createParticleGraph, advanceParticleGraph, particleElementMatrix } = await load(
	"src/engine/foundation/animation/particle-graph.ts"
);
const { HOLDER_FOLLOW, holderFollow, LINK_MATRIX, LINK_TRANSLATE, LINK_TURN } = await load(
	"src/engine/foundation/animation/particle-holder-link.ts"
);
const { writeGraphRecord, PARTICLE_RECORD, PARTICLE_ACTOR, ACTOR_PALETTE, ACTOR_FRACTION, ACTOR_FOLLOW } = await load(
	"src/engine/foundation/animation/particle-records.ts"
);
const { ROTATION_WORK } = await load( "src/engine/foundation/animation/particle-presentation.ts" );
const { presentSlot } = await import( "../helpers/particle-reference.mjs" );

const table = new Float32Array( [ .25, .5, .75 ] );
// The holder walks along x and turns about y.
const SPEED = 100;
const TURN_RATE = 2;
// The rigid element's authored offset from the holder.
const OFFSET = [ 10, 0, 0 ];
// Irregular frame steps (ms), as a stalling main thread draws them.
const FRAME_STEPS_MS = [ 7, 16, 33, 61, 9, 16, 48, 3, 25, 70 ];
const emitter = overrides => ({
	parent: -1,
	parents: [ 0 ],
	births: [ 0 ],
	frames: 400,
	keepMatrix: true,
	keepOrigin: true,
	positionDepth: 0,
	matrixDepth: 0,
	velocityDepth: 0,
	followDepth: 0,
	scales: [],
	positions: [],
	rotations: [],
	...overrides
});
// 0: position, follow and matrix links with an authored offset; 1: follow only.
const GRAPH = [
	emitter( { positionDepth: 1, followDepth: 1, matrixDepth: 1, positions: [ OFFSET ] } ),
	emitter( { followDepth: 1 } )
];

/*
================
holder

The holder's world matrix at seconds.
================
*/
function holder( seconds ) {
	const angle = TURN_RATE * seconds, c = Math.cos( angle ), s = Math.sin( angle );
	return new Float32Array( [ c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, SPEED * seconds, 0, 0, 1 ] );
}

/*
================
replay

Draw both elements at every jittered frame for two seconds. draw(element,
fraction, link) returns the drawn matrix; check gets the frame's holder.
================
*/
function replay( draw, check ) {
	const state = createParticleGraph( GRAPH, 0 );
	const link = {
		root: state.root,
		follow: new Float32Array( HOLDER_FOLLOW ),
		turned: new Float32Array( 16 ),
		shift: [ 0, 0, 0 ]
	};
	let ms = 0;
	for ( let frame = 0; ms <= 2000; frame++ ) {
		const seconds = ms / 1000, transform = holder( seconds );
		advanceParticleGraph( state, GRAPH, seconds, transform, table );
		holderFollow( state.root.matrix, state.root.state.position, transform, link.follow );
		const fraction = seconds * 20 - state.frame;
		const rigid = state.elements[0][0], follower = state.elements[1][0];
		if ( rigid?.alive && follower?.alive && state.frame > 1 ) {
			check( transform, draw( rigid, fraction, link, state ), draw( follower, fraction, link, state ), link );
		}
		ms += FRAME_STEPS_MS[frame % FRAME_STEPS_MS.length];
	}
}

/*
================
offsetError

How far a drawn rigid element and follower are from where the holder
carries them this frame, in position and in the rigid frame's columns.
================
*/
function offsetError( transform, rigid, follower ) {
	let error = 0;
	for ( let row = 0; row < 3; row++ ) {
		const expected = transform[row] * OFFSET[0] + transform[4 + row] * OFFSET[1] + transform[8 + row] * OFFSET[2] +
			transform[12 + row];
		error = Math.max(
			error,
			Math.abs( rigid[12 + row] - expected ),
			Math.abs( follower[12 + row] - transform[12 + row] )
		);
		for ( let column = 0; column < 3; column++ ) {
			error = Math.max( error, Math.abs( rigid[column * 4 + row] - transform[column * 4 + row] ) );
		}
	}
	return error;
}

/*
================
cpuDraw
================
*/
function cpuDraw( element, fraction, link ) {
	const out = new Float32Array( 16 );
	particleElementMatrix( element, out, 0, fraction, undefined, link );
	return out;
}

test("linked elements ride a walking, turning holder at every frame", () => {
	let worst = 0, checked = 0;
	replay( cpuDraw, ( transform, rigid, follower, link ) => {
		assert.equal( link.root.rootLink, 0 );
		worst = Math.max( worst, offsetError( transform, rigid, follower ) );
		checked++;
	} );
	assert.ok( checked > 50, `checked ${checked} frames` );
	assert.ok( worst < 0.01, `worst drift ${worst}` );
});

test("each element carries the links its depths name", () => {
	const state = createParticleGraph( GRAPH, 0 );
	advanceParticleGraph( state, GRAPH, .2, holder( .2 ), table );
	assert.equal( state.elements[0][0].rootLink, LINK_TURN | LINK_TRANSLATE | LINK_MATRIX );
	assert.equal( state.elements[1][0].rootLink, LINK_TRANSLATE );
});

test("drawn by last tick velocity alone, the elements trail and overshoot the holder", () => {
	// The behaviour this replaces, kept as the control of the test above.
	let worst = 0;
	replay( ( element, fraction ) => cpuDraw( element, fraction, undefined ), ( transform, rigid, follower ) => {
		worst = Math.max( worst, offsetError( transform, rigid, follower ) );
	} );
	assert.ok( worst > 1, `the unlinked draw drifts (worst ${worst})` );
});

test("the GPU record path draws what the CPU path draws", () => {
	let worst = 0;
	const gpuDraw = ( element, fraction, link, state ) => {
		const records = new Float32Array( PARTICLE_RECORD ), actors = new Float32Array( PARTICLE_ACTOR );
		writeGraphRecord( records, 0, element, new Float64Array( ROTATION_WORK ), link );
		actors.set( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ], ACTOR_PALETTE );
		actors.set( link.follow, ACTOR_FOLLOW );
		actors[0] = state.frame / 20;
		actors[ACTOR_FRACTION] = fraction;
		const particles = { rows: 1, slots: 1, graph: true, view: 0, lifetime: 1000, loop: false, actors };
		const slot = presentSlot( particles, records, 0 );
		assert.ok( slot, "the element is drawn" );
		const cpu = cpuDraw( element, fraction, link );
		slot.matrix.forEach( ( value, i ) => worst = Math.max( worst, Math.abs( value - cpu[i] ) ) );
		return slot.matrix;
	};
	replay( gpuDraw, () => {} );
	assert.ok( worst < 1e-3, `GPU and CPU differ by ${worst}` );
});

test("the actor row holds the holder follow, as the shader reads it", () => {
	// particle-shader.ts reads ACTOR_ROW (10) vec4s an actor, the follow at vec4 5.
	assert.equal( ACTOR_FOLLOW + HOLDER_FOLLOW, PARTICLE_ACTOR );
	assert.equal( PARTICLE_ACTOR, 40 );
	assert.equal( ACTOR_FOLLOW, 20 );
});
