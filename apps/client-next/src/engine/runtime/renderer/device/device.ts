/*
===========================================================================

device.ts - WebGPU generation and resource lifecycle

===========================================================================
*/
import { createBloom } from "./bloom";
import { createParticleQuery } from "./particle-query";
import { createGpuAnimationResources } from "./animation";
import { createGpuTiming } from "./timing";
import { createThunder } from "./thunder";
import { createFlares } from "./flares";
import { createUiResources } from "./ui";
import { createPipelines } from "./pipelines";
import { createImages } from "./images";
import { createGeometryResources } from "./geometry";
import type { DeviceOwner, FrameCommands, SurfaceCommands } from "@/engine/runtime/renderer/internal/gpu-contract";
import type { RuntimePhase } from "@/engine/contracts/runtime";

const DEFAULT_TEXTURE_DETAIL = 2;
const ENVIRONMENT_UNIFORM_BYTES = 336;
const FULLSCREEN_VERTEX_COUNT = 6;

/*
================
createDevice

Initialize one device generation and grant checked capabilities after its pipelines are ready.
================
*/
export function createDevice( timingEnabled = false, gpuAnimationEnabled = true ): DeviceOwner {
	let textureFiltered = true, textureDetail = DEFAULT_TEXTURE_DETAIL;
	let timing: ReturnType<typeof createGpuTiming> | null = null;
	let phase: RuntimePhase = "starting", failure: string | null = null, device: GPUDevice | null = null;
	let environmentBuffer: GPUBuffer | null = null,
		sky: import("@/engine/runtime/renderer/internal/gpu-contract").ImageDraw | null = null;
	let geometry: ReturnType<typeof createGeometryResources> | null = null,
		images: ReturnType<typeof createImages> | null = null;
	let ui: ReturnType<typeof createUiResources> | null = null;
	let thunder: ReturnType<typeof createThunder> | null = null;
	let flares: ReturnType<typeof createFlares> | null = null;
	let bloom: ReturnType<typeof createBloom> | null = null;
	let particleQuery: ReturnType<typeof createParticleQuery> | null = null;
	const depthTextures = new Set<GPUTexture>();
	let epoch = 1, recoverable = false;
	let commands: FrameCommands | null = null, surface: SurfaceCommands | null = null;
	const generation = epoch;

	/*
	================
	fail

	Record a terminal error only for the generation that created this callback.
	================
	*/
	const fail = ( error: unknown, canRecover = false ) => {
		if ( generation !== epoch ) {
			return;
		}
		recoverable = canRecover;
		failure = String( error );
		phase = "failed";
	};
	if ( !navigator.gpu ) {
		fail( "WebGPU is unavailable" );
	} else {
		navigator.gpu.requestAdapter().then( adapter => {
			if ( generation !== epoch ) {
				return null;
			}
			if ( !adapter ) {
				throw new Error( "No WebGPU adapter" );
			}
			const requiredFeatures: GPUFeatureName[] = [];
			if ( adapter.features.has( "texture-compression-bc" ) ) requiredFeatures.push( "texture-compression-bc" );
			if ( timingEnabled && adapter.features.has( "timestamp-query" ) ) {
				requiredFeatures.push( "timestamp-query" );
			}
			return adapter.requestDevice( { requiredFeatures } );
		} ).then( created => {
			if ( !created ) {
				return;
			}
			if ( generation !== epoch ) {
				created.destroy();
				return;
			}
			device = created;
			if ( timingEnabled && created.features.has( "timestamp-query" ) ) timing = createGpuTiming( created );
			created.lost.then( info => {
				if ( phase === "running" || phase === "starting" ) {
					fail( `Device lost: ${info.message}`, info.reason !== "destroyed" );
				}
			} );
			created.addEventListener( "uncapturederror", event => fail( event.error.message ) );

			/*
			================
			current

			Reject a capability used before initialization or after its device generation retires.
			================
			*/
			const current = () => {
				if ( generation !== epoch || phase !== "running" ) {
					throw new Error( "Stale device capability" );
				}
				return created;
			};
			commands = Object.freeze( {
				/*
				================
				prepare

				Prepare geometry through the current generation's resource owner.
				================
				*/
				prepare: ( ...args: Parameters<NonNullable<FrameCommands["prepare"]>> ) =>
					geometry?.prepare?.( ...args ),
				/*
				================
				beginTiming

				Start a timing sample only when the optional timing owner exists.
				================
				*/
				beginTiming: ( frameId?: number ) => timing?.begin( frameId ),
				/*
				================
				createBundleEncoder

				Use the same canvas and depth formats as the frame's render targets.
				================
				*/
				createBundleEncoder: ( depth = true ) =>
					current().createRenderBundleEncoder( {
						label: "retained-scene",
						colorFormats: [ navigator.gpu.getPreferredCanvasFormat() ],
						...(depth ? { depthStencilFormat: "depth24plus" as const } : {})
					} ),
				/*
				================
				createEncoder

				Create a frame command encoder through the checked device capability.
				================
				*/
				createEncoder: () => current().createCommandEncoder( { label: "sro-frame" } ),
				/*
				================
				submit

				Submit the completed command buffer through the current device generation.
				================
				*/
				submit: ( buffer: GPUCommandBuffer ) => current().queue.submit( [ buffer ] )
			} );
			surface = Object.freeze( {
				/*
				================
				createColor

				Own an intermediate color target until it is presented and explicitly released.
				================
				*/
				createColor( width: number, height: number ) {
					const texture = current().createTexture( {
						label: "deferred-frame-color",
						size: [ width, height ],
						format: navigator.gpu.getPreferredCanvasFormat(),
						usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC
					} );
					depthTextures.add( texture );
					return Object.freeze( {
						view: texture.createView(),

						/*
						================
						present

						Copy the intermediate frame into the current presentation target.
						================
						*/
						present( target: GPUTexture ) {
							if ( !depthTextures.has( texture ) ) throw Error( "Disposed frame color" );
							const encoder = current().createCommandEncoder( { label: "deferred-frame-present" } );
							encoder.copyTextureToTexture( { texture }, { texture: target }, [ width, height ] );
							current().queue.submit( [ encoder.finish() ] );
						},

						/*
						================
						dispose

						Release the resources owned by this capability; repeated retirement is harmless.
						================
						*/
						dispose() {
							if ( depthTextures.delete( texture ) ) texture.destroy();
						}
					} );
				},

				/*
				================
				createDepth

				Own a depth target usable by both geometry rendering and depth sampling.
				================
				*/
				createDepth( width: number, height: number ) {
					const texture = current().createTexture( {
						label: "surface-depth",
						size: [ width, height ],
						format: "depth24plus",
						usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
					} );
					depthTextures.add( texture );
					return Object.freeze( {
						view: texture.createView(),
						/*
						================
						dispose

						Release the resources owned by this capability; repeated retirement is harmless.
						================
						*/
						dispose() {
							if ( depthTextures.delete( texture ) ) {
								texture.destroy();
							}
						}
					} );
				},
				/*
				================
				configure

				Bind the presentation context to the current device and copy-capable surface.
				================
				*/
				configure: ( context: GPUCanvasContext, format: GPUTextureFormat ) =>
					context.configure( {
						device: current(),
						format,
						alphaMode: "opaque",
						usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_DST
					} )
			} );
			const pipelines = createPipelines( created, navigator.gpu.getPreferredCanvasFormat() );
			images = createImages( {
				current,
				fail,
				pipeline: pipelines.image,
				sampler: pipelines.sampler,

				/*
				================
				generateMips

				Generate levels for bitmap uploads; native resources already carry their complete mip chain.
				================
				*/
				generateMips: ( texture, levels, layers ) => {
					if ( levels <= 1 ) {
						return;
					}
					const encoder = current().createCommandEncoder( { label: "upload-texture-mips" } );
					for ( let layer = 0; layer < layers; layer++ ) {
						for ( let level = 1; level < levels; level++ ) {
							const binding = current().createBindGroup( {
								layout: pipelines.mips().getBindGroupLayout( 0 ),
								entries: [ { binding: 0, resource: pipelines.worldSampler }, {
									binding: 1,
									resource: texture.createView( {
										dimension: "2d",
										baseArrayLayer: layer,
										arrayLayerCount: 1,
										baseMipLevel: level - 1,
										mipLevelCount: 1
									} )
								} ]
							} );
							const pass = encoder.beginRenderPass( {
								colorAttachments: [ {
									view: texture.createView( {
										dimension: "2d",
										baseArrayLayer: layer,
										arrayLayerCount: 1,
										baseMipLevel: level,
										mipLevelCount: 1
									} ),
									loadOp: "clear",
									storeOp: "store"
								} ]
							} );
							pass.setPipeline( pipelines.mips() );
							pass.setBindGroup( 0, binding );
							pass.draw( FULLSCREEN_VERTEX_COUNT );
							pass.end();
						}
					}
					current().queue.submit( [ encoder.finish() ] );
				}
			} );
			environmentBuffer = created.createBuffer( {
				size: ENVIRONMENT_UNIFORM_BYTES,
				usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
			} );
			geometry = createGeometryResources(
				created,
				current,
				fail,
				pipelines.geometry,
				images.texture,
				pipelines.worldSampler,
				pipelines.lightmapSampler,
				environmentBuffer,
				pipelines.worldSampling,
				gpuAnimationEnabled ? createGpuAnimationResources( created ) : undefined,
				navigator.gpu.getPreferredCanvasFormat()
			);
			ui = createUiResources( created, navigator.gpu.getPreferredCanvasFormat() );

			thunder = createThunder( created, navigator.gpu.getPreferredCanvasFormat() );

			flares = createFlares( created, navigator.gpu.getPreferredCanvasFormat(), images.texture );

			bloom = createBloom( created, navigator.gpu.getPreferredCanvasFormat() );

			particleQuery = createParticleQuery( created, navigator.gpu.getPreferredCanvasFormat() );
			Promise.all( [
				pipelines.ready,
				ui.ready,
				flares.ready,
				thunder.ready,
				geometry.ready,
				particleQuery.ready,
				bloom.ready
			] ).then( () => {
				if ( generation === epoch && phase === "starting" ) {
					sky = {
						pipeline: pipelines.sky(),
						binding: created.createBindGroup( {
							layout: pipelines.sky().getBindGroupLayout( 0 ),
							entries: [ { binding: 0, resource: { buffer: environmentBuffer! } } ]
						} )
					};
					phase = "running";
					geometry!.textureOptions( textureFiltered, textureDetail );
				}
			} ).catch( fail );
		} ).catch( fail );
	}
	return {
		/*
		================
		bloom

		Prepare postprocessing targets only after device initialization succeeds.
		================
		*/
		bloom( width, height, enabled ) {
			if ( phase !== "running" || !bloom ) throw Error( "Bloom device is not ready" );
			return bloom.prepare( width, height, enabled );
		},
		/*
		================
		particleQuery

		Run occlusion queries through the current device generation.
		================
		*/
		particleQuery( points, matrix, color, depth ) {
			if ( phase !== "running" || !particleQuery ) throw Error( "Stale particle query capability" );
			return particleQuery.query( points, matrix, color, depth );
		},

		/*
		================
		textureOptions

		Retain sampling preferences across startup and apply them to ready geometry.
		================
		*/
		textureOptions( filtered, detail ) {
			textureFiltered = filtered;
			textureDetail = detail;
			if ( phase === "running" ) geometry?.textureOptions( filtered, detail );
		},

		/*
		================
		portraitTarget

		Resolve portrait targets through the UI resource owner.
		================
		*/
		portraitTarget( id?: string, width?: number, height?: number ) {
			if ( !ui ) throw Error( "UI device is not ready" );
			return ui.portraitTarget( id, width, height );
		},
		/*
		================
		uiTexture

		Transfer a UI image to the UI texture owner.
		================
		*/
		uiTexture: ( id, image ) => ui?.texture( id, image ),
		/*
		================
		ui

		Prepare UI draws only when the device's UI owner exists.
		================
		*/
		ui: scene => ui?.prepare( scene ) ?? [],
		/*
		================
		sky

		Expose the current sky draw without transferring its resource ownership.
		================
		*/
		sky: () => sky,
		/*
		================
		thunder

		Prepare the fullscreen thunder pass through its device-owned resources.
		================
		*/
		thunder( color ) {
			if ( phase !== "running" || !thunder ) throw Error( "Stale thunder capability" );
			return thunder.prepare( color );
		},
		/*
		================
		flares

		Prepare flare draws against the supplied depth target.
		================
		*/
		flares( input, depth ) {
			if ( phase !== "running" || !flares ) throw new Error( "Stale flare capability" );
			return flares.prepare( input, depth );
		},

		/*
		================
		worldView

		Publish the frame transform and environment uniforms together.
		================
		*/
		worldView( transform, environment ) {
			if ( phase === "running" ) {
				geometry?.worldView( transform );
				device!.queue.writeBuffer(
					environmentBuffer!,
					0,
					environment.buffer as ArrayBuffer,
					environment.byteOffset,
					environment.byteLength
				);
			}
		},
		/*
		================
		geometry

		Grant geometry commands only after the geometry owner exists.
		================
		*/
		geometry: () => geometry?.commands ?? null,
		/*
		================
		images

		Grant image commands only after the image owner exists.
		================
		*/
		images: () => images?.commands ?? null,
		/*
		================
		recoverable

		Report whether the last device failure permits a new generation.
		================
		*/
		recoverable: () => phase === "failed" && recoverable,
		/*
		================
		phase

		Expose the explicit device lifecycle state.
		================
		*/
		phase: () => phase,
		/*
		================
		error

		Expose the last recorded device failure.
		================
		*/
		error: () => failure,
		/*
		================
		gpuTiming

		Read completed GPU measurements without taking ownership of pending queries.
		================
		*/
		gpuTiming: () => timing?.stats() ?? null,
		/*
		================
		commands

		Expose frame commands for the current initialized generation.
		================
		*/
		commands: () => commands,
		/*
		================
		surfaceCommands

		Expose allocation and presentation commands through the surface owner.
		================
		*/
		surfaceCommands: () => surface,
		/*
		================
		format

		Use the browser's preferred canvas format consistently across frame resources.
		================
		*/
		format: () => navigator.gpu.getPreferredCanvasFormat(),
		/*
		================
		dispose

		Release the resources owned by this capability; repeated retirement is harmless.
		================
		*/
		dispose() {
			if ( phase === "disposed" ) {
				return;
			}
			epoch++;
			phase = "disposed";
			for ( const texture of depthTextures ) {
				texture.destroy();
			}
			depthTextures.clear();
			environmentBuffer?.destroy();
			environmentBuffer = null;
			sky = null;
			ui?.dispose();
			ui = null;
			thunder?.dispose();
			thunder = null;
			flares?.dispose();
			flares = null;
			bloom?.dispose();
			bloom = null;
			particleQuery?.dispose();
			particleQuery = null;
			geometry?.dispose();
			geometry = null;
			images?.dispose();
			images = null;
			commands = null;
			surface = null;
			timing?.dispose();
			timing = null;
			device?.destroy();
			device = null;
		}
	};
}
