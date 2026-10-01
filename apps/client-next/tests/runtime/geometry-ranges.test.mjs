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
			pushErrorScope() {},
			popErrorScope() {
				return Promise.resolve( null );
			},
			queue: {
				writeTexture() {},
				writeBuffer( buffer, offset, data, start, length ) {
					buffer.bytes.set( new Uint8Array( data, start, length ), offset );
					writes.push( { offset, length } );
				}
			}
		};
	const resources = createGeometryResources(
		gpu,
		() => gpu,
		e => {
			throw e;
		},
		() =>
			Array( 16 ).fill( {
				getBindGroupLayout() {
					return {};
				}
			} ),
		() => {},
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
	return { resources, data, draw, writes, samplers };
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
