/*
===========================================================================

geometry-uploads.test.mjs - submission upload counts, bytes and retirement

The immediate path is the old writeBuffer reference. A fake queue executes
copies only at submit and checks every byte of each original destination.
The shared loader imports the shipped TypeScript without a private bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } from "../helpers/strict-gpu.mjs";

const { createGeometryResources } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/device/geometry.ts" ).href
);
const { createRetirement } = await import( sourceFileUrl( "src/engine/runtime/renderer/device/retirement.ts" ).href );
globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;

const ACTORS = 32;
const VERTEX_FLOATS = 14;
const INSTANCE_FLOATS = 16;
const BONE_FLOATS = 32;

/*
================
fixture

Copies capture buffer references, not source bytes, until queue.submit.
================
*/
function fixture( options = {} ) {
	const buffers = [], writes = [], targets = [], retirement = createRetirement();
	let bindings = 0, submissions = 0;
	const device = {
		limits: { maxBufferSize: options.limit ?? 256 * 1024 * 1024 },
		createBuffer( descriptor ) {
			const buffer = {
				...descriptor,
				bytes: new Uint8Array( descriptor.size ),
				destroyed: false,
				destroy() {
					assert.equal( this.destroyed, false );
					this.destroyed = true;
				}
			};
			buffers.push( buffer );
			return buffer;
		},
		createTexture() {
			return { createView: () => ({}), destroy() {} };
		},
		createBindGroup( { entries } ) {
			bindings++;
			return entries;
		},
		pushErrorScope() {},
		popErrorScope: async () => null,
		createCommandEncoder() {
			const commands = [];
			return {
				copyBufferToBuffer( source, start, target, offset, size ) {
					assert.equal( source.usage & GPU_BUFFER_USAGE.COPY_SRC, GPU_BUFFER_USAGE.COPY_SRC );
					assert.equal( target.usage & GPU_BUFFER_USAGE.COPY_DST, GPU_BUFFER_USAGE.COPY_DST );
					assert.equal( (start | offset | size) % 4, 0 );
					assert.ok( start + size <= source.size && offset + size <= target.size );
					targets.push( target );
					commands.push( () => {
						assert.equal( source.destroyed || target.destroyed, false );
						target.bytes.set( source.bytes.subarray( start, start + size ), offset );
					} );
				},
				finish: () => commands
			};
		},
		queue: {
			writeTexture() {},
			writeBuffer( buffer, offset, data, start, size ) {
				assert.equal( buffer.destroyed, false );
				assert.equal( buffer.usage & GPU_BUFFER_USAGE.COPY_DST, GPU_BUFFER_USAGE.COPY_DST );
				assert.equal( (offset | size) % 4, 0 );
				assert.ok( offset + size <= buffer.size );
				buffer.bytes.set( new Uint8Array( data, start, size ), offset );
				writes.push( buffer );
			},
			submit( commandBuffers ) {
				submissions++;
				for ( const commands of commandBuffers ) for ( const command of commands ) command();
			}
		}
	};
	const resources = createGeometryResources(
		device,
		() => device,
		error => {
			throw error;
		},
		() => ({ getBindGroupLayout: () => ({}) }),
		() => ({}),
		{},
		{},
		{},
		undefined,
		options.animation,
		"rgba8unorm",
		options.particles,
		retirement.retire
	);
	return {
		resources,
		buffers,
		writes,
		targets,
		retirement,
		bindings: () => bindings,
		submissions: () => submissions
	};
}

/*
================
geometry
================
*/
function geometry() {
	return {
		positions: new Float32Array( 9 ),
		vertices: new Float32Array( VERTEX_FLOATS * 3 ),
		indices: Uint32Array.of( 0, 1, 2 ),
		transform: new Float32Array( INSTANCE_FLOATS ),
		joints: new Uint32Array( 12 ),
		weights: new Float32Array( 12 ),
		bones: new Float32Array( BONE_FLOATS ),
		dynamicVertices: true
	};
}

/*
================
bound
================
*/
function bound( draw, binding ) {
	return draw.binding.find( entry => entry.binding === binding ).resource.buffer;
}

