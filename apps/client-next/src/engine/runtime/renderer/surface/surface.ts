/*
===========================================================================

surface.ts - full-resolution frame targets and swapchain presentation

Retain offscreen color across deferred visibility queries. Acquire the
swapchain only when encoding presentation after those queries complete;
canvas textures cannot survive arbitrary asynchronous browser turns.

Port-only, not native: Experimental render scale (100 by default) splits the frame's
targets: below 100 the scene's colour and depth render at the scaled
size, while the presented frame colour and the HUD's depth stay at the
backing store's, so the UI and its text always compose and rasterize 1:1.
At 100 the surface is exactly the native pair, unless separateScene asks
for the scene in its own full-size colour (Experimental > FXAA resolves it
into the frame): the scene then shares the frame's depth, as at 100.

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
	// The render scale's scaled scene colour and the full-size HUD depth.
	let sceneColor: ColorTarget | null = null, frameDepth: DepthTarget | null = null;
	let width = 0, height = 0, scale = 100, separate = false, disposed = false;
	/*
	================
	releaseScale
	================
	*/
	function releaseScale() {
		sceneColor?.dispose();
		sceneColor = null;
		frameDepth?.dispose();
		frameDepth = null;
	}
	return {
		/*
		================
		depth

		The scene's depth: scaled while the render scale is below 100, the
		frame's own depth otherwise.
		================
		*/
		depth() {
			if ( disposed || !depth ) throw new Error( "No active depth surface" );
			return depth.view;
		},
		/*
		================
		frameView

		The full-size frame colour the UI composes into and the
		presentation presents; the scaled scene resolves into it first.
		================
		*/
		frameView() {
			if ( disposed || !color ) throw new Error( "No active frame surface" );
			return color.view;
		},
		/*
		================
		frameDepth

		The full-size depth the HUD and preview passes attach while the
		scene renders scaled; at full size the scene's depth is that depth.
		================
		*/
		frameDepth() {
			const target = frameDepth ?? depth;
			if ( disposed || !target ) throw new Error( "No active frame depth" );
			return target.view;
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
		acquire( viewport, retain = false, renderScale = 100, separateScene = false ) {
			if ( disposed ) {
				throw new Error( "Disposed surface" );
			}
			const scaled = renderScale < 100,
				sceneWidth = Math.max( 1, Math.round( viewport.width * renderScale / 100 ) ),
				sceneHeight = Math.max( 1, Math.round( viewport.height * renderScale / 100 ) );
			if (
				width !== viewport.width || height !== viewport.height || scale !== renderScale ||
				separate !== separateScene
			) {
				color?.dispose();
				color = null;
				width = viewport.width;
				height = viewport.height;
				scale = renderScale;
				separate = separateScene;
				canvas.width = width;
				canvas.height = height;
				commands.configure( context, format );
				// The main pass's depth follows the scene's size.
				const replacement = commands.createDepth( scaled ? sceneWidth : width, scaled ? sceneHeight : height );
				depth?.dispose();
				depth = replacement;
				releaseScale();
			}
			// A scaled or separate scene always presents through the
			// offscreen: the resolve into the frame is a presentation step.
			offscreen = retain || scaled || separateScene;
			if ( scaled || separateScene ) {
				if ( !color ) {
					color = commands.createColor( width, height );
				}
				if ( !sceneColor ) {
					sceneColor = commands.createColor( sceneWidth, sceneHeight );
					if ( scaled ) frameDepth = commands.createDepth( width, height );
				}
				return sceneColor.view;
			}
			releaseScale();
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
			releaseScale();
			context.unconfigure();
		}
	};
}
