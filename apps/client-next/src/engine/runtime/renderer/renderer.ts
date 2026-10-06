/*
===========================================================================

renderer.ts - GPU resource lifetime and ordered scene, character and UI passes

===========================================================================
*/
import { pickVolumeDepth } from "@/engine/foundation/rendering/pick-volume";
import { defaultVideoOptions, videoOptions, backgroundDrawDistance } from "@/engine/foundation/rendering/video-options";
import { experimentalOptions, experimentalVideo } from "@/engine/foundation/ui/experimental-options";
import { uiTextureResidency } from "@/engine/foundation/rendering/ui-texture-residency";
import { cameraBasis } from "@/engine/foundation/rendering/world-math";
import { createPortrait } from "./characters/portrait";
import { projectCharacterLabels } from "@/engine/foundation/ui/character-labels";
import { damageTextAssets, damageTextQuads } from "@/engine/foundation/ui/damage-text";
import { pickDestination } from "@/engine/foundation/rendering/pick-destination";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import { readPickAlpha } from "./readback/readback";
import { pickRay, pickRayProjector } from "@/engine/foundation/rendering/picking";
import { viewProjection } from "@/engine/foundation/rendering/world-math";
import { prepareUi, createUiPreparation } from "@/engine/foundation/ui/ui";
import { createCharacters } from "./characters/characters";
import { createWorldRenderer } from "./world/world";
import { copyGeometry } from "@/engine/foundation/rendering/geometry";
import type { Geometry } from "@/engine/contracts/geometry";
import { createDevice } from "./device/device";
import { createSurface } from "./surface/surface";
import { createFrame } from "./frame/frame";
import { createStaleDrawGuard } from "./frame/stale-draws";
import type { Renderer } from "@/engine/contracts/runtime";
import type { SurfaceOwner, FrameOwner, ImageDraw, GeometryDraw } from "./internal/gpu-contract";
import { hypot3 } from "@/engine/foundation/math/hypot";
const INVENTORY_DOLL_WIDTH = 176;
const INVENTORY_DOLL_HEIGHT = 318;

