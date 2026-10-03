/*
===========================================================================

particle-reference.mjs - the particle presentation pass, in JavaScript

The GPU pass (src/engine/runtime/renderer/device/particle-shader.ts) step
by step on the CPU functions it replaces: turnByStep, faceEffectMesh and
the pose product multiply. Runtime tests use it as their fake device's pass, and
the browser test holds the GPU pass to it.

Like the device it keeps its own copy of each draw's records and takes
only the slots the presentation marks dirty, so a record the CPU forgot
to mark shows up as a stale particle.

It loads in Node (after tests/helpers/native-source-loader.mjs) and in the
dev server's page alike, so it imports no Node module itself.

===========================================================================
*/
const records = await import( "../../src/engine/foundation/animation/particle-records.ts" );
const { turnByStep, ROTATION_WORK } = await import( "../../src/engine/foundation/animation/particle-presentation.ts" );
const { faceEffectMesh } = await import( "../../src/engine/foundation/rendering/effect-billboard.ts" );
const { multiply } = await import( "../../src/engine/foundation/math/pose-math.ts" );

const {
	PARTICLE_RECORD,
	PARTICLE_ACTOR,
	RECORD_MATRIX,
	RECORD_MATRIX_STEP,
	RECORD_MOTION,
	RECORD_FLAGS,
	RECORD_POSITION,
	RECORD_TICK,
	RECORD_POSITION_MOTION,
	RECORD_BIRTH,
	RECORD_SCALE,
	RECORD_SCALE_MOTION,
	RECORD_ROTATION,
	RECORD_ROTATION_STEP,
	RECORD_VELOCITY,
	RECORD_LIVE,
	RECORD_PRESENTED,
	ACTOR_TIME,
	ACTOR_OPACITY,
	ACTOR_FRACTION,
	ACTOR_PALETTE,
	PARTICLE_VIEW_NONE,
	PARTICLE_VIEW_V
} = records;
const TICKS_PER_SECOND = 20;

/*
================
basisView

A view matrix whose camera basis (cameraAxes "camera") is the
presentation's axes, so faceEffectMesh faces them.
================
*/
function basisView( axes ) {
	const view = new Float32Array( 16 );
	for ( let k = 0; k < 3; k++ ) {
		view[k * 4] = axes[k];
		view[k * 4 + 1] = axes[4 + k];
		view[k * 4 + 3] = axes[8 + k];
	}
	return view;
}

/*
================
clamp01
================
*/
function clamp01( value ) {
	return Math.max( 0, Math.min( 1, value ) );
}

