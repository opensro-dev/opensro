/*
===========================================================================

device.ts - WebGPU generation and resource lifecycle

===========================================================================
*/
import {
	type GpuFacts,
	isGpuToken,
	reportedDegradedPipelines,
	reportedGpuFeatures
} from "@/engine/foundation/rendering/gpu-facts";
import { createBloom } from "./bloom";
import { createFinish } from "./finish";
import { createHdr } from "./hdr";
import { createParticleQuery } from "./particle-query";
import { createGpuAnimationResources } from "./animation";
import { createParticlePresentation } from "./particles";
import { createGpuTiming } from "./timing";
import { createThunder } from "./thunder";
import { createFlares } from "./flares";
import { createUiResources } from "./ui";
import { createPipelines } from "./pipelines";
import { createImages } from "./images";
import { createGeometryResources } from "./geometry";
import { ENVIRONMENT_STAGES2_OFFSET, ENVIRONMENT_STAGES_OFFSET, ENVIRONMENT_UNIFORM_BYTES } from "./environment-block";
import type { DeviceOwner, FrameCommands, SurfaceCommands } from "@/engine/runtime/renderer/internal/gpu-contract";
import { createRetirement } from "./retirement";
import type { RuntimePhase } from "@/engine/contracts/runtime";

const DEFAULT_TEXTURE_DETAIL = 2;
// The environment block's byte layout lives in environment-block.ts: the
// native 336 bytes, the sun-direction vec4, then the two experimental
// stages vec4s and the sun shadow cascade matrix.
const FULLSCREEN_VERTEX_COUNT = 6;
const OPTIONAL_PIPELINE_ERROR =
	/\[(ComputePipeline|ShaderModule|RenderPipeline) (particle-presentation|skeletal-animation|flare-visibility|flare-over|flare-add|flare-chain)\]/;

/*
================
gpuFacts

The facts of one adapter for bug reports (foundation/rendering/gpu-facts.ts).
Vendor and architecture are kept only when they are short lowercase tokens.
Diagnostics never fail a device: an adapter missing a field reports 0 or
nothing for it.
================
*/
function gpuFacts( adapter: GPUAdapter, device?: GPUDevice | null ): GpuFacts {
	const info = (adapter as GPUAdapter & { info?: { vendor?: string; architecture?: string; }; }).info ??
		(device as (GPUDevice & { adapterInfo?: { vendor?: string; architecture?: string; }; }) | null | undefined)
			?.adapterInfo;
	const token = ( value: string | undefined ) => value && isGpuToken( value ) ? value : undefined;
	const vendor = token( info?.vendor ), architecture = token( info?.architecture );
	const limits = adapter.limits as Partial<GPUSupportedLimits> | undefined;
	return {
		features: reportedGpuFeatures().filter( name => adapter.features?.has( name ) === true ),
		limits: {
			maxBufferSize: limits?.maxBufferSize ?? 0,
			maxStorageBufferBindingSize: limits?.maxStorageBufferBindingSize ?? 0,
			maxTextureDimension2D: limits?.maxTextureDimension2D ?? 0
		},
		...(vendor ? { vendor } : {}),
		...(architecture ? { architecture } : {})
	};
}