/*
================
createRenderer
================
*/
export function createRenderer(
	canvas: HTMLCanvasElement,
	random?: PresentationRandom,
	sound?: ( event: import("@/engine/contracts/audio").SoundEvent ) => void,
	diagnostics: import("@/engine/contracts/runtime").RuntimeDiagnostics = {}
): Renderer {
	let video = defaultVideoOptions();
	// Experimental > Video, all off (native) until the saved preference arrives.
	let experimental = experimentalVideo( experimentalOptions() );
	const portrait = createPortrait( createCharacters() );
	// A released draw never reaches a submit; the report names who kept it.
	const staleDraws = createStaleDrawGuard(
		draw => device.geometry()?.releasedDraw?.( draw ),
		stale =>
			console.error(
				`[SRO renderer] stale draw: list "${stale.list}" still held a released draw ` +
					`(index ${stale.index} of ${stale.listLength}, released ${stale.releasedMsAgo} ms ago); ` +
					"dropped it instead of submitting destroyed storage. Released by:\n" + stale.releaseStack
			)
	);
	let portraitDepth: import("./internal/gpu-contract").DepthTarget | null = null;
	const partyPortraits = Array.from(
		{ length: 7 },
		() => createPortrait( createCharacters() )
	);
	const doll = createPortrait( createCharacters() );
	let dollWidth = 0, dollHeight = 0;
	let dollDepth: import("./internal/gpu-contract").DepthTarget | null = null;
	const uiPreparation = createUiPreparation();
	let uiProduct: ReturnType<typeof prepareUi> | null = null;
	const uiTextures = new Map<string, ImageBitmap | ImageData>(), dirtyUi = new Set<string>();
	let residentUi = new Set<string>(), residentUiProduct: ReturnType<typeof prepareUi> | null = null;
	// The damage text a scene's world annotations show (setDamageText), drawn
	// each frame; its glyphs stay resident while such a scene is.
	let damageRows: readonly import("@/engine/contracts/damage-text").DamageText[] = [];
	const damageTextures = damageTextAssets();
	const world = createWorldRenderer( undefined, readPickAlpha, random, sound ),
		characters = createCharacters( random );
	let device = createDevice( diagnostics.gpuTiming, diagnostics.gpuAnimation !== false ), recoveries = 0;
	let surface: SurfaceOwner | null = null, frame: FrameOwner | null = null;
	let transformDirty = false, instancesDirty = false;
	let mesh: Geometry | null = null, meshDraw: GeometryDraw | null = null;
	let source: ImageBitmap | null = null, draw: ImageDraw | null = null;
	let presentationCamera: import("@/engine/foundation/animation/entity-lod").LodPoint | null = null;
	let soundListener: import("@/engine/contracts/audio").SoundListener | null = null;
	let pickView: Float32Array | null = null, pickOrigin = 0, pickWidth = 1, pickHeight = 1;
	let preview: import("@/engine/contracts/scene").WorldCamera | null = null;
	let gates: readonly import("@/engine/contracts/world").EntityState[] = [];
	let disposed = false, failure: string | null = null;
	let readbackWait = 0;
	return {
		/*
		================
		setFrameWork
		================
		*/
		setFrameWork( value ) {
			world.frameWork( value );
			characters.frameWork( value );
		},
		/*
		================
		readbackWaitMs

		Only the pending visibility query is wait time. Deferred preparation
		and the second submission remain CPU work in the runtime's budget.
		================
		*/
		readbackWaitMs() {
			return readbackWait;
		},
		/*
		================
		setTeleportGates
		================
		*/
		setTeleportGates( entities ) {
			gates = entities.filter( e => e.kind === "teleport" ).map( e => ({
				...e,
				teleport: e.teleport ? { ...e.teleport } : undefined
			}) );
		},
		scenery: world.scenery,
		/*
		================
		videoOptions
		================
		*/
		videoOptions( value ) {
			const next = videoOptions( value ), before = video.records[video.active], after = next.records[next.active];
			video = next;
			if ( before[8] !== after[8] || before[9] !== after[9] ) {
				device.textureOptions( after[8] === 1, after[9]! );
				frame = null;
			}
		},
		/*
		================
		experimentalVideo
		================
		*/
		experimentalVideo( value ) {
			experimental = value;
			device.experimentalVideo( value );
			frame = null;
		},
		setFootprints: world.footprints,
		setSelectionDecal: world.selectionDecal,
		/*
		================
		pickGround
		================
		*/
		pickGround( x, y ) {
			if ( disposed || failure || !pickView || device.phase() !== "running" || !pickOrigin ) return null;
			const raw = pickRay( pickView, x, y );
			if ( !raw ) return null;
			const length = hypot3( raw.delta[0]!, raw.delta[1]!, raw.delta[2]! );
			if ( !length ) return null;
			const ray = { start: raw.start, delta: raw.delta.map( v => v / length * 1000 ) };
			return { originRegion: pickOrigin, ray, terrainDepth: world.pickGround( ray ) };
		},
		/*
		================
		pickDestination
		================
		*/
		pickDestination( x, y ) {
			if ( disposed || failure || !pickView || device.phase() !== "running" ) return null;
			const ray = pickRay( pickView, x, y );
			if ( !ray ) return null;
			const depth = world.pick( ray, 1, true );
			return depth === null ? null : pickDestination( ray, depth, pickOrigin );
		},
		/*
		================
		pickFrontendCharacter
		================
		*/
		pickFrontendCharacter( x, y, ids ) {
			if ( disposed || failure || !pickView || device.phase() !== "running" ) return null;
			const ray = pickRay( pickView, x, y );
			return ray ? characters.pickFrontend( ray, ids ) : null;
		},
		frontendRaceCenters: world.interfaceCenters,
		/*
		================
		pickFrontendRace
		================
		*/
		pickFrontendRace( x, y ) {
			const ray = pickView ? pickRay( pickView, x, y ) : null;
			return ray ? world.pickInterface( ray ) : null;
		},
		/*
		================
		setCharacterPreview
		================
		*/
		setCharacterPreview( camera ) {
			preview = camera ? structuredClone( camera ) : null;
		},
		/*
		================
		pickEntity
		================
		*/
		pickEntity( x, y, excluded, blindHeld = false ) {
			if ( disposed || failure || !pickView || device.phase() !== "running" ) return null;
			const project = pickRayProjector( pickView );
			if ( !project ) return null;
			const rays = [];
			for ( let row = 0; row < 3; row++ ) {
				for ( let col = 0; col < 3; col++ ) {
					const ray = project(
						Math.trunc( x * pickWidth + (col - 1) * pickWidth * .016 ) / pickWidth,
						Math.trunc( y * pickHeight + (row - 1) * pickHeight * .016 ) / pickHeight
					);
					if ( !ray ) return null;
					rays.push( ray );
				}
			}
			let hit = characters.pick( rays, excluded, blindHeld ),
				best = hit ?
					hit.depth * hypot3( rays[hit.ray]!.delta[0]!, rays[hit.ray]!.delta[1]!, rays[hit.ray]!.delta[2]! ) :
					Infinity;
			// CITeleportGate 8764C0: translation-only box, independent of map meshes.
			for ( const gate of gates ) {
				const b = gate.teleport;
				if ( !b ) throw Error( "Missing gate pick bounds" );
				const matrix = new Float32Array( [
					1,
					0,
					0,
					0,
					0,
					1,
					0,
					0,
					0,
					0,
					1,
					0,
					gate.x + ((gate.regionId & 255) - (pickOrigin & 255)) * 1920,
					gate.y,
					gate.z + ((gate.regionId >>> 8) - (pickOrigin >>> 8)) * 1920,
					1
				] );
				for ( let r = 0; r < rays.length; r++ ) {
					const depth = pickVolumeDepth( rays[r]!, [
						-b.radius,
						-b.height,
						-b.radius,
						b.radius,
						b.height,
						b.radius
					], matrix );
					if ( depth === null ) continue;
					const distance = depth * hypot3( rays[r]!.delta[0]!, rays[r]!.delta[1]!, rays[r]!.delta[2]! );
					if ( distance < best || (hit?.ray !== 4 && r === 4) ) {
						hit = { gid: gate.gid, depth, ray: r };
						best = distance;
					}
				}
			}
			// 692680 returns the winning entity virtual pick directly. Map scenery
			// is not a second veto (698740 consumes that same winner).
			return hit?.gid ?? null;
		},
		setUi: scene => {
			if ( disposed ) throw new Error( "Renderer disposed" );
			uiProduct = uiPreparation.prepare( scene );
		},
		/*
		================
		setDamageText
		================
		*/
		setDamageText( rows ) {
			damageRows = rows;
		},
		/*
		================
		setUiTexture
		================
		*/
		setUiTexture( id, image ) {
			const old = uiTextures.get( id );
			if ( old !== image && old instanceof ImageBitmap ) old.close();
			if ( image ) uiTextures.set( id, image );
			else uiTextures.delete( id );
			dirtyUi.add( id );
		},
		characterParticleSnapshot: characters.particleSnapshot,
		characterParticleTime: characters.particleTime,
		presentationNight: () => world.night(),
		characterLocalMatrix: characters.localMatrix,
		characterMatrix: characters.matrix,
		characterSocket: characters.socket,
		retainCharacterModels: characters.retain,
		setCharacterAssembly: characters.assembly,
		characterStats: characters.stats,
		presentationCamera: () => presentationCamera,
		audioListener: () => soundListener,
		setWeather: value => world.weather( value ),
		setWorldClock: value => world.clock( value ),
		setCharacterModel: characters.model,
		setCharacterAnimation: characters.animation,
		setCharacterActors: characters.actors,
		characterActors: characters.currentActors,
		cancelWorldUpdate: () => world.cancelPending(),
		setWorld: scene => world.scene( scene ),
		adoptWorld: ( lease, detail, terrain ) => world.adopt( lease, detail, terrain ),
		setWorldCamera: camera => world.camera( camera ),
		setWorldTexture: ( path, image, alpha ) => world.texture( path, image, alpha ),
		neededWorldTextures: () => world.neededTextures(),
		worldStats: () => world.stats(),
		/*
		================
		setGeometryInstances
		================
		*/
		setGeometryInstances( instances ) {
			if ( disposed || !mesh ) throw new Error( "No owned geometry" );
			if (
				!(instances instanceof Float32Array) || instances.length % 16 || instances.length > 4096 * 16 ||
				!instances.every( Number.isFinite )
			) throw new Error( "Invalid instance matrices" );
			if (
				mesh.instances?.length === instances.length &&
				mesh.instances.every( ( value, index ) => value === instances[index] )
			) return;
			mesh = { ...mesh, instances: instances.slice() };
			instancesDirty = true;
		},
		/*
		================
		setGeometryTransform
		================
		*/
		setGeometryTransform( transform ) {
			if ( disposed || !mesh ) throw new Error( "No owned geometry" );
			if (
				!(transform instanceof Float32Array) || transform.length !== 16 || !transform.every( Number.isFinite )
			) throw new Error( "Invalid geometry transform" );
			for ( let i = 0; i < 16; i++ ) {
				if ( mesh.transform[i] !== transform[i] ) {
					mesh.transform.set( transform );
					transformDirty = true;
					break;
				}
			}
		},
		/*
		================
		setGeometry
		================
		*/
		setGeometry( data ) {
			if ( disposed ) throw new Error( "Renderer disposed" );
			const replacement = data ? copyGeometry( data ) : null;
			if ( meshDraw ) device.geometry()?.release( meshDraw );
			meshDraw = null;
			transformDirty = false;
			instancesDirty = false;
			mesh = replacement;
		},
		/*
		================
		setImage
		================
		*/
		setImage( image ) {
			if ( disposed ) {
				image?.close();
				throw new Error( "Renderer disposed" );
			}
			if ( image === source ) return;
			if ( draw ) device.images()?.release( draw );
			draw = null;
			source?.close();
			source = image;
		},
		gpuTiming: () => ({
			enabled: diagnostics.gpuTiming === true,
			...(device.gpuTiming() ?? { supported: false, skipped: 0, failed: 0, samples: [] })
		}),
		phase: () => disposed ? "disposed" : failure ? "failed" : device.phase(),
		error: () => failure ?? device.error(),
		/*
		================
		frame
		================
		*/
		frame( viewport, timeSeconds = 0, frameId, probe ) {
			readbackWait = 0;
			probe?.renderBegin();
			characters.profile( probe );
			world.profile( probe );
			if ( disposed || failure ) {
				return;
			}
			// The device whose frame is open. Everything below prepares, records and
			// submits inside that bracket, so a resource an owner releases on the way
			// outlives the command buffers that name it (device/retirement.ts).
			let open: ReturnType<typeof createDevice> | null = null;
			try {
				if ( device.phase() === "failed" && device.recoverable() && recoveries < 3 ) {
					// Only renderer state restarts. The runtime's simulation and assets remain owned and live.
					portraitDepth?.dispose();
					portraitDepth = null;
					portrait.invalidate();
					for ( const child of partyPortraits ) child.invalidate();
					dollDepth?.dispose();
					dollDepth = null;
					doll.invalidate();
					surface?.dispose();
					surface = null;
					frame = null;
					draw = null;
					meshDraw = null;
					characters.invalidate();
					world.invalidate();
					device.dispose();
					recoveries++;
					residentUi.clear();
					residentUiProduct = null;
					device = createDevice( diagnostics.gpuTiming, diagnostics.gpuAnimation !== false );
					device.textureOptions( video.records[video.active][8] === 1, video.records[video.active][9]! );
					device.experimentalVideo( experimental );
					for ( const id of uiTextures.keys() ) dirtyUi.add( id );
				}
				if ( device.phase() !== "running" ) {
					return;
				}
				open = device;
				open.beginFrame();
				if ( !surface ) {
					surface = createSurface( canvas, device.surfaceCommands()!, device.format() );
					frame = createFrame( device.commands()! );
				}
				if ( !frame ) frame = createFrame( device.commands()! );
				if ( residentUiProduct !== uiProduct || dirtyUi.size ) {
					const demand = uiTextureResidency(
						uiProduct?.scene ?? null,
						uiTextures,
						residentUi,
						dirtyUi,
						uiProduct?.scene.damageText ? damageTextures : []
					);
					// Release first so window replacement cannot transiently
					// exceed the device budget. CPU bitmaps stay warm for reopen.
					for ( const id of demand.release ) device.uiTexture( id, null );
					for ( const id of demand.upload ) device.uiTexture( id, uiTextures.get( id )! );
					residentUi = demand.needed;
					residentUiProduct = uiProduct;
					dirtyUi.clear();
				}
				if ( source && !draw ) draw = device.images()!.upload( source );
				if ( mesh && !meshDraw ) {
					meshDraw = device.geometry()!.upload( mesh );
					transformDirty = false;
					instancesDirty = false;
				} else if ( mesh && meshDraw && transformDirty ) {
					device.geometry()!.updateTransform( meshDraw, mesh.transform );
					transformDirty = false;
				}
				if ( mesh?.instances && meshDraw && instancesDirty ) {
					meshDraw = device.geometry()!.updateInstances( meshDraw, mesh.instances );
					instancesDirty = false;
				}
				probe?.renderMark( "renderer-setup" );
				const scene = world.prepare(
					device.geometry()!,
					device.images()!,
					viewport.width / Math.max( 1, viewport.height ),
					timeSeconds,
					viewport.width,
					viewport.height,
					backgroundDrawDistance( video ),
					!preview && video.records[video.active][4] === 1,
					video.records[video.active][12] === 1
				);
				scene.environment[83] = video.records[video.active][6] === 1 ? 1 : 0;
				device.worldView( scene.matrix, scene.environment );
				presentationCamera = {
					regionId: scene.originRegion,
					x: scene.camera.eye[0],
					y: scene.camera.eye[1],
					z: scene.camera.eye[2]
				};
				probe?.renderMark( "world-prepare" );
				pickView = scene.matrix;
				pickOrigin = scene.originRegion;
				pickWidth = viewport.width;
				pickHeight = viewport.height;
				const basis = cameraBasis( scene.camera );
				soundListener = {
					position: [
						scene.camera.eye[0] + (scene.originRegion & 255) * 1920,
						scene.camera.eye[1],
						scene.camera.eye[2] + (scene.originRegion >>> 8) * 1920
					],
					forward: basis.forward,
					up: basis.up
				};
				const characterDraws = characters.prepare(
					device.geometry()!,
					device.images()!,
					preview ? 0 : scene.originRegion,
					preview ? viewProjection( preview, viewport.width / Math.max( 1, viewport.height ) ) : scene.matrix,
					!!preview,
					timeSeconds,
					false,
					video.records[video.active][7] === 1,
					world.night(),
					video.records[video.active][12] === 1,
					scene.reflectionMatrix
				);
				const shadowDraws = world.characterShadows(
					preview ?
						[] :
						characters.shadowCandidates(
							characterDraws,
							scene.camera.eye,
							video.records[video.active][1]!
						),
					device.geometry()!,
					device.images()!
				);
				probe?.renderMark( "character-prepare" );
				const uiScene = uiProduct?.scene ?? null, anchored = uiProduct?.anchors;
				// Damage text rises and fades with the frame's clock, the same clock
				// the interface used when it drew it into its own product.
				const damage = uiScene?.damageText && damageRows.length && !preview ?
					damageTextQuads( damageRows, timeSeconds, uiScene.width, uiScene.height ) :
					[];
				// Anchors are UI scene pixels, like the world anchors projected
				// beside them. The GPU viewport is the backing store (CSS size
				// times devicePixelRatio); projecting into it put every name at
				// 1.25x its actor under 125% display scaling (BUG-043).
				const projectedUi = uiScene && (anchored?.size || uiProduct?.worldAnchors || damage.length) ?
					projectCharacterLabels(
						uiScene,
						preview || !anchored?.size ?
							new Map() :
							characters.labelAnchors(
								scene.originRegion,
								scene.matrix,
								uiScene.width,
								uiScene.height,
								anchored!
							),
						preview ? undefined : { origin: scene.originRegion, matrix: scene.matrix },
						damage
					) :
					uiScene;
				const portraitGid = uiProduct?.portraitGid;
				const portraitDraws = portrait.prepare(
					portraitGid === undefined ? null : characters.portraitSource( portraitGid ),
					device.geometry()!,
					device.images()!
				);
				const portraitTarget = portraitGid === undefined ? undefined : device.portraitTarget();
				if ( portraitTarget && !portraitDepth ) {
					portraitDepth = device.surfaceCommands()!.createDepth( 128, 128 );
				}
				const partyDraws = partyPortraits.map( ( child, i ) => {
					const gid = uiProduct?.portraits[i + 1],
						draws = child.prepare(
							gid === undefined ? null : characters.portraitSource( gid ),
							device.geometry()!,
							device.images()!
						);
					// Checked before empty slots drop, so a report names the real slot.
					return gid === undefined ?
						null :
						{
							target: device.portraitTarget( "__portrait" + (i + 1) ),
							depth: portraitDepth!.view,
							draws: staleDraws.live( "party-portrait-" + (i + 1), draws )
						};
				} ).filter( ( r ): r is NonNullable<typeof r> => r !== null );
				const dollInput = uiProduct?.doll;
				// A hidden doll stays borrowed and warm for the local character, so
				// the inventory's first open does not prepare a model (portrait.warm).
				if ( !dollInput && portraitGid !== undefined ) {
					doll.warm( characters.portraitSource( portraitGid ), device.geometry()!, device.images()! );
				}
				const dollRect = uiProduct?.scene.quads.find( quad => quad.doll )?.rect;
				const width = Math.max( 1, Math.ceil( dollRect?.[2] ?? INVENTORY_DOLL_WIDTH ) );
				const height = Math.max( 1, Math.ceil( dollRect?.[3] ?? INVENTORY_DOLL_HEIGHT ) );
				const dollDraws = dollInput ?
					doll.prepare(
						characters.portraitSource( dollInput.gid ),
						device.geometry()!,
						device.images()!,
						{ yaw: dollInput.yaw, seconds: timeSeconds, aspect: width / height }
					) :
					[];
				const dollTarget = dollInput ? device.portraitTarget( "__doll", width, height ) : undefined;
				if ( dollTarget && (!dollDepth || dollWidth !== width || dollHeight !== height) ) {
					dollDepth?.dispose();
					dollDepth = device.surfaceCommands()!.createDepth( width, height );
					dollWidth = width;
					dollHeight = height;
				}
				probe?.renderMark( "labels-portraits" );
				const deferredPlan = preview ?
						null :
						characters.deferredPlan(
							scene.originRegion,
							scene.camera.eye,
							video.records[video.active][7] === 1,
							world.night()
						),
					targetSurface = surface;
				const color = surface.acquire( viewport, experimental.postProcessing || !!deferredPlan?.query );
				const finishDeferred = ( results?: readonly boolean[] ) => {
					if ( disposed ) throw Error( "Renderer disposed during particle query" );
					characters.completeDeferred( results );
					// The continuation's draws reach the second submit: same rule.
					return staleDraws.live(
						"deferred-particles",
						characters.prepare(
							device.geometry()!,
							device.images()!,
							scene.originRegion,
							scene.matrix,
							false,
							timeSeconds,
							true
						).filter( draw => draw.deferredParticle )
					);
				};
				const deferredPass = deferredPlan ?
					{
						asynchronous: deferredPlan.query,
						prepare: () => {
							if ( !deferredPlan.query ) return finishDeferred();
							const query = device.particleQuery(
								deferredPlan.points,
								scene.matrix,
								color,
								targetSurface.depth()
							);
							const started = performance.now();
							return query.then( results => {
								readbackWait += performance.now() - started;
								return finishDeferred( results );
							} );
						}
					} :
					undefined;
				// Each owner's list is checked under its own name.
				// Slices keep the scene's terrain/transparent boundaries exact.
				const terrainDraws = staleDraws.live( "world-terrain", scene.draws.slice( 0, scene.terrainEnd ?? 0 ) ),
					opaqueDraws = staleDraws.live(
						"world-opaque",
						scene.draws.slice( scene.terrainEnd ?? 0, scene.transparentStart )
					),
					transparentDraws = staleDraws.live(
						"world-transparent",
						scene.draws.slice( scene.transparentStart )
					),
					liveCharacters = staleDraws.live( "characters", characterDraws ),
					liveShadows = staleDraws.live( "character-shadows", shadowDraws );
				const pending = frame!.draw(
					color,
					draw ?? (scene.sky ? device.sky() ?? undefined : undefined),
					staleDraws.single( "mesh", meshDraw ?? undefined ),
					surface.depth(),
					[
						...terrainDraws,
						...staleDraws.live( "ground-decals", scene.groundDecalDraws ?? [] ),
						...liveShadows,
						...opaqueDraws,
						...(preview ? [] : liveCharacters.filter( draw => !draw.blended )),
						...transparentDraws,
						...staleDraws.live( "decals", scene.decalDraws ?? [] ),
						...(preview ? [] : liveCharacters.filter( draw => draw.blended )),
						...(preview ? [] : staleDraws.live( "weather", scene.weatherDraws ?? [] ))
					],
					device.ui( projectedUi ),
					preview ? liveCharacters : [],
					scene.flares && video.records[video.active][10] === 1 ?
						device.flares( scene.flares, surface.depth() ) :
						undefined,
					scene.thunder ? device.thunder( scene.thunder ) : undefined,
					portraitTarget ?
						{
							target: portraitTarget,
							depth: portraitDepth!.view,
							draws: staleDraws.live( "portrait", portraitDraws )
						} :
						undefined,
					dollTarget ?
						{ target: dollTarget, depth: dollDepth!.view, draws: staleDraws.live( "doll", dollDraws ) } :
						undefined,
					partyDraws,
					frameId,
					deferredPass,
					device.bloom( viewport.width, viewport.height, !preview && video.records[video.active][11] === 1 ),
					device.geometry()!.waterReflection( {
						matrix: scene.reflectionMatrix,
						height: scene.waterHeight ?? 0,
						above: scene.camera.eye[1] >= (scene.waterHeight ?? 0),
						seconds: timeSeconds
					}, [ ...scene.draws, ...(preview ? [] : liveCharacters) ] )
				);
				probe?.renderMark( "submit" );
				if ( pending ) {
					// The deferred pass records its second command buffer after the
					// visibility query: the frame stays open until that one is submitted.
					const frameDevice = open;
					open = null;
					return pending.then( () => {
						if ( !disposed ) targetSurface.present();
					} ).catch( error => {
						if ( !disposed ) failure = String( error );
					} ).finally( () => frameDevice.endFrame() );
				}
				targetSurface.present();
			} catch ( error ) {
				failure = String( error );
			} finally {
				open?.endFrame();
			}
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
			portraitDepth?.dispose();
			portraitDepth = null;
			portrait.invalidate();
			for ( const child of partyPortraits ) child.invalidate();
			dollDepth?.dispose();
			dollDepth = null;
			doll.invalidate();
			surface?.dispose();
			surface = null;
			frame = null;
			for ( const image of uiTextures.values() ) if ( image instanceof ImageBitmap ) image.close();
			uiTextures.clear();
			dirtyUi.clear();
			uiProduct = null;
			uiPreparation.reset();
			source?.close();
			source = null;
			draw = null;
			mesh = null;
			meshDraw = null;
			portrait.dispose( device.geometry(), device.images() );
			for ( const child of partyPortraits ) child.dispose( device.geometry(), device.images() );
			doll.dispose( device.geometry(), device.images() );
			characters.dispose( device.geometry(), device.images() );
			world.dispose( device.geometry(), device.images() );
			device.dispose();
		}
	};
}