/*
================
destinations
================
*/
function destinations( draw ) {
	return [ draw.vertices, bound( draw, 7 ), bound( draw, 1 ) ];
}

/*
================
compare
================
*/
function compare( expected, actual ) {
	for ( let i = 0; i < expected.length; i++ ) {
		const before = destinations( expected[i] ), after = destinations( actual[i] );
		for ( let stream = 0; stream < before.length; stream++ ) {
			assert.deepEqual( after[stream].bytes, before[stream].bytes, `actor ${i} stream ${stream}` );
		}
	}
}

/*
================
frame

All actors reuse caller views and instance packing scratch. Changing them
after each command must not change the already admitted upload snapshot.
================
*/
function frame( resources, draws, tick ) {
	const commands = resources.commands;
	const vertices = new Float32Array( VERTEX_FLOATS * 3 + 2 ).subarray( 1, -1 );
	const bones = new Float32Array( BONE_FLOATS + 2 ).subarray( 1, -1 );
	const instances = new Float32Array( INSTANCE_FLOATS + 2 ).subarray( 1, -1 );
	for ( let actor = 0; actor < draws.length; actor++ ) {
		const value = actor + tick * ACTORS;
		vertices.fill( value + .25 );
		bones.fill( value + .5 );
		instances.fill( value + .75 );
		commands.writeVertices( draws[actor], 0, vertices );
		commands.updateBones( draws[actor], bones );
		commands.updateInstances( draws[actor], instances );
		vertices.fill( -1 );
		bones.fill( -2 );
		instances.fill( -3 );
	}
}

/*
================
32 actors use one upload per frame with identical bytes and draw order
================
*/
test("32 actors use one upload per frame with identical bytes and draw order", () => {
	const old = fixture(), arena = fixture();
	const expected = Array.from( { length: ACTORS }, () => old.resources.commands.upload( geometry() ) );
	const actual = Array.from( { length: ACTORS }, () => arena.resources.commands.upload( geometry() ) );
	const bindings = arena.bindings();
	for ( let tick = 0; tick < 3; tick++ ) {
		old.writes.length = 0;
		arena.writes.length = 0;
		arena.retirement.open();
		arena.resources.beginFrame();
		frame( old.resources, expected, tick );
		frame( arena.resources, actual, tick );
		const observed = [];
		old.resources.uploads.submit( [] );
		arena.resources.uploads.submit( actual.map( ( draw, index ) => () => {
			compare( [ expected[index] ], [ draw ] );
			observed.push( index );
		} ) );
		assert.deepEqual( observed, Array.from( { length: ACTORS }, ( _, index ) => index ) );
		assert.equal( old.writes.length, ACTORS * 3 );
		assert.equal( arena.writes.length, 1 );
		assert.equal( arena.bindings(), bindings );
		arena.resources.endFrame();
		arena.retirement.close();
	}
	assert.equal( arena.submissions(), 3 );
	assert.equal( arena.buffers.filter( buffer => buffer.label === "geometry-upload-arena" ).length, 1 );
	old.resources.dispose();
	arena.resources.dispose();
});

/*
================
Partial overlapping writes, growth and two deferred submissions retain bytes
================
*/
test("partial overlapping writes, growth and two deferred submissions retain bytes", () => {
	const old = fixture(), arena = fixture();
	const expected = old.resources.commands.upload( geometry() ),
		actual = arena.resources.commands.upload( geometry() );
	arena.retirement.open();
	arena.resources.beginFrame();
	for ( const [reference, draw] of [ [ old, expected ], [ arena, actual ] ] ) {
		reference.resources.commands.writeVertices( draw, 1, new Float32Array( VERTEX_FLOATS ).fill( 7 ) );
		reference.resources.commands.writeVertices( draw, 1, new Float32Array( VERTEX_FLOATS ).fill( 9 ) );
		reference.resources.commands.updateBones( draw, Float32Array.of( -0, Infinity, -Infinity, NaN ) );
	}
	arena.resources.uploads.submit( [ () => compare( [ expected ], [ actual ] ) ] );
	const first = arena.buffers.find( buffer => buffer.label === "geometry-upload-arena" );
	for ( const [reference, draw] of [ [ old, expected ], [ arena, actual ] ] ) {
		const instances = new Float32Array( 256 * INSTANCE_FLOATS ).fill( 3 );
		reference.resources.commands.updateInstances( draw, instances );
		reference.resources.commands.updatePositions( draw, new Float32Array( 9 ).fill( 4 ), undefined, undefined, [
			[ 0, 1 ],
			[ 2, 1 ]
		] );
	}
	arena.resources.uploads.submit( [ () => compare( [ expected ], [ actual ] ) ] );
	assert.equal( first.destroyed, false );
	arena.resources.endFrame();
	arena.retirement.close();
	assert.equal( first.destroyed, true );
	old.resources.dispose();
	arena.resources.dispose();
});

