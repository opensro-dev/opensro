/*
===========================================================================

geometry-ranges.test.mjs - tests for geometry.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGeometryResources } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/device/geometry.ts" ).href
);
globalThis.GPUBufferUsage = { UNIFORM: 1, COPY_DST: 2, STORAGE: 4, VERTEX: 8, INDEX: 16 };
globalThis.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2 };
function fixture( dynamicVertices = true ) {
	const samplers = [],
		writes = [],
		// The buffer each write landed in, in order.
		targets = [],
		gpu = {
			createBuffer( { size } ) {
				return { size, bytes: new Uint8Array( size ), destroy() {} };
			},
			createTexture() {
				return {
					createView() {
						return {};
					},
					destroy() {}
				};
			},
			createBindGroup() {
				return {};
			},
			// The sun cascade's comparison sampler (bindings 13/14).
			createSampler() {
				return {};
			},
			pushErrorScope() {},
			popErrorScope() {
				return Promise.resolve( null );
			},
			queue: {
				writeTexture() {},
				writeBuffer( buffer, offset, data, start, length ) {
					buffer.bytes.set( new Uint8Array( data, start, length ), offset );
					writes.push( { offset, length } );
					targets.push( buffer );
				}
			}
		};
	const resources = createGeometryResources(
		gpu,
		() => gpu,
		e => {
			throw e;
		},
		// One geometry pipeline per state.
		() => ({
			getBindGroupLayout() {
				return {};
			}
		}),
		{ texture() {}, acquire() {}, drop() {} },
		{},
		{},
		{},
		( filtered, detail ) => {
			const sampler = { filtered, detail };
			samplers.push( sampler );
			return sampler;
		}
	);
	const data = {
		positions: Float32Array.from( { length: 300 }, ( _, i ) => i / 10 ),
		normals: Float32Array.from( { length: 300 }, ( _, i ) => i % 3 ),
		uvs: new Float32Array( 200 ).fill( .25 ),
		colors: new Float32Array( 400 ).fill( .75 ),
		maskUVs: new Float32Array( 200 ).fill( .5 ),
		indices: Uint32Array.of( 0, 1, 2 ),
		transform: new Float32Array( 16 ),
		dynamicVertices
	};
	const draw = resources.commands.upload( data );
	writes.length = 0;
	return { resources, data, draw, writes, samplers, instanceStore: () => targets.at( -1 ) };
}
test("sparse writes are byte-identical to full repacking, including unchanged vertex attributes", () => {
	const sparse = fixture(), full = fixture();
	for ( let frame = 0; frame < 40; frame++ ) {
		const ranges = [ [ 70, 3 ], [ 2, 4 ], [ 3, 5 ], [ 73, 2 ] ];
		for ( const f of [ sparse, full ] ) {
			for ( const [start, count] of ranges ) {
				for ( let i = start; i < start + count; i++ ) {
					f.data.positions[i * 3 + 1] = frame / 7;
					f.data.colors[i * 4] = frame / 80;
					f.data.uvs[i * 2] = frame / 40;
				}
			}
		}
		sparse.writes.length = 0;
		sparse.resources.commands.updatePositions(
			sparse.draw,
			sparse.data.positions,
			sparse.data.colors,
			sparse.data.uvs,
			ranges
		);
		full.resources.commands.updatePositions( full.draw, full.data.positions, full.data.colors, full.data.uvs );
		assert.deepEqual( sparse.draw.vertices.bytes, full.draw.vertices.bytes );
		assert.deepEqual( sparse.writes, [ { offset: 112, length: 336 }, { offset: 3920, length: 280 } ] );
	}
	for ( const f of [ sparse, full ] ) f.resources.dispose();
});
test("invalid later ranges and attributes fail before any mutation; empty updates do nothing", () => {
	const f = fixture(), before = f.draw.vertices.bytes.slice();
	f.data.positions[0] = 999;
	for ( const ranges of [ [ [ 0, 1 ], [ 99, 2 ] ], [ [ 0, 1 ], [ -1, 1 ] ], [ [ 0, 1 ], [ 2, .5 ] ] ] ) {
		assert.throws(
			() => f.resources.commands.updatePositions( f.draw, f.data.positions, undefined, undefined, ranges ),
			/range/
		);
	}
	f.data.positions[90] = NaN;
	assert.throws(
		() =>
			f.resources.commands.updatePositions( f.draw, f.data.positions, undefined, undefined, [ [ 0, 1 ], [
				30,
				1
			] ] ),
		/update/
	);
	f.resources.commands.updatePositions( f.draw, f.data.positions, undefined, undefined, [] );
	assert.equal( f.writes.length, 0 );
	assert.deepEqual( f.draw.vertices.bytes, before );
	// A subsequent valid write must not expose an earlier rejected CPU mutation.
	f.data.positions[90] = 3;
	f.resources.commands.updatePositions( f.draw, f.data.positions, undefined, undefined, [ [ 30, 1 ] ] );
	assert.deepEqual( f.draw.vertices.bytes.subarray( 0, 56 ), before.subarray( 0, 56 ) );
	f.resources.dispose();
});

test("a layer member updates its slot, the first slot included; a whole-mesh update must cover the draw", () => {
	const f = fixture(), member = f.data.positions.slice( 0, 30 );
	member.fill( 7 );
	for ( const slot of [ 0, 40 ] ) {
		f.writes.length = 0;
		f.resources.commands.updatePositions( f.draw, member, undefined, undefined, [ [ 2, 3 ] ], slot );
		assert.deepEqual( f.writes, [ { offset: (slot + 2) * 56, length: 3 * 56 } ] );
		const floats = new Float32Array( f.draw.vertices.bytes.buffer, (slot + 2) * 56, 14 );
		assert.deepEqual( [ ...floats.subarray( 0, 3 ) ], [ 7, 7, 7 ] );
	}
	assert.throws(
		() => f.resources.commands.updatePositions( f.draw, member, undefined, undefined, [ [ 0, 1 ] ], 95 ),
		/update/
	);
	assert.throws( () => f.resources.commands.updatePositions( f.draw, member ), /update/ );
	f.resources.dispose();
});

test("a mesh uploaded without dynamicVertices keeps no vertex mirror and refuses a position update", () => {
	const f = fixture( false );
	assert.throws(
		() => f.resources.commands.updatePositions( f.draw, f.data.positions ),
		/dynamicVertices/
	);
	assert.equal( f.writes.length, 0 );
	f.resources.dispose();
});

test("texture settings rebind retained geometry without uploading buffers and survive instance growth", () => {
	const f = fixture(), before = f.draw.binding;
	f.resources.textureOptions( false, 0 );
	assert.notEqual( f.draw.binding, before );
	assert.deepEqual( f.samplers, [ { filtered: false, detail: 0 } ] );
	assert.equal( f.writes.length, 0 );
	const changed = f.draw.binding;
	f.resources.textureOptions( false, 0 );
	assert.equal( f.draw.binding, changed );
	assert.equal( f.samplers.length, 1 );
	const expanded = f.resources.commands.updateInstances( f.draw, new Float32Array( 32 ) );
	const grown = expanded.binding;
	f.resources.textureOptions( true, 2 );
	assert.notEqual( expanded.binding, grown );
	assert.equal( expanded.instanceCount, 2 );
	f.resources.dispose();
});

test("a ranged instance update writes only its rows, byte-identical to a full repack, cloth base included", () => {
	for ( const cloth of [ false, true ] ) {
		const run = ranged => {
			const f = fixture();
			const create = f.resources.commands;
			// The draw under test: plain, or a skinned mesh with a cloth stream,
			// whose rows carry the stream's base (markCloth).
			const draw = cloth ?
				create.upload( {
					...f.data,
					dynamicVertices: false,
					joints: new Uint16Array( 400 ),
					weights: Float32Array.from( { length: 400 }, ( _, i ) => i % 4 ? 0 : 1 ),
					bones: Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ),
					clothStream: Int32Array.of( 0, 1, 2 )
				} ) :
				f.draw;
			const rows = count => Float32Array.from( { length: count * 16 }, ( _, i ) => i % 17 );
			const opacity = count => Float32Array.from( { length: count }, ( _, i ) => (i + 1) / 10 );
			let current = create.updateInstances( draw, rows( 8 ), opacity( 8 ) );
			const next = rows( 8 ), fade = opacity( 8 );
			for ( let i = 3 * 16; i < 6 * 16; i++ ) next[i] += 5;
			for ( let i = 3; i < 6; i++ ) fade[i] = .05;
			f.writes.length = 0;
			current = ranged ?
				create.updateInstances( current, next, fade, undefined, undefined, undefined, { first: 3, end: 6 } ) :
				create.updateInstances( current, next, fade );
			return { f, writes: f.writes.slice(), current };
		};
		const sparse = run( true ), full = run( false );
		assert.equal( sparse.current.instanceCount, 8 );
		assert.equal( sparse.writes.length, 1, "one coalesced write" );
		assert.equal( sparse.writes[0].length, 3 * 160 );
		assert.equal( sparse.writes[0].offset - full.writes[0].offset, 3 * 160, "written at row 3" );
		// Every byte of the written instance store equals the full repack's.
		const store = run => new Uint8Array( run.f.instanceStore().bytes );
		assert.deepEqual( store( sparse ), store( full ), cloth ? "cloth" : "plain" );
		sparse.f.resources.dispose();
		full.f.resources.dispose();
	}
});
