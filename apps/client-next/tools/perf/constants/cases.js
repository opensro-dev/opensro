/*
===========================================================================

cases.js - the WebGPU call-cost page's cases (constants.js runs them)

Each case is a description and a prepare function. prepare( gpu, spec, N )
builds what one size needs and returns run(), which encodes and submits
one frame of the case's N calls, and release(). Every frame draws tiny
triangles into a 64x64 offscreen target, so fill costs nothing, or
writes or dispatches into buffers nobody reads. What all sizes share is
made once for the largest N by createCaseResources.

===========================================================================
*/

const TARGET_SIZE = 64;
const TARGET_FORMAT = "rgba8unorm";
// D1 and D7 rotate their vertex and index buffers among this many.
const BUFFER_ROTATION = 8;
// Distinct tiny triangles in the shared geometry; draw i picks one by
// firstIndex and another by baseVertex.
const TRIANGLE_POOL = 64;
const BASE_VERTEX_STEP = 7;
// One 256-byte slot per uniform, storage or upload record:
// minUniformBufferOffsetAlignment and minStorageBufferOffsetAlignment are at
// most 256 on every device.
const SLOT_BYTES = 256;
const UNIFORM_BYTES = 16;
const INDIRECT_RECORD_WORDS = 5;
const INDIRECT_RECORD_BYTES = INDIRECT_RECORD_WORDS * 4;
const WORKGROUP_SIZE = 64;

const DRAW_SHADER = `
struct Slot { offset: vec4f }
@group( 0 ) @binding( 0 ) var<uniform> slot: Slot;

@vertex
fn vs( @location( 0 ) position: vec2f, @builtin( instance_index ) instance: u32 ) -> @builtin( position ) vec4f {
	let shift = vec2f( f32( instance % 61u ), f32( instance % 53u ) ) * 0.02 - 0.6;
	return vec4f( position + slot.offset.xy + shift, 0.0, 1.0 );
}

@fragment
fn fs() -> @location( 0 ) vec4f {
	return vec4f( 1.0, 1.0, 1.0, 1.0 );
}
`;

const COMPUTE_SHADER = `
@group( 0 ) @binding( 0 ) var<storage, read_write> words: array<u32>;

@compute @workgroup_size( ${WORKGROUP_SIZE} )
fn main( @builtin( global_invocation_id ) id: vec3u ) {
	words[id.x] = id.x * 2654435761u;
}
`;

// ============================================================================

/*
================
firstIndex

Draw i's arguments: a triangle from the index pool, a vertex window from
the vertex pool and its own instance, so consecutive draws differ.
================
*/
function firstIndex( i ) {
	return 3 * (i % TRIANGLE_POOL);
}

/*
================
baseVertex
================
*/
function baseVertex( i ) {
	return 3 * ((i * BASE_VERTEX_STEP) % TRIANGLE_POOL);
}

/*
================
createFilledBuffer
================
*/
function createFilledBuffer( device, usage, data ) {
	const buffer = device.createBuffer( { size: data.byteLength, usage, mappedAtCreation: true } );
	new data.constructor( buffer.getMappedRange() ).set( data );
	buffer.unmap();
	return buffer;
}

/*
================
createDrawPipeline

The draw shader's pipeline over one bind group layout. D6's layout
differs from the others' only in hasDynamicOffset, so every draw case runs
the same shader with the same reads.
================
*/
function createDrawPipeline( device, module, layout ) {
	return device.createRenderPipeline( {
		layout: device.createPipelineLayout( { bindGroupLayouts: [ layout ] } ),
		vertex: {
			module,
			entryPoint: "vs",
			buffers: [ { arrayStride: 8, attributes: [ { shaderLocation: 0, offset: 0, format: "float32x2" } ] } ]
		},
		fragment: { module, entryPoint: "fs", targets: [ { format: TARGET_FORMAT } ] },
		primitive: { topology: "triangle-list" }
	} );
}

