/*
===========================================================================

frame.ts - the frame owner: render passes and bundles

Records and submits one frame's passes (world, overlays, UI, portraits)
from the prepared draw lists, retaining render bundles while their inputs
are unchanged.

===========================================================================
*/
import type {
	FrameCommands,
	FrameOwner,
	ImageDraw,
	GeometryDraw,
	UiDraw,
	FlareDraw
} from "@/engine/runtime/renderer/internal/gpu-contract";
import { BLOOM_BLEND_BYTE } from "@/engine/foundation/rendering/blend-state";
/*
================
createFrame
================
*/
export function createFrame( commands: FrameCommands ): FrameOwner {
	// D3DRS_BLENDFACTOR as the native device holds it: white until the first
	// bloom composite sets it (SWorld_CompositeBloom 8AA560), which never
	// restores it. BLENDFACTOR blends read it in every geometry pass.
	let blendFactor: GPUColor = [ 1, 1, 1, 1 ];
	const bloomBlendFactor: GPUColor = [
		BLOOM_BLEND_BYTE / 255,
		BLOOM_BLEND_BYTE / 255,
		BLOOM_BLEND_BYTE / 255,
		1
	];
	let recordedUi: readonly UiDraw[] = [],
		uiBundle: GPURenderBundle | null = null,
		backgroundBundle: GPURenderBundle | null = null,
		worldUiBundle: GPURenderBundle | null = null;
	let recordedFlares: FlareDraw["entries"] | null = null, flareBundle: GPURenderBundle | null = null;
	const geometryBundles = new WeakMap<
		GeometryDraw,
		{
			draws: readonly GeometryDraw[];
			counts: readonly number[];
			bindings: readonly GPUBindGroup[];
			bundle: GPURenderBundle;
		}
	>();
	let anchors = new Set<GeometryDraw>(), nextAnchors = new Set<GeometryDraw>();
	// The frame's drawable geometry, refilled every frame.
	const live: GeometryDraw[] = [];
	// The water refraction split, refilled every frame it is armed.
	const opaqueWorld: GeometryDraw[] = [], waterTail: GeometryDraw[] = [];
	let recordedImage: ImageDraw | undefined, imageBundle: GPURenderBundle | null = null;
	/*
	================
	bundleCurrent

	True when cached still records live[start..end): the same draws, with
	the same counts and bindings.
	================
	*/
	function bundleCurrent(
		cached: { draws: readonly GeometryDraw[]; counts: readonly number[]; bindings: readonly GPUBindGroup[]; },
		start: number,
		end: number
	): boolean {
		if ( cached.draws.length !== end - start ) return false;
		for ( let i = 0; i < end - start; i++ ) {
			const draw = live[start + i]!;
			if (
				draw !== cached.draws[i] || draw.binding !== cached.bindings[i] ||
				draw.indexCount !== cached.counts[i * 2] || draw.instanceCount !== cached.counts[i * 2 + 1]
			) return false;
		}
		return true;
	}
	/*
	================
	splitWater

	Partitions world for the water refraction stage: opaqueWorld takes
	world[0..transparentStart) without its blended water; waterTail takes
	that water, then the transparent tail world[transparentStart..), so
	the tail keeps its native order behind the water. False when no
	blended water is in view.
	================
	*/
	function splitWater( world: readonly GeometryDraw[], transparentStart: number ): boolean {
		opaqueWorld.length = 0;
		waterTail.length = 0;
		let found = false;
		for ( let i = 0; i < world.length; i++ ) {
			const draw = world[i]!, water = draw.water === true && draw.blended === true;
			found ||= water;
			if ( i >= transparentStart || water ) waterTail.push( draw );
			else opaqueWorld.push( draw );
		}
		return found;
	}
	/*
	================
	recordGeometry

	Appends to bundles the retained bundles that replay first and draws in
	order. Retain bounded contiguous runs: individual bundles add submission
	overhead; one whole-world bundle makes a terrain change re-record the
	entire city. Run starts are this frame's anchors (nextAnchors), so a
	frame may record several lists before the anchors swap.
	================
	*/
	function recordGeometry(
		bundles: GPURenderBundle[],
		draws: readonly GeometryDraw[],
		first?: GeometryDraw
	) {
		live.length = 0;
		if ( first && first.indexCount > 0 && first.instanceCount > 0 ) live.push( first );
		for ( const draw of draws ) if ( draw.indexCount > 0 && draw.instanceCount > 0 ) live.push( draw );
		for ( let start = 0; start < live.length; ) {
			let end = start + 1;
			while ( end < live.length && end - start < 32 && (end - start < 16 || !anchors.has( live[end]! )) ) {
				end++;
			}
			const head = live[start]!;
			let cached = geometryBundles.get( head );
			if ( !cached || !bundleCurrent( cached, start, end ) ) {
				const run = live.slice( start, end ), counts: number[] = [];
				const encoder = commands.createBundleEncoder( true, true );
				let pipeline: GPURenderPipeline | undefined, binding: GPUBindGroup | undefined;
				let vertices: GPUBuffer | undefined, indices: GPUBuffer | undefined;
				for ( const draw of run ) {
					counts.push( draw.indexCount, draw.instanceCount );
					if ( pipeline !== draw.pipeline ) {
						encoder.setPipeline( draw.pipeline );
						pipeline = draw.pipeline;
					}
					if ( binding !== draw.binding ) {
						encoder.setBindGroup( 0, draw.binding );
						binding = draw.binding;
					}
					if ( vertices !== draw.vertices ) {
						encoder.setVertexBuffer( 0, draw.vertices );
						vertices = draw.vertices;
					}
					if ( indices !== draw.indices ) {
						encoder.setIndexBuffer( draw.indices, "uint32" );
						indices = draw.indices;
					}
					encoder.drawIndexed(
						draw.indexCount,
						draw.instanceCount,
						draw.firstIndex ?? 0,
						draw.baseVertex ?? 0,
						0
					);
				}
				cached = { draws: run, counts, bindings: run.map( draw => draw.binding ), bundle: encoder.finish() };
				geometryBundles.set( head, cached );
			}
			bundles.push( cached.bundle );
			nextAnchors.add( head );
			start = end;
		}
	}
	return {
		/*
		================
		draw
		================
		*/
		draw(
			view,
			image,
			geometry,
			depth,
			world = [],
			ui = [],
			preview = [],
			flares,
			thunder,
			portrait,
			doll,
			partyPortraits = [],
			frameId,
			deferred,
			bloom,
			reflection,
			experimental,
			presentation
		) {
			const sceneScale = experimental?.sceneScale;
			const msaa = experimental?.msaa;
			const sceneView = bloom?.view ?? experimental?.hdr?.view ?? sceneScale?.view ?? view;
			const uiDepth = sceneScale?.frameDepth ?? depth;
			// The HDR stage resolves the world before UI. Its float target can
			// then be reused for an independently composited character preview.
			const hdrScene = !!experimental?.hdr;
			// The MSAA stage renders the scene into its 4x colour and resolves
			// into the stage chain's own scene view; every later pass, and the
			// UI, read the resolved target single-sampled.
			//
			// resolvedScene: the stages whose scene must be resolved (tone
			// mapped, upscaled or multisample-resolved) before the world labels
			// draw, so the labels move to the full-resolution label pass.
			// separateScene: every stage that renders the scene away from the
			// frame target (those, plus the bloom capture), so the labels, the
			// background and the UI wait for a later pass instead of the main
			// pass or the deferred tail.
			const resolvedScene = hdrScene || !!sceneScale || !!msaa;
			const separateScene = !!bloom || resolvedScene;
			if ( ui !== recordedUi ) {
				/*
				================
				record

				Each encoder starts without bindings. Skip only equal adjacent
				state within this bundle; the draw order and ranges stay intact.
				================
				*/
				function record( layer: UiDraw["layer"] ) {
					const selected = ui.filter( draw => draw.layer === layer && draw.count > 0 );
					if ( !selected.length ) return null;
					const encoder = commands.createBundleEncoder();
					let pipeline: GPURenderPipeline | undefined, binding: GPUBindGroup | undefined;
					for ( const draw of selected ) {
						if ( pipeline !== draw.pipeline ) {
							encoder.setPipeline( draw.pipeline );
							pipeline = draw.pipeline;
						}
						if ( binding !== draw.binding ) {
							encoder.setBindGroup( 0, draw.binding );
							binding = draw.binding;
						}
						encoder.draw( 6, draw.count, 0, draw.first );
					}
					return encoder.finish();
				}
				backgroundBundle = record( "background" );
				uiBundle = record( undefined );
				worldUiBundle = record( "world" );
				recordedUi = ui;
			}
			if ( image !== recordedImage ) {
				imageBundle = null;
				if ( image ) {
					const encoder = commands.createBundleEncoder( true, true );
					encoder.setPipeline( image.pipeline );
					encoder.setBindGroup( 0, image.binding );
					encoder.draw( 6 );
					imageBundle = encoder.finish();
				}
				recordedImage = image;
			}
			const bundles: GPURenderBundle[] = imageBundle ? [ imageBundle ] : [];
			nextAnchors.clear();
			// Port-only water refraction: with blended water in view, the main
			// pass draws the opaque world without it; the water, then the
			// transparent tail, draw in their own pass after the stage copies
			// the opaque frame. With none in view the frame keeps one pass.
			const water = experimental?.water && splitWater( world, experimental.water.transparentStart ) ?
				experimental.water :
				undefined;
			const waterBundles: GPURenderBundle[] = [];
			if ( water ) {
				recordGeometry( bundles, opaqueWorld, geometry );
				recordGeometry( waterBundles, waterTail );
			} else recordGeometry( bundles, world, geometry );
			const previousAnchors = anchors;
			anchors = nextAnchors;
			nextAnchors = previousAnchors;
			if ( (flares?.entries ?? null) !== recordedFlares ) {
				recordedFlares = flares?.entries ?? null;
				flareBundle = null;
				if ( recordedFlares ) {
					const encoder = commands.createBundleEncoder( false );
					for ( const draw of recordedFlares ) {
						if ( draw.count <= 0 ) continue;
						encoder.setPipeline( draw.pipeline );
						encoder.setBindGroup( 0, draw.binding );
						encoder.draw( draw.count, 1, 0, draw.index );
					}
					flareBundle = encoder.finish();
				}
			}
			const encoder = commands.createEncoder(), timing = commands.beginTiming?.( frameId );
			commands.prepare?.( encoder, timing );
			// The sun cascade renders before anything shades its receivers.
			experimental?.sunShadow?.encode( encoder, experimental.casters ?? [], timing );
			reflection?.encode( encoder );
			const portraitPreview = portrait;
			for ( const portrait of [ portraitPreview, ...partyPortraits, doll ] ) {
				if ( portrait ) {
					const pass = encoder.beginRenderPass( {
						timestampWrites: timing?.pass( portrait === doll ? "equipment-doll" : "portrait-128" ),
						label: portrait === doll ? "equipment-doll" : "portrait-128",
						colorAttachments: [ {
							view: portrait.target,
							clearValue: { r: 0, g: 0, b: 0, a: 0 },
							loadOp: "clear",
							storeOp: "store"
						} ],
						depthStencilAttachment: {
							view: portrait.depth,
							depthClearValue: 1,
							depthLoadOp: "clear",
							depthStoreOp: "discard"
						}
					} );
					pass.setBlendConstant( blendFactor );
					for ( const draw of portrait.draws ) {
						if ( draw.indexCount <= 0 || draw.instanceCount <= 0 ) continue;
						pass.setPipeline( draw.pipeline );
						pass.setBindGroup( 0, draw.binding );
						pass.setVertexBuffer( 0, draw.vertices );
						pass.setIndexBuffer( draw.indices, "uint32" );
						pass.drawIndexed(
							draw.indexCount,
							draw.instanceCount,
							draw.firstIndex ?? 0,
							draw.baseVertex ?? 0
						);
					}
					pass.end();
				}
			}
			// A pass after the scene reads its depth: the flares' occlusion, the
			// deferred particles, the occlusion stage, or the labels after a
			// separate scene.
			const laterDepthRead = !!flares || !!deferred || !!experimental?.ao || (separateScene && !!worldUiBundle);
			const pass = encoder.beginRenderPass( {
				timestampWrites: timing?.pass( "main-pass" ),
				label: "main-pass",
				...(depth ?
					{
						depthStencilAttachment: {
							view: msaa ? msaa.depth : depth,
							depthClearValue: 1,
							depthLoadOp: "clear" as const,
							// The water pass loads the depth the main pass leaves.
							depthStoreOp: msaa || laterDepthRead || water ? "store" as const : "discard" as const
						}
					} :
					{}),
				colorAttachments: [ {
					view: msaa ? msaa.view : sceneView,
					...(msaa ? { resolveTarget: sceneView } : {}),
					clearValue: { r: 0.035, g: 0.07, b: 0.09, a: 1 },
					loadOp: "clear",
					// The water pass draws on into the 4x colour, so it keeps it.
					storeOp: msaa && !water ? "discard" as const : "store" as const
				} ]
			} );
			pass.setBlendConstant( blendFactor );
			if ( bundles.length ) pass.executeBundles( bundles );
			// Natively everything draws here; any separate scene or the
			// deferred tail moves the labels, background and UI later.
			// With the water armed, its pass is the last scene pass instead.
			const sceneDrawsUi = !separateScene && !deferred;
			/*
			================
			sceneUi

			The labels, background and interface draw inside the last scene
			pass when no separate scene or deferred tail claims them later.
			================
			*/
			const sceneUi = ( target: GPURenderPassEncoder ) => {
				if ( !sceneDrawsUi ) return;
				if ( worldUiBundle ) target.executeBundles( [ worldUiBundle ] );
				if ( flares || thunder ) return;
				if ( backgroundBundle ) target.executeBundles( [ backgroundBundle ] );
				if ( !preview.length && uiBundle ) target.executeBundles( [ uiBundle ] );
			};
			if ( !water ) sceneUi( pass );
			pass.end();
			// The multisampled depth cannot be resolved by the driver: write the
			// samples' minimum into the scene's single-sampled depth now,
			// before the deferred tail, the flare probe and the labels read it.
			// The render-scale stage upscales this depth as it always does.
			if ( msaa && depth ) msaa.resolveDepth( encoder, depth );
			// Port-only water refraction: the opaque frame is complete and
			// resolved. The stage copies its colour and depth, then the water
			// and the transparent tail draw over it in their native order, so a
			// blended surface in front of the water still covers it. Under MSAA
			// they draw on into the 4x buffers (the retained bundles are 4x) and
			// resolve again; their depth is resolved once more for later readers.
			if ( water ) {
				if ( !depth ) throw Error( "Water refraction needs the scene depth" );
				water.stage.encode( encoder, sceneView, depth );
				const overlay = encoder.beginRenderPass( {
					timestampWrites: timing?.pass( "water-refraction" ),
					label: "water-refraction",
					depthStencilAttachment: {
						view: msaa ? msaa.depth : depth,
						depthLoadOp: "load",
						depthStoreOp: laterDepthRead ? "store" : "discard"
					},
					colorAttachments: [ {
						view: msaa ? msaa.view : sceneView,
						...(msaa ? { resolveTarget: sceneView } : {}),
						loadOp: "load",
						storeOp: msaa ? "discard" : "store"
					} ]
				} );
				overlay.setBlendConstant( blendFactor );
				if ( waterBundles.length ) overlay.executeBundles( waterBundles );
				sceneUi( overlay );
				overlay.end();
				if ( msaa && laterDepthRead ) msaa.resolveDepth( encoder, depth );
			}
			/*
			================
			previewPass

			The character preview draws over fresh depth after every consumer
			of the scene depth. HDR uses the float target cleared transparent;
			natively it composes directly into the frame as before.
			================
			*/
			const previewPass = ( encoder: GPUCommandEncoder, color: GPUTextureView ) => {
				const previewDepth = hdrScene ? depth : uiDepth;
				const overlay = encoder.beginRenderPass( {
					timestampWrites: timing?.pass( "character-preview" ),
					label: "character-preview",
					...(previewDepth ?
						{
							depthStencilAttachment: {
								view: previewDepth,
								depthClearValue: 1,
								depthLoadOp: "clear" as const,
								depthStoreOp: "discard" as const
							}
						} :
						{}),
					colorAttachments: [ {
						view: color,
						loadOp: hdrScene ? "clear" : "load",
						clearValue: [ 0, 0, 0, 0 ],
						storeOp: "store"
					} ]
				} );
				overlay.setBlendConstant( blendFactor );
				for ( const draw of preview ) {
					if ( draw.indexCount <= 0 || draw.instanceCount <= 0 ) continue;
					overlay.setPipeline( draw.pipeline );
					overlay.setBindGroup( 0, draw.binding );
					overlay.setVertexBuffer( 0, draw.vertices );
					overlay.setIndexBuffer( draw.indices, "uint32" );
					overlay.drawIndexed(
						draw.indexCount,
						draw.instanceCount,
						draw.firstIndex ?? 0,
						draw.baseVertex ?? 0,
						0
					);
				}
				if ( !hdrScene && uiBundle ) overlay.executeBundles( [ uiBundle ] );
				overlay.end();
			};
			/*
			================
			finish
			================
			*/
			const finish = ( encoder: GPUCommandEncoder, extra: readonly GeometryDraw[] = [] ) => {
				if ( deferred ) {
					commands.prepare?.( encoder, timing );
					const tail = encoder.beginRenderPass( {
						label: "deferred-particles",
						timestampWrites: timing?.pass( "deferred-particles" ),
						colorAttachments: [ { view: sceneView, loadOp: "load", storeOp: "store" } ],
						...(depth ?
							{
								depthStencilAttachment: {
									view: depth,
									depthLoadOp: "load" as const,
									depthStoreOp: flares || experimental?.ao || (separateScene && worldUiBundle) ?
										"store" as const :
										"discard" as const
								}
							} :
							{})
					} );
					tail.setBlendConstant( blendFactor );
					for ( const draw of extra ) {
						if ( draw.indexCount <= 0 || draw.instanceCount <= 0 ) continue;
						tail.setPipeline( draw.pipeline );
						tail.setBindGroup( 0, draw.binding );
						tail.setVertexBuffer( 0, draw.vertices );
						tail.setIndexBuffer( draw.indices, "uint32" );
						tail.drawIndexed(
							draw.indexCount,
							draw.instanceCount,
							draw.firstIndex ?? 0,
							draw.baseVertex ?? 0
						);
					}
					if ( !separateScene && worldUiBundle ) tail.executeBundles( [ worldUiBundle ] );
					if ( !separateScene && !flares && !thunder && backgroundBundle ) {
						tail.executeBundles( [ backgroundBundle ] );
					}
					if ( !separateScene && !flares && !thunder && !preview.length && uiBundle ) {
						tail.executeBundles( [ uiBundle ] );
					}
					tail.end();
				}
				if ( thunder ) {
					const overlay = encoder.beginRenderPass( {
						timestampWrites: timing?.pass( "weather-thunder" ),
						label: "weather-thunder",
						colorAttachments: [ { view: sceneView, loadOp: "load", storeOp: "store" } ]
					} );
					overlay.setBlendConstant( blendFactor );
					overlay.setPipeline( thunder.pipeline );
					overlay.setBindGroup( 0, thunder.binding );
					overlay.draw( 6 );
					overlay.end();
				}
				// Ambient occlusion (experimental, #2094): this frame's
				// stored depth totals into the factor the NEXT frame's
				// surfaces sample (binding 15) - the one-frame lag is the
				// design (the alternative is a depth prepass). It reads
				// the depth after the scene and particle passes and ahead
				// of every post chain; while off the owner is absent and
				// the depth above was discarded as natively.
				if ( experimental?.ao && depth ) experimental.ao.encode( encoder, depth );
				bloom?.encode( encoder, view );
				if ( bloom ) blendFactor = bloomBlendFactor;
				// Without bloom the HDR stage owns its tone-map pass; with it,
				// the chain's copy-back step already tonemapped into the frame.
				if ( !bloom && experimental?.hdr ) experimental.hdr.encode( encoder, view );
				else if ( !bloom && sceneScale ) sceneScale.resolve( encoder, view );
				if ( flares ) {
					const compute = encoder.beginComputePass( {
						timestampWrites: timing?.pass( "flare-visibility" ),
						label: "flare-visibility"
					} );
					compute.setPipeline( flares.compute );
					compute.setBindGroup( 0, flares.binding );
					compute.dispatchWorkgroups( 1 );
					compute.end();
					const overlay = encoder.beginRenderPass( {
						timestampWrites: timing?.pass( "flare-chain" ),
						label: "flare-chain",
						colorAttachments: [ { view, loadOp: "load", storeOp: "store" } ]
					} );
					overlay.setBlendConstant( blendFactor );
					if ( flareBundle ) overlay.executeBundles( [ flareBundle ] );
					overlay.end();
				}
				// Port-only, not native: labels keep full-resolution pixels and
				// scene occlusion after HDR or reduced-resolution scene rendering.
				// The MSAA depth was already resolved right after the scene pass.
				if ( resolvedScene && worldUiBundle ) {
					sceneScale?.resolveDepth( encoder );
					const labels = encoder.beginRenderPass( {
						label: "hdr-world-ui",
						colorAttachments: [ { view, loadOp: "load", storeOp: "store" } ],
						...(uiDepth ?
							{
								depthStencilAttachment: {
									view: uiDepth,
									depthLoadOp: "load" as const,
									depthStoreOp: "discard" as const
								}
							} :
							{})
					} );
					labels.executeBundles( [ worldUiBundle ] );
					labels.end();
				}
				// Bloom alone keeps the world labels for the HUD pass, over the
				// scene depth it loads; a resolved scene drew them above.
				const hudDrawsLabels = !!bloom && !resolvedScene && !!worldUiBundle;
				if ( flares || thunder || separateScene ) {
					const hud = encoder.beginRenderPass( {
						timestampWrites: timing?.pass( "hud-after-flares" ),
						label: "hud-after-flares",
						...(uiDepth ?
							{
								depthStencilAttachment: {
									view: uiDepth,
									depthClearValue: 1,
									depthLoadOp: hudDrawsLabels ? "load" as const : "clear" as const,
									depthStoreOp: "discard" as const
								}
							} :
							{}),
						colorAttachments: [ { view, loadOp: "load", storeOp: "store" } ]
					} );
					if ( hudDrawsLabels ) hud.executeBundles( [ worldUiBundle! ] );
					if ( backgroundBundle ) hud.executeBundles( [ backgroundBundle ] );
					if ( !preview.length && uiBundle ) hud.executeBundles( [ uiBundle ] );
					hud.end();
				}
				if ( preview.length ) {
					if ( experimental?.hdr ) {
						// Port-only, not native: preserve background -> preview ->
						// foreground without sending authored UI through the curve.
						previewPass( encoder, experimental.hdr.view );
						experimental.hdr.encodePreview( encoder, view );
						if ( uiBundle ) {
							const foreground = encoder.beginRenderPass( {
								label: "hdr-preview-ui",
								colorAttachments: [ { view, loadOp: "load", storeOp: "store" } ],
								...(uiDepth ?
									{
										depthStencilAttachment: {
											view: uiDepth,
											depthClearValue: 1,
											depthLoadOp: "clear" as const,
											depthStoreOp: "discard" as const
										}
									} :
									{})
							} );
							foreground.executeBundles( [ uiBundle ] );
							foreground.end();
						}
					} else previewPass( encoder, view );
				}
				// Preserve every pass and depth operation above. Only append the
				// final presentation here instead of submitting it separately.
				presentation?.encodePresent( encoder );
				const query = timing?.resolve();
				if ( query ) {
					encoder.resolveQuerySet( query.query, 0, query.count, query.resolve, 0 );
					encoder.copyBufferToBuffer( query.resolve, 0, query.read, 0, query.count * 8 );
				}
				commands.submit( encoder.finish() );
				timing?.submitted();
			};
			if ( !deferred ) return finish( encoder );
			if ( deferred.asynchronous ) {
				commands.submit( encoder.finish() );
				return Promise.resolve( deferred.prepare() ).then( extra => finish( commands.createEncoder(), extra ) );
			}
			const extra = deferred.prepare();
			if ( extra instanceof Promise ) throw Error( "Unexpected asynchronous deferred pass" );
			return finish( encoder, extra );
		}
	};
}
