/*
===========================================================================

particle-query.ts - per-point occlusion answers for deferred particles

AEBD90: one point per query, LESS_EQUAL, depth writes on, color writes
off. Queries stay ordered on the GPU; one readback replaces native's
per-point GetData busy-wait without changing any depth dependency
between points.

The read is owned until dispose(): a query still in flight when the
owner is disposed (its device generation being torn down) ends
quietly with no point visible - dispose() destroys the read buffer
under the pending map, and that rejection used to travel to the
renderer's failure path as a defect (#2560). Only a rejection on a
live owner is an error.

===========================================================================
*/
import { CHARACTER_ACTORS } from "@/engine/foundation/animation/character-budget";

/*
================
noPointsVisible

A query that ended without an answer: every point reports not
visible for this one cycle, and the next due query asks again.
================
*/
function noPointsVisible( count: number ): readonly boolean[] {
	return new Array( count ).fill( false );
}

/*
================
createParticleQuery
================
*/
export function createParticleQuery( device: GPUDevice, format: GPUTextureFormat ) {
	const shader = device.createShaderModule( {
		label: "native-particle-query",
		code: `
 @group(0) @binding(0) var<uniform> matrix:mat4x4f;
 @group(0) @binding(1) var<storage,read> points:array<vec4f>;
 @vertex fn vs(@builtin(vertex_index) i:u32)->@builtin(position) vec4f {return matrix*vec4f(points[i].xyz,1);}
 @fragment fn fs()->@location(0) vec4f {return vec4f(0);}
 `
	} );
	let pipeline: GPURenderPipeline,
		storage: GPUBuffer | undefined,
		uniform: GPUBuffer | undefined,
		resolve: GPUBuffer | undefined,
		read: GPUBuffer | undefined,
		queries: GPUQuerySet | undefined,
		binding: GPUBindGroup;
	let disposed = false, pending = false;
	const ready = shader.getCompilationInfo().then( info => {
		const errors = info.messages.filter( m => m.type === "error" );
		if ( errors.length ) throw Error( errors.map( m => m.message ).join( "\n" ) );
		return device.createRenderPipelineAsync( {
			layout: "auto",
			vertex: { module: shader, entryPoint: "vs" },
			fragment: { module: shader, entryPoint: "fs", targets: [ { format, writeMask: 0 } ] },
			primitive: { topology: "point-list" },
			depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "less-equal" }
		} );
	} ).then( value => {
		pipeline = value;
	} );
	return {
		ready,
		/*
		================
		query
		================
		*/
		async query(
			points: Float32Array,
			matrix: Float32Array,
			color: GPUTextureView,
			depth: GPUTextureView
		): Promise<readonly boolean[]> {
			if (
				disposed || pending || !points.length || points.length % 4 ||
				points.length > CHARACTER_ACTORS * 4 || !points.every( Number.isFinite ) ||
				matrix.length !== 16 || !matrix.every( Number.isFinite )
			) throw Error( "Invalid particle query" );
			pending = true;
			const count = points.length / 4;
			try {
				await ready;
				if ( disposed ) return noPointsVisible( count );
				if ( !storage ) {
					storage = device.createBuffer( {
						label: "particle-query-points",
						size: CHARACTER_ACTORS * 16,
						usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
					} );
					uniform = device.createBuffer( {
						label: "particle-query-view",
						size: 64,
						usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
					} );
					resolve = device.createBuffer( {
						label: "particle-query-resolve",
						size: CHARACTER_ACTORS * 8,
						usage: GPUBufferUsage.QUERY_RESOLVE | GPUBufferUsage.COPY_SRC
					} );
					read = device.createBuffer( {
						label: "particle-query-read",
						size: CHARACTER_ACTORS * 8,
						usage: GPUBufferUsage.MAP_READ | GPUBufferUsage.COPY_DST
					} );
					queries = device.createQuerySet( { type: "occlusion", count: CHARACTER_ACTORS } );
					binding = device.createBindGroup( {
						layout: pipeline.getBindGroupLayout( 0 ),
						entries: [
							{ binding: 0, resource: { buffer: uniform } },
							{ binding: 1, resource: { buffer: storage } }
						]
					} );
				}
				device.queue.writeBuffer(
					storage,
					0,
					points.buffer as ArrayBuffer,
					points.byteOffset,
					points.byteLength
				);
				device.queue.writeBuffer(
					uniform!,
					0,
					matrix.buffer as ArrayBuffer,
					matrix.byteOffset,
					matrix.byteLength
				);
				const encoder = device.createCommandEncoder( { label: "particle-query" } ),
					pass = encoder.beginRenderPass( {
						label: "particle-query-points",
						occlusionQuerySet: queries!,
						colorAttachments: [ { view: color, loadOp: "load", storeOp: "store" } ],
						depthStencilAttachment: { view: depth, depthLoadOp: "load", depthStoreOp: "store" }
					} );
				pass.setPipeline( pipeline );
				pass.setBindGroup( 0, binding );
				for ( let i = 0; i < count; i++ ) {
					pass.beginOcclusionQuery( i );
					pass.draw( 1, 1, i );
					pass.endOcclusionQuery();
				}
				pass.end();
				encoder.resolveQuerySet( queries!, 0, count, resolve!, 0 );
				encoder.copyBufferToBuffer( resolve!, 0, read!, 0, count * 8 );
				device.queue.submit( [ encoder.finish() ] );
				try {
					await read!.mapAsync( GPUMapMode.READ, 0, count * 8 );
				} catch ( error ) {
					// dispose() destroyed the read buffer under this map:
					// the rejection is the disposal, not a failure (#2560).
					if ( disposed ) return noPointsVisible( count );
					throw error;
				}
				if ( disposed ) return noPointsVisible( count );
				const samples = new BigUint64Array( read!.getMappedRange( 0, count * 8 ) );
				return Array.from( samples, value => value > 0n );
			} finally {
				if ( read?.mapState === "mapped" ) read.unmap();
				pending = false;
			}
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			disposed = true;
			storage?.destroy();
			uniform?.destroy();
			resolve?.destroy();
			read?.destroy();
			queries?.destroy();
		}
	};
}