/*
================
Abandoned frames flush before retirement and outside-frame writes stay immediate
================
*/
test("abandoned frames flush before retirement and outside-frame writes stay immediate", () => {
	const arena = fixture(), draw = arena.resources.commands.upload( geometry() );
	arena.writes.length = 0;
	arena.resources.commands.updateBones( draw, new Float32Array( BONE_FLOATS ).fill( 5 ) );
	assert.equal( arena.writes.length, 1 );
	arena.retirement.open();
	arena.resources.beginFrame();
	arena.resources.commands.writeVertices( draw, 0, new Float32Array( VERTEX_FLOATS ).fill( 6 ) );
	arena.resources.commands.release( draw );
	arena.resources.endFrame();
	assert.equal( draw.vertices.destroyed, false );
	assert.equal( new Float32Array( draw.vertices.bytes.buffer )[0], 6 );
	arena.retirement.close();
	assert.equal( draw.vertices.destroyed, true );
	arena.resources.dispose();
});

/*
================
GPU-written palettes and particle destinations bypass staging
================
*/
test("GPU-written palettes and particle destinations bypass staging", () => {
	const particles = { beginFrame() {}, release() {}, dispose() {} };
	const animation = { cancel() {}, release() {}, dispose() {} };
	for ( const options of [ { particles }, { animation } ] ) {
		const arena = fixture( options ), data = geometry(), shared = !!options.animation;
		const draw = arena.resources.commands.upload( data, undefined, shared ? Uint32Array.of( 0 ) : undefined );
		arena.writes.length = 0;
		arena.retirement.open();
		arena.resources.beginFrame();
		arena.resources.commands.updateBones( draw, data.bones.fill( 2 ), shared ? 0 : undefined );
		arena.resources.commands.updateInstances(
			draw,
			new Float32Array( INSTANCE_FLOATS ).fill( 3 ),
			undefined,
			undefined,
			undefined,
			shared ? Uint32Array.of( 0 ) : undefined
		);
		arena.resources.commands.writeVertices( draw, 0, new Float32Array( VERTEX_FLOATS ).fill( 4 ) );
		arena.resources.uploads.submit( [] );
		assert.ok( !arena.targets.includes( bound( draw, 7 ) ) );
		if ( !shared ) assert.ok( !arena.targets.includes( bound( draw, 1 ) ) );
		arena.resources.endFrame();
		arena.retirement.close();
		arena.resources.dispose();
	}
});

/*
================
Device-sized pages spill without exceeding maxBufferSize or losing byte order
================
*/
test("device-sized pages spill without exceeding maxBufferSize or losing byte order", () => {
	const old = fixture(), arena = fixture( { limit: 512 } );
	const expected = old.resources.commands.upload( geometry() ),
		actual = arena.resources.commands.upload( geometry() );
	arena.retirement.open();
	arena.resources.beginFrame();
	for ( let tick = 0; tick < 8; tick++ ) {
		frame( old.resources, [ expected ], tick );
		frame( arena.resources, [ actual ], tick );
	}
	arena.resources.uploads.submit( [ () => compare( [ expected ], [ actual ] ) ] );
	assert.ok(
		arena.buffers.filter( buffer => buffer.label === "geometry-upload-arena" ).every( buffer => buffer.size <= 512 )
	);
	arena.resources.endFrame();
	arena.retirement.close();
	old.resources.dispose();
	arena.resources.dispose();
});
