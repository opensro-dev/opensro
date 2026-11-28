/*
===========================================================================

index-cull.test.mjs - the GPU terrain cull's CPU contract

The cull pass itself runs on the GPU (index-cull.ts); these tests pin what
the CPU side promises around it, on a fake device that records commands:
a culled draw submits its slot's arguments, a still view dispatches
nothing, a changed view or table culls again, a regrown arguments buffer
re-points every draw (and the frame re-records its bundle), and tables
that would overrun a draw are refused.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { GPU_BUFFER_USAGE } from "../helpers/strict-gpu.mjs";

globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
const { createIndexCull, RANGE_WORDS } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/device/index-cull.ts" ).href
);
const { DeviceDraw } = await import( sourceFileUrl( "src/engine/runtime/renderer/device/device-draw.ts" ).href );
const { createFrame } = await import( sourceFileUrl( "src/engine/runtime/renderer/frame/frame.ts" ).href );

/*
================
fakeDevice

Buffers keep their bytes; every compute pass is logged as its dispatches.
================
*/
function fakeDevice() {
	const passes = [];
	const pipeline = label => ({ label, getBindGroupLayout: () => ({}) });
	const device = {
		limits: {},
		createBuffer( descriptor ) {
			return {
				...descriptor,
				bytes: new Uint8Array( descriptor.size ),
				destroyed: false,
				destroy() {
					this.destroyed = true;
				}
			};
		},
		createShaderModule: () => ({}),
		createComputePipelineAsync: async descriptor => pipeline( descriptor.label ),
		createBindGroup: descriptor => descriptor,
		queue: {
			writeBuffer( buffer, offset, data, start = 0, size = data.byteLength - start ) {
				buffer.bytes.set( new Uint8Array( data, start, size ), offset );
			}
		}
	};
	const encoder = {
		beginComputePass() {
			const pass = { dispatches: [] };
			passes.push( pass );
			let current;
			return {
				setPipeline: value => current = value.label,
				setBindGroup() {},
				dispatchWorkgroups: count => pass.dispatches.push( [ current, count ] ),
				end() {}
			};
		}
	};
	return { device, encoder, passes };
}

/*
================
culledDraw
================
*/
function culledDraw( count, firstIndex = 0, baseVertex = 0 ) {
	return new DeviceDraw(
		{ pipeline: "pipeline", vertices: "vertices", indices: { label: "indices" }, firstIndex, baseVertex, count },
		"binding",
		1,
		1
	);
}

/*
================
table

A range table of (start, count) pairs, sphere at the origin.
================
*/
function table( pairs ) {
	const words = new Uint32Array( pairs.length * RANGE_WORDS ), floats = new Float32Array( words.buffer );
	pairs.forEach( ( [start, count], i ) => {
		floats[i * RANGE_WORDS + 3] = 10;
		words[i * RANGE_WORDS + 4] = start;
		words[i * RANGE_WORDS + 5] = count;
	} );
	return words;
}

const PLANES = Float64Array.from( { length: 30 }, ( _, i ) => i % 5 === 4 ? 1 : i % 5 === 3 ? 100 : 0 );

test("a culled draw submits its slot's arguments, and a still view dispatches nothing", async () => {
	const { device, encoder, passes } = fakeDevice(), retired = [];
	const cull = createIndexCull( device, resource => retired.push( resource ) );
	await cull.ready;
	const draw = culledDraw( 600, 40, 7 );
	cull.adopt( draw, 600 );
	assert.ok( draw.indirect, "the draw now submits indirect arguments" );
	const words = new Uint32Array( draw.indirect.bytes.buffer, draw.indirectOffset, 5 );
	assert.deepEqual( [ ...words ], [ 0, 1, 40, 7, 0 ], "count 0 until culled; its own first index and base vertex" );
	cull.ranges( draw, table( [ [ 0, 300 ], [ 300, 300 ] ] ), 2 );
	cull.view( PLANES, null );
	cull.encode( encoder );
	assert.deepEqual( passes.at( -1 ).dispatches, [ [ "index-cull-reset", 1 ], [ "index-cull", 2 ] ] );
	// The same view again: nothing to cull, the arguments keep their counts.
	cull.view( PLANES, null );
	cull.encode( encoder );
	assert.equal( passes.length, 1 );
	// A turned view culls again; so does a new table under a still view.
	const turned = PLANES.slice();
	turned[3] = 99;
	cull.view( turned, null );
	cull.encode( encoder );
	assert.equal( passes.length, 2 );
	cull.ranges( draw, table( [ [ 0, 300 ] ] ), 1 );
	cull.encode( encoder );
	assert.deepEqual( passes.at( -1 ).dispatches.at( -1 ), [ "index-cull", 1 ] );
	// A mirror is part of the view.
	cull.view( turned, PLANES );
	cull.encode( encoder );
	assert.equal( passes.length, 4 );
	// A second encode in the same frame finds nothing due.
	cull.encode( encoder );
	assert.equal( passes.length, 4 );
});

