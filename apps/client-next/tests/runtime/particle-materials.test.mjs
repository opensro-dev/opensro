/*
===========================================================================

particle-materials.test.mjs - the particle pass's resident material tables

A stream's material frames are uploaded once, to a table every stream of
those frames shares, instead of with every frame's input page (#4692).
These tests drive the production pass (device/particles.ts) against the
strict fake device and read every queue write: what is uploaded and when,
where each stream's frame parameters say its table starts, and that a
buffer an encoded pass names outlives its submit.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createStrictGpu, GPU_BUFFER_USAGE } from "../helpers/strict-gpu.mjs";

globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;

const { createParticlePresentation } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/device/particles.ts" ).href
);
const { PARTICLE_ACTOR, PARTICLE_RECORD } = await import(
	sourceFileUrl( "src/engine/foundation/animation/particle-records.ts" ).href
);

// particles.ts: the frame parameters ahead of a stream's actor rows, the
// word of them that names the table's first vec4f, and a pass's smallest buffer.
const PARAMS_FLOATS = 24, TABLE_START_WORD = 9, INITIAL_BYTES = 16384;
const MATERIALS = "particle-materials";

/*
================
frames

Material frames of count frames, every value marked by seed.
================
*/
function frames( count, seed ) {
	return {
		fps: 20,
		colors: new Float32Array( count * 4 ).fill( seed ),
		windows: new Float32Array( count * 4 ).fill( seed + .5 )
	};
}

/*
================
fixture

The pass on a strict device (limits as given), recording queue writes.
stream(frames) is a one-slot live stream; encode encodes the presented
ones and returns the table start each one's parameters named. A pass's
input page is two buffers, its dirty records then its frame parameters
(particles.ts pageFor): role says which.
================
*/
async function fixture( limits = {} ) {
	const gpu = createStrictGpu(), writes = [];
	let pageBuffers = 0;
	const createBuffer = descriptor => {
		const buffer = gpu.device.createBuffer( descriptor );
		if ( descriptor.label === "particle-batch" ) buffer.role = pageBuffers++ % 2 ? "parameters" : "records";
		return buffer;
	};
	const queue = {
		...gpu.device.queue,
		writeBuffer( buffer, offset, data, dataOffset, size ) {
			/** @type {any} */ (gpu.device.queue).writeBuffer( buffer, offset, data, dataOffset, size );
			const bytes = data.slice( dataOffset, dataOffset + size );
			writes.push( {
				label: buffer.label,
				buffer,
				offset,
				floats: new Float32Array( bytes ),
				words: new Uint32Array( bytes )
			} );
		}
	};
	const device = Object.assign( Object.create( gpu.device ), { limits, queue, createBuffer } );
	const particles = createParticlePresentation( device );
	await particles.ready;
	const presented = [];
	return {
		gpu,
		particles,
		writes,
		materialWrites: () => writes.filter( write => write.label === MATERIALS ),
		stream( material ) {
			const entry = {
				instances: gpu.device.createBuffer( { label: "own-instances", size: 160 } ),
				bones: gpu.device.createBuffer( { label: "own-bones", size: 64 } ),
				input: {
					rows: 1,
					slots: 1,
					live: 1,
					graph: false,
					view: 0,
					lifetime: 1,
					loop: false,
					records: new Float32Array( PARTICLE_RECORD ),
					actors: new Float32Array( PARTICLE_ACTOR ),
					axes: new Float32Array( 12 ),
					dirtyStart: 0,
					dirtyEnd: 1,
					...(material ? { frames: material } : {})
				}
			};
			entry.present = () => {
				particles.present( entry, entry.instances, entry.bones, entry.input, () => {} );
				presented.push( entry );
			};
			entry.release = () => particles.release( entry );
			return entry;
		},
		/*
		================
		encode

		Encode the presented streams (no submit: the caller owns the frame)
		and return each one's table start in floats, in presentation order.
		Every stream is one workgroup, so a pass of n streams puts stream k's
		parameters at 4 n + k (PARAMS_FLOATS + PARTICLE_ACTOR) words.
		================
		*/
		encode( encoder = gpu.device.createCommandEncoder( { label: "particles" } ) ) {
			const from = writes.length, count = presented.length;
			particles.encode( encoder );
			presented.length = 0;
			const starts = [];
			for ( const page of writes.slice( from ).filter( write => write.buffer.role === "parameters" ) ) {
				const streams = page.words.length / (4 + PARAMS_FLOATS + PARTICLE_ACTOR);
				for ( let k = 0; k < streams; k++ ) {
					starts.push(
						page.words[streams * 4 + k * (PARAMS_FLOATS + PARTICLE_ACTOR) + TABLE_START_WORD] * 4
					);
				}
			}
			assert.equal( starts.length, count, "every presented stream's parameters were uploaded" );
			return { encoder, starts };
		},
		submit( encoder ) {
			gpu.device.queue.submit( [ encoder.finish() ] );
		}
	};
}