/*
================
createDevice

Initialize one device generation and grant checked capabilities after its pipelines are ready.
`recovering` marks a replacement for a lost device: the adapter can be
missing for a moment during a GPU reset, where on a first start it means
this machine has no WebGPU.
================
*/
export function createDevice( timingEnabled = false, gpuAnimationEnabled = true, recovering = false ): DeviceOwner {
	let textureFiltered = true, textureDetail = DEFAULT_TEXTURE_DETAIL;
	// Experimental > Image / World / Lighting. All off is the native frame:
	// the plain copy to the swapchain, retail samplers, env.stages zero and
	// env.stages2 zero.
	let finishEnabled = false, anisotropic = false, bloomFloat = false, hdrOn = false, sunShadowOn = false;
	const stages = new Float32Array( 8 );
	let timing: ReturnType<typeof createGpuTiming> | null = null;
	let phase: RuntimePhase = "starting", failure: string | null = null, device: GPUDevice | null = null;
	let environmentBuffer: GPUBuffer | null = null,
		sky: import("@/engine/runtime/renderer/internal/gpu-contract").ImageDraw | null = null,
		skyFloat: import("@/engine/runtime/renderer/internal/gpu-contract").ImageDraw | null = null,
		skyFactory:
			| (( sceneFloat: boolean ) => import("@/engine/runtime/renderer/internal/gpu-contract").ImageDraw)
			| null = null;
	let geometry: ReturnType<typeof createGeometryResources> | null = null,
		animation: ReturnType<typeof createGpuAnimationResources> | undefined,
		particles: ReturnType<typeof createParticlePresentation> | null = null,
		images: ReturnType<typeof createImages> | null = null;
	let ui: ReturnType<typeof createUiResources> | null = null;
	let thunder: ReturnType<typeof createThunder> | null = null;
	let flares: ReturnType<typeof createFlares> | null = null;
	let bloom: ReturnType<typeof createBloom> | null = null;
	let hdr: ReturnType<typeof createHdr> | null = null;
	let finish: ReturnType<typeof createFinish> | null = null;
	let particleQuery: ReturnType<typeof createParticleQuery> | null = null;
	const degradedPipelines = new Set<string>();
	const depthTextures = new Set<GPUTexture>();
	// Every owner below hands its released buffers and textures to this queue;
	// beginFrame/endFrame bracket the span in which a frame may still name them.
	const retirement = createRetirement();
	let epoch = 1, recoverable = false;
	let commands: FrameCommands | null = null, surface: SurfaceCommands | null = null;
	const generation = epoch;

	/*
	================
	fail

	Record a terminal error only for the generation that created this callback.
	================
	*/
	// The texture compression features this device samples, once it exists;
	// empty when it fails for good. A recoverable failure leaves them to the
	// renderer's replacement device (renderer.ts followFeatures).
	let texturesKnown: ( features: readonly string[] ) => void = () => {};
	const textureFeatures = new Promise<readonly string[]>( resolve => {
		texturesKnown = resolve;
	} );
	const fail = ( error: unknown, canRecover = false ) => {
		if ( !canRecover ) texturesKnown( [] );
		if ( generation !== epoch ) {
			return;
		}
		// Keep the first failure. WebGPU reports the cause (say, createTexture
		// rejecting a format or usage) and then every consequence ("createView:
		// texture is not valid"); the report must name the cause (BUG-022).
		if ( phase === "failed" ) return;
		recoverable = canRecover;
		failure = String( error );
		phase = "failed";
	};
	// The adapter's features and limits for bug reports (gpuFacts above).
	let facts: GpuFacts | null = null;
	// A rejected request for the device says the GPU is unavailable now, not
	// that it never will be (#2337: requestDevice rejects after a GPU reset).
	// Failures past that point are the device's own and stay permanent.
	let retryable = true;
	if ( !navigator.gpu ) {
		fail( "WebGPU is unavailable" );
	} else {
		navigator.gpu.requestAdapter().then( adapter => {
			if ( generation !== epoch ) {
				return null;
			}
			if ( !adapter ) {
				retryable = recovering;
				throw new Error( "No WebGPU adapter" );
			}
			facts = gpuFacts( adapter );
			const requiredFeatures: GPUFeatureName[] = [];
			if ( adapter.features.has( "texture-compression-bc" ) ) requiredFeatures.push( "texture-compression-bc" );
			// The port-only variant formats (texture-variants.ts) a device without
			// BC may be served: asked for whenever offered, used only when served.
			for ( const name of [ "texture-compression-etc2", "texture-compression-astc" ] as const ) {
				if ( adapter.features.has( name ) ) requiredFeatures.push( name );
			}
			if ( timingEnabled && adapter.features.has( "timestamp-query" ) ) {
				requiredFeatures.push( "timestamp-query" );
			}
			return adapter.requestDevice( { requiredFeatures } );
		} ).then( created => {
			retryable = false;
			if ( !created ) {
				return;
			}
			if ( generation !== epoch ) {
				created.destroy();
				return;
			}
			device = created;
			texturesKnown(
				[ "texture-compression-bc", "texture-compression-etc2", "texture-compression-astc" ].filter( name =>
					created.features?.has( name ) === true
				)
			);
			if ( timingEnabled && created.features.has( "timestamp-query" ) ) timing = createGpuTiming( created );
			created.lost.then( info => {
				// This owner destroys a device only from dispose(), which leaves
				// running first: a loss seen while running was the browser's, even
				// with reason "destroyed" (Chrome destroys a page's device after a
				// GPU-process or driver reset). The renderer recreates the device
				// (renderer.ts, at most three times) instead of ending the session.
				if ( phase === "running" || phase === "starting" ) {
					fail( `Device lost (${info.reason}): ${info.message}`, true );
				}
			} );
			created.addEventListener( "uncapturederror", event => {
				const message = event.error?.message || "Uncaptured WebGPU error";
				const match = OPTIONAL_PIPELINE_ERROR.exec( message );
				if ( match ) {
					const label = match[2]!;
					degradedPipelines.add( label.startsWith( "flare-" ) ? "flare-visibility" : label );
					return;
				}
				// Out of GPU memory is the one uncaptured error a new device can
				// cure: it starts empty. Validation errors would only repeat.
				fail(
					message,
					typeof GPUOutOfMemoryError !== "undefined" && event.error instanceof GPUOutOfMemoryError
				);
			} );

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
				createBundleEncoder: ( depth = true, scene = false ) =>
					current().createRenderBundleEncoder( {
						label: "retained-scene",
						// Scene bundles execute into the frame's scene target: the
						// canvas format, or the HDR stage's float intermediate.
						colorFormats: [ scene && hdrOn ? "rgba16float" : navigator.gpu.getPreferredCanvasFormat() ],
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
				submit( buffer: GPUCommandBuffer ) {
					current();
					geometry!.uploads.submit( buffer );
				}
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
						usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC |
							GPUTextureUsage.TEXTURE_BINDING
					} );
					depthTextures.add( texture );
					return Object.freeze( {
						view: texture.createView(),

						/*
						================
						encodePresent

						Append presentation after all existing frame passes, keeping
						the finish shader or native copy in the final frame submission.
						================
						*/
						encodePresent( encoder: GPUCommandEncoder, target: GPUTexture ) {
							if ( !depthTextures.has( texture ) ) throw Error( "Disposed frame color" );
							if ( finishEnabled && finish ) {
								finish.encode( encoder, texture, target );
								return;
							}
							encoder.copyTextureToTexture( { texture }, { texture: target }, [ width, height ] );
						},

						/*
						================
						dispose

						Release the resources owned by this capability; repeated retirement is harmless.
						================
						*/
						dispose() {
							if ( depthTextures.delete( texture ) ) retirement.retire( texture );
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
								retirement.retire( texture );
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
				retire: retirement.retire,
				current,
				fail,
				// The fullscreen image draws in the main pass, so its pipeline
				// follows the scene format the HDR stage selects.
				pipeline: () => pipelines.image( hdrOn ),
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
				label: "environment",
				size: ENVIRONMENT_UNIFORM_BYTES,
				usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
			} );
			animation = gpuAnimationEnabled ? createGpuAnimationResources( created, retirement.retire ) : undefined;
			particles = createParticlePresentation( created, retirement.retire );
			geometry = createGeometryResources(
				created,
				current,
				fail,
				pipelines.geometry,
				images.leases,
				pipelines.worldSampler,
				pipelines.lightmapSampler,
				environmentBuffer,
				pipelines.worldSampling,
				animation,
				navigator.gpu.getPreferredCanvasFormat(),
				particles,
				retirement.retire,
				pipelines.lightmapSampling
			);
			ui = createUiResources( created, navigator.gpu.getPreferredCanvasFormat(), () => (
				hdrOn ? "rgba16float" : navigator.gpu.getPreferredCanvasFormat()
			), retirement.retire );

			thunder = createThunder( created, navigator.gpu.getPreferredCanvasFormat() );

			flares = createFlares( created, navigator.gpu.getPreferredCanvasFormat(), images.texture );

			bloom = createBloom( created, navigator.gpu.getPreferredCanvasFormat(), retirement.retire );

			hdr = createHdr( created, navigator.gpu.getPreferredCanvasFormat(), retirement.retire );

			finish = createFinish( created, navigator.gpu.getPreferredCanvasFormat() );

			particleQuery = createParticleQuery( created, navigator.gpu.getPreferredCanvasFormat() );
			/*
			================
			skyDraw

			The gradient sky draw for one scene format: native at startup, the
			HDR stage's float sibling lazily on first use.
			================
			*/
			const skyDraw = ( sceneFloat: boolean ) => {
				const pipeline = pipelines.sky( sceneFloat );
				return {
					pipeline,
					binding: created.createBindGroup( {
						layout: pipeline.getBindGroupLayout( 0 ),
						entries: [ { binding: 0, resource: { buffer: environmentBuffer! } } ]
					} )
				};
			};
			Promise.all( [
				pipelines.ready,
				ui.ready,
				flares.ready,
				thunder.ready,
				geometry.ready,
				particleQuery.ready,
				bloom.ready,
				finish.ready
			] ).then( () => {
				if ( generation === epoch && phase === "starting" ) {
					sky = skyDraw( false );
					skyFactory = skyDraw;
					phase = "running";
					geometry!.textureOptions( textureFiltered, textureDetail, anisotropic );
					geometry!.sceneBindings( hdrOn );
					geometry!.refreshShadowBindings( sunShadowOn );
				}
			} ).catch( fail );
		} ).catch( error => fail( error, retryable ) );
	}
	return {
		/*
		================
		beginFrame

		The renderer starts preparing a frame: from here until endFrame no
		retired buffer or texture is destroyed (retirement.ts).
		================
		*/
		beginFrame() {
			retirement.open();
			geometry?.beginFrame();
		},
		/*
		================
		endFrame

		The frame's last command buffer is submitted, or the frame was abandoned.
		================
		*/
		endFrame() {
			try {
				geometry?.endFrame();
			} finally {
				retirement.close();
			}
		},
		/*
		================
		bloom

		Prepare postprocessing targets only after device initialization succeeds.
		================
		*/
		bloom( width, height, enabled ) {
			if ( phase !== "running" || !bloom ) throw Error( "Bloom device is not ready" );
			return bloom.prepare( width, height, enabled, bloomFloat, hdrOn );
		},
		/*
		================
		hdr

		Prepare the float scene intermediate only after device
		initialization succeeds; undefined while the stage is off.
		================
		*/
		hdr( width, height, enabled ) {
			if ( phase !== "running" || !hdr ) throw Error( "HDR device is not ready" );
			return hdr.prepare( width, height, enabled && hdrOn );
		},
		/*
		================
		sunShadow

		The experimental cascade owner, created by the geometry resources on
		first use; the renderer prepares it per frame and encodes its
		caster pass through the frame owner.
		================
		*/
		sunShadow() {
			if ( phase !== "running" || !geometry ) throw Error( "Stale sun shadow capability" );
			return geometry.sunShadow();
		},
		/*
		================
		upscale

		The render scale's plain linear resolve, through the presentation
		owner's upscale entry.
		================
		*/
		upscale() {
			if ( phase !== "running" || !finish ) throw Error( "Upscale device is not ready" );
			return finish;
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
			if ( phase === "running" ) geometry?.textureOptions( filtered, detail, anisotropic );
		},

		/*
		================
		experimentalVideo

		Experimental > Image / World / Lighting: the presentation pass, the anisotropic
		samplers, the shader stages, the HDR scene format and the sun cascade.
		Retained across startup like textureOptions.
		================
		*/
		experimentalVideo( value ) {
			finishEnabled = value.postProcessing;
			bloomFloat = value.floatBloom;
			stages.set( [
				value.heightFog ? 1 : 0,
				value.dynamicSun ? 1 : 0,
				// z and w were terrain relief and textured horizon, removed at the
				// owner's request (2026-10-11); the shaders no longer read them.
				0,
				0,
				value.hdrToneMap ? 1 : 0,
				value.sunShadow ? 1 : 0,
				value.perPixelLighting ? 1 : 0,
				0
			] );
			// The HDR stage flips every scene target's format: geometry
			// re-pipelines, the mirror reformats, the portraits retire.
			if ( hdrOn !== value.hdrToneMap && phase === "running" ) {
				hdrOn = value.hdrToneMap;
				skyFloat = null;
				geometry?.sceneBindings( hdrOn );
				ui?.releasePortraits();
			} else hdrOn = value.hdrToneMap;
			// The cascade toggling only changes bindings 13/14's view.
			const shadowWasOn = sunShadowOn;
			sunShadowOn = value.sunShadow;
			if ( shadowWasOn !== sunShadowOn && phase === "running" ) geometry?.refreshShadowBindings( sunShadowOn );
			if ( anisotropic === value.anisotropicFiltering ) return;
			anisotropic = value.anisotropicFiltering;
			if ( phase === "running" ) geometry?.textureOptions( textureFiltered, textureDetail, anisotropic );
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
		readPortrait

		Read a portrait target back through the UI resource owner (the 3D
		viewer's still).
		================
		*/
		readPortrait( id: string ) {
			if ( !ui ) throw Error( "UI device is not ready" );
			return ui.readPortrait( id );
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
		uiAtlas

		Port-only, not native: install or clear the icon atlas mapping; the
		UI packer remaps the icons it maps into their sheet.
		================
		*/
		uiAtlas: ( mapping: import("@/engine/foundation/ui/icon-atlas").IconAtlas | null ) => ui?.atlas( mapping ),
		/*
		================
		ui

		Prepare UI draws only when the device's UI owner exists.
		================
		*/
		ui: ( scene, targetWidth ) => ui?.prepare( scene, targetWidth ) ?? [],
		/*
		================
		sky

		Expose the current sky draw without transferring its resource ownership.
		================
		*/
		sky: () => {
			if ( !hdrOn || !skyFactory ) return sky;
			skyFloat ??= skyFactory( true );
			return skyFloat;
		},
		/*
		================
		thunder

		Prepare the fullscreen thunder pass through its device-owned resources.
		================
		*/
		thunder( color ) {
			if ( phase !== "running" || !thunder ) throw Error( "Stale thunder capability" );
			return thunder.prepare( color, hdrOn );
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
		worldView( transform, environment, shadowEnabled = true, directional = false ) {
			if ( phase === "running" ) {
				// Preview scenes have no cascade; keep the saved preference for world re-entry.
				stages[5] = sunShadowOn && shadowEnabled ? 1 : 0;
				geometry?.worldView( transform );
				device!.queue.writeBuffer(
					environmentBuffer!,
					0,
					environment.buffer as ArrayBuffer,
					environment.byteOffset,
					environment.byteLength
				);
				// The viewer's studio light uses the sun-direction stage's switch for
				// its own frames only; the saved Experimental choice is kept.
				const dynamicSun = stages[1]!;
				if ( directional ) stages[1] = 1;
				device!.queue.writeBuffer( environmentBuffer!, ENVIRONMENT_STAGES_OFFSET, stages );
				stages[1] = dynamicSun;
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
		textureFeatures: () => textureFeatures,
		/*
		================
		adapterFacts
		================
		*/
		adapterFacts: () => {
			if ( !facts ) return null;
			const degraded = reportedDegradedPipelines().filter(
				name =>
					degradedPipelines.has( name ) ||
					(name === "skeletal-animation" && animation?.stats().failure !== null &&
						animation?.stats().failure !== undefined) ||
					(name === "particle-presentation" && particles?.failed() === true) ||
					(name === "flare-visibility" && flares?.failed() === true)
			);
			return degraded.length ? { ...facts, degradedPipelines: degraded } : facts;
		},
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
			hdr?.dispose();
			hdr = null;
			finish?.dispose();
			finish = null;
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
			// The owners above retired into the queue if a frame was still open.
			retirement.close();
			device?.destroy();
			device = null;
		}
	};
}
