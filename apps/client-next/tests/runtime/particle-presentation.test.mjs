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
const { snapshotParticleTick, tickRotationStep, turnByStep, ROTATION_WORK } = await load(
	"src/engine/foundation/animation/particle-presentation.ts"
);
const {
	writeEmittedRecord,
	writeGraphRecord,
	writeRibbonTick,
	PARTICLE_RECORD,
	PARTICLE_ACTOR,
	ACTOR_PALETTE,
	ACTOR_FRACTION
} = await load( "src/engine/foundation/animation/particle-records.ts" );
const { presentSlot } = await import( "../helpers/particle-reference.mjs" );
const { advanceParticleGraph } = await load( "src/engine/foundation/animation/particle-graph.ts" );
const { createParticleGraph, ribbonElementFrames, SLOT_ALIVE, SLOT_OCCUPIED, SLOT_SNAPSHOT } = await load(
	"src/engine/foundation/animation/particle-graph-slots.ts"
);
const { createParticleGraphPlan } = await load( "src/engine/foundation/animation/particle-graph-plan.ts" );
const { particleElements } = await import( "../helpers/particle-slots.mjs" );
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
// One element drawn both as a ribbon and by a particle stream.
const DRAWS = [ { particleEmitter: 0, ribbon: {} }, { particleEmitter: 0 } ];
const graphEmitter = overrides => ({
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
	...overrides
});
const particle = overrides => ({
	position: [ 0, 0, 0 ],
	velocity: [ 0, 0, 0 ],
	scale: [ 1, 1, 1 ],
	rotation: identity(),
	frame: 0,
	...overrides
});

/*
================
drawParticle

The palette the presentation pass draws for one program particle born at
0, at fraction of the tick after its latest one, from an identity actor
palette and birth matrix.
================
*/
function drawParticle( state, fraction ) {
	const records = new Float32Array( PARTICLE_RECORD ), actors = new Float32Array( PARTICLE_ACTOR );
	writeEmittedRecord( records, 0, identity(), 0, 0, state, new Float64Array( ROTATION_WORK ) );
	actors[0] = (state.frame + fraction) / 20;
	actors.set( identity(), ACTOR_PALETTE );
	const particles = { rows: 1, slots: 1, graph: false, view: 0, lifetime: 100, loop: false, actors };
	const drawn = presentSlot( particles, records, 0 );
	assert.ok( drawn, "the particle is drawn" );
	return drawn.palette;
}

test("a drawn particle continues its last tick between ticks", () => {
	const state = particle( { position: [ 0, 0, 0 ], scale: [ 1, 1, 1 ], rotation: spinZ( .2 ) } );
	snapshotParticleTick( state );
	state.position = [ 4, 0, -2 ];
	state.scale = [ 2, 2, 2 ];
	state.rotation = spinZ( .4 );
	state.frame = 1;
	const palette = drawParticle( state, .5 );
	assert.deepEqual( [ ...palette.subarray( 12, 15 ) ], [ 6, 0, -3 ] );
	assert.ok( Math.abs( Math.hypot( palette[0], palette[1], palette[2] ) - 2.5 ) < 1e-5, "scale continues" );
	assert.ok( Math.abs( angleZ( palette ) - .5 ) < 1e-5, "spin continues by half a tick" );
	// The tick state is presentation input only.
	assert.deepEqual( state.position, [ 4, 0, -2 ] );
	assert.ok( Math.abs( angleZ( state.rotation ) - .4 ) < 1e-6 );
});

test("fraction zero and a fresh particle draw the tick state", () => {
	const state = particle( { position: [ 1, 2, 3 ], rotation: spinZ( .3 ) } );
	assert.deepEqual( [ ...drawParticle( state, .7 ).subarray( 12, 15 ) ], [ 1, 2, 3 ] );
	snapshotParticleTick( state );
	state.position = [ 5, 2, 3 ];
	const palette = drawParticle( state, 0 );
	assert.deepEqual( [ ...palette.subarray( 12, 15 ) ], [ 5, 2, 3 ] );
	assert.ok( Math.abs( angleZ( palette ) - .3 ) < 1e-6 );
});

/*
================
continued

current continued by fraction of the rotation the last tick took from
previous: the tick's step (tickRotationStep), turned by each frame
(turnByStep).
================
*/
function continued( previous, current, fraction, out, offset ) {
	const step = new Float64Array( 4 ), work = new Float64Array( ROTATION_WORK );
	tickRotationStep( previous, current, step, 0, work );
	turnByStep( current, step[0], step[1], step[2], step[3], fraction, out, offset, work );
}

