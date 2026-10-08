/*
===========================================================================

surface.ts - full-resolution frame targets and swapchain presentation

Retain offscreen color across deferred visibility queries. Acquire the
swapchain only when encoding presentation after those queries complete;
canvas textures cannot survive arbitrary asynchronous browser turns.

===========================================================================
*/
import type {
	SurfaceCommands,
	SurfaceOwner,
	DepthTarget,
	ColorTarget
} from "@/engine/runtime/renderer/internal/gpu-contract";
/*
================
createSurface
================
*/
export function createSurface(
	canvas: HTMLCanvasElement,
	commands: SurfaceCommands,
	format: GPUTextureFormat
): SurfaceOwner {
	const context = canvas.getContext( "webgpu" );
	if ( !context ) {
		throw new Error( "WebGPU canvas unavailable" );
	}
	let depth: DepthTarget | null = null, color: ColorTarget | null = null, offscreen = false;
	let width = 0, height = 0, disposed = false;
	return {
		/*
		================
		depth
		================
		*/
		depth() {
			if ( disposed || !depth ) throw new Error( "No active depth surface" );
			return depth.view;
		},
		/*
		================
		encodePresent

		Keep acquisition inside the final synchronous encode/submit interval.
		A query readback may have expired any previously acquired texture.
		================
		*/
		encodePresent( encoder ) {
			if ( disposed ) throw Error( "Disposed surface" );
			if ( offscreen ) {
				color!.encodePresent( encoder, context.getCurrentTexture() );
				offscreen = false;
			}
		},
		/*
		================
		acquire
		================
		*/
		acquire( viewport, retain = false ) {
			if ( disposed ) {
				throw new Error( "Disposed surface" );
			}
			if ( width !== viewport.width || height !== viewport.height ) {
				color?.dispose();
				color = null;
				width = viewport.width;
				height = viewport.height;
				canvas.width = width;
				canvas.height = height;
				commands.configure( context, format );
				const replacement = commands.createDepth( width, height );
				depth?.dispose();
				depth = replacement;
			}
			offscreen = retain;
			if ( retain ) {
				if ( !color ) {
					const replacement = commands.createColor( width, height );
					color = replacement;
				}
				return color.view;
			}
			return context.getCurrentTexture().createView();
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) {
				return;
			}
			disposed = true;
			depth?.dispose();
			depth = null;
			color?.dispose();
			color = null;
			context.unconfigure();
		}
	};
}
