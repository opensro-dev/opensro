/*
===========================================================================

water-reflection.ts - the native 512-square planar reflection target

Owns separate main/capture uniforms so queue writes cannot change a pass
already recorded for the same frame. Capture bindings never sample the target.

===========================================================================
*/
import { WATER_REFLECTION_SIZE } from "@/engine/foundation/rendering/water-reflection";
import type { GeometryDraw } from "../internal/gpu-contract";
import type { Retire } from "./retirement";
const UNIFORM_BYTES = 96;

/*
================
createWaterReflection
================
*/
export function createWaterReflection( device: GPUDevice, format: GPUTextureFormat, retire: Retire ) {
	const main = device.createBuffer( {
		label: "water-main",
		size: UNIFORM_BYTES,
		usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
	} );
	const capture = device.createBuffer( {
		label: "water-capture",
		size: UNIFORM_BYTES,
		usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
	} );
	let color: GPUTexture | undefined, depth: GPUTexture | undefined;
	let target: GPUTextureView | undefined, depthView: GPUTextureView | undefined;
	const values = new Float32Array( UNIFORM_BYTES / 4 );
	return {
		main,
		capture,
		/*
		================
		update

		Return true when bind groups must be rebuilt after an option transition.
		================
		*/
		update( matrix: Float32Array | undefined, height: number, above: boolean, seconds: number ) {
			const changed = !!matrix !== !!color;
			if ( matrix && !color ) {
				color = device.createTexture( {
					label: "water-reflection",
					size: [ WATER_REFLECTION_SIZE, WATER_REFLECTION_SIZE ],
					format,
					usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
				} );
				depth = device.createTexture( {
					label: "water-reflection-depth",
					size: [ WATER_REFLECTION_SIZE, WATER_REFLECTION_SIZE ],
					format: "depth24plus",
					usage: GPUTextureUsage.RENDER_ATTACHMENT
				} );
				target = color.createView();
				depthView = depth.createView();
			} else if ( !matrix && color ) {
				retire( color );
				retire( depth! );
				color = undefined;
				depth = undefined;
				target = undefined;
				depthView = undefined;
			}
			values.fill( 0 );
			if ( matrix ) {
				const angle = Math.fround( Math.fround( seconds * .5 ) * 9 ),
					c = Math.fround( Math.cos( angle ) ),
					s = Math.fround( Math.sin( angle ) );
				values.set( [ c * .03999999910593033, s * -.03999999910593033, s * .03999999910593033, c ], 20 );
				values.set( matrix );
				values[16] = height;
				values[17] = above ? 1 : -1;
				values[18] = 1;
			}
			device.queue.writeBuffer( main, 0, values );
			values[19] = 1;
			device.queue.writeBuffer( capture, 0, values );
			return changed;
		},
		/*
		================
		view
		================
		*/
		view() {
			return target;
		},
		/*
		================
		encode
		================
		*/
		encode( encoder: GPUCommandEncoder, draws: readonly GeometryDraw[] ) {
			if ( !target ) return;
			const pass = encoder.beginRenderPass( {
				label: "water-reflection",
				colorAttachments: [ { view: target, clearValue: [ 0, 0, 0, 1 ], loadOp: "clear", storeOp: "store" } ],
				depthStencilAttachment: {
					view: depthView!,
					depthClearValue: 1,
					depthLoadOp: "clear",
					depthStoreOp: "discard"
				}
			} );
			pass.setBlendConstant( { r: 1, g: 1, b: 1, a: 1 } );
			for ( const draw of draws ) {
				if ( !draw.indexCount || !draw.instanceCount ) continue;
				pass.setPipeline( draw.pipeline );
				pass.setBindGroup( 0, draw.binding );
				pass.setVertexBuffer( 0, draw.vertices );
				pass.setIndexBuffer( draw.indices, "uint32" );
				pass.drawIndexed( draw.indexCount, draw.instanceCount );
			}
			pass.end();
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			main.destroy();
			capture.destroy();
			color?.destroy();
			depth?.destroy();
		}
	};
}