test("a graph record draws the frame the CPU ribbon draws, between ticks and across a reset", () => {
	const graph = [ graphEmitter() ];
	const state = createParticleGraph( graph, 0, createParticleGraphPlan( graph, DRAWS ) ), s = state.plan.base[0];
	const vector = ( array, value ) => array.set( value, s * 3 );
	// One live element with its tick pose, written by hand tick by tick.
	state.counts[0] = 1;
	state.flags[s] = SLOT_OCCUPIED | SLOT_ALIVE | SLOT_SNAPSHOT;
	vector( state.position, [ 1, 2, 3 ] );
	state.rotation.set( spinZ( .1 ), s * 16 );
	state.matrix.set( spinZ( .4 ), s * 16 );
	const snapshot = () => {
		state.previousPosition.set( state.position.subarray( s * 3, s * 3 + 3 ), s * 3 );
		state.previousScale.set( state.scale.subarray( s * 3, s * 3 + 3 ), 0 );
		state.previousRotation.set( state.rotation.subarray( s * 16, s * 16 + 16 ), 0 );
		state.previousMatrix.set( state.matrix.subarray( s * 16, s * 16 + 16 ), 0 );
	};
	for ( const [turn, position] of [ [ .7, [ 2, 2, 5 ] ], [ 3, [ 2, 4, 5 ] ], [ .7, [ 2, 4, 5 ] ] ] ) {
		snapshot();
		state.matrix.set( spinZ( turn ), s * 16 );
		vector( state.position, position );
		state.frames[s]++;
		const records = new Float32Array( PARTICLE_RECORD ), actors = new Float32Array( PARTICLE_ACTOR );
		writeGraphRecord( records, 0, state, s, 0, 0 );
		writeRibbonTick( state.ribbonTicks, state.plan.ribbons[0], state, s, 0, 0 );
		actors.set( identity(), ACTOR_PALETTE );
		for ( const fraction of [ 0, .3, .9 ] ) {
			actors[0] = state.frames[s] / 20;
			actors[ACTOR_FRACTION] = fraction;
			const particles = { rows: 1, slots: 1, graph: true, view: 0, lifetime: 100, loop: false, actors };
			const slot = presentSlot( particles, records, 0 ), expected = new Float32Array( 16 );
			assert.ok( slot, "the element is drawn" );
			ribbonElementFrames(
				state,
				0,
				fraction,
				new Float32Array( 20 ),
				expected,
				new Float64Array( ROTATION_WORK )
			);
			slot.matrix.forEach( ( value, i ) =>
				assert.ok( Math.abs( value - expected[i] ) < 1e-5, `${turn} ${fraction} ${i}` )
			);
		}
	}
});

test("a rotation reset is drawn as is, not swept", () => {
	const out = new Float32Array( 16 );
	continued( spinZ( 0 ), spinZ( 2.5 ), .5, out, 0 );
	assert.ok( Math.abs( angleZ( out ) - 2.5 ) < 1e-6 );
});

test("rotation continues on top of a scaled frame and keeps translation", () => {
	const previous = spinZ( .1 ), current = spinZ( .2 );
	for ( const m of [ previous, current ] ) {
		for ( const i of [ 0, 1, 2, 4, 5, 6, 8, 9, 10 ] ) m[i] *= 3;
		m[12] = 7;
	}
	const out = new Float32Array( 32 );
	continued( previous, current, .5, out, 16 );
	assert.ok( Math.abs( angleZ( out, 16 ) - .25 ) < 1e-5 );
	assert.ok( Math.abs( Math.hypot( out[16], out[17], out[18] ) - 3 ) < 1e-5, "the column scale survives" );
	assert.equal( out[16 + 12], 7 );
});

test("graph elements record their previous tick for the drawn frame", () => {
	const graph = [
		graphEmitter( {
			program: { vectors: [ { name: "SetVelocity", value: [ 2, 0, 0 ], flags: 0, frames: [ 0 ] } ] }
		} )
	];
	const state = createParticleGraph( graph, 0, createParticleGraphPlan( graph, DRAWS ) );
	advanceParticleGraph( state, graph, .2, identity(), new Float32Array( [ .5 ] ) );
	const element = particleElements( state, 0 )[0];
	assert.deepEqual( element.previousPosition, [ 6, 0, 0 ] );
	assert.deepEqual( element.position, [ 8, 0, 0 ] );
	const drawn = new Float32Array( 16 );
	ribbonElementFrames( state, 0, .25, new Float32Array( 20 ), drawn, new Float64Array( ROTATION_WORK ) );
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