/*
================
createDrawResources

What every draw case shares, made once for the largest N: one pipeline
over a tiny 64x64 target, BUFFER_ROTATION copies of the triangle pool's
vertex and index buffers, and one 16-byte uniform slot with a bind group
of its own per draw. D6 gets a second pipeline whose layout has a dynamic
offset, and one bind group over the same slots that a per-draw offset
moves. Triangles are about one pixel, so fill costs nothing.
================
*/
function createDrawResources( device, largest ) {
	const uniformEntry = { binding: 0, visibility: GPUShaderStage.VERTEX, buffer: { type: "uniform" } };
	const layout = device.createBindGroupLayout( { entries: [ uniformEntry ] } );
	const dynamicLayout = device.createBindGroupLayout( {
		entries: [ { ...uniformEntry, buffer: { type: "uniform", hasDynamicOffset: true } } ]
	} );
	const module = device.createShaderModule( { code: DRAW_SHADER } );
	const vertices = new Float32Array( TRIANGLE_POOL * 3 * 2 );
	const pixel = 2 / TARGET_SIZE;
	for ( let t = 0; t < TRIANGLE_POOL; t++ ) {
		const x = ((t % 8) - 4) * 0.1, y = ((t >> 3) - 4) * 0.1;
		vertices.set( [ x, y, x + pixel, y, x, y + pixel ], t * 6 );
	}
	// Every triangle's indices are 0, 1, 2: baseVertex chooses its vertices.
	const indices = new Uint16Array( TRIANGLE_POOL * 3 );
	for ( let i = 0; i < indices.length; i++ ) indices[i] = i % 3;
	const vertexBuffers = [], indexBuffers = [];
	for ( let i = 0; i < BUFFER_ROTATION; i++ ) {
		vertexBuffers.push( createFilledBuffer( device, GPUBufferUsage.VERTEX, vertices ) );
		indexBuffers.push( createFilledBuffer( device, GPUBufferUsage.INDEX, indices ) );
	}
	const slots = new Float32Array( largest * SLOT_BYTES / 4 );
	for ( let i = 0; i < largest; i++ ) {
		slots[i * SLOT_BYTES / 4] = (i % 7) * 0.01;
		slots[i * SLOT_BYTES / 4 + 1] = (i % 5) * 0.01;
	}
	const uniforms = createFilledBuffer( device, GPUBufferUsage.UNIFORM, slots );
	const bindGroups = [];
	for ( let i = 0; i < largest; i++ ) {
		bindGroups.push( device.createBindGroup( {
			layout,
			entries: [ { binding: 0, resource: { buffer: uniforms, offset: i * SLOT_BYTES, size: UNIFORM_BYTES } } ]
		} ) );
	}
	const dynamicGroup = device.createBindGroup( {
		layout: dynamicLayout,
		entries: [ { binding: 0, resource: { buffer: uniforms, offset: 0, size: UNIFORM_BYTES } } ]
	} );
	const target = device.createTexture( {
		size: [ TARGET_SIZE, TARGET_SIZE ],
		format: TARGET_FORMAT,
		usage: GPUTextureUsage.RENDER_ATTACHMENT
	} );
	return {
		pipeline: createDrawPipeline( device, module, layout ),
		dynamicPipeline: createDrawPipeline( device, module, dynamicLayout ),
		vertexBuffers,
		indexBuffers,
		bindGroups,
		dynamicGroup,
		passDescriptor: {
			colorAttachments: [ {
				view: target.createView(),
				loadOp: "clear",
				storeOp: "store",
				clearValue: { r: 0, g: 0, b: 0, a: 1 }
			} ]
		}
	};
}

/*
================
bindShared

D2 to D5's whole state: one pipeline, one bind group, one vertex and one
index buffer.
================
*/
function bindShared( encoder, draw ) {
	encoder.setPipeline( draw.pipeline );
	encoder.setBindGroup( 0, draw.bindGroups[0] );
	encoder.setVertexBuffer( 0, draw.vertexBuffers[0] );
	encoder.setIndexBuffer( draw.indexBuffers[0], "uint16" );
}

/*
================
encodeStateChanges

D1: every draw binds a bind group of its own and the next of the rotating
vertex and index buffers, so all three change between consecutive draws.
================
*/
function encodeStateChanges( encoder, draw, count ) {
	encoder.setPipeline( draw.pipeline );
	for ( let i = 0; i < count; i++ ) {
		const slot = i % BUFFER_ROTATION;
		encoder.setBindGroup( 0, draw.bindGroups[i] );
		encoder.setVertexBuffer( 0, draw.vertexBuffers[slot] );
		encoder.setIndexBuffer( draw.indexBuffers[slot], "uint16" );
		encoder.drawIndexed( 3, 1, firstIndex( i ), baseVertex( i ), i );
	}
}

