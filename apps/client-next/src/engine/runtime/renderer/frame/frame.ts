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
export function createFrame( commands: FrameCommands ): FrameOwner {
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
	let anchors = new Set<GeometryDraw>();
	let recordedImage: ImageDraw | undefined, imageBundle: GPURenderBundle | null = null;
	return {
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
			bloom
		) {
			const sceneView = bloom?.view ?? view;
			if ( ui !== recordedUi ) {
				function record( layer: UiDraw["layer"] ) {
					const selected = ui.filter( draw => draw.layer === layer && draw.count > 0 );
					if ( !selected.length ) return null;
					const encoder = commands.createBundleEncoder();
					for ( const draw of selected ) {
						encoder.setPipeline( draw.pipeline );
						encoder.setBindGroup( 0, draw.binding );
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
					const encoder = commands.createBundleEncoder();
					encoder.setPipeline( image.pipeline );
					encoder.setBindGroup( 0, image.binding );
					encoder.draw( 6 );
					imageBundle = encoder.finish();
				}
				recordedImage = image;
			}
			// Retain bounded contiguous runs. Individual bundles add submission overhead;
			// one whole-world bundle makes a terrain change re-record the entire city.
			const bundles: GPURenderBundle[] = imageBundle ? [ imageBundle ] : [];
			const all = (geometry ? [ geometry, ...world ] : world).filter( draw =>
					draw.indexCount > 0 && draw.instanceCount > 0
				),
				nextAnchors = new Set<GeometryDraw>();
			for ( let start = 0; start < all.length; ) {
				let end = start + 1;
				while ( end < all.length && end - start < 32 && (end - start < 16 || !anchors.has( all[end]! )) ) end++;
				const first = all[start]!, draws = all.slice( start, end ), counts: number[] = [];
				for ( const draw of draws ) counts.push( draw.indexCount, draw.instanceCount );
				let cached = geometryBundles.get( first );
				if (
					!cached || cached.draws.length !== draws.length || draws.some( ( draw, i ) =>
						draw !== cached!.draws[i]
					) || counts.some( ( count, i ) => count !== cached!.counts[i] ) || draws.some( ( draw, i ) =>
						draw.binding !== cached!.bindings[i]
					)
				) {
					const encoder = commands.createBundleEncoder();
					for ( const draw of draws ) {
						encoder.setPipeline( draw.pipeline );
						encoder.setBindGroup( 0, draw.binding );
						encoder.setVertexBuffer( 0, draw.vertices );
						encoder.setIndexBuffer( draw.indices, "uint32" );
						encoder.drawIndexed( draw.indexCount, draw.instanceCount, 0, 0, 0 );
					}
					cached = { draws, counts, bindings: draws.map( draw => draw.binding ), bundle: encoder.finish() };
					geometryBundles.set( first, cached );
				}
				bundles.push( cached.bundle );
				nextAnchors.add( first );
				start = end;
			}
			anchors = nextAnchors;
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
					for ( const draw of portrait.draws ) {
						if ( draw.indexCount <= 0 || draw.instanceCount <= 0 ) continue;
						pass.setPipeline( draw.pipeline );
						pass.setBindGroup( 0, draw.binding );
						pass.setVertexBuffer( 0, draw.vertices );
						pass.setIndexBuffer( draw.indices, "uint32" );
						pass.drawIndexed( draw.indexCount, draw.instanceCount );
					}
					pass.end();
				}
			}
			const pass = encoder.beginRenderPass( {
				timestampWrites: timing?.pass( "main-pass" ),
				label: "main-pass",
				...(depth ?
					{
						depthStencilAttachment: {
							view: depth,
							depthClearValue: 1,
							depthLoadOp: "clear" as const,
							depthStoreOp: flares || deferred ? "store" as const : "discard" as const
						}
					} :
					{}),
				colorAttachments: [ {
					view: sceneView,
					clearValue: { r: 0.035, g: 0.07, b: 0.09, a: 1 },
					loadOp: "clear",
					storeOp: "store"
				} ]
			} );
			if ( bundles.length ) pass.executeBundles( bundles );
			if ( !bloom && !deferred && worldUiBundle ) pass.executeBundles( [ worldUiBundle ] );
			if ( !bloom && !deferred && !flares && !thunder && backgroundBundle ) {
				pass.executeBundles( [ backgroundBundle ] );
			}
			if ( !bloom && !deferred && !flares && !thunder && !preview.length && uiBundle ) {
				pass.executeBundles( [ uiBundle ] );
			}
			pass.end();
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
									depthStoreOp: flares ? "store" as const : "discard" as const
								}
							} :
							{})
					} );
					for ( const draw of extra ) {
						if ( draw.indexCount <= 0 || draw.instanceCount <= 0 ) continue;
						tail.setPipeline( draw.pipeline );
						tail.setBindGroup( 0, draw.binding );
						tail.setVertexBuffer( 0, draw.vertices );
						tail.setIndexBuffer( draw.indices, "uint32" );
						tail.drawIndexed( draw.indexCount, draw.instanceCount );
					}
					if ( !bloom && worldUiBundle ) tail.executeBundles( [ worldUiBundle ] );
					if ( !bloom && !flares && !thunder && backgroundBundle ) {
						tail.executeBundles( [ backgroundBundle ] );
					}
					if ( !bloom && !flares && !thunder && !preview.length && uiBundle ) {
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
					overlay.setPipeline( thunder.pipeline );
					overlay.setBindGroup( 0, thunder.binding );
					overlay.draw( 6 );
					overlay.end();
				}
				bloom?.encode( encoder, view );
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
					if ( flareBundle ) overlay.executeBundles( [ flareBundle ] );
					overlay.end();
				}
				if ( flares || thunder || bloom ) {
					const hud = encoder.beginRenderPass( {
						timestampWrites: timing?.pass( "hud-after-flares" ),
						label: "hud-after-flares",
						...(depth ?
							{
								depthStencilAttachment: {
									view: depth,
									depthClearValue: 1,
									depthLoadOp: "clear" as const,
									depthStoreOp: "discard" as const
								}
							} :
							{}),
						colorAttachments: [ { view, loadOp: "load", storeOp: "store" } ]
					} );
					if ( bloom && worldUiBundle ) hud.executeBundles( [ worldUiBundle ] );
					if ( backgroundBundle ) hud.executeBundles( [ backgroundBundle ] );
					if ( !preview.length && uiBundle ) hud.executeBundles( [ uiBundle ] );
					hud.end();
				}
				if ( preview.length ) {
					const overlay = encoder.beginRenderPass( {
						timestampWrites: timing?.pass( "character-preview" ),
						label: "character-preview",
						...(depth ?
							{
								depthStencilAttachment: {
									view: depth,
									depthClearValue: 1,
									depthLoadOp: "clear" as const,
									depthStoreOp: "discard" as const
								}
							} :
							{}),
						colorAttachments: [ { view, loadOp: "load", storeOp: "store" } ]
					} );
					for ( const draw of preview ) {
						if ( draw.indexCount <= 0 || draw.instanceCount <= 0 ) continue;
						overlay.setPipeline( draw.pipeline );
						overlay.setBindGroup( 0, draw.binding );
						overlay.setVertexBuffer( 0, draw.vertices );
						overlay.setIndexBuffer( draw.indices, "uint32" );
						overlay.drawIndexed( draw.indexCount, draw.instanceCount, 0, 0, 0 );
					}
					if ( uiBundle ) overlay.executeBundles( [ uiBundle ] );
					overlay.end();
				}
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
