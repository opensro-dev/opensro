/*
===========================================================================

particle-shader.ts - the emitted particle presentation pass

One invocation draws one particle slot. It reads the slot's tick record
and its actor's row (foundation/animation/particle-records.ts) and writes
the slot's instance (geometry.ts's 40-float instance layout) and its
one-joint palette, continuing the last tick by the frame's fraction of
the next one (particle-presentation.ts, a deliberate smoothing
deviation). The steps follow the CPU order they replace:

  instance  the tick matrix, turned by its rotation step and moved by its
            motion (particleElementMatrix);
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
struct Record {
	matrix: mat4x4f,
	matrixStep: vec4f,
	motion: vec3f,
	flags: f32,
	position: vec3f,
	tick: f32,
	positionMotion: vec3f,
	birth: f32,
	scale: vec4f,
	scaleMotion: vec4f,
	rotation: mat4x4f,
	rotationStep: vec4f,
	velocity: vec4f,
}
struct Actor {
	clock: vec4f,		// time, opacity, graph tick fraction
	palette: mat4x4f,
}
struct Params {
	shape: vec4u,		// slots per actor, slots, graph, view mode
	timing: vec4f,		// lifetime, loop, frame rate, frames
	sampling: vec4u,	// step sampling
	axis0: vec4f,		// the camera basis of the view mode
	axis1: vec4f,
	axis2: vec4f,
}
// A stream's block of the frame arena (particles.ts).
struct Frame {
	params: Params,
	actors: array<Actor>,
}
struct Instance {
	matrix: mat4x4f,
	opacity: vec4f,
	color: vec4f,
	window: vec4f,
	light: array<vec4f, 3>,
}
@group(0) @binding(0) var<storage, read> records: array<Record>;
@group(0) @binding(1) var<storage, read> frame: Frame;
@group(0) @binding(2) var<storage, read> frames: array<vec4f>;
@group(0) @binding(3) var<storage, read_write> instances: array<Instance>;
@group(0) @binding(4) var<storage, read_write> bones: array<mat4x4f>;

const RECORD_LIVE = 1u;
const RECORD_PRESENTED = 2u;
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

// velocityBasis (effect-billboard.ts): ViewVBillboard's basis from the
// velocity, or false when the element keeps its own rotation.
fn velocityBasis( velocity: vec3f, axes: ptr<function, mat3x3f> ) -> bool {
	let speed = length( velocity );
	if ( !( speed > 0.0 ) ) {
		return false;
	}
	let v = velocity / speed;
	if ( v.x == 0.0 && v.y == 1.0 && v.z == 0.0 ) {
		return false;
	}
	let across = cross( v, vec3f( 0.0, 1.0, 0.0 ) );
	if ( !( length( across ) > 0.0 ) ) {
		return false;
	}
	let side = normalize( across );
	let ahead = cross( v, side );
	if ( !( length( ahead ) > 0.0 ) ) {
		return false;
	}
	*axes = mat3x3f( normalize( ahead ), v, side );
	return true;
}

// faceEffectMesh (effect-billboard.ts): the palette's rotation replaced by
// the view mode's basis, converted back through the instance matrix,
// keeping the palette's scale and translation.
fn face( palette: mat4x4f, instance: mat4x4f, mode: u32, velocity: vec3f ) -> mat4x4f {
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
	let params = frame.params;
	var axes = mat3x3f( params.axis0.xyz, params.axis1.xyz, params.axis2.xyz );
	var kept = false;
	if ( mode == VIEW_V && !velocityBasis( velocity, &axes ) ) {
		kept = true;
		axes = mat3x3f( p0, p1, p2 );
	}
	let s0 = select( length( p0 ), p0.x, !kept && p0.y == 0.0 && p0.z == 0.0 );
	let s1 = select( length( p1 ), p1.y, !kept && p1.x == 0.0 && p1.z == 0.0 );
	let s2 = select( length( p2 ), p2.z, !kept && p2.x == 0.0 && p2.y == 0.0 );
	let scale = vec3f( length( a ) * s0, length( b ) * s1, length( c ) * s2 );
	var out = palette;
	for ( var column = 0u; column < 3u; column++ ) {
		let axis = axes[column];
		let size = length( axis );
		if ( !( size >= MIN_DETERMINANT ) ) {
			continue;
		}
		let turned = vec3f( dot( r0, axis ), dot( r1, axis ), dot( r2, axis ) ) / det / size * scale[column];
		out[column] = vec4f( turned, palette[column].w );
	}
	return out;
}

@compute @workgroup_size(64) fn main( @builtin(global_invocation_id) id: vec3u ) {
	let slot = id.x;
	let params = frame.params;
	if ( slot >= params.shape.y ) {
		return;
	}
	let record = records[slot];
	let actor = frame.actors[slot / params.shape.x];
	let lifetime = params.timing.x;
	var age = actor.clock.x - record.birth;
	if ( params.timing.y > 0.5 && age >= 0.0 ) {
		age = age % lifetime;
	}
	let flags = u32( record.flags );
	if ( ( flags & RECORD_LIVE ) == 0u || age < 0.0 || age >= lifetime ) {
		instances[slot].matrix = mat4x4f();
		return;
	}
	// A graph ticks as a whole: its fraction is the actor's. A program
	// particle is timed by its own age from the tick it was advanced to.
	var fraction = clamp( actor.clock.z, 0.0, 1.0 );
	if ( params.shape.z == 0u ) {
		fraction = clamp( age * TICKS_PER_SECOND - record.tick, 0.0, 1.0 );
	}
	var instance = turn( record.matrix, record.matrixStep, fraction );
	instance[3] = vec4f( record.matrix[3].xyz + record.motion * fraction, record.matrix[3].w );
	let presented = ( flags & RECORD_PRESENTED ) != 0u;
	var palette = actor.palette;
	if ( presented ) {
		let scale = record.scale.xyz + record.scaleMotion.xyz * fraction;
		palette[0] = vec4f( palette[0].xyz * scale.x, palette[0].w );
		palette[1] = vec4f( palette[1].xyz * scale.y, palette[1].w );
		palette[2] = vec4f( palette[2].xyz * scale.z, palette[2].w );
		if ( params.shape.z == 0u ) {
			let position = record.position + record.positionMotion * fraction;
			palette[3] = vec4f( palette[3].xyz + position, palette[3].w );
		}
	}
	if ( params.shape.w != VIEW_NONE ) {
		palette = face( palette, instance, params.shape.w, select( vec3f( 0.0 ), record.velocity.xyz, presented ) );
	}
	if ( presented ) {
		palette = palette * turn( record.rotation, record.rotationStep, fraction );
	}
	var color = vec4f( 1.0 );
	var window = vec4f( 1.0, 1.0, 0.0, 0.0 );
	let count = u32( params.timing.w );
	if ( count > 0u ) {
		let at = clamp( age * params.timing.z, 0.0, f32( count - 1u ) );
		let index = u32( floor( at ) );
		let next = min( count - 1u, index + 1u );
		let blend = select( at - f32( index ), 0.0, params.sampling.x == 1u );
		color = frames[index] * ( 1.0 - blend ) + frames[next] * blend;
		window = frames[count + index];
	}
	instances[slot] = Instance( instance, vec4f( actor.clock.y, 0.0, 0.0, 0.0 ), color, window, array<vec4f, 3>() );
	bones[slot] = palette;
}`;
