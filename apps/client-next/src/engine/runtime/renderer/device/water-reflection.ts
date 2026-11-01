/*
===========================================================================

water-reflection.ts - the native 512-square planar reflection target

Owns separate main/capture uniforms so queue writes cannot change a pass
already recorded for the same frame. Capture bindings never sample the target.

===========================================================================
*/
import { WATER_REFLECTION_SIZE, waterTextureProjection } from "@/engine/foundation/rendering/water-reflection";
import type { GeometryDraw } from "../internal/gpu-contract";
import type { Retire } from "./retirement";
const UNIFORM_BYTES = 112;

/*
================
createWaterReflection
================
*/
export function createWaterReflection( device: GPUDevice, initialFormat: GPUTextureFormat, retire: Retire ) {
	let format = initialFormat, samples = 1;
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
	let color: GPUTexture | undefined,
		resolve: GPUTexture | undefined,
		depth: GPUTexture | undefined;
	let target: GPUTextureView | undefined,
		resolveView: GPUTextureView | undefined,
		depthView: GPUTextureView | undefined;
	const values = new Float32Array( UNIFORM_BYTES / 4 );
	/*
	================
	allocateColour / allocateDepth

	The mirror pair for the current format and sample count: a 4x mirror
	(the MSAA stage renders the reflected pass through the scene's own
	pipelines) resolves into its own single-sampled texture, which alone is
	sampled by the water shader. A format change alone never touches the
	depth; only a sample-count change does.
	================
	*/
	function allocateColour() {
		color = device.createTexture( {
			label: "water-reflection",
			size: [ WATER_REFLECTION_SIZE, WATER_REFLECTION_SIZE ],
			sampleCount: samples,
			format,
			usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
		} );
		if ( samples > 1 ) {
			resolve = device.createTexture( {
				label: "water-reflection-resolve",
				size: [ WATER_REFLECTION_SIZE, WATER_REFLECTION_SIZE ],
				format,
				usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
			} );
			resolveView = resolve.createView();
		} else {
			resolve = undefined;
			resolveView = undefined;
		}
		target = color.createView();
	}
	function allocateDepth() {
		depth = device.createTexture( {
			label: "water-reflection-depth",
			size: [ WATER_REFLECTION_SIZE, WATER_REFLECTION_SIZE ],
			sampleCount: samples,
			format: "depth24plus",
			usage: GPUTextureUsage.RENDER_ATTACHMENT
		} );
		depthView = depth.createView();
	}
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
				allocateColour();
				allocateDepth();
			} else if ( !matrix && color ) {
				retire( color );
				resolve && retire( resolve );
				retire( depth! );
				color = undefined;
				resolve = undefined;
				depth = undefined;
				target = undefined;
				resolveView = undefined;
				depthView = undefined;
			}
			values.fill( 0 );
			if ( matrix ) {
				const angle = Math.fround( Math.fround( seconds * .5 ) * 9 ),
					c = Math.fround( Math.cos( angle ) ),
					s = Math.fround( Math.sin( angle ) );
				values.set( [ c * .03999999910593033, s * -.03999999910593033, s * .03999999910593033, c ], 20 );
				values.set( matrix );
				values.set( waterTextureProjection( above ), 24 );
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
		reformat

		Recreate the mirror in a new colour format and sample count: the HDR
		stage toggles the scene intermediate and the MSAA stage its sample
		count, and the reflected pass renders through the same pipelines as
		the scene. Callers rebuild the capture bindings.
		================
		*/
		reformat( next: GPUTextureFormat, nextSamples = 1 ) {
			if ( format === next && samples === nextSamples ) return false;
			const depthChanged = samples !== nextSamples;
			format = next;
			samples = nextSamples;
			// A later first capture must use the current scene surface too.
			if ( !color ) return false;
			retire( color );
			resolve && retire( resolve );
			color = undefined;
			resolve = undefined;
			target = undefined;
			resolveView = undefined;
			if ( depthChanged ) {
				retire( depth! );
				depth = undefined;
				depthView = undefined;
			}
			allocateColour();
			if ( !depth ) allocateDepth();
			return true;
		},
		/*
		================
		view

		The single-sampled view the water shader samples: the resolved texture
		under MSAA, the mirror itself otherwise.
		================
		*/
		view() {
			return resolveView ?? target;
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
				colorAttachments: [ {
					view: target,
					...(resolveView ? { resolveTarget: resolveView } : {}),
					clearValue: [ 0, 0, 0, 1 ],
					loadOp: "clear",
					storeOp: "store"
				} ],
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
				pass.drawIndexed( draw.indexCount, draw.instanceCount, draw.firstIndex ?? 0, draw.baseVertex ?? 0 );
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
			resolve?.destroy();
			depth?.destroy();
		}
	};
}
