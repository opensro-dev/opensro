/*
===========================================================================

world.ts - the world renderer: scene residency, selection and preparation

Owns world scene admission and GPU residency, terrain and object visibility,
fades and animation, camera collision, picking and the per-frame prepared
world. Diagnostics observe it only through the frame probe (profile).

===========================================================================
*/
import { characterShadowReceiver, BLOB_SHADOW_TEXTURE } from "@/engine/foundation/rendering/character-shadow";
import { footprintGeometry, footprintTextures } from "@/engine/foundation/rendering/footprints";
import { createMaterialTimeline } from "@/engine/foundation/rendering/material-timeline";
import { createTextureAtlas } from "@/engine/foundation/rendering/texture-atlas";
import { createTerrainVisibility } from "@/engine/foundation/rendering/terrain-visibility";
import {
	terrainInteractionCells,
	pickTerrainCells,
	selectionDecalGeometry,
	selectionTextures
} from "@/engine/foundation/rendering/terrain-interaction";
import { createWeather } from "../weather/weather";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import type { WorldSceneLease } from "@/engine/contracts/world-admission";
import { skyGroups } from "@/engine/foundation/rendering/sky-geometry";
import { flareUniforms } from "@/engine/foundation/rendering/flares";
import {
	pickGeometry,
	occludesGeometry,
	geometryPickBounds,
	geometryPickBlocks,
	rayIntersectsBounds,
	type PickBounds,
	type PickRay,
	type PickAlpha
} from "@/engine/foundation/rendering/picking";
import {
	cameraCollisionParts,
	prepareCameraCollisionParts,
	refitAnimatedCameraParts,
	resolveFollowCamera,
	followCameraQuery,
	animatedCameraCandidates
} from "@/engine/foundation/rendering/follow-camera";
import { initialStarFlicker } from "@/engine/foundation/rendering/star-flicker";
import { advanceObjectFade } from "@/engine/foundation/rendering/object-visibility";
import type { ObjectFade } from "@/engine/foundation/rendering/object-visibility";
import { createCharacterPose } from "@/engine/foundation/animation/animation-pose";
import {
	worldEnvironment,
	environmentTarget,
	environmentTime,
	advanceEnvironment
} from "@/engine/foundation/rendering/world-environment";
import {
	FRONTEND_SCENE_BYTES,
	FRONTEND_RESIDENCY_BYTES,
	worldSceneBytes,
	copyWorldScene,
	WORLD_RESIDENCY_BYTES,
	WORLD_SCENE_BYTES
} from "@/engine/foundation/rendering/world-scene";
import type { WorldScene, WorldCamera, WorldRenderStats, WorldGroup, TerrainRange } from "@/engine/contracts/scene";
import { createTextureMotion } from "@/engine/foundation/rendering/texture-motion";
import type {
	GeometryCommands,
	GeometryDraw,
	ImageCommands,
	ImageDraw,
	PreparedWorld
} from "@/engine/runtime/renderer/internal/gpu-contract";
import {
	viewProjection,
	prepareViewFrustum,
	visibleFrustumSphere,
	visibleFrustumBox,
	terrainLod
} from "@/engine/foundation/rendering/world-math";
import { validPickAlpha } from "@/engine/foundation/rendering/pick-alpha";
/*
================
drawPhase

Finish the ground before depth-writing objects blend against it. Asset names
cannot order these passes: even a zero-alpha native object writes depth.
================
*/
function drawPhase( { material }: WorldGroup ): number {
	if ( material.sky ) return -1;
	if ( material.lightmap ) return 2;
	if ( material.terrain ) return material.blend ? 1 : 0;
	if ( material.objectFade ) return 3;
	return material.blend ? 4 : 3;
}

// followInput's slots: target xyz, yaw, pitch, distance, height, mounted,
// offset xy, collision distance.
const FOLLOW_INPUT_LENGTH = 11;

/*
================
imageDrawKey

The imageDraws key of an upload's ordered frame paths. Paths never contain
a newline, so a single path is its own key and a multi-frame key can never
equal one.
================
*/
function imageDrawKey( paths: readonly string[] ): string {
	return paths.length === 1 ? paths[0]! : paths.join( "\n" );
}

/*
================
followInput

Writes the inputs the follow-camera collision resolve depends on into out
and reports whether they differ from what out held. An absent optional is
NaN; NaN slots compare equal to NaN, so they never force a resolve.
Replaces a JSON key built every frame.
================
*/
function followInput(
	target: readonly [number, number, number],
	follow: NonNullable<WorldCamera["follow"]>,
	distance: number | null,
	out: Float64Array
): boolean {
	let changed = false;
	/*
	================
	write
	================
	*/
	function write( slot: number, value: number ) {
		const old = out[slot]!;
		if ( old !== value && !(old !== old && value !== value) ) changed = true;
		out[slot] = value;
	}
	write( 0, target[0] );
	write( 1, target[1] );
	write( 2, target[2] );
	write( 3, follow.yaw );
	write( 4, follow.pitch );
	write( 5, follow.distance );
	write( 6, follow.height ?? NaN );
	write( 7, follow.mounted === undefined ? NaN : follow.mounted ? 1 : 0 );
	write( 8, follow.offset?.[0] ?? NaN );
	write( 9, follow.offset?.[1] ?? NaN );
	write( 10, distance ?? NaN );
	return changed;
}

/*
================
objectFadeDistance

The eye-to-placement distance SWorld_AdvanceObjectFade (0x8C4C60) is fed,
computed as its caller does at 8B988F..8B98F0: each delta stored as a
float, ( dy*dy + dx*dx ) + dz*dz stored as a float, then CRT_sqrt stored
as a float. The client runs x87 at 53-bit precision, so doubles reproduce
the intermediate sums.
================
*/
function objectFadeDistance( eye: ArrayLike<number>, x: number, y: number, z: number ): number {
	const dx = Math.fround( eye[0]! - x ), dy = Math.fround( eye[1]! - y ), dz = Math.fround( eye[2]! - z );
	return Math.fround( Math.sqrt( Math.fround( dy * dy + dx * dx + dz * dz ) ) );
}

