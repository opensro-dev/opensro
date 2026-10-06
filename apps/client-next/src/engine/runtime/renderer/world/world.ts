/*
===========================================================================

world.ts - the world renderer: scene residency, selection and preparation

Owns world scene admission and GPU residency, terrain and object visibility,
fades and animation, camera collision, picking and the per-frame prepared
world. Diagnostics observe it only through the frame probe (profile).

===========================================================================
*/
import { createClothVertices } from "@/engine/foundation/animation/cloth-vertices";
import { waterReflectionMatrix } from "@/engine/foundation/rendering/water-reflection";
import { blendAdds } from "@/engine/foundation/rendering/blend-state";
import {
	characterShadowReceiver,
	shadowReceiverBounds,
	BLOB_SHADOW_TEXTURE
} from "@/engine/foundation/rendering/character-shadow";
import { createShadowSurfaces } from "./shadow-surfaces";
import { createObjectFades } from "./object-fades";
import {
	compileWalkTable,
	fadeKeeps,
	residentSlots,
	FADE_KEEPS_IN,
	FADE_KEEPS_OUT,
	SLOT_SCENERY,
	SLOT_UNFADED,
	WALK_INSTANCED,
	WALK_SKY,
	WALK_TERRAIN
} from "./walk-table";
import { footprintGeometry, footprintTextures } from "@/engine/foundation/rendering/footprints";
import { createMaterialTimeline } from "@/engine/foundation/rendering/material-timeline";
import { createTextureFactorPulse } from "@/engine/foundation/rendering/texture-factor-pulse";
import { createTextureAtlas } from "@/engine/foundation/rendering/texture-atlas";
import { createWorldResidency } from "./residency";
import { createTerrainLayers, type TerrainLayer } from "./terrain-layers";
import {
	terrainInteractionCells,
	terrainCellKey,
	createTerrainPickCache,
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
	animatedCameraCandidates,
	type RegionCollision
} from "@/engine/foundation/rendering/follow-camera";
import { initialStarFlicker } from "@/engine/foundation/rendering/star-flicker";
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
import { hypot3 } from "@/engine/foundation/math/hypot";
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
// GPU geometry bytes admitted per frame while a scene loads. A region's
// terrain arrives as a few megabytes; spreading it keeps crossings smooth.
// At least one group is admitted each frame, whatever its size.
const UPLOAD_BYTES_PER_FRAME = 4 << 20;

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
	/*
 ================
 clothRandom
 ================
 */
	function clothRandom() {
		if ( !random ) throw Error( "Missing shared presentation RNG for cloth" );
		return random.range( 0, 32768 );
	}

	let interactionScene: WorldScene | null = null, interactionCells = terrainInteractionCells( null );
	// Ground pick bounds and triangles of resident terrain cells (terrain-interaction.ts).
	const terrainPicks = createTerrainPickCache();
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
	// The admitted copy of the startup stars and its byte size, made once: every
	// outdoor adoption composes it, and a crossing keeps its draw like any group.
	let ownedStars: { readonly group: WorldGroup; readonly bytes: number; } | null = null;
	/*
	================
	ownedStarGroup
	================
	*/
	function ownedStarGroup() {
		if ( !random ) throw Error( "Missing shared presentation RNG" );
		startupStars ??= skyGroups( { starPrimitive: random.sky() } ).find( group => group.material.sky === 2 )!;
		if ( !ownedStars ) {
			// The application, not the asset worker, owns the shared star RNG. Only this
			// small generated group needs local admission; the transferred city does not.
			const stars: WorldScene = { id: "stars", originRegion: 0, warnings: [], groups: [ startupStars ] };
			ownedStars = { group: copyWorldScene( stars ).groups[0]!, bytes: worldSceneBytes( stars ) };
		}
		return ownedStars;
	}
	let clock: { timeOfDay: number; lunarDay: number; } | null = null;
	let hasStars = false;
	let pickGroups: WorldGroup[] = [], pickSeconds = 0;
	// The frustum of the last selection walk. Instanced groups submit placements
	// outside it for the GPU to clip; picks keep to the ones inside it.
	let pickFrustum: Float64Array | null = null;
	const alphaMasks = new WeakMap<ImageBitmap, PickAlpha>();
	// Immutable admitted positions own these bounds; replacement storage invalidates them.
	const pickBounds = new WeakMap<Float32Array, PickBounds>();
	const pickBlocks = new WeakMap<WorldGroup, Float64Array>();
	// Camera collision parts of a region's terrain survive crossings with it.
	const terrainCollision = new WeakMap<WorldGroup, RegionCollision>();
	// Region terrain associations draw through shared layers (terrain-layers.ts).
	const layers = createTerrainLayers();
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
	let frameWork: import("@/engine/contracts/runtime").FrameWork | undefined;
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
			const paths = new Set(
				[
					...texturePaths( group ),
					...(group.material.water && pending?.waterBump ? [ pending.waterBump ] : [])
				].filter( path => !images.has( path ) )
			);
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
			// Placement bounds (undefined for animated meshes), once resolved.
			bounds?: PickBounds;
			boundsResolved?: boolean;
			seams?: Map<number, { mask: number; cellX: number; cellZ: number; }>;
			// The ranges the eye cell selects, the previous cell's (a camera that
			// steps back reuses them), and the ranges the group's draw holds.
			candidates?: readonly TerrainRange[];
			previousCandidates?: { cellX: number; cellZ: number; ranges: readonly TerrainRange[]; };
			chosen?: readonly TerrainRange[];
			cellX?: number;
			cellZ?: number;
		}
	>();
	/*
	================
	instanceSelection

	The selection cache of an instanced group: the placements it shows and
	their opacity. Admission builds it for every new group, a few per frame,
	so the first frame of a new scene does not.
	================
	*/
	function instanceSelection( group: WorldGroup ) {
		const source = group.geometry.instances!;
		let cache = selections.get( group );
		if ( !cache ) {
			cache = {
				indices: new Uint32Array(),
				instances: new Float32Array( source.length ),
				sourceInstances: new Int32Array( source.length / 16 ).fill( -1 ),
				alphas: new Uint8Array( source.length / 16 )
			};
			if ( group.visibility ) cache.opacity = new Float32Array( source.length / 16 );
			selections.set( group, cache );
		}
		return cache;
	}
	// These are derived from owned scene metadata. Only blended depth order is
	// camera-dependent; native material-set order remains fixed until replacement.
	let orderedGroups: WorldGroup[] = [], objectOrder = new Map<string, number>();
	// Optional measurements from the frame owner (runtime.ts frameProbe). The
	// profiler never rewrites this source; it observes these explicit hooks.
	let probe: import("@/engine/contracts/runtime").RenderFrameProbe | undefined;
	// Terrain triangles by cell for shadow receivers, revised per cell as the
	// selection walk changes a group's chosen ranges (shadow-surfaces.ts).
	const shadowSurfaces = createShadowSurfaces();
	// Receivers by shadow key, kept only while a shadow uses them this frame.
	// A receiver is rebuilt when a terrain cell under it changes, not when any
	// selection anywhere does: a dragged camera changes one nearly every frame.
	const shadowReceivers = new Map<string, {
		readonly cells: ReturnType<typeof terrainInteractionCells>;
		readonly stamp: number;
		readonly receiver: ReturnType<typeof characterShadowReceiver>;
	}>();
	/*
	================
	receiverChanged

	True when a terrain cell under the receiver of a shadow at point changed
	after stamp.
	================
	*/
	function receiverChanged( stamp: number, point: readonly [number, number, number], blobSize?: number ) {
		const bounds = shadowReceiverBounds( point, blobSize );
		return shadowSurfaces.changedSince(
			stamp,
			Math.floor( bounds.loX / 320 ),
			Math.floor( bounds.loZ / 320 ),
			Math.floor( bounds.hiX / 320 ),
			Math.floor( bounds.hiZ / 320 )
		);
	}
	// Each ordered group's index in orderedGroups.
	let groupOrder = new Map<WorldGroup, number>();
	// Visible marks by draw order: the walk marks, ordering reads them back.
	// The draw phase of each group, by draw order.
	let visibleMarks = new Uint8Array( 0 ), orderPhase = new Int8Array( 0 );
	// Per order index: the terrain layer the group draws through, else its own
	// draw (orderGroups), so submission reads arrays, not maps; and the
	// selection walk's scratch (visible order indices, transparent depths).
	let orderLayers: (TerrainLayer | undefined)[] = [], orderDraws: (GeometryDraw | undefined)[] = [];
	let orderVisible = new Int32Array( 0 ), orderDepth = new Float64Array( 0 );
	// The current scene's groups in walk order, with their typed bounds.
	let walk = compileWalkTable( [], pickBounds );
	// Instanced selection caches by walk index, so the walk does not look them up.
	let walkCaches: (ReturnType<typeof instanceSelection> | undefined)[] = [];
	/*
	================
	orderGroups

	Sets the draw order of the current scene and compiles its walk table.
	Shadow surfaces follow the draw order, so they are rebuilt from the
	retained selections.
	================
	*/
	function orderGroups( sceneGroups: readonly WorldGroup[] ) {
		const groups = [ ...sceneGroups ].sort( compareGroups );
		orderedGroups = groups;
		groupOrder = new Map( groups.map( ( group, index ) => [ group, index ] ) );
		visibleMarks = new Uint8Array( groups.length );
		orderPhase = Int8Array.from( groups, drawPhase );
		orderLayers = groups.map( group => layers.layerOf( group ) );
		orderDraws = groups.map( group => draws.get( group ) );
		orderVisible = new Int32Array( groups.length );
		orderDepth = new Float64Array( groups.length );
		walk = compileWalkTable( sceneGroups, pickBounds );
		walkCaches = new Array( sceneGroups.length );
		for ( let i = 0; i < sceneGroups.length; i++ ) walk.order[i] = groupOrder.get( sceneGroups[i]! )!;
		shadowSurfaces.reset();
		for ( let index = 0; index < groups.length; index++ ) {
			const group = groups[index]!, chosen = selections.get( group )?.chosen;
			if ( group.material.terrain && chosen ) shadowSurfaces.replace( group, index, undefined, chosen );
		}
		shadowSurfaces.commit();
	}
	// Selection walks so far (walk-table.ts seen), and layer submission passes.
	let walkStamp = 0, submitPass = 0;
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
	// Object fades by placement (object-fades.ts). Row numbers change when the
	// fades are compacted or cleared; fadeEpoch tells a group to resolve its
	// rows again.
	const fades = createObjectFades();
	let fadeEpoch = 0;
	let selectedFadeFrame = -1, fadeFrame = 0, fadeSeconds: number | null = null, fadeScene: WorldScene | null = null;
	let retainedFadeFrame: number | null = null,
		fadesChanging = true,
		retainedTargetX = NaN,
		selectedLayerRevision = -1,
		retainedTargetZ = NaN;
	const poses = new Map<
		string,
		{ pose: ReturnType<typeof createCharacterPose>; revision: number; seconds: number | null; }
	>();
	const animationKeys = new Map<WorldGroup, string>(), animationVersions = new Map<WorldGroup, number>();
	let animated: WorldGroup[] = [], activeAnimated: WorldGroup[] = [];
	let dynamicAnimation = false;
	const clothMeshes = new Map<WorldGroup, ReturnType<typeof createClothVertices>>();
	const dirtyAnimation = new Set<WorldGroup>();
	let materialTimelines: {
		group: WorldGroup;
		clock: ReturnType<typeof createMaterialTimeline>;
		draw?: GeometryDraw;
	}[] = [];
	let textureMotions: { group: WorldGroup; motion: ReturnType<typeof createTextureMotion>; draw?: GeometryDraw; }[] =
		[];
	// Groups whose material modifier pulses TEXTUREFACTOR (texture-factor-pulse.ts).
	let texturePulses: {
		group: WorldGroup;
		pulse: ReturnType<typeof createTextureFactorPulse>;
		draw?: GeometryDraw;
	}[] = [];
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
			const pulses = new Map( texturePulses.map( r => [ r.group.id, r ] ) );
			texturePulses = scene?.groups.filter( g => g.material.textureFactorPulse ).map( group => {
				const kept = pulses.get( group.id );
				const same = kept && JSON.stringify( kept.group.material.textureFactorPulse ) ===
						JSON.stringify( group.material.textureFactorPulse );
				return {
					group,
					pulse: same ? kept.pulse : createTextureFactorPulse( group.material.textureFactorPulse! )
				};
			} ) ?? [];
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
		for ( const group of clothMeshes.keys() ) if ( !scene?.groups.includes( group ) ) clothMeshes.delete( group );
		for ( const group of scene?.groups ?? [] ) {
			if ( group.geometry.cloth && !clothMeshes.has( group ) ) {
				clothMeshes.set(
					group,
					createClothVertices( { geometry: group.geometry, cloth: group.geometry.cloth }, clothRandom )
				);
			}
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
		for ( const row of texturePulses ) {
			const changed = row.pulse.step( seconds ), draw = draws.get( row.group );
			if ( draw && draw.instanceCount > 0 && (changed || force || row.draw !== draw) ) {
				geometry.updateTextureFactor( draw, row.pulse.factor );
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
		for ( const [group, cloth] of clothMeshes ) {
			const draw = draws.get( group );
			if ( !draw || !draw.instanceCount ) continue;
			const matrix = group.geometry.instances ?? group.geometry.transform;
			geometry.writeVertices(
				draw,
				0,
				cloth.update( group.geometry.bones ?? new Float32Array( 0 ), seconds, dynamicAnimation, {
					direction: [ matrix[8]!, matrix[9]!, matrix[10]! * (group.animation ? -1 : 1) ],
					speed: 0
				} )
			);
		}
		for ( const group of activeAnimated ) {
			if ( !group.geometry.cloth && (force || dirtyAnimation.has( group )) ) {
				geometry.updateBones( draws.get( group )!, group.geometry.bones! );
			}
			dirtyAnimation.delete( group );
		}
	}
	const retired: WorldScene[] = [];
	let lastReflection = false;
	let lastView: Float32Array | null = null, lastEye: WorldCamera["eye"] | null = null;
	const residency = createWorldResidency();
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
		return imageBytes + residency.retained();
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
			...(current?.waterBump ? [ current.waterBump ] : []),
			...(pending?.waterBump ? [ pending.waterBump ] : []),
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
		if ( !scene ) return;
		// Groups another held scene still uses (a retained region) keep their draws.
		for ( const group of residency.release( scene, [ current, pending, ...retired ] ) ) {
			const draw = draws.get( group );
			if ( layers.member( group ) ) layers.remove( geometry, group );
			else if ( draw ) geometry.release( draw );
			draws.delete( group );
			selections.delete( group );
		}
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
				group.material.sky || group.material.lightmap || blendAdds( group.material ) ||
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
				if ( group.instanceRadius !== undefined && pickFrustum ) {
					const inFrustum = bounds ?
						visibleFrustumBox( pickFrustum, bounds, instances, i * 16 ) :
						visibleFrustumSphere(
							pickFrustum,
							instances[i * 16 + 12]!,
							instances[i * 16 + 13]!,
							instances[i * 16 + 14]!,
							group.instanceRadius
						);
					if ( !inFrustum ) continue;
				}
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
				const depth = pickGeometry( ray, mesh, matrix, source.bones, surface );
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
		frameWork

		Admission shares the runtime budget with optional actor work. Keep the
		resident scene and collision usable until the replacement is complete.
		================
		*/
		frameWork( value: import("@/engine/contracts/runtime").FrameWork ) {
			frameWork = value;
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
			const surfaces = shadowSurfaces.surfaces();
			// A receiver depends only on the shadow's point and size, the blob
			// size and the terrain. A still character reuses last frame's mesh.
			const used = new Set<string>();
			const requests = candidates.flatMap( c => {
				if ( c.blobSize !== undefined && !image ) return [];
				const point = c.projection.point,
					key = `${c.blobSize ?? ""}:${c.projection.size}:${point[0]}:${point[1]}:${point[2]}`;
				used.add( key );
				let cached = shadowReceivers.get( key );
				if (
					!cached || cached.cells !== interactionCells || receiverChanged( cached.stamp, point, c.blobSize )
				) {
					cached = {
						cells: interactionCells,
						stamp: shadowSurfaces.revision(),
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
				emitters: current.scenery.filter( e => {
					const row = fades.find( e.placement );
					return row === undefined ||
						fades.visited( row ) === selectedFadeFrame && fades.published( row ) > 0;
				} ),
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
			return pickTerrainCells( interactionCells, ray, terrainPicks );
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
		adopt(
			lease: WorldSceneLease,
			detail?: WorldScene["terrainDetail"],
			terrain: readonly import("@/engine/contracts/world-admission").WorldTerrainPart[] = []
		) {
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
				const stars = ownedStarGroup();
				size += stars.bytes - prepared.starBytes;
				replacement = {
					...replacement,
					starRandomState: undefined,
					groups: replacement.groups.map( group => group.material.sky === 2 ? stars.group : group )
				};
			}
			if ( detail !== undefined ) replacement = { ...replacement, terrainDetail: detail };
			// Terrain parts join in the scene's own coordinates, each region once.
			const regions = new Set<number>();
			let whole = size;
			for ( const part of terrain ) {
				if (
					part.origin !== replacement.originRegion || regions.has( part.region ) ||
					!Number.isSafeInteger( part.bytes ) || part.bytes < 0 || replacement.residency === "frontend"
				) throw new Error( "Invalid world terrain part" );
				regions.add( part.region );
				whole += part.bytes;
			}
			if ( terrain.length ) {
				replacement = {
					...replacement,
					groups: [ ...replacement.groups, ...terrain.flatMap( part => part.groups ) ]
				};
			}
			if (
				whole > (replacement.residency === "frontend" ? FRONTEND_SCENE_BYTES : WORLD_SCENE_BYTES) ||
				retainedBytes() + residency.incoming( size, terrain ) > capacity( replacement )
			) {
				throw new Error(
					`World CPU residency budget exceeded (incoming ${size}, retained ${retainedBytes()}, images ${imageBytes}, capacity ${
						capacity( replacement )
					})`
				);
			}
			if ( pending ) retired.push( pending );
			residency.hold( replacement, size, terrain );
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
			residency.hold( replacement, size );
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
			backgroundDistance?: number,
			reflectWater = false,
			enableDynamicAnimation = false
		): PreparedWorld {
			dynamicAnimation = enableDynamicAnimation;
			probe?.worldBegin?.();
			while ( retired.length ) release( retired.pop()!, geometry );
			let budget = 8, uploadBytes = UPLOAD_BYTES_PER_FRAME;
			if ( pending ) {
				if ( camera.follow && !preparedCollision ) {
					collisionPreparation ??= prepareCameraCollisionParts( pending, terrainCollision );
					// Prepare the exact collision product alongside GPU admission. Keep the
					// old scene until both are ready; never remove collision for a fast handoff.
					for ( let work = 0; work < 40 && (!frameWork || frameWork.remaining() > 0); work++ ) {
						const started = frameWork ? performance.now() : 0;
						const result = collisionPreparation.next();
						frameWork?.spend( performance.now() - started );
						if ( result.done ) {
							preparedCollision = result.value;
							collisionPreparation = null;
							break;
						}
					}
				}
				for ( const group of readyGroups ) {
					if ( !budget-- || uploadBytes <= 0 || (frameWork && frameWork.remaining() <= 0) ) break;
					const started = frameWork ? performance.now() : 0;
					uploadBytes -= group.geometry.positions.length / 3 * 56 + group.geometry.indices.byteLength;
					const paths = texturePaths( group );
					// Alpha readback belongs to bounded resource admission, not the first
					// hover over a resident object. Animated texture frames are admitted too.
					if (
						readAlpha && !group.material.sky && !group.material.lightmap && !blendAdds( group.material )
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
					let waterBump: ImageDraw | undefined;
					if ( group.material.water && pending.waterBump ) {
						waterBump = imageDraws.get( pending.waterBump );
						if ( !waterBump ) {
							waterBump = textures.upload( images.get( pending.waterBump )!.source );
							imageDraws.set( pending.waterBump, waterBump );
						}
					}
					// Build immutable pick bounds within the bounded upload work, so the first
					// nameplate hover cannot scan the entire scene's vertex buffers.
					if ( !group.geometry.bones && !pickBounds.has( group.geometry.positions ) ) {
						pickBounds.set( group.geometry.positions, geometryPickBounds( group.geometry.positions ) );
					}
					if ( !group.geometry.bones && !group.ranges && !pickBlocks.has( group ) ) {
						pickBlocks.set( group, geometryPickBlocks( group.geometry ) );
					}
					draws.set(
						group,
						layers.eligible( group ) ?
							layers.admit( geometry, pending, group, imageDraw ) :
							geometry.upload(
								group.geometry.cloth ?
									{
										...group.geometry,
										joints: undefined,
										weights: undefined,
										bones: undefined,
										dynamicVertices: true
									} :
									group.geometry,
								imageDraw,
								undefined,
								waterBump
							)
					);
					if ( group.instanceRadius !== undefined ) instanceSelection( group );
					readyGroups.delete( group );
					pendingGroupCount--;
					frameWork?.spend( performance.now() - started );
				}
				if ( pendingGroupCount === 0 && missingTextures.size === 0 && (!camera.follow || preparedCollision) ) {
					const previous = current;
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
					// Placements keep their fade identity across outdoor scenes: a crossing
					// must not fade every retained building in again.
					const sameWorld = !!previous && fadeScene === previous &&
						previous.residency === current.residency &&
						!((previous.originRegion | current.originRegion) & 0x8000);
					if ( sameWorld ) {
						const placed = new Set<string>();
						for ( const group of current.groups ) {
							for ( const descriptor of group.visibility ?? [] ) placed.add( descriptor.id );
						}
						fades.keepOnly( placed );
						fadeEpoch++;
						fadeScene = current;
						fadesChanging = true;
					} else if ( fadeScene !== current ) {
						fades.clear();
						fadeEpoch++;
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
					orderGroups( current.groups );
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
					collisionParts = cameraCollisionParts( current, terrainCollision );
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
			const matrix = viewProjection( localCamera, aspect );
			const mainFrustum = prepareViewFrustum( matrix );
			const reflectionWater = reflectWater ?
				current?.groups.find( group =>
					group.material.water &&
					visibleFrustumSphere( mainFrustum, group.center[0], group.center[1], group.center[2], group.radius )
				) :
				undefined;
			const waterHeight = reflectionWater?.center[1];
			const reflectionMatrix = waterHeight === undefined ?
				undefined :
				waterReflectionMatrix( matrix, waterHeight, localCamera.eye[1] >= waterHeight );
			const reflectionFrustum = reflectionMatrix ? prepareViewFrustum( reflectionMatrix ) : undefined;
			const viewChanged = reflectWater || lastReflection !== reflectWater || !lastView ||
				!matrix.every( ( v, i ) => v === lastView![i] ) || !lastEye ||
				localCamera.eye.some( ( v, i ) => v !== lastEye![i] );
			lastReflection = reflectWater;
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
				!viewChanged && !fadesChanging && targetCellX === retainedTargetX && targetCellZ === retainedTargetZ &&
				layers.revision() === selectedLayerRevision
			) {
				// Every resident object would receive this frame stamp without changing
				// selection or opacity. Materialize those stamps only on the next walk.
				retainedFadeFrame = fadeFrame;
				animate( geometry, seconds );
				probe?.worldMark?.( "world-finalize" );
				return {
					camera: localCamera,
					waterHeight,
					reflectionMatrix,
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
			if ( retainedFadeFrame !== null ) fades.stampActive( retainedFadeFrame );
			// Only the stationary early-out defers frame stamps. An ordinary selection
			// walk has already committed them; do not replay those writes next frame.
			retainedFadeFrame = null;
			fades.clearActive();
			let changing = false;
			lastView = matrix;
			const sampleDetails = probe?.sampleDetails?.() ?? false;
			const frustum = prepareViewFrustum( matrix ), visible: WorldGroup[] = [];
			pickFrustum = frustum;
			triangles = 0;
			const eyeCellX = Math.floor( localCamera.eye[0] / 320 ), eyeCellZ = Math.floor( localCamera.eye[2] / 320 );
			// The fade range of scenery objects follows the background distance.
			const sceneryRange = Math.min( 2500, Math.fround( (backgroundDistance ?? 3500) * Math.fround( .8 ) ) ) -
				480;
			const selectTerrainLod = ( distanceSquared: number ) =>
				current?.terrainDetail === "full" ? 0 : terrainLod( distanceSquared );
			const walkGroups = walk.groups, stamp = ++walkStamp;
			for ( let walked = 0; walked < walkGroups.length; walked++ ) {
				const group = walkGroups[walked]!, kind = walk.kind[walked]!;
				if ( current?.dungeonVisibility && group.dungeonBlock !== undefined ) {
					const visibleBlocks = camera.dungeonBlock === undefined ?
						undefined :
						current.dungeonVisibility[camera.dungeonBlock];
					if ( !visibleBlocks?.includes( group.dungeonBlock ) ) continue;
				}
				if ( kind === WALK_SKY ) {
					visible.push( group );
					visibleMarks[walk.order[walked]!] = 1;
					walk.seen[walked] = stamp;
					continue;
				}
				if ( !viewChanged && !walk.faded[walked] ) {
					if ( walk.seen[walked] === stamp - 1 ) {
						visible.push( group );
						visibleMarks[walk.order[walked]!] = 1;
						walk.seen[walked] = stamp;
						triangles += walk.triangles[walked]!;
					}
					continue;
				}
				const trianglesBefore = triangles;
				// A terrain group's choice depends on the eye cell alone (below): an
				// unchanged cell is decided from the table, without the group's objects.
				if (
					kind === WALK_TERRAIN && walk.cell[walked * 2] === eyeCellX &&
					walk.cell[walked * 2 + 1] === eyeCellZ
				) {
					const indices = walk.count[walked]!;
					if ( indices > 0 ) {
						visible.push( group );
						visibleMarks[walk.order[walked]!] = 1;
						triangles += indices / 3;
						walk.seen[walked] = stamp;
						walk.triangles[walked] = indices / 3;
					}
					continue;
				}
				if ( kind === WALK_TERRAIN && group.ranges ) {
					let cache = selections.get( group );
					if ( !cache ) {
						cache = { indices: new Uint32Array( group.geometry.indices.length ), seams: new Map() };
						selections.set( group, cache );
					}
					// The terrain a group submits depends on the eye cell alone: every range
					// at the LOD its distance from the eye cell selects. Native culls cells
					// against the frustum each frame (A2D1B0); the GPU clips the off-screen
					// ones here instead, at the cost of a few thousand clipped triangles, so
					// turning the camera changes nothing to choose or upload.
					const cellChanged = cache.cellX !== eyeCellX || cache.cellZ !== eyeCellZ;
					if ( cache.chosen && !cellChanged ) {
						if ( cache.indexCount ) {
							visible.push( group );
							visibleMarks[walk.order[walked]!] = 1;
							triangles += cache.indexCount / 3;
							walk.seen[walked] = stamp;
							walk.triangles[walked] = cache.indexCount / 3;
						}
						continue;
					}
					if ( sampleDetails ) probe?.detailBegin?.( "terrain-candidates" );
					if ( !cache.candidates || (cellChanged && current?.terrainDetail !== "full") ) {
						// Retain only two cell-dependent plans; geometry retirement owns both.
						const previous = cache.previousCandidates;
						cache.previousCandidates = cache.candidates ?
							{ cellX: cache.cellX!, cellZ: cache.cellZ!, ranges: cache.candidates } :
							undefined;
						if ( previous && previous.cellX === eyeCellX && previous.cellZ === eyeCellZ ) {
							cache.candidates = previous.ranges;
						} else {
							cache.candidates = group.ranges.filter( range => {
								const dx = range.cell[0] - eyeCellX, dz = range.cell[1] - eyeCellZ;
								return range.lod === selectTerrainLod( dx * dx + dz * dz );
							} );
						}
					}
					cache.cellX = eyeCellX;
					cache.cellZ = eyeCellZ;
					if ( sampleDetails ) probe?.detailEnd?.( "terrain-candidates" );
					if ( sampleDetails ) probe?.detailBegin?.( "terrain-indices" );
					const chosen = cache.candidates, indicesDirty = chosen !== cache.chosen;
					let count = 0;
					let positionRanges: [number, number][] | null = null;
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
						if ( lastChanged >= 0 ) {
							(positionRanges ??= []).push( [ firstChanged, lastChanged - firstChanged + 1 ] );
						}
					}
					if ( sampleDetails ) probe?.detailEnd?.( "terrain-seams" );
					cache.indexCount = count;
					walk.count[walked] = count;
					walk.cell[walked * 2] = eyeCellX;
					walk.cell[walked * 2 + 1] = eyeCellZ;
					if ( sampleDetails ) probe?.detailBegin?.( "terrain-index-upload" );
					if ( indicesDirty ) {
						if ( layers.member( group ) ) layers.select( group, cache.indices, count );
						else geometry.updateIndices( draws.get( group )!, cache.indices.subarray( 0, count ) );
						if ( group.material.terrain ) {
							shadowSurfaces.replace( group, groupOrder.get( group )!, cache.chosen, chosen );
						}
						cache.chosen = chosen;
					}
					if ( sampleDetails ) probe?.detailEnd?.( "terrain-index-upload" );
					if ( sampleDetails ) probe?.detailBegin?.( "terrain-position-upload" );
					if ( positionRanges ) {
						if ( layers.member( group ) ) {
							layers.updatePositions( geometry, group, group.geometry.positions, positionRanges );
						} else {
							geometry.updatePositions(
								draws.get( group )!,
								group.geometry.positions,
								undefined,
								undefined,
								positionRanges
							);
						}
					}
					if ( sampleDetails ) probe?.detailEnd?.( "terrain-position-upload" );
					if ( count ) {
						visible.push( group );
						visibleMarks[walk.order[walked]!] = 1;
						triangles += count / 3;
						walk.seen[walked] = stamp;
						walk.triangles[walked] = triangles - trianglesBefore;
					}
					continue;
				}
				if ( kind === WALK_INSTANCED ) {
					if ( sampleDetails ) probe?.detailBegin?.( "world-instance-setup" );
					const base = walk.slotBase[walked]!, at = walked * 5, sphere = walk.sphere, eye = localCamera.eye;
					const cellX = walk.residentCell[walked * 2], cellZ = walk.residentCell[walked * 2 + 1];
					const resident = residentSlots( walk, walked, targetCellX, targetCellZ ),
						faded = walk.faded[walked] === 1;
					// A new resident list renumbers the per-slot alphas below.
					let listChanged = walk.count[walked]! < 0 || cellX !== targetCellX || cellZ !== targetCellZ;
					// Placement identity resolves once each fade epoch (rows change when
					// the fades compact or clear). A building's material pieces share
					// one row.
					if ( faded && walk.rowsEpoch[walked] !== fadeEpoch ) {
						const descriptors = group.visibility!;
						for ( let slot = 0; slot < descriptors.length; slot++ ) {
							walk.row[base + slot] = fades.row( descriptors[slot]!.id );
						}
						walk.rowsEpoch[walked] = fadeEpoch;
					}
					// Objects in a steady state this group's eye distances cannot leave
					// take only the frame stamp advanceObjectFade would give them.
					const keeps = faded ? fadeKeeps( walk, walked, eye, sceneryRange ) : 0;
					if ( sampleDetails ) {
						probe?.detailEnd?.( "world-instance-setup" );
						probe?.detailBegin?.( "world-instance-loop" );
					}
					// Fade state ticks for every resident object, whatever the frustum.
					// Each resident slot keeps its published alpha, or -1 when out; the
					// group's placement list changes only when one of those does.
					for ( let n = 0; n < resident; n++ ) {
						const slot = walk.resident[base + n]!, g = base + slot, slotKind = walk.slotKind[g]!;
						let alpha = 255;
						if ( slotKind !== SLOT_UNFADED ) {
							const row = walk.row[g]!;
							fades.visit( row, fadeFrame );
							if ( fades.lastFrame( row ) !== fadeFrame ) {
								const state = fades.state( row );
								if (
									!viewChanged && fades.lastFrame( row ) === ((fadeFrame - 1) >>> 0) &&
										(state === 0 || state === 2) ||
									state === 2 && keeps & FADE_KEEPS_IN || state === 0 && keeps & FADE_KEEPS_OUT
								) fades.stamp( row, fadeFrame );
								else {
									fades.advance(
										row,
										objectFadeDistance(
											eye,
											walk.origin[g * 3]!,
											walk.origin[g * 3 + 1]!,
											walk.origin[g * 3 + 2]!
										),
										walk.radius[g]!,
										slotKind === SLOT_SCENERY ? sceneryRange : walk.range[g]!,
										dt,
										fadeFrame
									);
								}
							}
							const state = fades.state( row );
							if ( state === 1 || state === 3 ) changing = true;
							alpha = state !== 0 ? fades.published( row ) : -1;
						}
						if ( walk.alpha[base + n] !== alpha ) {
							walk.alpha[base + n] = alpha;
							listChanged = true;
						}
					}
					if ( sampleDetails ) {
						probe?.detailEnd?.( "world-instance-loop" );
						probe?.detailBegin?.( "world-instance-upload" );
					}
					// The group draws every resident placement that is faded in, in slot
					// order. Native rejects each placement's bound against the frustum
					// before submission (8AA9C5..8AA9D8, A2D410); here the group's sphere
					// is tested and the GPU clips placements outside the view, so turning
					// the camera neither re-tests nor re-uploads placements.
					if ( listChanged ) {
						const cache = walkCaches[walked] ??= instanceSelection( group ),
							source = group.geometry.instances!;
						let count = 0, instancesDirty = false;
						for ( let n = 0; n < resident; n++ ) {
							const alpha = walk.alpha[base + n]!;
							if ( alpha < 0 ) continue;
							const i = walk.resident[base + n]! * 16;
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
						instancesDirty ||= count !== (cache.instanceCount ?? 0);
						cache.instanceCount = count;
						if ( instancesDirty ) {
							const draw = geometry.updateInstances(
								draws.get( group )!,
								cache.instances!.subarray( 0, count * 16 ),
								cache.opacity?.subarray( 0, count )
							);
							draws.set( group, draw );
							orderDraws[walk.order[walked]!] = draw;
						}
						walk.count[walked] = count;
					}
					if ( sampleDetails ) probe?.detailEnd?.( "world-instance-upload" );
					const count = walk.count[walked]!;
					if (
						count &&
						(visibleFrustumSphere(
							frustum,
							sphere[at]!,
							sphere[at + 1]!,
							sphere[at + 2]!,
							sphere[at + 3]!
						) ||
							!!reflectionFrustum &&
								visibleFrustumSphere(
									reflectionFrustum,
									sphere[at]!,
									sphere[at + 1]!,
									sphere[at + 2]!,
									sphere[at + 3]!
								))
					) {
						visible.push( group );
						visibleMarks[walk.order[walked]!] = 1;
						triangles += walk.instanceTriangles[walked]! * count;
						walk.seen[walked] = stamp;
						walk.triangles[walked] = triangles - trianglesBefore;
					}
					continue;
				}
				if (
					(visibleFrustumSphere( frustum, group.center[0], group.center[1], group.center[2], group.radius ) ||
						!!reflectionFrustum &&
							visibleFrustumSphere(
								reflectionFrustum,
								group.center[0],
								group.center[1],
								group.center[2],
								group.radius
							))
				) {
					visible.push( group );
					visibleMarks[walk.order[walked]!] = 1;
					triangles += group.geometry.indices.length / 3 * (group.geometry.instances!.length / 16);
					walk.seen[walked] = stamp;
					walk.triangles[walked] = triangles - trianglesBefore;
				}
			}
			// CObjRenderer::QueueObject (0xAAE9C0) appends; 0xA5EE80 consumes
			// that order. Keep scene submission order between resource batches:
			// sorting filenames can put a fading prop before the solid structure behind it.
			layers.flush( geometry );
			// The selection below resolves every layer's current draw. A pending
			// scene's admission can grow (replace) a layer the current one shares
			// while the camera stands still; that must reopen this walk.
			selectedLayerRevision = layers.revision();
			shadowSurfaces.commit();
			selectedFadeFrame = fadeFrame;
			probe?.worldMark?.( "world-selection" );
			fadesChanging = changing;
			retainedTargetX = targetCellX;
			retainedTargetZ = targetCellZ;
			// Draw order of the visible groups, without walking every resident one;
			// the phase boundaries fall out of the same pass.
			activeAnimated = animated.filter( group => visibleMarks[groupOrder.get( group ) ?? -1] === 1 );
			let visibleCount = 0;
			terrainEnd = -1;
			transparentStart = -1;
			for ( let index = 0; index < visibleMarks.length; index++ ) {
				if ( !visibleMarks[index] ) continue;
				visibleMarks[index] = 0;
				const phase = orderPhase[index]!;
				if ( terrainEnd < 0 && phase >= 3 ) terrainEnd = visibleCount;
				if ( transparentStart < 0 && phase === 4 ) transparentStart = visibleCount;
				orderVisible[visibleCount++] = index;
			}
			if ( terrainEnd < 0 ) terrainEnd = visibleCount;
			if ( transparentStart < 0 ) transparentStart = visibleCount;
			// Far to near. Each group's distance is taken once, not per comparison;
			// ties keep draw order.
			for ( let i = transparentStart; i < visibleCount; i++ ) {
				const index = orderVisible[i]!, group = orderedGroups[index]!;
				orderDepth[index] = hypot3(
					group.center[0] - localCamera.eye[0],
					group.center[1] - localCamera.eye[1],
					group.center[2] - localCamera.eye[2]
				);
			}
			orderVisible.subarray( transparentStart, visibleCount ).sort( ( a, b ) =>
				orderDepth[b]! - orderDepth[a]! || a - b
			);
			const ordered: WorldGroup[] = new Array( visibleCount );
			for ( let i = 0; i < visibleCount; i++ ) ordered[i] = orderedGroups[orderVisible[i]!]!;
			pickGroups = ordered;
			// Upload gives every admitted group a unique resource handle. Its identity
			// already covers scene replacement and binding growth; rebuilding a string
			// of every asset name each frame adds no invalidation information.
			// Layer members share their layer's draw: submit it once, at its first
			// member. Members are terrain, so only the opaque phases shift.
			const nextDraws: GeometryDraw[] = [], pass = ++submitPass;
			let shifted = 0;
			for ( let i = 0; i < visibleCount; i++ ) {
				const index = orderVisible[i]!, layer = orderLayers[index];
				const draw = layer ? layers.drawInPass( layer, pass ) : orderDraws[index]!;
				if ( draw === null ) {
					if ( i < terrainEnd ) shifted++;
					continue;
				}
				nextDraws.push( draw );
			}
			terrainEnd -= shifted;
			transparentStart -= shifted;
			if (
				nextDraws.length !== selected.length || nextDraws.some( ( draw, index ) => draw !== selected[index] )
			) {
				selected = nextDraws;
				rebuilds++;
			}

			animate( geometry, seconds );
			probe?.worldMark?.( "world-finalize" );
			return {
				camera: localCamera,
				waterHeight,
				reflectionMatrix,
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
			orderGroups( [] );
			objectOrder.clear();
			lastEye = null;
			pickGroups = [];
			resetAnimations( undefined, true );
			draws.clear();
			layers.clear();
			selections.clear();
			retired.length = 0;
			imageDraws.clear();
			pending = pending ?? current;
			residency.keepOnly( pending );
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
			clothMeshes.clear();
			for ( const row of footprintDraws.values() ) geometry?.release( row.draw );
			footprintDraws.clear();
			footprints = [];
			if ( decalDraw ) geometry?.release( decalDraw );
			decalDraw = null;
			interactionCells.clear();
			interactionScene = null;
			decalState = null;
			weather.dispose( geometry );
			orderGroups( [] );
			objectOrder.clear();
			lastEye = null;
			pickGroups = [];
			collisionScene = null;
			collisionParts = [];
			collisionCamera = null;
			fades.clear();
			fadeEpoch++;
			fadeScene = null;
			resetAnimations();
			if ( geometry ) {
				for ( const [group, draw] of draws ) if ( !layers.member( group ) ) geometry.release( draw );
			}
			layers.dispose( geometry );
			retired.length = 0;
			if ( textures ) { for ( const draw of imageDraws.values() ) textures.release( draw ); }
			imageDraws.clear();
			for ( const image of images.values() ) if ( !("kind" in image.source) ) image.source.close();
			images.clear();
			imageBytes = 0;
			residency.clear();
			retired.length = 0;
			draws.clear();
			selections.clear();
			current = pending = null;
			selected = [];
			indexPending();
		}
	};
}