/*
================
encodeSharedState

D2: one state, count draws with different arguments.
================
*/
function encodeSharedState( encoder, draw, count ) {
	bindShared( encoder, draw );
	for ( let i = 0; i < count; i++ ) encoder.drawIndexed( 3, 1, firstIndex( i ), baseVertex( i ), i );
}

/*
================
encodeDynamicOffsets

D6: one bind group, one vertex and one index buffer; every draw rebinds
the group with its own 256-byte-aligned dynamic offset into the uniform
buffer, so the offset is the only state that changes. The shader reads
the same slot D1's draw i reads.
================
*/
function encodeDynamicOffsets( encoder, draw, count ) {
	encoder.setPipeline( draw.dynamicPipeline );
	encoder.setVertexBuffer( 0, draw.vertexBuffers[0] );
	encoder.setIndexBuffer( draw.indexBuffers[0], "uint16" );
	const offsets = [ 0 ];
	for ( let i = 0; i < count; i++ ) {
		offsets[0] = i * SLOT_BYTES;
		encoder.setBindGroup( 0, draw.dynamicGroup, offsets );
		encoder.drawIndexed( 3, 1, firstIndex( i ), baseVertex( i ), i );
	}
}

/*
================
encodeBufferChanges

D7: one pipeline and bind group; every draw binds the next of the
rotating vertex and index buffers, so the buffers are the only state that
changes.
================
*/
function encodeBufferChanges( encoder, draw, count ) {
	encoder.setPipeline( draw.pipeline );
	encoder.setBindGroup( 0, draw.bindGroups[0] );
	for ( let i = 0; i < count; i++ ) {
		const slot = i % BUFFER_ROTATION;
		encoder.setVertexBuffer( 0, draw.vertexBuffers[slot] );
		encoder.setIndexBuffer( draw.indexBuffers[slot], "uint16" );
		encoder.drawIndexed( 3, 1, firstIndex( i ), baseVertex( i ), i );
	}
}

/*
================
encodeIndirect

D3 and D4: as D2, each draw reading its record from one indirect buffer.
================
*/
function encodeIndirect( encoder, draw, count, indirect ) {
	bindShared( encoder, draw );
	for ( let i = 0; i < count; i++ ) encoder.drawIndexedIndirect( indirect, i * INDIRECT_RECORD_BYTES );
}

/*
================
encodeInstanced

D5, the floor: one draw of count instances.
================
*/
function encodeInstanced( encoder, draw, count ) {
	bindShared( encoder, draw );
	encoder.drawIndexed( 3, count, 0, 0, 0 );
}

/*
================
createIndirectRecords

count drawIndexedIndirect records. Without indirect-first-instance a
record with a non-zero firstInstance draws nothing, so firstInstance is
then 0.
================
*/
function createIndirectRecords( gpu, count, instanceCount ) {
	const words = new Uint32Array( count * INDIRECT_RECORD_WORDS );
	for ( let i = 0; i < count; i++ ) {
		words.set(
			[ 3, instanceCount, firstIndex( i ), baseVertex( i ), gpu.firstInstance ? i : 0 ],
			i * INDIRECT_RECORD_WORDS
		);
	}
	return createFilledBuffer( gpu.device, GPUBufferUsage.INDIRECT, words );
}

/*
================
prepareDraw

One draw case at one size. The pass form encodes the draws into the pass
every frame; the bundle form records them once into a GPURenderBundle and
every frame only executes it.
================
*/
function prepareDraw( gpu, spec, count ) {
	const { device } = gpu;
	const draw = gpu.draw;
	const indirect = spec.instanceCount === undefined ? null : createIndirectRecords( gpu, count, spec.instanceCount );
	let bundles = null;
	if ( spec.form === "bundle" ) {
		const recorder = device.createRenderBundleEncoder( { colorFormats: [ TARGET_FORMAT ] } );
		spec.encode( recorder, draw, count, indirect );
		bundles = [ recorder.finish() ];
	}
	return {
		run() {
			const encoder = device.createCommandEncoder();
			const pass = encoder.beginRenderPass( draw.passDescriptor );
			if ( bundles ) {
				pass.executeBundles( bundles );
			} else {
				spec.encode( pass, draw, count, indirect );
			}
			pass.end();
			device.queue.submit( [ encoder.finish() ] );
		},
		release() {
			indirect?.destroy();
		}
	};
}