/*
================
createWorldRenderer
================
*/
export function createWorldRenderer(
	budget?: number,
	readAlpha?: ( image: ImageBitmap ) => PickAlpha,
	random?: PresentationRandom,
	sound?: ( event: import("@/engine/contracts/audio").SoundEvent ) => void
) {
	const weather = createWeather( random, sound );
	let interactionScene: WorldScene | null = null, interactionCells = terrainInteractionCells( null );
	let decalState:
		| { readonly pose: import("@/engine/contracts/gameplay").Pose; readonly slot: 0 | 1 | 2 | 3; }
		| null = null;
	let decalDraw: GeometryDraw | null = null, decalVertexCount = 0, decalSlot = -1;
	// The point the retained decal geometry was built for; decalValid false
	// forces a rebuild (new interaction cells, GPU loss).
	let decalValid = false, decalBuiltSlot = -1;
	const decalPoint: [number, number, number] = [ 0, 0, 0 ];
	let decalImage: ImageDraw | null = null;
	// Scene membership owns decal demand. indexPending refreshes it on admission,
	// cancellation, commit, device recovery and disposal; camera and texture
	// updates cannot change the admitted topology. No retired scenes are retained.
	let selectionDemand: readonly string[] = [];
	const selectionPaths = () => selectionDemand;
	let footprints: readonly import("@/engine/contracts/footprint").Footprint[] = [];
	const footprintDraws = new Map<number, { draw: GeometryDraw; scene: WorldScene; image: ImageDraw; }>();
	/*
	================
	prepareFootprints
	================
	*/
	function prepareFootprints( geometry: GeometryCommands, textures: ImageCommands, seconds: number ): GeometryDraw[] {
		const live = new Set( footprints.filter( p => seconds < p.started + 20 ).map( p => p.id ) );
		for ( const [id, row] of footprintDraws ) {
			if ( !live.has( id ) || row.scene !== current ) {
				geometry.release( row.draw );
				footprintDraws.delete( id );
			}
		}
		if ( !current || current.residency === "frontend" ) return [];
		const result: GeometryDraw[] = [];
		for ( const footprint of footprints ) {
			if ( !live.has( footprint.id ) ) continue;
			const { pose } = footprint;
			if ( ((pose.regionId | current.originRegion) & 0x8000) && pose.regionId !== current.originRegion ) continue;
			const path = footprintTextures()[footprint.surface === "SAND" ? 0 : 1], source = images.get( path )?.source;
			if ( !source ) continue;
			let image = imageDraws.get( path );
			if ( !image ) {
				image = textures.upload( source, [ source ], false );
				imageDraws.set( path, image );
			}
			let row = footprintDraws.get( footprint.id );
			if ( row?.image !== image ) {
				if ( row ) geometry.release( row.draw );
				const point: [number, number, number] = [
					pose.x + ((pose.regionId & 255) - (current.originRegion & 255)) * 1920,
					pose.y,
					pose.z + ((pose.regionId >>> 8) - (current.originRegion >>> 8)) * 1920
				];
				const data = footprintGeometry( interactionCells, point, footprint.yaw, footprint.right );
				if ( !data ) continue;
				row = { draw: geometry.upload( data, image ), scene: current, image };
				footprintDraws.set( footprint.id, row );
			}
			// 8ABFD0: integer alpha fades during the last 1020 milliseconds.
			const alpha = Math.floor( 255 * Math.min( 1, Math.max( 0, footprint.started + 20 - seconds ) / 1.02 ) ) /
				255;
			geometry.updateMaterialColors(
				row.draw,
				new Float32Array(
					[ ...(environmentState?.subarray( 9, 12 ) ?? [ 1, 1, 1 ]) ].map( v =>
						(Math.trunc( v * 255 ) & 255) / 255
					)
				),
				2
			);
			row.draw = geometry.updateInstances(
				row.draw,
				new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] ),
				new Float32Array( [ alpha ] )
			);
			result.push( row.draw );
		}
		return result;
	}
	/*
	================
	updateInteractionCells
	================
	*/
	function updateInteractionCells() {
		if ( interactionScene !== current ) {
			interactionScene = current;
			interactionCells = terrainInteractionCells( current );
			decalValid = false;
		}
	}
	/*
	================
	prepareDecal
	================
	*/
	function prepareDecal( geometry: GeometryCommands, textures: ImageCommands ): readonly GeometryDraw[] {
		updateInteractionCells();
		if ( !current || current.residency === "frontend" || !decalState ) return [];
		const { pose, slot } = decalState;
		if ( ((pose.regionId | current.originRegion) & 0x8000) && pose.regionId !== current.originRegion ) return [];
		const path = selectionTextures()[slot]!, source = images.get( path )?.source;
		if ( !source ) return [];
		const x = pose.x + ((pose.regionId & 255) - (current.originRegion & 255)) * 1920,
			y = pose.y,
			z = pose.z + ((pose.regionId >>> 8) - (current.originRegion >>> 8)) * 1920;
		if (
			decalValid && decalBuiltSlot === slot && decalPoint[0] === x && decalPoint[1] === y &&
			decalPoint[2] === z &&
			decalDraw && decalImage === imageDraws.get( path )
		) return [ decalDraw ];
		const data = selectionDecalGeometry( interactionCells, [ x, y, z ] );
		if ( !data ) return [];
		let image = imageDraws.get( path );
		if ( !image ) {
			image = textures.upload( source, [ source ], false );
			imageDraws.set( path, image );
		}
		if ( decalDraw && decalVertexCount === data.positions.length && decalSlot === slot && decalImage === image ) {
			geometry.updatePositions( decalDraw, data.positions, undefined, data.uvs );
		} else {
			if ( decalDraw ) geometry.release( decalDraw );
			decalDraw = geometry.upload( { ...data, dynamicVertices: true }, image );
			decalVertexCount = data.positions.length;
			decalSlot = slot;
		}
		decalImage = image;
		decalValid = true;
		decalBuiltSlot = slot;
		decalPoint[0] = x;
		decalPoint[1] = y;
		decalPoint[2] = z;
		return [ decalDraw ];
	}

	let environmentState: Float32Array | null = null, environmentSeconds: number | null = null;
	let weatherOptions: import("@/engine/foundation/gameplay/weather").WeatherOptions | null = null;
	let stars = initialStarFlicker(), starTimerStarted = false;
	let startupStars: WorldGroup | undefined;
	let clock: { timeOfDay: number; lunarDay: number; } | null = null;
	let hasStars = false;
	let pickGroups: WorldGroup[] = [], pickSeconds = 0;
	const alphaMasks = new WeakMap<ImageBitmap, PickAlpha>();
	// Immutable admitted positions own these bounds; replacement storage invalidates them.
	const pickBounds = new WeakMap<Float32Array, PickBounds>();
	const pickBlocks = new WeakMap<WorldGroup, Float64Array>();
	const terrainVisibility = new WeakMap<WorldScene, ReturnType<typeof createTerrainVisibility>>();
	let collisionScene: WorldScene | null = null,
		collisionParts: ReturnType<typeof cameraCollisionParts> = [],
		collisionDistance: number | null = null;
	let collisionCamera: WorldCamera | null = null;
	// The inputs the retained collisionCamera was resolved for (followInput).
	const collisionInput = new Float64Array( FOLLOW_INPUT_LENGTH );
	let collisionInputValid = false;
	let current: WorldScene | null = null,
		pending: WorldScene | null = null,
		camera: WorldCamera = {
			eye: [ 960, 500, 1400 ],
			target: [ 960, 0, 960 ],
			fov: Math.PI / 3,
			near: 1,
			far: 3500
		};
	const images = new Map<string, { source: import("@/engine/contracts/texture").WorldTexture; }>(),
		draws = new Map<WorldGroup, GeometryDraw>();
	// Decoded pixels are shared by path; GPU arrays are shared by their ordered
	// layers, keyed by imageDrawKey. A single path is its own key, so the
	// per-frame lookups (footprints, decal, flares, weather) allocate nothing.
	const imageDraws = new Map<string, ImageDraw>();
	/*
	================
	texturePaths
	================
	*/
	function texturePaths( group: WorldGroup ): readonly string[] {
		return group.material.frames ?? (group.material.texture ? [ group.material.texture ] : []);
	}
	// These indexes describe only the owned pending scene. Reads do not walk its
	// geometry; replacement, cancellation and GPU loss rebuild them atomically.
	const missingTextures = new Set<string>(), readyGroups = new Set<WorldGroup>();
	const textureWaiters = new Map<string, Set<WorldGroup>>(), groupWaits = new Map<WorldGroup, number>();
	let pendingGroupCount = 0;
	let collisionPreparation: ReturnType<typeof prepareCameraCollisionParts> | null = null,
		preparedCollision: ReturnType<typeof cameraCollisionParts> | null = null;
	/*
	================
	indexPending
	================
	*/
	function indexPending() {
		selectionDemand = [ current, pending ].some( scene =>
				scene && scene.residency !== "frontend" &&
				scene.groups.some( group => group.material.terrain && group.ranges?.length )
			) ?
			[ ...selectionTextures(), ...footprintTextures(), BLOB_SHADOW_TEXTURE ] :
			[];
		collisionPreparation = null;
		preparedCollision = null;
		missingTextures.clear();
		readyGroups.clear();
		textureWaiters.clear();
		groupWaits.clear();
		pendingGroupCount = 0;
		for ( const path of pending?.flareTextures ?? [] ) if ( !images.has( path ) ) missingTextures.add( path );
		for ( const group of pending?.groups ?? [] ) {
			if ( draws.has( group ) ) continue;
			pendingGroupCount++;
			const paths = new Set( texturePaths( group ).filter( path => !images.has( path ) ) );
			if ( !paths.size ) {
				readyGroups.add( group );
				continue;
			}
			groupWaits.set( group, paths.size );
			for ( const path of paths ) {
				missingTextures.add( path );
				let waiters = textureWaiters.get( path );
				if ( !waiters ) {
					waiters = new Set();
					textureWaiters.set( path, waiters );
				}
				waiters.add( group );
			}
		}
	}
	const selections = new Map<
		WorldGroup,
		{
			indices: Uint32Array;
			indexCount?: number;
			instanceCount?: number;
			instances?: Float32Array;
			opacity?: Float32Array;
			sourceInstances?: Int32Array;
			alphas?: Uint8Array;
			fadeRows?: FadeRow[];
			resident?: Uint8Array;
			targetCellX?: number;
			targetCellZ?: number;
			seams?: Map<number, { mask: number; cellX: number; cellZ: number; }>;
			candidates?: readonly TerrainRange[];
			candidateVolumes?: Uint32Array;
			previousCandidates?: {
				cellX: number;
				cellZ: number;
				ranges: readonly TerrainRange[];
				volumes: Uint32Array;
			};
			chosen?: readonly TerrainRange[];
			cellX?: number;
			cellZ?: number;
		}
	>();
	// These are derived from owned scene metadata. Only blended depth order is
	// camera-dependent; native material-set order remains fixed until replacement.
	let orderedGroups: WorldGroup[] = [], objectOrder = new Map<string, number>();
	// Optional measurements from the frame owner (runtime.ts frameProbe). The
	// profiler never rewrites this source; it observes these explicit hooks.
	let probe: import("@/engine/contracts/runtime").RenderFrameProbe | undefined;
	// Shadow receivers by terrain cell. The visible terrain ranges change only
	// when a group's selection does, so the map is rebuilt on that change, not
	// every frame (it was the largest self cost of the frame).
	type ShadowSurfaces = Map<string, import("@/engine/foundation/rendering/character-shadow").ShadowTerrainSurface[]>;
	// Receivers by shadow key, kept only while a shadow uses them this frame.
	const shadowReceivers = new Map<string, {
		readonly cells: ReturnType<typeof terrainInteractionCells>;
		readonly surfaces: ShadowSurfaces;
		readonly receiver: ReturnType<typeof characterShadowReceiver>;
	}>();
	let shadowSurfaces: ShadowSurfaces = new Map(), shadowSurfaceInputs: readonly unknown[] = [];
	/*
	================
	terrainShadowSurfaces
	================
	*/
	function terrainShadowSurfaces(): ShadowSurfaces {
		const inputs: unknown[] = [ orderedGroups ];
		for ( const group of orderedGroups ) {
			if ( group.material.terrain ) inputs.push( selections.get( group )?.chosen );
		}
		if (
			inputs.length === shadowSurfaceInputs.length &&
			inputs.every( ( input, i ) => input === shadowSurfaceInputs[i] )
		) return shadowSurfaces;
		const surfaces: ShadowSurfaces = new Map();
		for ( const group of orderedGroups ) {
			if ( group.material.terrain ) {
				for ( const range of selections.get( group )?.chosen ?? [] ) {
					const key = range.cell.join( ":" ), rows = surfaces.get( key ) ?? [];
					rows.push( {
						positions: group.geometry.positions,
						indices: group.geometry.indices,
						start: range.indexStart,
						count: range.indexCount
					} );
					surfaces.set( key, rows );
				}
			}
		}
		shadowSurfaces = surfaces;
		shadowSurfaceInputs = inputs;
		return surfaces;
	}
	let previousVisible = new Set<WorldGroup>(), previousTriangles = new Map<WorldGroup, number>();
	/*
	================
	compareGroups
	================
	*/
	function compareGroups( a: WorldGroup, b: WorldGroup ): number {
		const layerA = drawPhase( a ), layerB = drawPhase( b );
		if ( layerA !== layerB ) return layerA - layerB;
		if ( layerA === -1 ) return a.material.sky! - b.material.sky!;
		if ( layerA === 1 ) return (a.material.order ?? 0) - (b.material.order ?? 0) || a.id.localeCompare( b.id );
		if ( layerA === 3 ) {
			const set = objectOrder.get( a.materialOrder?.set ?? a.id )! -
				objectOrder.get( b.materialOrder?.set ?? b.id )!;
			if ( set ) return set;
			const index = (a.materialOrder?.index ?? 0) - (b.materialOrder?.index ?? 0);
			if ( index ) return index;
		}
		return layerA === 4 ? 0 : a.id.localeCompare( b.id );
	}
	type FadeRow = { value: ObjectFade; visited: number; alpha: number; };
	const fades = new Map<string, FadeRow>();
	let selectedFadeFrame = -1, fadeFrame = 0, fadeSeconds: number | null = null, fadeScene: WorldScene | null = null;
	let activeFades: FadeRow[] = [],
		retainedFadeFrame: number | null = null,
		fadesChanging = true,
		retainedTargetX = NaN,
		retainedTargetZ = NaN;
	const poses = new Map<
		string,
		{ pose: ReturnType<typeof createCharacterPose>; revision: number; seconds: number | null; }
	>();
	const animationKeys = new Map<WorldGroup, string>(), animationVersions = new Map<WorldGroup, number>();
	let animated: WorldGroup[] = [], activeAnimated: WorldGroup[] = [];
	const dirtyAnimation = new Set<WorldGroup>();
	let materialTimelines: {
		group: WorldGroup;
		clock: ReturnType<typeof createMaterialTimeline>;
		draw?: GeometryDraw;
	}[] = [];
	let textureMotions: { group: WorldGroup; motion: ReturnType<typeof createTextureMotion>; draw?: GeometryDraw; }[] =
		[];
	/*
	================
	resetAnimations
	================
	*/
	function resetAnimations( scene?: WorldScene, retainTextureMotion = false ) {
		if ( !retainTextureMotion ) {
			const old = new Map( materialTimelines.map( r => [ r.group.id, r ] ) );
			materialTimelines = scene?.groups.filter( g => g.material.colorTimeline ).map( group => ({
				group,
				clock: old.has( group.id ) &&
						JSON.stringify( old.get( group.id )!.group.material.colorTimeline ) ===
							JSON.stringify( group.material.colorTimeline ) ?
					old.get( group.id )!.clock :
					createMaterialTimeline( group.material.colorTimeline! )
			}) ) ?? [];
		}
		const previous = new Map( textureMotions.map( row => [ row.group.id, row ] ) );
		if ( !retainTextureMotion ) {
			textureMotions = scene?.groups.filter( g => (g.material.uvVelocity || g.material.uvAtlas) ).map( group => {
				const old = previous.get( group.id ),
					velocity = group.material.uvVelocity,
					atlas = group.material.uvAtlas;
				return {
					group,
					motion: old &&
							JSON.stringify( [ old.group.material.uvVelocity, old.group.material.uvAtlas ] ) ===
								JSON.stringify( [ velocity, atlas ] ) ?
						old.motion :
						atlas ?
						createTextureAtlas( atlas ) :
						createTextureMotion( velocity! )
				};
			} ) ?? [];
		}
		poses.clear();
		animationKeys.clear();
		animationVersions.clear();
		dirtyAnimation.clear();
		animated = scene?.groups.filter( g => g.animation ) ?? [];
		activeAnimated = [];
		for ( const group of animated ) {
			animationKeys.set( group, JSON.stringify( [ group.animation!.model, group.animation!.clip ] ) );
		}
	}
	/*
	================
	evaluateAnimations
	================
	*/
	function evaluateAnimations( seconds: number, groups: readonly WorldGroup[] = activeAnimated ) {
		const palettes = new Set<Float32Array>();
		for ( const group of groups ) {
			const a = group.animation!, key = animationKeys.get( group )!;
			let state = poses.get( key );
			if ( !state ) {
				state = { pose: createCharacterPose( current!.models![a.model]! ), revision: 0, seconds: null };
				poses.set( key, state );
			}
			if ( state.seconds !== seconds ) {
				if ( state.pose.evaluate( a.clip, Math.max( 0, seconds ) ) ) state.revision++;
				state.seconds = seconds;
			}
			if ( animationVersions.get( group ) !== state.revision ) {
				state.pose.palette( current!.models![a.model]!.primitives[a.primitive]!, group.geometry.bones! );
				animationVersions.set( group, state.revision );
				dirtyAnimation.add( group );
				palettes.add( group.geometry.bones! );
			}
		}
		return palettes;
	}
	/*
	================
	animate
	================
	*/
	function animate( geometry: GeometryCommands, seconds: number, force = false ) {
		for ( const row of materialTimelines ) {
			const changed = row.clock.step( seconds ), draw = draws.get( row.group );
			if ( draw && draw.instanceCount > 0 && (changed || force || row.draw !== draw) ) {
				geometry.updateMaterialColors( draw, row.clock.rgb, row.group.material.colorTimeline!.flags );
				row.draw = draw;
			}
		}
		for ( const row of textureMotions ) {
			const changed = row.motion.step( seconds ), draw = draws.get( row.group );
			if ( draw && draw.instanceCount > 0 && (changed || force || row.draw !== draw) ) {
				geometry.updateTextureTransform( draw, row.motion.matrix );
				row.draw = draw;
			}
		}
		evaluateAnimations( seconds );
		for ( const group of activeAnimated ) {
			if ( force || dirtyAnimation.has( group ) ) {
				geometry.updateBones( draws.get( group )!, group.geometry.bones! );
				dirtyAnimation.delete( group );
			}
		}
	}
	const retired: WorldScene[] = [];
	let lastView: Float32Array | null = null, lastEye: WorldCamera["eye"] | null = null;
	const sizes = new Map<WorldScene, number>();
	let imageBytes = 0, disposed = false;
	/*
	================
	capacity
	================
	*/
	function capacity( scene?: WorldScene | null ) {
		return budget ??
			(scene?.residency === "frontend" || current?.residency === "frontend" || pending?.residency === "frontend" ?
				FRONTEND_RESIDENCY_BYTES :
				WORLD_RESIDENCY_BYTES);
	}
	/*
	================
	retainedBytes
	================
	*/
	function retainedBytes() {
		let bytes = imageBytes;
		for ( const size of sizes.values() ) bytes += size;
		return bytes;
	}
	let terrainEnd = 0, transparentStart = 0;
	let selected: GeometryDraw[] = [], rebuilds = 0, triangles = 0;
	let prune = false;
	/*
	================
	collectUnused
	================
	*/
	function collectUnused( textures: ImageCommands ) {
		const groups = [ ...(current?.groups ?? []), ...(pending?.groups ?? []) ];
		const flarePaths = [
			...(current?.flareTextures ?? []),
			...(pending?.flareTextures ?? []),
			...weather.paths(),
			...selectionPaths()
		];
		const usedDraws = new Set( [
			...groups.map( group => imageDrawKey( texturePaths( group ) ) ),
			...flarePaths
		] );
		for ( const [key, draw] of imageDraws ) {
			if ( !usedDraws.has( key ) ) {
				textures.release( draw );
				imageDraws.delete( key );
			}
		}
		const used = new Set( [ ...groups.flatMap( group => [ ...texturePaths( group ) ] ), ...flarePaths ] );
		for ( const [path, image] of images ) {
			if ( !used.has( path ) ) {
				imageBytes -= image.source.width * image.source.height * 5;
				if ( !("kind" in image.source) ) image.source.close();
				images.delete( path );
			}
		}
		prune = false;
	}
	/*
	================
	release
	================
	*/
	function release( scene: WorldScene | null, geometry: GeometryCommands ) {
		for ( const group of scene?.groups ?? [] ) {
			const draw = draws.get( group );
			if ( draw ) geometry.release( draw );
			draws.delete( group );
			selections.delete( group );
		}
		if ( scene ) sizes.delete( scene );
	}
	/*
	================
	weatherGround
	================
	*/
	function weatherGround( x: number, y: number, z: number ) {
		const distance = pick( { start: [ x, y, z ], delta: [ 0, -1, 0 ] }, 1e6, true );
		return distance === null ? null : y - distance;
	}
	/*
	================
	pick
	================
	*/
	function pick( ray: PickRay, limit: number, excludeWater = false, terrainOnly = false, occlusionOnly = false ) {
		let nearest = limit;
		for ( const group of pickGroups ) {
			if ( terrainOnly && !group.material.terrain && !group.material.water ) continue;
			if (
				group.material.sky || group.material.lightmap || group.material.additive ||
				excludeWater && group.material.water
			) continue;
			const cache = selections.get( group ), draw = draws.get( group );
			if ( !draw ) continue;
			const source = group.geometry,
				indices = cache && group.ranges ? cache.indices.subarray( 0, cache.indexCount ?? 0 ) : source.indices;
			const instances = cache?.instances ?? source.instances!,
				count = cache?.instances ? cache.instanceCount ?? 0 : instances.length / 16;
			let bounds = pickBounds.get( source.positions );
			if ( !bounds && !source.bones ) {
				bounds = geometryPickBounds( source.positions );
				pickBounds.set( source.positions, bounds );
			}
			for ( let i = 0; i < count; i++ ) {
				const matrix = instances.subarray( i * 16, i * 16 + 16 );
				if ( bounds && !rayIntersectsBounds( ray, bounds, matrix, nearest ) ) continue;
				const paths = texturePaths( group ),
					path = paths.length ? paths[Math.floor( pickSeconds * 10 ) % paths.length] : undefined,
					image = path ? images.get( path )?.source : undefined;
				const bitmap = image && !("kind" in image) ? image : undefined;
				let alpha = bitmap ? alphaMasks.get( bitmap ) : undefined;
				if ( bitmap && !alpha ) {
					if ( !readAlpha ) throw new Error( "Missing picking readback capability" );
					alpha = readAlpha( bitmap );
					alphaMasks.set( bitmap, alpha );
				}
				const mesh = { ...source, indices, material: group.material },
					surface = {
						alpha,
						opacity: cache?.opacity?.[i] ?? 1,
						blocks: pickBlocks.get( group ),
						ranges: group.ranges ? cache?.chosen : undefined
					};
				if ( occlusionOnly ) {
					if ( occludesGeometry( ray, mesh, matrix, nearest, source.bones, surface ) ) return 0;
					continue;
				}
				const pickStarted = probe?.pickCensus ? performance.now() : 0;
				const depth = pickGeometry( ray, mesh, matrix, source.bones, surface );
				probe?.pickCensus?.( {
					group: group.id,
					ms: performance.now() - pickStarted,
					triangles: indices.length / 3,
					vertices: source.positions.length / 3,
					ranges: !!group.ranges,
					skinned: !!source.bones
				} );
				if ( depth !== null && depth < nearest ) nearest = depth;
			}
		}
		return nearest < limit ? nearest : null;
	}
	return {
		/*
		================
		profile
		================
		*/
		profile( value: import("@/engine/contracts/runtime").RenderFrameProbe | undefined ) {
			probe = value;
		},
		/*
		================
		night
		================
		*/
		night() {
			if ( !current ) return false;
			const time = environmentTime(
				current.environment,
				pickSeconds,
				current.residency === "frontend" ? null : clock
			);
			return time < .25 || time > .75;
		},
		characterShadows(
			candidates: readonly {
				projection: import("@/engine/foundation/rendering/character-shadow").ShadowProjection;
				blobSize?: number;
				parts: readonly { draw: GeometryDraw; instance: number; }[];
			}[],
			geometry: GeometryCommands,
			textures: ImageCommands
		) {
			updateInteractionCells();
			let image: ImageDraw | undefined;
			if ( candidates.some( c => c.blobSize !== undefined ) ) {
				const source = images.get( BLOB_SHADOW_TEXTURE )?.source;
				if ( source ) {
					image = imageDraws.get( BLOB_SHADOW_TEXTURE );
					if ( !image ) {
						image = textures.upload( source, [ source ], false );
						imageDraws.set( BLOB_SHADOW_TEXTURE, image );
					}
				}
			}
			const surfaces = terrainShadowSurfaces();
			// A receiver depends only on the shadow's point and size, the blob
			// size and the terrain. A still character reuses last frame's mesh.
			const used = new Set<string>();
			const requests = candidates.flatMap( c => {
				if ( c.blobSize !== undefined && !image ) return [];
				const point = c.projection.point,
					key = `${c.blobSize ?? ""}:${c.projection.size}:${point[0]}:${point[1]}:${point[2]}`;
				used.add( key );
				let cached = shadowReceivers.get( key );
				if ( !cached || cached.cells !== interactionCells || cached.surfaces !== surfaces ) {
					cached = {
						cells: interactionCells,
						surfaces,
						receiver: characterShadowReceiver( interactionCells, c.projection, c.blobSize, surfaces )
					};
					shadowReceivers.set( key, cached );
				}
				const receiver = cached.receiver;
				return receiver ?
					[ { matrix: c.projection.matrix, receiver, parts: c.parts, blob: c.blobSize !== undefined } ] :
					[];
			} );
			for ( const key of shadowReceivers.keys() ) if ( !used.has( key ) ) shadowReceivers.delete( key );
			return geometry.characterShadows?.( requests, image ) ?? [];
		},
		/*
		================
		scenery
		================
		*/
		scenery() {
			if ( !current?.scenery?.length ) return null;
			const time = environmentTime(
				current.environment,
				pickSeconds,
				current.residency === "frontend" ? null : clock
			);
			return {
				emitters: current.scenery.filter( e =>
					!fades.has( e.placement ) ||
					(fades.get( e.placement )!.visited === selectedFadeFrame && fades.get( e.placement )!.alpha > 0)
				),
				night: time < .25 || time > .75
			};
		},
		/*
		================
		pickInterface
		================
		*/
		pickInterface( ray: PickRay ) {
			for ( const id of [ 2, 3 ] ) {
				for ( const group of pickGroups ) {
					if ( !group.id.startsWith( "interface:" + id + ":" ) ) continue;
					const matrix = group.geometry.instances;
					if ( matrix && pickGeometry( ray, group.geometry, matrix ) !== null ) return id === 2 ? 0 : 1;
				}
			}
			return null;
		},
		/*
		================
		interfaceCenters
		================
		*/
		interfaceCenters() {
			return [ 2, 3 ].map( id => {
				const groups = pickGroups.filter( group => group.id.startsWith( "interface:" + id + ":" ) );
				if ( !groups.length ) return null;
				const low = [ Infinity, Infinity, Infinity ], high = [ -Infinity, -Infinity, -Infinity ];
				for ( const group of groups ) {
					const g = group.geometry, m = g.instances!;
					for ( let i = 0; i < g.positions.length; i += 3 ) {
						for ( let axis = 0; axis < 3; axis++ ) {
							const v = m[axis]! * g.positions[i]! + m[axis + 4]! * g.positions[i + 1]! +
								m[axis + 8]! * g.positions[i + 2]! + m[axis + 12]!;
							low[axis] = Math.min( low[axis]!, v );
							high[axis] = Math.max( high[axis]!, v );
						}
					}
				}
				return low.map( ( v, i ) => (v + high[i]!) / 2 ) as [number, number, number];
			} );
		},
		pick,
		/*
		================
		occludes
		================
		*/
		occludes( ray: PickRay, limit: number ) {
			return pick( ray, limit, false, false, true ) !== null;
		},
		/*
		================
		pickGround
		================
		*/
		pickGround( ray: PickRay ) {
			updateInteractionCells();
			return pickTerrainCells( interactionCells, ray );
		},
		/*
		================
		footprints
		================
		*/
		footprints( value: readonly import("@/engine/contracts/footprint").Footprint[] ) {
			footprints = value;
		},
		/*
		================
		selectionDecal
		================
		*/
		selectionDecal( value: typeof decalState ) {
			decalState = value ? { pose: { ...value.pose }, slot: value.slot } : null;
		},
		/*
		================
		weather
		================
		*/
		weather( value: import("@/engine/foundation/gameplay/weather").WeatherOptions | null ) {
			weatherOptions = value;
			weather.set( value );
		},
		/*
		================
		clock
		================
		*/
		clock( value: { timeOfDay: number; lunarDay: number; } | null ) {
			clock = value ? { ...value } : null;
		},
		/*
		================
		cancelPending
		================
		*/
		cancelPending() {
			if ( disposed ) return;
			if ( pending ) retired.push( pending );
			pending = null;
			indexPending();
			prune = true;
		},
		/*
		================
		adopt
		================
		*/
		adopt( lease: WorldSceneLease, detail?: WorldScene["terrainDetail"] ) {
			if ( disposed ) throw new Error( "World renderer disposed" );
			if ( detail !== undefined && detail !== "full" && detail !== "distance" ) {
				throw new Error( "Invalid terrain detail policy" );
			}
			const prepared = lease.takeWorld();
			let replacement = prepared.scene, size = prepared.bytes;
			if (
				!Number.isSafeInteger( size ) || size < 0 || !Number.isSafeInteger( prepared.starBytes ) ||
				prepared.starBytes < 0 || prepared.starBytes > size
			) throw new Error( "Invalid world admission receipt" );
			if ( replacement.groups.some( group => group.material.sky === 2 ) ) {
				if ( !random ) throw Error( "Missing shared presentation RNG" );
				startupStars ??= skyGroups( { starPrimitive: random.sky() } ).find( group =>
					group.material.sky === 2
				)!;
				// The application, not the asset worker, owns the shared star RNG. Only this
				// small generated group needs local admission; the transferred city does not.
				const stars: WorldScene = { id: "stars", originRegion: 0, warnings: [], groups: [ startupStars ] };
				size += worldSceneBytes( stars ) - prepared.starBytes;
				const ownedStars = copyWorldScene( stars ).groups[0]!;
				replacement = {
					...replacement,
					starRandomState: undefined,
					groups: replacement.groups.map( group => group.material.sky === 2 ? ownedStars : group )
				};
			}
			if ( detail !== undefined ) replacement = { ...replacement, terrainDetail: detail };
			if (
				size > (replacement.residency === "frontend" ? FRONTEND_SCENE_BYTES : WORLD_SCENE_BYTES) ||
				retainedBytes() + size > capacity( replacement )
			) {
				throw new Error(
					`World CPU residency budget exceeded (incoming ${size}, retained ${retainedBytes()}, images ${imageBytes}, capacity ${
						capacity( replacement )
					})`
				);
			}
			if ( pending ) retired.push( pending );
			sizes.set( replacement, size );
			pending = replacement;
			indexPending();
		},
		/*
		================
		scene
		================
		*/
		scene( scene: WorldScene | null ) {
			if ( !scene ) {
				environmentState = null;
				environmentSeconds = null;
				weatherOptions = null;
				weather.set( null );
			}
			if ( disposed ) throw new Error( "World renderer disposed" );
			if ( scene?.groups.some( group => group.material.sky === 2 ) ) {
				if ( !random ) throw Error( "Missing shared presentation RNG" );
				startupStars ??= skyGroups( { starPrimitive: random.sky() } ).find( group =>
					group.material.sky === 2
				)!;
				scene = {
					...scene,
					starRandomState: undefined,
					groups: scene.groups.map( group => group.material.sky === 2 ? startupStars! : group )
				};
			}
			const size = worldSceneBytes( scene );
			if (
				size > (scene?.residency === "frontend" ? FRONTEND_SCENE_BYTES : WORLD_SCENE_BYTES) ||
				retainedBytes() + size > capacity( scene )
			) throw new Error( "World CPU residency budget exceeded" );
			const replacement = scene ?
				copyWorldScene( scene ) :
				{ id: "empty", originRegion: 0, groups: [], warnings: [] };
			if ( pending ) retired.push( pending );
			sizes.set( replacement, size );
			pending = replacement;
			indexPending();
		},
		/*
		================
		camera
		================
		*/
		camera( value: WorldCamera ) {
			if ( value.dungeonBlock !== camera.dungeonBlock ) lastView = null;
			camera = {
				...value,
				follow: value.follow ? { ...value.follow } : undefined,
				eye: [ ...value.eye ],
				target: [ ...value.target ]
			};
		},
		/*
		================
		neededTextures
		================
		*/
		neededTextures() {
			return [
				...new Set( [
					...missingTextures,
					...[ ...weather.paths(), ...selectionPaths() ].filter( path => !images.has( path ) )
				] )
			];
		},
		texture(
			path: string,
			image: import("@/engine/contracts/texture").WorldTexture,
			alpha?: PickAlpha
		) {
			if ( disposed ) {
				if ( !("kind" in image) ) image.close();
				throw new Error( "World renderer disposed" );
			}
			if ( images.has( path ) ) {
				if ( !("kind" in image) ) image.close();
				return;
			}
			const bytes = image.width * image.height * 5;
			if ( !Number.isSafeInteger( bytes ) || bytes <= 0 || retainedBytes() + bytes > capacity() ) {
				if ( !("kind" in image) ) image.close();
				throw new Error( "World texture residency budget exceeded" );
			}
			imageBytes += bytes;
			images.set( path, { source: image } );
			// A mask decoded with the texture spares the main-thread readback.
			if ( !("kind" in image) && validPickAlpha( alpha, image.width, image.height ) ) {
				alphaMasks.set( image, alpha );
			}
			missingTextures.delete( path );
			for ( const group of textureWaiters.get( path ) ?? [] ) {
				const count = groupWaits.get( group )! - 1;
				if ( count ) groupWaits.set( group, count );
				else {
					groupWaits.delete( group );
					readyGroups.add( group );
				}
			}
			textureWaiters.delete( path );
		},
		prepare(
			geometry: GeometryCommands,
			textures: ImageCommands,
			aspect: number,
			seconds = 0,
			viewportWidth = 1,
			viewportHeight = 1,
			backgroundDistance?: number
		): PreparedWorld {
			probe?.worldBegin?.();
			for ( const scene of retired ) release( scene, geometry );
			retired.length = 0;
			let budget = 8;
			if ( pending ) {
				if ( camera.follow && !preparedCollision ) {
					collisionPreparation ??= prepareCameraCollisionParts( pending );
					// Prepare the exact collision product alongside GPU admission. Keep the
					// old scene until both are ready; never remove collision for a fast handoff.
					for ( let work = 0; work < 40; work++ ) {
						const result = collisionPreparation.next();
						if ( result.done ) {
							preparedCollision = result.value;
							collisionPreparation = null;
							break;
						}
					}
				}
				for ( const group of readyGroups ) {
					if ( !budget-- ) break;
					const paths = texturePaths( group );
					if ( group.ranges ) {
						let visibility = terrainVisibility.get( pending );
						if ( !visibility ) {
							visibility = createTerrainVisibility();
							terrainVisibility.set( pending, visibility );
						}
						for ( const range of group.ranges ) visibility.admit( range );
					}
					// Alpha readback belongs to bounded resource admission, not the first
					// hover over a resident object. Animated texture frames are admitted too.
					if (
						readAlpha && !group.material.sky && !group.material.lightmap && !group.material.additive
					) {
						for ( const path of paths ) {
							const bitmap = images.get( path )?.source;
							if ( bitmap && !("kind" in bitmap) && !alphaMasks.has( bitmap ) ) {
								alphaMasks.set( bitmap, readAlpha( bitmap ) );
							}
						}
					}
					const key = imageDrawKey( paths );
					let imageDraw = imageDraws.get( key );
					if ( paths.length && !imageDraw ) {
						const frames = paths.map( path => images.get( path )!.source );
						imageDraw = textures.upload( frames[0]!, frames );
						imageDraws.set( key, imageDraw );
					}
					// Build immutable pick bounds within the bounded upload work, so the first
					// nameplate hover cannot scan the entire scene's vertex buffers.
					if ( !group.geometry.bones && !pickBounds.has( group.geometry.positions ) ) {
						pickBounds.set( group.geometry.positions, geometryPickBounds( group.geometry.positions ) );
					}
					if ( !group.geometry.bones && !group.ranges && !pickBlocks.has( group ) ) {
						pickBlocks.set( group, geometryPickBlocks( group.geometry ) );
					}
					draws.set( group, geometry.upload( group.geometry, imageDraw ) );
					readyGroups.delete( group );
					pendingGroupCount--;
				}
				if ( pendingGroupCount === 0 && missingTextures.size === 0 && (!camera.follow || preparedCollision) ) {
					release( current, geometry );
					current = pending;
					pending = null;
					if ( preparedCollision ) {
						collisionScene = current;
						collisionParts = preparedCollision;
						collisionInputValid = false;
						collisionCamera = null;
						if ( !current.groups.length ) collisionDistance = null;
					}
					indexPending();
					if ( fadeScene !== current ) {
						fades.clear();
						activeFades.length = 0;
						retainedFadeFrame = null;
						fadesChanging = true;
						fadeScene = current;
						fadeSeconds = null;
					}
					resetAnimations( current );
					selected = [];
					lastView = null;
					const nextHasStars = current.groups.some( group => group.material.sky === 2 );
					// Asset upload completion starts the sky timer. Streaming the same sky and
					// rebuilding GPU resources and hiding the sky must not restart its random state.
					if ( nextHasStars && !starTimerStarted ) {
						stars = initialStarFlicker( 0, seconds * 1000 );
						starTimerStarted = true;
					}
					hasStars = nextHasStars;
					objectOrder = new Map();
					for ( const group of current.groups ) {
						const key = group.materialOrder?.set ?? group.id;
						if ( !objectOrder.has( key ) ) objectOrder.set( key, objectOrder.size );
					}
					orderedGroups = [ ...current.groups ].sort( compareGroups );
					previousVisible.clear();
					previousTriangles.clear();
					prune = true;
				}
			}
			const dx = camera.originRegion && current ?
					((camera.originRegion & 255) - (current.originRegion & 255)) * 1920 :
					0,
				dz = camera.originRegion && current ?
					((camera.originRegion >>> 8) - (current.originRegion >>> 8)) * 1920 :
					0;
			if ( prune ) collectUnused( textures );
			let localCamera: WorldCamera = {
				...camera,
				...(backgroundDistance !== undefined ? { far: backgroundDistance } : {}),
				eye: [ camera.eye[0] + dx, camera.eye[1], camera.eye[2] + dz ] as const,
				target: [ camera.target[0] + dx, camera.target[1], camera.target[2] + dz ] as const
			};
			if ( camera.follow && current ) {
				if ( collisionScene !== current ) {
					collisionScene = current;
					collisionParts = cameraCollisionParts( current );
					collisionInputValid = false;
					collisionCamera = null;
					if ( !current.groups.length ) collisionDistance = null;
				}
				const ray = followCameraQuery( localCamera, collisionDistance ).ray,
					candidates = animatedCameraCandidates( collisionParts, ray ),
					required = new Set( candidates.map( part => part.geometry.bones! ) );
				const changed = evaluateAnimations(
					seconds,
					animated.filter( group => required.has( group.geometry.bones! ) )
				);
				// A visible draw can update its palette after the previous camera query.
				// Refresh ray candidates against current palettes, including that case.
				refitAnimatedCameraParts( candidates );
				if ( changed.size ) collisionInputValid = false;
			}
			if ( camera.follow ) {
				if (
					followInput( localCamera.target, camera.follow, collisionDistance, collisionInput ) ||
					!collisionInputValid
				) {
					const result = resolveFollowCamera( localCamera, collisionParts, collisionDistance );
					collisionCamera = result.camera;
					collisionDistance = result.collision;
					// The recorded inputs carry the pre-resolve distance: a resolve that
					// moves it resolves once more next frame, as the string key did.
					collisionInputValid = true;
				}
				localCamera = { ...localCamera, eye: collisionCamera!.eye, target: collisionCamera!.target };
			} else {
				collisionScene = null;
				collisionParts = [];
				collisionDistance = null;
				collisionInputValid = false;
				collisionCamera = null;
			}
			probe?.worldMark?.( "world-camera" );
			const frameClock = current?.residency === "frontend" ? null : clock;
			environmentState = advanceEnvironment(
				current?.environment ? environmentState : null,
				environmentTarget(
					current?.environment,
					environmentTime( current?.environment, seconds, frameClock ),
					weatherOptions,
					backgroundDistance ?? 3500
				),
				environmentSeconds === null ? 0 : Math.max( 0, seconds - environmentSeconds )
			);
			environmentSeconds = seconds;
			const environment = worldEnvironment(
					current?.environment,
					localCamera,
					aspect,
					seconds,
					frameClock,
					weatherOptions,
					environmentState
				),
				sky = !!current?.environment;
			if ( !current?.environment ) {
				environmentState = null;
				environmentSeconds = null;
			}
			const dt = fadeSeconds === null ? 0 : Math.max( 0, seconds - fadeSeconds );
			fadeSeconds = seconds;
			fadeFrame = (fadeFrame + 1) >>> 0;
			environment[57] = 1 / Math.max( 1, viewportWidth );
			environment[58] = 1 / Math.max( 1, viewportHeight );
			// Native weather emission belongs to update; star trials belong to render.
			weather.update(
				localCamera,
				seconds,
				current?.originRegion ?? 0,
				!!current && current.residency !== "frontend" && !(current.originRegion & 0x8000),
				weatherGround,
				[ camera.target[0] + dx, camera.target[1], camera.target[2] + dz ]
			);
			if ( hasStars ) {
				stars = random!.flicker( stars, seconds * 1000, environment[51]! > 0 );
				environment.set( stars.bytes, 60 );
			}
			const flareFrame =
				!weatherOptions?.eventRain && (!weatherOptions || weatherOptions.mode === 1) && current?.flareTextures ?
					flareUniforms( localCamera, environment[48]!, viewportWidth, viewportHeight, dt ) :
					undefined;
			const flares = flareFrame?.[25] === 1 ?
				{
					uniforms: flareFrame,
					textures: current!.flareTextures!.map( path => {
						let draw = imageDraws.get( path );
						if ( !draw ) {
							draw = textures.upload( images.get( path )!.source );
							imageDraws.set( path, draw );
						}
						return draw;
					} )
				} :
				undefined;
			pickSeconds = seconds;
			const matrix = viewProjection( localCamera, aspect ),
				viewChanged = !lastView || !matrix.every( ( v, i ) => v === lastView![i] ) || !lastEye ||
					localCamera.eye.some( ( v, i ) => v !== lastEye![i] );
			lastEye = localCamera.eye;
			const weatherImages = new Map<string, ImageDraw>();
			for ( const path of weather.paths() ) {
				const image = images.get( path );
				if ( !image ) continue;
				let draw = imageDraws.get( path );
				if ( !draw ) {
					draw = textures.upload( image.source );
					imageDraws.set( path, draw );
				}
				weatherImages.set( path, draw );
			}
			const decalDraws = prepareDecal( geometry, textures ),
				groundDecalDraws = prepareFootprints( geometry, textures, seconds );
			const weatherDraws = weather.prepare( geometry, weatherImages, localCamera, matrix );
			probe?.worldMark?.( "world-environment" );
			const targetCellX = Math.floor( localCamera.target[0] / 320 ),
				targetCellZ = Math.floor( localCamera.target[2] / 320 );
			if (
				probe?.worldReplay?.( !!lastView ) ||
				(!viewChanged && !fadesChanging && targetCellX === retainedTargetX && targetCellZ === retainedTargetZ)
			) {
				// Every resident object would receive this frame stamp without changing
				// selection or opacity. Materialize those stamps only on the next walk.
				retainedFadeFrame = fadeFrame;
				animate( geometry, seconds );
				probe?.worldMark?.( "world-finalize" );
				return {
					camera: localCamera,
					terrainEnd,
					groundDecalDraws,
					decalDraws,
					thunder: weather.overlay(),
					weatherDraws,
					flares,
					matrix,
					draws: selected,
					transparentStart,
					environment,
					sky,
					originRegion: current?.originRegion ?? 0
				};
			}
			if ( retainedFadeFrame !== null ) {
				for ( const row of activeFades ) {
					row.value.lastFrame = retainedFadeFrame;
				}
			}
			// Only the stationary early-out defers frame stamps. An ordinary selection
			// walk has already committed them; do not replay those writes next frame.
			retainedFadeFrame = null;
			activeFades.length = 0;
			let changing = false;
			lastView = matrix;
			const sampleDetails = probe?.sampleDetails?.() ?? false;
			const frustum = prepareViewFrustum( matrix ), visible: WorldGroup[] = [];
			triangles = 0;
			const terrainVolumes = current ? terrainVisibility.get( current ) : undefined;
			const terrainMask = terrainVolumes?.begin( frustum );
			const eyeCellX = Math.floor( localCamera.eye[0] / 320 ), eyeCellZ = Math.floor( localCamera.eye[2] / 320 );
			const selectTerrainLod = ( distanceSquared: number ) =>
				current?.terrainDetail === "full" ? 0 : terrainLod( distanceSquared );
			for ( const group of current?.groups ?? [] ) {
				if ( group.dungeonBlock !== undefined && current?.dungeonVisibility ) {
					const visibleBlocks = camera.dungeonBlock === undefined ?
						undefined :
						current.dungeonVisibility[camera.dungeonBlock];
					if ( !visibleBlocks?.includes( group.dungeonBlock ) ) continue;
				}
				if ( group.material.sky ) {
					visible.push( group );
					continue;
				}
				if ( !viewChanged && !group.visibility ) {
					if ( previousVisible.has( group ) ) {
						visible.push( group );
						triangles += previousTriangles.get( group ) ?? 0;
					}
					continue;
				}
				const trianglesBefore = triangles;
				if ( group.ranges ) {
					let cache = selections.get( group );
					if ( !cache ) {
						cache = { indices: new Uint32Array( group.geometry.indices.length ), seams: new Map() };
						selections.set( group, cache );
					}
					if ( sampleDetails ) probe?.detailBegin?.( "terrain-candidates" );
					const cellChanged = cache.cellX !== eyeCellX || cache.cellZ !== eyeCellZ;
					if ( !cache.candidates || (cellChanged && current?.terrainDetail !== "full") ) {
						// Retain only two cell-dependent plans. Frustum selection below remains
						// live; geometry retirement owns both plans, including their volume slots.
						const previous = cache.previousCandidates;
						cache.previousCandidates = cache.candidates ?
							{
								cellX: cache.cellX!,
								cellZ: cache.cellZ!,
								ranges: cache.candidates,
								volumes: cache.candidateVolumes!
							} :
							undefined;
						if ( previous && previous.cellX === eyeCellX && previous.cellZ === eyeCellZ ) {
							cache.candidates = previous.ranges;
							cache.candidateVolumes = previous.volumes;
						} else {
							cache.candidates = group.ranges.filter( range => {
								const dx = range.cell[0] - eyeCellX, dz = range.cell[1] - eyeCellZ;
								return range.lod === selectTerrainLod( dx * dx + dz * dz );
							} );
							cache.candidateVolumes = terrainVolumes!.indices( cache.candidates );
						}
					}
					cache.cellX = eyeCellX;
					cache.cellZ = eyeCellZ;
					const chosen = cache.candidates.filter( ( range, i ) =>
						terrainMask![cache.candidateVolumes![i]!] === 1
					);
					if ( sampleDetails ) probe?.detailEnd?.( "terrain-candidates" );
					if ( sampleDetails ) probe?.detailBegin?.( "terrain-indices" );
					const indicesDirty = !cache.chosen || chosen.length !== cache.chosen.length ||
						chosen.some( ( range, i ) => range !== cache.chosen![i] );
					let count = 0;
					const positionRanges: [number, number][] = [];
					for ( const range of chosen ) {
						if ( indicesDirty ) {
							cache.indices.set(
								group.geometry.indices.subarray(
									range.indexStart,
									range.indexStart + range.indexCount
								),
								count
							);
						}
						count += range.indexCount;
					}
					if ( sampleDetails ) probe?.detailEnd?.( "terrain-indices" );
					if ( sampleDetails ) probe?.detailBegin?.( "terrain-seams" );
					for ( const range of chosen ) {
						// Installed vertices and current-cell validation have different lifetimes.
						// Hidden ranges keep their installed mask but must validate on re-entry.
						// Records are bounded by admitted ranges and retire with their geometry.
						const installed = cache.seams!.get( range.indexStart );
						if (
							installed &&
							(current?.terrainDetail === "full" ||
								installed.cellX === eyeCellX && installed.cellZ === eyeCellZ)
						) continue;
						const step = 1 << range.lod, [cx, cz] = range.cell, heights = range.heights;
						const neighbor = ( x: number, z: number ) =>
							selectTerrainLod( (x - eyeCellX) ** 2 + (z - eyeCellZ) ** 2 );
						const north = neighbor( cx, cz - 1 ) > range.lod,
							east = neighbor( cx + 1, cz ) > range.lod,
							south = neighbor( cx, cz + 1 ) > range.lod,
							west = neighbor( cx - 1, cz ) > range.lod;
						const seam = Number( north ) | (Number( east ) << 1) | (Number( south ) << 2) |
							(Number( west ) << 3);
						if ( installed ) {
							installed.cellX = eyeCellX;
							installed.cellZ = eyeCellZ;
							if ( installed.mask === seam ) continue;
							installed.mask = seam;
						} else cache.seams!.set( range.indexStart, { mask: seam, cellX: eyeCellX, cellZ: eyeCellZ } );
						let firstChanged = Infinity, lastChanged = -1;
						const seamVertices = range.seamVertices!;
						for ( let v = 0; v < seamVertices.length; v += 2 ) {
							const i = seamVertices[v]!,
								height = seamVertices[v + 1]!,
								p = group.geometry.positions,
								x = height % 17,
								z = Math.floor( height / 17 );
							let y = heights[height]!;
							if ( ((z === 0 && north) || (z === 16 && south)) && (x / step) % 2 === 1 ) {
								y = (heights[z * 17 + x - step]! + heights[z * 17 + x + step]!) /
									2;
							} else if ( ((x === 0 && west) || (x === 16 && east)) && (z / step) % 2 === 1 ) {
								y = (heights[(z - step) * 17 + x]! + heights[(z + step) * 17 + x]!) /
									2;
							}
							if ( p[i * 3 + 1] !== Math.fround( y ) ) {
								p[i * 3 + 1] = y;
								firstChanged = Math.min( firstChanged, i );
								lastChanged = i;
							}
						}
						if ( lastChanged >= 0 ) positionRanges.push( [ firstChanged, lastChanged - firstChanged + 1 ] );
					}
					if ( sampleDetails ) probe?.detailEnd?.( "terrain-seams" );
					cache.indexCount = count;
					if ( sampleDetails ) probe?.detailBegin?.( "terrain-index-upload" );
					if ( indicesDirty ) {
						geometry.updateIndices( draws.get( group )!, cache.indices.subarray( 0, count ) );
						cache.chosen = chosen;
					}
					if ( sampleDetails ) probe?.detailEnd?.( "terrain-index-upload" );
					if ( sampleDetails ) probe?.detailBegin?.( "terrain-position-upload" );
					if ( positionRanges.length ) {
						geometry.updatePositions(
							draws.get( group )!,
							group.geometry.positions,
							undefined,
							undefined,
							positionRanges
						);
					}
					if ( sampleDetails ) probe?.detailEnd?.( "terrain-position-upload" );
					if ( count ) {
						visible.push( group );
						triangles += count / 3;
					}
					previousTriangles.set( group, triangles - trianglesBefore );
					continue;
				}
				if ( group.instanceRadius !== undefined ) {
					if ( sampleDetails ) probe?.detailBegin?.( "world-instance-setup" );
					let cache = selections.get( group );
					if ( !cache ) {
						cache = {
							indices: new Uint32Array(),
							instances: new Float32Array( group.geometry.instances!.length ),
							sourceInstances: new Int32Array( group.geometry.instances!.length / 16 ).fill( -1 ),
							alphas: new Uint8Array( group.geometry.instances!.length / 16 )
						};
						selections.set( group, cache );
					}
					const source = group.geometry.instances!,
						bounds = group.geometry.bones ? undefined : pickBounds.get( group.geometry.positions );
					let count = 0, instancesDirty = false;
					// Native association membership depends on the target cell, not on each
					// sub-cell camera sample or animation clock. Fade state still ticks below.
					if (
						group.visibility &&
						(!cache.resident || cache.targetCellX !== targetCellX || cache.targetCellZ !== targetCellZ)
					) {
						cache.resident ??= new Uint8Array( source.length / 16 );
						for ( let slot = 0; slot < cache.resident.length; slot++ ) {
							const descriptor = group.visibility[slot];
							cache.resident[slot] = Number(
								!descriptor ||
									descriptor.cells.some( cell =>
										Math.abs( cell[0] - targetCellX ) <= descriptor.cellRadius &&
										Math.abs( cell[1] - targetCellZ ) <= descriptor.cellRadius
									)
							);
						}
						cache.targetCellX = targetCellX;
						cache.targetCellZ = targetCellZ;
					}
					if ( group.visibility && !cache.opacity ) cache.opacity = new Float32Array( source.length / 16 );
					// Resolve placement identity once for each admitted mesh. A building's
					// material pieces share one fade owner; the render loop uses direct slots.
					if ( group.visibility && !cache.fadeRows ) {
						cache.fadeRows = group.visibility.map( descriptor => {
							let row = fades.get( descriptor.id );
							if ( !row ) {
								row = { value: { state: 0, alpha: 0, lastFrame: 0 }, visited: -1, alpha: 0 };
								fades.set( descriptor.id, row );
							}
							return row;
						} );
					}
					if ( sampleDetails ) {
						probe?.detailEnd?.( "world-instance-setup" );
						probe?.detailBegin?.( "world-instance-loop" );
					}
					for ( let i = 0; i < source.length; i += 16 ) {
						const descriptor = group.visibility?.[i / 16];
						let alpha = 255, visible = false;
						if ( descriptor ) {
							if ( !cache.resident![i / 16] ) continue;
							const row = cache.fadeRows![i / 16]!;
							if ( row.visited !== fadeFrame ) {
								row.visited = fadeFrame;
								activeFades.push( row );
							}
							let state = row.value;
							if ( state.lastFrame !== fadeFrame ) {
								if (
									!viewChanged && state.lastFrame === ((fadeFrame - 1) >>> 0) &&
									(state.state === 0 || state.state === 2)
								) state.lastFrame = fadeFrame;
								else {
									const distance = objectFadeDistance(
										localCamera.eye,
										source[i + 12]!,
										source[i + 13]!,
										source[i + 14]!
									);
									state = advanceObjectFade(
										state,
										distance,
										descriptor.radius,
										descriptor.sceneryRange ?
											Math.min(
												2500,
												Math.fround( (backgroundDistance ?? 3500) * Math.fround( .8 ) )
											) - 480 :
											descriptor.range,
										dt,
										fadeFrame,
										state
									);
								}
								// Placement-owned publication, shared by every material piece. Native
								// 8B98FF..8B9932 likewise converts alpha after the object fade update.
								row.alpha = Math.max( 0, Math.min( 255, Math.trunc( state.alpha ) ) );
							}
							if ( state.state === 1 || state.state === 3 ) changing = true;
							// Range/fade admission is independent of camera visibility. Keep its
							// clock current offscreen, then reject draws (and draw-only animation)
							// using the decoder's conservative all-pose bound. Retail likewise
							// tests object bounds before submission: 8AA9C5..8AA9D8, A2D410.
							alpha = row.alpha;
							visible = state.state !== 0;
						} else visible = true;
						if ( visible ) {
							visible = bounds ?
								visibleFrustumBox( frustum, bounds, source, i ) :
								visibleFrustumSphere(
									frustum,
									source[i + 12]!,
									source[i + 13]!,
									source[i + 14]!,
									group.instanceRadius
								);
						}
						if ( visible ) {
							if ( cache.sourceInstances![count] !== i ) {
								cache.instances!.set( source.subarray( i, i + 16 ), count * 16 );
								cache.sourceInstances![count] = i;
								instancesDirty = true;
							}
							if ( cache.alphas![count] !== alpha ) {
								cache.alphas![count] = alpha;
								if ( cache.opacity ) cache.opacity[count] = alpha / 255;
								instancesDirty = true;
							}
							count++;
						}
					}
					if ( sampleDetails ) {
						probe?.detailEnd?.( "world-instance-loop" );
						probe?.detailBegin?.( "world-instance-upload" );
					}
					instancesDirty ||= count !== (cache.instanceCount ?? 0);
					cache.instanceCount = count;
					if ( instancesDirty ) {
						draws.set(
							group,
							geometry.updateInstances(
								draws.get( group )!,
								cache.instances!.subarray( 0, count * 16 ),
								cache.opacity?.subarray( 0, count )
							)
						);
					}
					if ( sampleDetails ) probe?.detailEnd?.( "world-instance-upload" );
					if ( count ) {
						visible.push( group );
						triangles += group.geometry.indices.length / 3 * count;
					}
					previousTriangles.set( group, triangles - trianglesBefore );
					continue;
				}
				if (
					visibleFrustumSphere( frustum, group.center[0], group.center[1], group.center[2], group.radius )
				) {
					visible.push( group );
					triangles += group.geometry.indices.length / 3 * (group.geometry.instances!.length / 16);
				}
				previousTriangles.set( group, triangles - trianglesBefore );
			}
			// CObjRenderer::QueueObject (0xAAE9C0) appends; 0xA5EE80 consumes
			// that order. Keep scene submission order between resource batches:
			// sorting filenames can put a fading prop before the solid structure behind it.
			selectedFadeFrame = fadeFrame;
			probe?.worldMark?.( "world-selection" );
			fadesChanging = changing;
			retainedTargetX = targetCellX;
			retainedTargetZ = targetCellZ;
			const visibleSet = new Set( visible );
			previousVisible = visibleSet;
			const ordered = orderedGroups.filter( group => visibleSet.has( group ) );
			terrainEnd = ordered.findIndex( group => drawPhase( group ) >= 3 );
			if ( terrainEnd < 0 ) terrainEnd = ordered.length;
			transparentStart = ordered.findIndex( group => drawPhase( group ) === 4 );
			if ( transparentStart < 0 ) transparentStart = ordered.length;
			const transparent = ordered.splice( transparentStart );
			// Far to near. Each group's distance is taken once, not per comparison.
			const depth = new Map<WorldGroup, number>();
			for ( const group of transparent ) {
				depth.set(
					group,
					Math.hypot(
						group.center[0] - localCamera.eye[0],
						group.center[1] - localCamera.eye[1],
						group.center[2] - localCamera.eye[2]
					)
				);
			}
			transparent.sort( ( a, b ) => depth.get( b )! - depth.get( a )! );
			ordered.push( ...transparent );
			pickGroups = ordered;
			// Upload gives every admitted group a unique resource handle. Its identity
			// already covers scene replacement and binding growth; rebuilding a string
			// of every asset name each frame adds no invalidation information.
			const nextDraws = ordered.map( group => draws.get( group )! );
			if (
				nextDraws.length !== selected.length || nextDraws.some( ( draw, index ) => draw !== selected[index] )
			) {
				selected = nextDraws;
				rebuilds++;
			}

			activeAnimated = animated.filter( g => visibleSet.has( g ) );
			animate( geometry, seconds );
			probe?.worldMark?.( "world-finalize" );
			return {
				camera: localCamera,
				terrainEnd,
				groundDecalDraws,
				decalDraws,
				thunder: weather.overlay(),
				weatherDraws,
				flares,
				matrix,
				draws: selected,
				transparentStart,
				environment,
				sky,
				originRegion: current?.originRegion ?? 0
			};
		},
		/*
		================
		invalidate
		================
		*/
		invalidate() {
			footprintDraws.clear();
			decalImage = null;
			decalDraw = null;
			decalValid = false;
			interactionScene = null;
			interactionCells.clear();
			weather.invalidate();
			orderedGroups = [];
			objectOrder.clear();
			previousVisible.clear();
			previousTriangles.clear();
			lastEye = null;
			pickGroups = [];
			resetAnimations( undefined, true );
			draws.clear();
			selections.clear();
			retired.length = 0;
			imageDraws.clear();
			pending = pending ?? current;
			const size = pending ? sizes.get( pending ) ?? 0 : 0;
			sizes.clear();
			if ( pending ) sizes.set( pending, size );
			current = null;
			selected = [];
			lastView = null;
			triangles = 0;
			indexPending();
		},
		/*
		================
		stats
		================
		*/
		stats(): WorldRenderStats {
			return {
				sceneId: current?.id ?? null,
				pendingGroups: Math.max( pendingGroupCount, collisionPreparation ? 1 : 0 ),
				residentGroups: draws.size,
				visibleGroups: selected.length,
				triangles,
				pendingTextures: missingTextures.size + selectionPaths().filter( path => !images.has( path ) ).length,
				bundleRebuilds: rebuilds
			};
		},
		/*
		================
		dispose
		================
		*/
		dispose( geometry: GeometryCommands | null, textures: ImageCommands | null ) {
			if ( disposed ) return;
			disposed = true;
			for ( const row of footprintDraws.values() ) geometry?.release( row.draw );
			footprintDraws.clear();
			footprints = [];
			if ( decalDraw ) geometry?.release( decalDraw );
			decalDraw = null;
			interactionCells.clear();
			interactionScene = null;
			decalState = null;
			weather.dispose( geometry );
			orderedGroups = [];
			objectOrder.clear();
			previousVisible.clear();
			previousTriangles.clear();
			lastEye = null;
			pickGroups = [];
			collisionScene = null;
			collisionParts = [];
			collisionCamera = null;
			fades.clear();
			activeFades.length = 0;
			fadeScene = null;
			resetAnimations();
			if ( geometry ) {
				release( current, geometry );
				release( pending, geometry );
				for ( const scene of retired ) release( scene, geometry );
				retired.length = 0;
			}
			if ( textures ) { for ( const draw of imageDraws.values() ) textures.release( draw ); }
			imageDraws.clear();
			for ( const image of images.values() ) if ( !("kind" in image.source) ) image.source.close();
			images.clear();
			imageBytes = 0;
			sizes.clear();
			retired.length = 0;
			draws.clear();
			selections.clear();
			current = pending = null;
			selected = [];
			indexPending();
		}
	};
}