/*
================
presentSlot

One slot's instance matrix, palette, opacity and appearance (color,
window), or null when the slot draws nothing.
================
*/
export function presentSlot( particles, data, slot ) {
	const at = slot * PARTICLE_RECORD, row = Math.floor( slot / particles.slots ) * PARTICLE_ACTOR;
	const actors = particles.actors, lifetime = particles.lifetime;
	let age = actors[row + ACTOR_TIME] - data[at + RECORD_BIRTH];
	if ( particles.loop && age >= 0 ) age %= lifetime;
	const flags = data[at + RECORD_FLAGS];
	if ( !(flags & RECORD_LIVE) || age < 0 || age >= lifetime ) return null;
	const fraction = particles.graph ?
		clamp01( actors[row + ACTOR_FRACTION] ) :
		clamp01( age * TICKS_PER_SECOND - data[at + RECORD_TICK] );
	const work = new Float64Array( ROTATION_WORK ), matrix = new Float32Array( 16 );
	const step = data.subarray( at + RECORD_MATRIX_STEP, at + RECORD_MATRIX_STEP + 4 );
	turnByStep(
		data.subarray( at + RECORD_MATRIX, at + RECORD_MATRIX + 16 ),
		step[0],
		step[1],
		step[2],
		step[3],
		fraction,
		matrix,
		0,
		work
	);
	for ( let axis = 0; axis < 3; axis++ ) {
		matrix[12 + axis] = data[at + RECORD_MATRIX + 12 + axis] + data[at + RECORD_MOTION + axis] * fraction;
	}
	const presented = (flags & RECORD_PRESENTED) !== 0;
	const palette = actors.slice( row + ACTOR_PALETTE, row + ACTOR_PALETTE + 16 );
	if ( presented ) {
		for ( let axis = 0; axis < 3; axis++ ) {
			const scale = data[at + RECORD_SCALE + axis] + data[at + RECORD_SCALE_MOTION + axis] * fraction;
			for ( let k = 0; k < 3; k++ ) palette[axis * 4 + k] *= scale;
			if ( !particles.graph ) {
				palette[12 + axis] += data[at + RECORD_POSITION + axis] +
					data[at + RECORD_POSITION_MOTION + axis] * fraction;
			}
		}
	}
	if ( particles.view !== PARTICLE_VIEW_NONE ) {
		const velocity = presented ?
			Array.from( data.subarray( at + RECORD_VELOCITY, at + RECORD_VELOCITY + 3 ) ) :
			undefined;
		faceEffectMesh(
			palette,
			0,
			matrix,
			basisView( particles.axes ),
			particles.view === PARTICLE_VIEW_V ? "v" : "camera",
			velocity
		);
	}
	if ( presented ) {
		const rotation = new Float32Array( 16 ),
			turn = data.subarray( at + RECORD_ROTATION_STEP, at + RECORD_ROTATION_STEP + 4 );
		turnByStep(
			data.subarray( at + RECORD_ROTATION, at + RECORD_ROTATION + 16 ),
			turn[0],
			turn[1],
			turn[2],
			turn[3],
			fraction,
			rotation,
			0,
			work
		);
		multiply( palette.slice(), rotation, palette );
	}
	let color = [ 1, 1, 1, 1 ], window = [ 1, 1, 0, 0 ];
	const frames = particles.frames;
	if ( frames ) {
		const count = frames.colors.length / 4,
			position = Math.max( 0, Math.min( count - 1, age * frames.fps ) ),
			index = Math.floor( position ),
			next = Math.min( count - 1, index + 1 ),
			blend = frames.sampling === "step" ? 0 : position - index;
		color = [ 0, 1, 2, 3 ].map( c =>
			frames.colors[index * 4 + c] * (1 - blend) + frames.colors[next * 4 + c] * blend
		);
		window = Array.from( frames.windows.subarray( index * 4, index * 4 + 4 ) );
	}
	return { matrix, palette, opacity: actors[row + ACTOR_OPACITY], color, window };
}

/*
================
createParticleReference

A fake device's particle pass. present( draw, particles ) takes the
presentation as the device does and returns the drawn slots in slot order:
their instance matrices, palettes (16 floats each), opacities and
appearance (color and window, 8 floats each), like the old packed upload.
================
*/
export function createParticleReference() {
	const mirrors = new Map();
	return {
		present( draw, particles ) {
			const count = particles.rows * particles.slots;
			let data = mirrors.get( draw );
			if ( !data ) {
				data = particles.records.slice();
				mirrors.set( draw, data );
			} else if ( particles.dirtyStart < particles.dirtyEnd ) {
				data.set(
					particles.records.subarray(
						particles.dirtyStart * PARTICLE_RECORD,
						particles.dirtyEnd * PARTICLE_RECORD
					),
					particles.dirtyStart * PARTICLE_RECORD
				);
			}
			const drawn = [];
			for ( let slot = 0; slot < count; slot++ ) {
				const value = presentSlot( particles, data, slot );
				if ( value ) drawn.push( value );
			}
			const matrices = new Float32Array( drawn.length * 16 ),
				palettes = new Float32Array( drawn.length * 16 ),
				opacities = new Float32Array( drawn.length ),
				appearance = new Float32Array( drawn.length * 8 );
			drawn.forEach( ( value, i ) => {
				matrices.set( value.matrix, i * 16 );
				palettes.set( value.palette, i * 16 );
				opacities[i] = value.opacity;
				appearance.set( value.color, i * 8 );
				appearance.set( value.window, i * 8 + 4 );
			} );
			return { matrices, palettes, opacities, appearance };
		},
		release( draw ) {
			mirrors.delete( draw );
		}
	};
}