// ============================================================================

/*
================
createUploadResources

The upload cases' buffers for the largest N: one 256-byte destination
per write or copy (U1, U4), one destination of N slots (U2, U3, U5), the
staging buffer U4 and U5 copy from, and the source bytes.
================
*/
function createUploadResources( device, largest ) {
	const small = [];
	for ( let i = 0; i < largest; i++ ) {
		small.push( device.createBuffer( { size: SLOT_BYTES, usage: GPUBufferUsage.COPY_DST } ) );
	}
	const source = new Uint8Array( largest * SLOT_BYTES );
	for ( let i = 0; i < source.length; i++ ) source[i] = i & 255;
	return {
		small,
		large: device.createBuffer( { size: largest * SLOT_BYTES, usage: GPUBufferUsage.COPY_DST } ),
		staging: device.createBuffer( {
			size: largest * SLOT_BYTES,
			usage: GPUBufferUsage.COPY_SRC | GPUBufferUsage.COPY_DST
		} ),
		source
	};
}

/*
================
prepareUpload

One upload case at one size. Every frame submits one command buffer
after its queue writes, as a real frame submits its work; U4 and U5 put
their copies in it, as the client's staging path does
(renderer/device/geometry-uploads.ts: one write, then a copy per stream).
================
*/
function prepareUpload( gpu, spec, count ) {
	const { device } = gpu;
	const { small, large, staging, source } = gpu.upload;
	const { queue } = device;
	const uploads = {
		U1() {
			for ( let i = 0; i < count; i++ ) queue.writeBuffer( small[i], 0, source, i * SLOT_BYTES, SLOT_BYTES );
		},
		U2() {
			for ( let i = 0; i < count; i++ ) {
				queue.writeBuffer( large, i * SLOT_BYTES, source, i * SLOT_BYTES, SLOT_BYTES );
			}
		},
		U3() {
			queue.writeBuffer( large, 0, source, 0, count * SLOT_BYTES );
		},
		U4( encoder ) {
			queue.writeBuffer( staging, 0, source, 0, count * SLOT_BYTES );
			for ( let i = 0; i < count; i++ ) {
				encoder.copyBufferToBuffer( staging, i * SLOT_BYTES, small[i], 0, SLOT_BYTES );
			}
		},
		U5( encoder ) {
			queue.writeBuffer( staging, 0, source, 0, count * SLOT_BYTES );
			for ( let i = 0; i < count; i++ ) {
				encoder.copyBufferToBuffer( staging, i * SLOT_BYTES, large, i * SLOT_BYTES, SLOT_BYTES );
			}
		}
	};
	const upload = uploads[spec.id];
	return {
		run() {
			const encoder = device.createCommandEncoder();
			upload( encoder );
			queue.submit( [ encoder.finish() ] );
		},
		release() {}
	};
}

// ============================================================================

/*
================
createComputeResources

One compute pipeline that writes one 256-byte slot per workgroup, a
storage buffer of the largest N slots, one bind group per slot (C1) and
one over the whole buffer (C2).
================
*/
function createComputeResources( device, largest ) {
	const layout = device.createBindGroupLayout( {
		entries: [ { binding: 0, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } } ]
	} );
	const pipeline = device.createComputePipeline( {
		layout: device.createPipelineLayout( { bindGroupLayouts: [ layout ] } ),
		compute: { module: device.createShaderModule( { code: COMPUTE_SHADER } ), entryPoint: "main" }
	} );
	const storage = device.createBuffer( { size: largest * SLOT_BYTES, usage: GPUBufferUsage.STORAGE } );
	const slotGroups = [];
	for ( let i = 0; i < largest; i++ ) {
		slotGroups.push( device.createBindGroup( {
			layout,
			entries: [ { binding: 0, resource: { buffer: storage, offset: i * SLOT_BYTES, size: SLOT_BYTES } } ]
		} ) );
	}
	const wholeGroup = device.createBindGroup( { layout, entries: [ { binding: 0, resource: { buffer: storage } } ] } );
	return { pipeline, slotGroups, wholeGroup };
}

