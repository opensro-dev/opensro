/*
===========================================================================

gpu-animation-owner.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const { createGpuAnimationResources } = { ...(await load( "src/engine/runtime/renderer/device/animation.ts" )) };
globalThis.GPUBufferUsage = { UNIFORM: 1, COPY_DST: 2, STORAGE: 4 };
const I = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
function fixture( { reject = false, failBinding = false } = {} ) {
	const buffers = [],
		writes = [],
		gpu = {
			createShaderModule() {
				return {};
			},
			createComputePipelineAsync() {
				return reject ? Promise.reject( Error( "pipeline failed" ) ) : Promise.resolve( {
					getBindGroupLayout() {
						return {};
					}
				} );
			},
			createBuffer( { size, label } ) {
				const b = {
					size,
					label,
					destroyed: 0,
					destroy() {
						this.destroyed++;
					}
				};
				buffers.push( b );
				return b;
			},
			createBindGroup() {
				if ( failBinding ) throw Error( "binding failed" );
				return {};
			},
			queue: {
				writeBuffer( buffer, offset, data, start = 0, length = data.byteLength ) {
					writes.push( { label: buffer.label, values: [ ...new Float32Array( data, start, length / 4 ) ] } );
				}
			}
		};
	const primitive = { joints: [ 0 ], inverseBind: I() },
		clip = {
			name: "step",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "STEP",
				times: Float32Array.of( 0, .5, 1 ),
				values: Float32Array.of( 0, 0, 0, 1, 0, 0, 2, 0, 0 )
			} ]
		},
		model = {
			nodes: [ {
				name: "root",
				parent: -1,
				translation: [ 0, 0, 0 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ 1, 1, 1 ]
			} ],
			clips: [ clip ],
			primitives: [ primitive ]
		};
	return {
		buffers,
		writes,
		primitive,
		clip,
		model,
		owner: createGpuAnimationResources( gpu ),
		source: new Float32Array( 16 ),
		output: { size: 64 }
	};
}
test("STEP phase cannot round forward across a key; cancellation removes queued work", async () => {
	const f = fixture();
	await f.owner.ready;
	assert.ok(
		f.owner.prepare( f.source, f.output, f.model, f.primitive, [ { clip: f.clip, time: .5 - Number.EPSILON } ] )
	);
	assert.ok( f.writes.at( -1 ).values[0] < .5 );
	f.owner.cancel( f.source );
	f.owner.encode( {
		beginComputePass() {
			throw Error( "cancelled dispatch" );
		}
	} );
	f.owner.release( f.source );
	assert.equal( f.owner.stats().models, 0 );
	assert.equal( f.owner.stats().streamBytes, 0 );
	f.owner.dispose();
	assert.ok( f.buffers.every( b => b.destroyed === 1 ) );
});
test("failed pipeline permits CPU fallback and partial allocation failure retires owned buffers", async () => {
	const f = fixture( { reject: true } );
	await f.owner.ready;
	assert.equal( f.owner.prepare( f.source, f.output, f.model, f.primitive, [ { clip: f.clip, time: 0 } ] ), false );
	assert.match( f.owner.stats().failure, /pipeline failed/ );
	f.owner.dispose();
	const g = fixture( { failBinding: true } );
	await g.owner.ready;
	assert.throws(
		() => g.owner.prepare( g.source, g.output, g.model, g.primitive, [ { clip: g.clip, time: 0 } ] ),
		/binding failed/
	);
	assert.equal( g.owner.stats().models, 0 );
	assert.equal( g.owner.stats().staticBytes, 0 );
	g.owner.dispose();
	assert.ok( g.buffers.every( b => b.destroyed === 1 ) );
});
test("invalid requests cannot admit orphan model storage; disposal invalidates pending dispatch", async () => {
	const f = fixture();
	await f.owner.ready;
	assert.throws(
		() => f.owner.prepare( f.source, { size: 1 }, f.model, f.primitive, [ { clip: f.clip, time: 0 } ] ),
		/capacity/
	);
	assert.equal( f.owner.stats().models, 0 );
	assert.equal( f.owner.prepare( f.source, f.output, f.model, f.primitive, [ { clip: f.clip, time: NaN } ] ), false );
	assert.equal( f.owner.stats().models, 0 );
	f.owner.prepare( f.source, f.output, f.model, f.primitive, [ { clip: f.clip, time: 0 } ] );
	f.owner.dispose();
	assert.throws( () => f.owner.encode( {} ), /Disposed/ );
	assert.ok( f.buffers.every( b => b.destroyed === 1 ) );
});

test("sparse GPU inputs preserve destination indices around CPU-owned slots", async () => {
	const f = fixture();
	await f.owner.ready;
	f.owner.prepare( new Float32Array( 64 ), { size: 256 }, f.model, f.primitive, [
		null,
		{ clip: f.clip, time: .1 },
		null,
		{ clip: f.clip, time: .6 }
	] );
	const inputs = f.writes.at( -1 ).values;
	assert.equal( inputs.length, 8 );
	assert.equal( inputs[2], 1 );
	assert.equal( inputs[6], 3 );
	let count = 0;
	f.owner.encode( {
		beginComputePass() {
			return {
				setPipeline() {},
				setBindGroup() {},
				dispatchWorkgroups( n ) {
					count = n;
				},
				end() {}
			};
		}
	} );
	assert.equal( count, 2 );
	f.owner.dispose();
});