test("streams of one primitive share one table, uploaded once and never with a later frame", async () => {
	const f = await fixture(), glow = frames( 3, 1 ), spark = frames( 2, 7 );
	const a = f.stream( glow ), b = f.stream( glow ), c = f.stream( spark ), bare = f.stream();
	for ( let frame = 0; frame < 3; frame++ ) {
		f.particles.beginFrame();
		for ( const stream of [ a, b, c, bare ] ) stream.present();
		const { encoder, starts } = f.encode();
		f.submit( encoder );
		assert.deepEqual( starts, [ 0, 0, 24, 0 ], "each stream names its frames' table" );
	}
	const uploads = f.materialWrites();
	assert.equal( uploads.length, 2, "one upload a distinct frames, whatever the streams and frames" );
	assert.deepEqual( [ ...uploads[0].floats ], [ ...glow.colors, ...glow.windows ] );
	assert.equal( uploads[1].offset, 24 * 4 );
	assert.deepEqual( [ ...uploads[1].floats ], [ ...spark.colors, ...spark.windows ] );
	assert.deepEqual( f.particles.stats().materials, { tables: 2, stores: 1, bytes: 40 * 4, uploads: 2 } );
	assert.equal( f.particles.stats().dispatches, 3, "one pass a frame" );
	f.particles.dispose();
});

test("a released table's range is packed after the frame, and its neighbours' streams follow", async () => {
	const f = await fixture(), first = frames( 4, 1 ), second = frames( 2, 5 );
	const a = f.stream( first ), b = f.stream( second );
	f.particles.beginFrame();
	a.present();
	b.present();
	let pass = f.encode();
	f.submit( pass.encoder );
	assert.deepEqual( pass.starts, [ 0, 32 ] );
	// The frame that releases a writes nothing: an encoded pass may still read it.
	f.particles.beginFrame();
	a.release();
	b.present();
	pass = f.encode();
	f.submit( pass.encoder );
	assert.deepEqual( pass.starts, [ 32 ] );
	assert.equal( f.materialWrites().length, 2 );
	f.particles.beginFrame();
	const packed = f.materialWrites().at( -1 );
	assert.equal( f.materialWrites().length, 3, "the next frame packs the store" );
	assert.equal( packed.offset, 0 );
	assert.deepEqual( [ ...packed.floats ], [ ...second.colors, ...second.windows ] );
	b.present();
	pass = f.encode();
	f.submit( pass.encoder );
	assert.deepEqual( pass.starts, [ 0 ], "the stream reads its table where the pack put it" );
	assert.deepEqual( f.particles.stats().materials, { tables: 1, stores: 1, bytes: 16 * 4, uploads: 3 } );
	// The frames return after their table was dropped: a new table, one more upload.
	const again = f.stream( first );
	again.present();
	b.present();
	pass = f.encode();
	f.submit( pass.encoder );
	assert.deepEqual( pass.starts, [ 16, 0 ] );
	assert.equal( f.materialWrites().length, 4 );
	b.release();
	again.release();
	f.particles.beginFrame();
	assert.deepEqual( f.particles.stats().materials, { tables: 0, stores: 0, bytes: 0, uploads: 4 } );
	f.particles.dispose();
});

test("a store that grows after a pass was encoded stays alive until that frame has submitted", async () => {
	const f = await fixture(), small = frames( 1, 1 ), large = frames( INITIAL_BYTES / 16, 3 );
	const a = f.stream( small ), b = f.stream( large );
	f.particles.beginFrame();
	a.present();
	const encoder = f.gpu.device.createCommandEncoder( { label: "frame" } );
	assert.deepEqual( f.encode( encoder ).starts, [ 0 ] );
	// The deferred half of the frame admits a table the buffer cannot hold.
	a.present();
	b.present();
	assert.deepEqual( f.encode( encoder ).starts, [ 0, 8 ] );
	const [first, grown] = f.materialWrites();
	assert.equal( f.materialWrites().length, 2 );
	assert.notEqual( grown.buffer, first.buffer, "the store moved to a larger buffer" );
	assert.equal( grown.offset, 0, "written whole" );
	assert.equal( grown.floats.length, 8 + large.colors.length * 2 );
	assert.equal( first.buffer.destroyed, false );
	// The strict device refuses a submit that names a destroyed buffer.
	f.submit( encoder );
	f.particles.beginFrame();
	assert.equal( first.buffer.destroyed, true, "the replaced buffer goes once its frame submitted" );
	f.particles.dispose();
	assert.equal( f.gpu.live(), 4, "only the streams' own buffers remain" );
});

test("tables that cannot share a storage binding get a store and a pass each", async () => {
	const limit = INITIAL_BYTES, f = await fixture( { maxStorageBufferBindingSize: limit, maxBufferSize: limit } );
	// Each table is over half the limit, so no two fit one store.
	const count = limit / 64 + 1;
	const a = f.stream( frames( count, 1 ) ), b = f.stream( frames( count, 2 ) ), bare = f.stream();
	f.particles.beginFrame();
	for ( const stream of [ a, bare, b ] ) stream.present();
	const from = f.particles.stats().dispatches;
	const encoder = f.gpu.device.createCommandEncoder( { label: "frame" } );
	f.particles.encode( encoder );
	f.submit( encoder );
	assert.equal( f.particles.stats().dispatches - from, 2, "a stream without frames joins either pass" );
	assert.equal( f.particles.stats().materials.stores, 2 );
	const buffers = new Set( f.materialWrites().map( write => write.buffer ) );
	assert.equal( buffers.size, 2 );
	assert.throws( () => f.stream( frames( limit / 32 + 1, 9 ) ).present(), /storage limit/ );
	f.particles.dispose();
});