/*
================
prepareCompute

One compute case at one size: C1 dispatches count single workgroups with
a bind group each; C2 dispatches count workgroups at once.
================
*/
function prepareCompute( gpu, spec, count ) {
	const { device } = gpu;
	const { pipeline, slotGroups, wholeGroup } = gpu.compute;
	return {
		run() {
			const encoder = device.createCommandEncoder();
			const pass = encoder.beginComputePass();
			pass.setPipeline( pipeline );
			if ( spec.id === "C1" ) {
				for ( let i = 0; i < count; i++ ) {
					pass.setBindGroup( 0, slotGroups[i] );
					pass.dispatchWorkgroups( 1 );
				}
			} else {
				pass.setBindGroup( 0, wholeGroup );
				pass.dispatchWorkgroups( count );
			}
			pass.end();
			device.queue.submit( [ encoder.finish() ] );
		},
		release() {}
	};
}

// ============================================================================

/*
================
caseList

Every case in run order. D1 to D4, D6 and D7 run in two forms each.
================
*/
export function caseList() {
	const draws = [
		{
			id: "D1",
			what: "own bind group, vertex and index buffer (8 rotating) per drawIndexed",
			encode: encodeStateChanges
		},
		{
			id: "D2",
			what: "one state; drawIndexed with varying firstIndex, baseVertex, firstInstance",
			encode: encodeSharedState
		},
		{
			id: "D3",
			what: "as D2 through drawIndexedIndirect, one indirect buffer, instanceCount 1",
			encode: encodeIndirect,
			instanceCount: 1
		},
		{
			id: "D4",
			what: "as D3 with instanceCount 0 in every record (empty slots)",
			encode: encodeIndirect,
			instanceCount: 0
		},
		{
			id: "D6",
			what: "as D2, rebinding one bind group with its own dynamic uniform offset per drawIndexed",
			encode: encodeDynamicOffsets
		},
		{
			id: "D7",
			what: "as D2, binding the next vertex and index buffer (8 rotating) per drawIndexed",
			encode: encodeBufferChanges
		}
	];
	const cases = [];
	for ( const form of [ "pass", "bundle" ] ) {
		for ( const draw of draws ) cases.push( { ...draw, form, per: "draw", prepare: prepareDraw } );
	}
	cases.push(
		{
			id: "D5",
			form: "pass",
			per: "instance",
			what: "one instanced draw of N instances (the floor)",
			encode: encodeInstanced,
			prepare: prepareDraw
		},
		{
			id: "U1",
			form: "queue",
			per: "write",
			what: "N writeBuffer calls of 256 bytes into N buffers",
			prepare: prepareUpload
		},
		{
			id: "U2",
			form: "queue",
			per: "write",
			what: "N writeBuffer calls of 256 bytes into one buffer at different offsets",
			prepare: prepareUpload
		},
		{ id: "U3", form: "queue", per: "256 B", what: "one writeBuffer of N x 256 bytes", prepare: prepareUpload },
		{
			id: "U4",
			form: "queue",
			per: "copy",
			what: "one writeBuffer of N x 256 bytes to staging, then N copyBufferToBuffer of 256 bytes into N buffers",
			prepare: prepareUpload
		},
		{
			id: "U5",
			form: "queue",
			per: "copy",
			what: "as U4, the N copies into one buffer at different offsets",
			prepare: prepareUpload
		},
		{
			id: "C1",
			form: "compute",
			per: "dispatch",
			what: "N dispatches of one workgroup, each with its own bind group",
			prepare: prepareCompute
		},
		{
			id: "C2",
			form: "compute",
			per: "workgroup",
			what: "one dispatch of N workgroups (same total work)",
			prepare: prepareCompute
		}
	);
	return cases;
}

/*
================
createCaseResources

What the cases share, made once for the largest N. firstInstance says
whether the device has indirect-first-instance.
================
*/
export function createCaseResources( device, largest, firstInstance ) {
	return {
		device,
		firstInstance,
		draw: createDrawResources( device, largest ),
		upload: createUploadResources( device, largest ),
		compute: createComputeResources( device, largest )
	};
}