test("a draw with no candidates is reset but not dispatched", async () => {
	const { device, encoder, passes } = fakeDevice();
	const cull = createIndexCull( device, () => {} );
	await cull.ready;
	const empty = culledDraw( 30 ), full = culledDraw( 30 );
	cull.adopt( empty, 30 );
	cull.adopt( full, 30 );
	cull.ranges( empty, table( [] ), 0 );
	cull.ranges( full, table( [ [ 0, 30 ] ] ), 1 );
	cull.view( PLANES, null );
	cull.encode( encoder );
	assert.deepEqual( passes[0].dispatches, [ [ "index-cull-reset", 1 ], [ "index-cull", 1 ] ] );
});

test("growing past the slots re-points every draw at the new arguments buffer", async () => {
	const { device } = fakeDevice(), retired = [];
	const cull = createIndexCull( device, resource => retired.push( resource ) );
	await cull.ready;
	const draws = Array.from( { length: 65 }, ( _, i ) => culledDraw( 3, i * 3, i ) );
	for ( const draw of draws.slice( 0, 64 ) ) cull.adopt( draw, 3 );
	const first = draws[0].indirect;
	cull.adopt( draws[64], 3 );
	assert.notEqual( draws[0].indirect, first, "the first draw follows the regrown buffer" );
	assert.ok( retired.includes( first ), "the old buffer goes through retirement" );
	for ( const [i, draw] of draws.entries() ) {
		assert.equal( draw.indirect, draws[64].indirect );
		const words = new Uint32Array( draw.indirect.bytes.buffer, draw.indirectOffset, 5 );
		assert.deepEqual( [ ...words ], [ 0, 1, i * 3, i, 0 ], `slot ${i} keeps its words` );
	}
});

test("tables that overrun a draw are refused, and a released slot is reused", async () => {
	const { device } = fakeDevice();
	const cull = createIndexCull( device, () => {} );
	await cull.ready;
	const draw = culledDraw( 10 );
	assert.throws( () => cull.adopt( draw, 11 ), /Invalid cull capacity/ );
	cull.adopt( draw, 10 );
	assert.throws( () => cull.adopt( draw, 10 ), /already culled/ );
	assert.throws( () => cull.ranges( draw, table( [ [ 8, 3 ] ] ), 1 ), /Invalid cull range/ );
	assert.throws( () => cull.source( draw, 9, new Uint32Array( 2 ) ), /Invalid cull source write/ );
	assert.throws( () => cull.ranges( culledDraw( 10 ), table( [] ), 0 ), /not culled/ );
	const slot = draw.indirectOffset;
	cull.forget( draw );
	const next = culledDraw( 10 );
	cull.adopt( next, 10 );
	assert.equal( next.indirectOffset, slot, "the freed slot is taken again" );
});

test("an encode before the pipeline is ready refuses instead of drawing stale counts", async () => {
	const { device, encoder } = fakeDevice();
	device.createComputePipelineAsync = () => new Promise( () => {} );
	const cull = createIndexCull( device, () => {} );
	cull.adopt( culledDraw( 3 ), 3 );
	cull.view( PLANES, null );
	assert.throws( () => cull.encode( encoder ), /not ready/ );
});

test("a frame re-records a bundle whose draw's arguments buffer changed", () => {
	let recordings = 0;
	const frame = createFrame( {
		createBundleEncoder() {
			recordings++;
			return {
				setPipeline() {},
				setBindGroup() {},
				setVertexBuffer() {},
				setIndexBuffer() {},
				drawIndexed() {},
				drawIndexedIndirect() {},
				finish: () => "bundle"
			};
		},
		createEncoder: () => ({
			beginRenderPass: () => ({ executeBundles() {}, setBlendConstant() {}, end() {} }),
			finish: () => "commands"
		}),
		submit() {}
	} );
	const draw = culledDraw( 6 );
	DeviceDraw.drawIndirect( draw, { label: "a" }, 0 );
	frame.draw( {}, undefined, draw );
	frame.draw( {}, undefined, draw );
	assert.equal( recordings, 1, "an unchanged culled draw keeps its bundle" );
	DeviceDraw.drawIndirect( draw, { label: "b" }, 0 );
	frame.draw( {}, undefined, draw );
	assert.equal( recordings, 2, "a new arguments buffer is re-recorded" );
});
