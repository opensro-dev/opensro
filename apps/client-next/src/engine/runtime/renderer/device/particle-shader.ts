/*
===========================================================================

particle-shader.ts - the emitted particle presentation pass

One invocation draws one particle slot. A workgroup descriptor selects its
stream's resident records, output slots and immutable frame rows. It writes
the slot's instance (geometry.ts's 40-float instance layout) and its
one-joint palette, continuing the last tick by the frame's fraction of
the next one (particle-presentation.ts, a deliberate smoothing
deviation). The steps follow the CPU order they replace:

  instance  the tick matrix, turned by its rotation step and moved by its
            motion (particleElementMatrix), then carried by the actor's
            holder follow when its links ride the holder
            (particle-holder-link.ts followMatrix, native B153A0);
  palette   the actor palette; columns scaled by the presented scale and,
            for a primitive's own program, moved to the presented
            position; faced by the view mode (faceEffectMesh); then
            times the presented rotation;
  look      the material frame at the particle's age.

An empty slot, or one outside its lifetime, is a zero matrix: the draw
keeps one instance per slot and the slot's vertices collapse.

===========================================================================
*/
export const particleShader = `
struct Actor {
	clock: vec4f,		// time, opacity, graph tick fraction
	palette: mat4x4f,
	follow: mat4x4f,	// the holder's turn and move since the graph's tick
	pivot: vec4f,		// the holder's position at that tick
}
struct Params {
	shape: vec4u,		// slots per actor, slots, graph, view mode
	timing: vec4f,		// lifetime, loop, frame rate, frames
	sampling: vec4u,	// step sampling
	axis0: vec4f,		// the camera basis of the view mode
	axis1: vec4f,
	axis2: vec4f,
}
struct Instance {
	matrix: mat4x4f,
	opacity: vec4f,
	color: vec4f,
	window: vec4f,
	pointPosition: vec4f,
	pointAmbient: vec4f,
	pointDiffuse: vec4f,
}
@group(0) @binding(0) var<storage, read> records: array<vec4f>;
// One descriptor per workgroup, followed by each stream's Params and Actors.
@group(0) @binding(1) var<storage, read> frame: array<vec4u>;
@group(0) @binding(2) var<storage, read> frames: array<vec4f>;
@group(0) @binding(3) var<storage, read_write> instances: array<Instance>;
@group(0) @binding(4) var<storage, read_write> bones: array<mat4x4f>;

const RECORD_LIVE = 1u;
const RECORD_PRESENTED = 2u;
// particle-holder-link.ts LINK_TRANSLATE, LINK_TURN, LINK_MATRIX.
const LINK_TRANSLATE = 4u;
const LINK_TURN = 8u;
const LINK_MATRIX = 16u;
// vec4s in one slot's record (particle-records.ts PARTICLE_RECORD / 4).
const RECORD_ROW = 16u;
// vec4s in one actor row (particle-records.ts PARTICLE_ACTOR / 4).
const ACTOR_ROW = 10u;
const VIEW_NONE = 0u;
const VIEW_V = 3u;
const TICKS_PER_SECOND = 20.0;
const MIN_DETERMINANT = 1e-12;

// turnByStep: m with its 3x3 part times the step's rotation by fraction of
// its angle about its unit axis; the rest of m is kept.
fn turn( m: mat4x4f, step: vec4f, fraction: f32 ) -> mat4x4f {
	if ( step.w == 0.0 || fraction <= 0.0 ) {
		return m;
	}
	let x = step.x;
	let y = step.y;
	let z = step.z;
	let angle = step.w * fraction;
	let c = cos( angle );
	let s = sin( angle );
	let t = 1.0 - c;
	let p0 = vec3f( t * x * x + c, t * x * y + s * z, t * x * z - s * y );
	let p1 = vec3f( t * x * y - s * z, t * y * y + c, t * y * z + s * x );
	let p2 = vec3f( t * x * z + s * y, t * y * z - s * x, t * z * z + c );
	let r = mat3x3f( m[0].xyz, m[1].xyz, m[2].xyz );
	return mat4x4f( vec4f( r * p0, m[0].w ), vec4f( r * p1, m[1].w ), vec4f( r * p2, m[2].w ), m[3] );
}

fn faceColumn(
	r0: vec3f,
	r1: vec3f,
	r2: vec3f,
	det: f32,
	axis: vec3f,
	columnScale: f32,
	fallback: vec4f
) -> vec4f {
	let size = length( axis );
	if ( !( size >= MIN_DETERMINANT ) ) {
		return fallback;
	}
	let turned = vec3f( dot( r0, axis ), dot( r1, axis ), dot( r2, axis ) ) / det / size * columnScale;
	return vec4f( turned, fallback.w );
}

// faceEffectMesh (effect-billboard.ts): the palette's rotation replaced by
// the view mode's basis, converted back through the instance matrix,
// keeping the palette's scale and translation.
fn face( palette: mat4x4f, instance: mat4x4f, mode: u32, velocity: vec3f, params: Params ) -> mat4x4f {
	let a = instance[0].xyz;
	let b = instance[1].xyz;
	let c = instance[2].xyz;
	let r0 = cross( b, c );
	let r1 = cross( c, a );
	let r2 = cross( a, b );
	let det = dot( a, r0 );
	if ( abs( det ) < MIN_DETERMINANT ) {
		return palette;
	}
	let p0 = palette[0].xyz;
	let p1 = palette[1].xyz;
	let p2 = palette[2].xyz;
	var axis0 = params.axis0.xyz;
	var axis1 = params.axis1.xyz;
	var axis2 = params.axis2.xyz;
	var kept = false;
	if ( mode == VIEW_V ) {
		var valid = false;
		let speed = length( velocity );
		if ( speed > 0.0 ) {
			let v = velocity / speed;
			if ( !( v.x == 0.0 && v.y == 1.0 && v.z == 0.0 ) ) {
				let across = cross( v, vec3f( 0.0, 1.0, 0.0 ) );
				if ( length( across ) > 0.0 ) {
					let side = normalize( across );
					let ahead = cross( v, side );
					if ( length( ahead ) > 0.0 ) {
						axis0 = normalize( ahead );
						axis1 = v;
						axis2 = side;
						valid = true;
					}
				}
			}
		}
		if ( !valid ) {
			kept = true;
			axis0 = p0;
			axis1 = p1;
			axis2 = p2;
		}
	}
	let s0 = select( length( p0 ), p0.x, !kept && p0.y == 0.0 && p0.z == 0.0 );
	let s1 = select( length( p1 ), p1.y, !kept && p1.x == 0.0 && p1.z == 0.0 );
	let s2 = select( length( p2 ), p2.z, !kept && p2.x == 0.0 && p2.y == 0.0 );
	let scale = vec3f( length( a ) * s0, length( b ) * s1, length( c ) * s2 );
	return mat4x4f(
		faceColumn( r0, r1, r2, det, axis0, scale.x, palette[0] ),
		faceColumn( r0, r1, r2, det, axis1, scale.y, palette[1] ),
		faceColumn( r0, r1, r2, det, axis2, scale.z, palette[2] ),
		palette[3]
	);
}

@compute @workgroup_size(64) fn main( @builtin(workgroup_id) group: vec3u, @builtin(local_invocation_index) local: u32 ) {
	let descriptor = frame[group.x];
	let slot = descriptor.w + local;
	let at = descriptor.x;
	let params = Params( frame[at], bitcast<vec4f>( frame[at + 1u] ), frame[at + 2u],
		bitcast<vec4f>( frame[at + 3u] ), bitcast<vec4f>( frame[at + 4u] ), bitcast<vec4f>( frame[at + 5u] ) );
	if ( slot >= params.shape.y ) {
		return;
	}
	let output = descriptor.z + slot;
	let base = ( descriptor.y + slot ) * RECORD_ROW;
	let motionFlags = records[base + 5u];
	let positionTick = records[base + 6u];
	let positionMotionBirth = records[base + 7u];
	let row = at + 6u + ( slot / params.shape.x ) * ACTOR_ROW;
	let actor = Actor( bitcast<vec4f>( frame[row] ), mat4x4f(
		bitcast<vec4f>( frame[row + 1u] ), bitcast<vec4f>( frame[row + 2u] ),
		bitcast<vec4f>( frame[row + 3u] ), bitcast<vec4f>( frame[row + 4u] ) ), mat4x4f(
		bitcast<vec4f>( frame[row + 5u] ), bitcast<vec4f>( frame[row + 6u] ),
		bitcast<vec4f>( frame[row + 7u] ), bitcast<vec4f>( frame[row + 8u] ) ), bitcast<vec4f>( frame[row + 9u] ) );
	let lifetime = params.timing.x;
	var age = actor.clock.x - positionMotionBirth.w;
	if ( params.timing.y > 0.5 && age >= 0.0 && lifetime > 0.0 ) {
		age = age - floor( age / lifetime ) * lifetime;
	}
	let flags = u32( motionFlags.w );
	if ( ( flags & RECORD_LIVE ) == 0u || age < 0.0 || age >= lifetime ) {
		instances[output].matrix = mat4x4f();
		return;
	}
	// A graph ticks as a whole: its fraction is the actor's. A program
	// particle is timed by its own age from the tick it was advanced to.
	var fraction = clamp( actor.clock.z, 0.0, 1.0 );
	if ( params.shape.z == 0u ) {
		fraction = clamp( age * TICKS_PER_SECOND - positionTick.w, 0.0, 1.0 );
	}
	let recordMatrix = mat4x4f( records[base], records[base + 1u], records[base + 2u], records[base + 3u] );
	var instance = turn( recordMatrix, records[base + 4u], fraction );
	instance[3] = vec4f( recordMatrix[3].xyz + motionFlags.xyz * fraction, recordMatrix[3].w );
	// followMatrix: turn about the holder's tick position, move, turn the frame.
	let turn3 = mat3x3f( actor.follow[0].xyz, actor.follow[1].xyz, actor.follow[2].xyz );
	if ( ( flags & LINK_TURN ) != 0u ) {
		instance[3] = vec4f( turn3 * ( instance[3].xyz - actor.pivot.xyz ) + actor.pivot.xyz, instance[3].w );
	}
	if ( ( flags & LINK_TRANSLATE ) != 0u ) {
		instance[3] = vec4f( instance[3].xyz + actor.follow[3].xyz, instance[3].w );
	}
	if ( ( flags & LINK_MATRIX ) != 0u ) {
		instance[0] = vec4f( turn3 * instance[0].xyz, instance[0].w );
		instance[1] = vec4f( turn3 * instance[1].xyz, instance[1].w );
		instance[2] = vec4f( turn3 * instance[2].xyz, instance[2].w );
	}
	let presented = ( flags & RECORD_PRESENTED ) != 0u;
	var palette = actor.palette;
	if ( presented ) {
		let scale = records[base + 8u].xyz + records[base + 9u].xyz * fraction;
		palette[0] = vec4f( palette[0].xyz * scale.x, palette[0].w );
		palette[1] = vec4f( palette[1].xyz * scale.y, palette[1].w );
		palette[2] = vec4f( palette[2].xyz * scale.z, palette[2].w );
		if ( params.shape.z == 0u ) {
			let position = positionTick.xyz + positionMotionBirth.xyz * fraction;
			palette[3] = vec4f( palette[3].xyz + position, palette[3].w );
		}
	}
	if ( params.shape.w != VIEW_NONE ) {
		let velocity = select( vec3f( 0.0 ), records[base + 15u].xyz, presented );
		palette = face( palette, instance, params.shape.w, velocity, params );
	}
	if ( presented ) {
		let recordRotation = mat4x4f(
			records[base + 10u],
			records[base + 11u],
			records[base + 12u],
			records[base + 13u]
		);
		palette = palette * turn( recordRotation, records[base + 14u], fraction );
	}
	var color = vec4f( 1.0 );
	var window = vec4f( 1.0, 1.0, 0.0, 0.0 );
	let count = u32( params.timing.w );
	if ( count > 0u ) {
		let at = clamp( age * params.timing.z, 0.0, f32( count - 1u ) );
		let index = u32( floor( at ) );
		let next = min( count - 1u, index + 1u );
		let blend = select( at - f32( index ), 0.0, params.sampling.x == 1u );
		let start = params.sampling.y;
		color = frames[start + index] * ( 1.0 - blend ) + frames[start + next] * blend;
		window = frames[start + count + index];
	}
	instances[output] = Instance(
		instance,
		vec4f( actor.clock.y, 0.0, 0.0, 0.0 ),
		color,
		window,
		vec4f( 0.0 ),
		vec4f( 0.0 ),
		vec4f( 0.0 )
	);
	bones[output] = palette;
}`;
