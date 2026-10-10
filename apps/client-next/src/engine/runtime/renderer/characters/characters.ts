/*
===========================================================================

characters.ts - admitted character models, world batches and separate portrait sources

===========================================================================
*/
import { createClothVertices } from "@/engine/foundation/animation/cloth-vertices";
import { characterLabelHeight } from "@/engine/foundation/ui/character-labels";
import type { WorldTexture } from "@/engine/contracts/texture";
import { shadowProjection, SHADOW_LIMIT, SHADOW_DISTANCE } from "@/engine/foundation/rendering/character-shadow";
import { appendEquipmentSockets } from "@/engine/foundation/animation/equipment-sockets";
import { selectEquipmentGlow } from "@/engine/foundation/rendering/equipment-glow";
import { createPoseLod } from "@/engine/foundation/animation/pose-lod";
import { createDeferredParticles, particleQueryPoint } from "@/engine/foundation/animation/deferred-particles";
import { createModelMaterialClocks } from "@/engine/foundation/animation/model-material-clock";
import {
	createParticleGraph,
	advanceParticleGraph,
	particleElementMatrix,
	type ParticleGraphState
} from "@/engine/foundation/animation/particle-graph";
import { bindNativeClip, type NativeClip } from "@/engine/foundation/animation/native-clip";
import { createActorSnapshots } from "./actor-snapshots";
import type { PortraitPart, PortraitSource } from "@/engine/contracts/portrait";
import { bsrParticleAttachment } from "@/engine/foundation/animation/bsr-particle-transform";
import { createPaletteStreams } from "./palette-streams";
import {
	createCharacterRenderPlan,
	isCharacterAnimationExtension
} from "@/engine/foundation/animation/character-render-plan";
import { createCharacterHierarchy } from "@/engine/foundation/animation/character-hierarchy";
import { attachedOpacity } from "@/engine/foundation/animation/character-fade";
import { characterBindBounds, characterPickVolume } from "@/engine/foundation/animation/character-pick-volume";
import { meshUnderRays, selectPickCandidate, type PickCandidate } from "@/engine/foundation/animation/character-pick";
import { pickVolume, pickVolumeDepth } from "@/engine/foundation/rendering/pick-volume";
import {
	createRibbonChain,
	pushRibbonPoint,
	ribbonPolyline,
	ribbonSpline,
	ribbonStrip,
	type RibbonChain
} from "@/engine/foundation/rendering/particle-ribbon";
import { particleRandomTable, initializeParticle } from "@/engine/foundation/animation/particle-program";
import { PARTICLE_TICKS_PER_SECOND, ROTATION_WORK } from "@/engine/foundation/animation/particle-presentation";
import {
	beginParticleFrame,
	createParticleRow,
	createParticleStream,
	endParticleFrame,
	writeParticleRow,
	type ParticleHistory,
	type ParticleRandom,
	type ParticleStream
} from "./particle-streams";
import { type PickBounds, type PickRay } from "@/engine/foundation/rendering/picking";
import { faceEffectMesh } from "@/engine/foundation/rendering/effect-billboard";
import { characterRadius, createCharacterBoundsCache } from "@/engine/foundation/animation/character-bounds";
import {
	CHARACTER_ASSEMBLIES,
	CHARACTER_MODELS,
	characterBytes,
	CHARACTER_ACTORS,
	CHARACTER_RENDER_BYTES,
	CHARACTER_RESIDENT_BYTES
} from "@/engine/foundation/animation/character-budget";
import { multiply } from "@/engine/foundation/math/pose-math";
import { createCharacterPose } from "./animation/animation";
import {
	placement,
	identity,
	nativeModelOffset,
	prepareViewFrustum,
	visibleFrustumSphere
} from "@/engine/foundation/rendering/world-math";
import type { CharacterActor, CharacterModel } from "@/engine/contracts/character";
import type { GeometryCommands, GeometryDraw, ImageCommands, ImageDraw } from "../internal/gpu-contract";
import { hypot3 } from "@/engine/foundation/math/hypot";

/*
================
samePoseInputs

Whether two actors request the same evaluated pose: the inputs
CharacterPose.evaluate and bodyVolume read (clip, time, loop, and each
layer's clip, time, loop, weight and lane). Layer rate and activation are
presentation state the pose never reads. Replaces a JSON key built for
every actor every frame.
================
*/
function samePoseInputs( a: CharacterActor, b: CharacterActor ): boolean {
	if ( a.clip !== b.clip || a.time !== b.time || a.loop !== b.loop ) return false;
	// A rotated bone is the actor's own (spine-aim.ts).
	if ( a.boneRotation || b.boneRotation ) return false;
	if ( a.bodyVolume?.index !== b.bodyVolume?.index || a.bodyVolume?.female !== b.bodyVolume?.female ) return false;
	const x = a.layers, y = b.layers;
	if ( x === y ) return true;
	if ( !x || !y || x.length !== y.length ) return false;
	for ( let i = 0; i < x.length; i++ ) {
		const l = x[i]!, r = y[i]!;
		if (
			l.clip !== r.clip || l.time !== r.time || l.loop !== r.loop || l.weight !== r.weight || l.lane !== r.lane
		) return false;
	}
	return true;
}

/*
================
RowStreams

One renderer-owned scratch, bounded by the admitted actor count. Geometry
copies its values synchronously before the next group can overwrite them.
================
*/
interface RowStreams {
	rowScratch?: Float32Array;
	lightScratch?: Float32Array;
}
const POINT_LIGHT_FLOATS = 12;

/*
================
fillRowOpacities

Write fading opacities once per group instead of once per primitive. One
scratch serves every group and frame; updateInstances packs these values
synchronously before returning, so later reuse cannot alter queued draws.
================
*/
export function fillRowOpacities(
	streams: RowStreams,
	rows: readonly CharacterActor[],
	opacity: ( actor: CharacterActor ) => number
): Float32Array {
	if ( rows.length > CHARACTER_ACTORS ) throw Error( "Character row scratch exceeds actor limit" );
	if ( !streams.rowScratch || streams.rowScratch.length < rows.length ) {
		streams.rowScratch = new Float32Array( rows.length );
	}
	for ( let i = 0; i < rows.length; i++ ) streams.rowScratch[i] = opacity( rows[i]! );
	return streams.rowScratch.subarray( 0, rows.length );
}

/*
================
fillRowPointLights

The group's shared hit-point-light stream (position, attenuation, ambient,
diffuse - twelve floats a row), written once per group instead of once per
primitive. Same renderer-owned reuse contract as the opacity scratch.
================
*/
export function fillRowPointLights(
	streams: RowStreams,
	rows: readonly CharacterActor[],
	origin: number
): Float32Array {
	if ( rows.length > CHARACTER_ACTORS ) throw Error( "Character row scratch exceeds actor limit" );
	const needed = rows.length * POINT_LIGHT_FLOATS;
	if ( !streams.lightScratch || streams.lightScratch.length < needed ) {
		streams.lightScratch = new Float32Array( needed );
	}
	const lights = streams.lightScratch;
	lights.fill( 0, 0, needed );
	for ( let i = 0; i < rows.length; i++ ) {
		const light = rows[i]!.pointLight;
		if ( !light ) continue;
		const pos = placement(
			light.pose.regionId,
			origin,
			light.pose.x,
			light.pose.y,
			light.pose.z,
			rows[i]!.pose.yaw
		);
		const at = i * POINT_LIGHT_FLOATS;
		lights[at] = pos[12]!;
		lights[at + 1] = pos[13]!;
		lights[at + 2] = pos[14]!;
		lights[at + 3] = light.attenuation;
		lights[at + 4] = light.ambient[0]!;
		lights[at + 5] = light.ambient[1]!;
		lights[at + 6] = light.ambient[2]!;
		lights[at + 8] = light.diffuse[0]!;
		lights[at + 9] = light.diffuse[1]!;
		lights[at + 10] = light.diffuse[2]!;
	}
	return lights.subarray( 0, needed );
}

/*
================
RibbonBuffers

One ribbon primitive's vertex streams, sized for its batch's capacity.
================
*/
interface RibbonBuffers {
	readonly capacity: number;
	used: number;
	readonly positions: Float32Array;
	readonly colors: Float32Array;
	readonly uvs: Float32Array;
	readonly indices: Uint32Array;
}
const MAX_ATTACHMENT_DEPTH = 8;

/*
================
createCharacters

Own source resources separately from borrowed assemblies and per-frame draw batches.
================
*/
export function createCharacters( random?: import("@/engine/contracts/presentation-random").PresentationRandom ) {
	// prepareGroup consumes this scratch synchronously before the next group.
	// Account it separately from admission: caching must not remove actors
	// that fit the existing draw budget. Actor snapshots bound both arrays.
	const rowStreams: RowStreams = {};
	/*
 ================
 clothRandom
 ================
 */
	function clothRandom() {
		if ( !random ) throw Error( "Missing shared presentation RNG for cloth" );
		return random.range( 0, 32768 );
	}
	let probe: import("@/engine/contracts/runtime").RenderFrameProbe | undefined;
	// Per-geometry radius work: an assembled character reuses its parts'.
	const bounds = createCharacterBoundsCache();
	const models = new Map<string, {
		model: CharacterModel;
		plan: ReturnType<typeof createCharacterRenderPlan>;
		images: WorldTexture[];
		textures: ImageDraw[];
		owned: boolean;
		bytes: number;
		radius: number;
		dependencies?: readonly string[];
	}>();
	const materialClocks = createModelMaterialClocks(), deferred = createDeferredParticles();
	// Scratch for the ribbon element frames' rotation (particleElementMatrix).
	const rotationWork = new Float64Array( ROTATION_WORK );
	// Scratch basis for faceEffectMesh, which runs per billboard actor.
	const billboardAxes = new Float64Array( 9 );
	// The vertex range a ribbon upload covers, [ start, count ].
	const ribbonRange: [number, number][] = [ [ 0, 0 ] ];
	// Ribbon scratch, reused every frame: the chains of one actor's groups
	// (ribbonChains[k] for its k-th group, found by groupChains), their drawn
	// form, the spline's work, and its elements' order.
	const ribbonChains: RibbonChain[] = [], groupChains = new Map<unknown, RibbonChain>();
	const ribbonGroups: RibbonChain[] = [], ribbonDrawn = createRibbonChain(), ribbonWork = createRibbonChain();
	let ribbonOrder = new Int32Array( 64 );
	const particleSnapshots = new Map<number, { matrix: Float32Array; regionId: number; }>();
	let hasDeferred = false, deferredVisible = new Set<number>();
	const hierarchy = createCharacterHierarchy(), snapshots = createActorSnapshots();
	// Per admitted model: the native pick box, and the bind bounds that size
	// labels and shadows. Both are immutable for the model's lifetime.
	const pickVolumes = new WeakMap<CharacterModel, PickBounds>(),
		bindBounds = new WeakMap<CharacterModel, PickBounds>();
	/*
	================
	pickVolumeOf
	================
	*/
	const pickVolumeOf = ( model: CharacterModel ) => {
		let bounds = pickVolumes.get( model );
		if ( !bounds ) {
			bounds = characterPickVolume( model );
			pickVolumes.set( model, bounds );
		}
		return bounds;
	};
	/*
	================
	bindBoundsOf
	================
	*/
	const bindBoundsOf = ( model: CharacterModel ) => {
		let bounds = bindBounds.get( model );
		if ( !bounds ) {
			bounds = characterBindBounds( model );
			bindBounds.set( model, bounds );
		}
		return bounds;
	};
	// Scratch evaluators for mesh-refined picks, one per admitted model.
	const pickPoses = new WeakMap<CharacterModel, ReturnType<typeof createCharacterPose>>();
	const textures = new Map<WorldTexture, ImageDraw>();
	// Snapshots retain identity while their fields change. Recheck the complete
	// variant each frame, but retain the long assembly prefix and its string hash.
	// Weak ownership retires keys with snapshots, including temporary effect rows.
	const batchKeys = new WeakMap<CharacterActor, { model: string; variant: string; key: string; }>();
	const poses = new Map<number, {
		model: string;
		pose: ReturnType<typeof createCharacterPose>;
	}>();
	// Borrowing an identical frame result must not replace an actor's scratch.
	// At most one owned evaluator per needed actor, already charged by the
	// frame's characterPoseBytes reservation. Both maps retire together.
	const ownedPoses = new Map<
		number,
		{
			model: string;
			pose: ReturnType<typeof createCharacterPose>;
			lod: ReturnType<typeof createPoseLod>;
			clips: CharacterModel["clips"];
			sampled?: number;
			clip?: string;
		}
	>();
	let poseFrame = 0;
	let frameWork: import("@/engine/contracts/runtime").FrameWork | undefined;
	let poseSeconds = 0;
	const poseOrder: CharacterActor[] = [];
	// Birth transforms belong to the actor lifetime, not GPU batches. Keep them
	// across culling, batch membership changes and device recreation.
	const particleRandom: ParticleRandom = { table: particleRandomTable(), index: 0 };
	// One actor's inputs to its particle streams, refilled per actor.
	const particleRow = createParticleRow();
	const particleBirths = new Map<
		number,
		ParticleHistory & {
			model: string;
			time: number;
			cycle: number;
			origin: number;
			bytes: number;
			graph?: ParticleGraphState;
		}
	>();
	const batches = new Map<string, {
		signature: string;
		capacity: number;
		gids: readonly number[];
		draws: GeometryDraw[];
		instances: Float32Array;
		times: Float64Array;
		palettes: Float32Array[];
		streams?: ReturnType<typeof createPaletteStreams>;
		// Emitted primitives' GPU presentation streams (particle-streams.ts).
		particles: (ParticleStream | undefined)[];
		appearances: (Float32Array | undefined)[];
		// Ribbon vertex streams by primitive, kept across frames; used is the
		// vertex count the last frame wrote.
		ribbons: (RibbonBuffers | undefined)[];
		cloth?: Map<number, ReturnType<typeof createClothVertices>>;
		poseKey?: string;
	}>();
	// ownedModels counts the owned entries of models (decoded sources); the
	// rest are borrowed or assembled views, so their count is the difference.
	let residentBytes = 0, ownedModels = 0, renderBytes = 0, deferredActors = 0;
	let retained: Set<string> | null = null, retainedScratch = new Set<string>();
	// Residency depends on model membership, not interpolated poses or clocks.
	// Snapshot the model ids: actor snapshots are mutated by the next publication.
	let residencyDirty = true, residencyPasses = 0, hasMaterialClocks = false;
	// The batches the frame's first pass planned. Its draws are recorded in
	// the frame's command buffer before the deferred continuation runs, so the
	// continuation may not release or resize them: their buffers must outlive
	// the submit. The next full pass retires what the frame left behind.
	const submitted = new Set<string>();
	// Assembly requests precede actor publication. Protect every requested
	// assembly, including cache hits, until that publication takes ownership.
	const requestedAssemblies = new Set<string>();
	// Draws of models retired outside a prepare pass (retireModels under budget
	// pressure, as in a hidden tab, which draws nothing). The next full pass
	// releases them through the geometry commands.
	let retiredDraws: GeometryDraw[] = [];
	let actors: readonly CharacterActor[] = [], disposed = false;
	const portraitSnapshots = createActorSnapshots();
	let portraits: readonly CharacterActor[] = [];
	let gpuAnimation: GeometryCommands["gpuAnimationStats"], deferPoses = false;
	// A frame may contain many instances requesting exactly the same model pose.
	// Share only identical inputs; particles retain independent mutable age samples.
	// Indexed by model, then sample time; samePoseInputs settles the rest.
	let framePoses:
		| Map<string, Map<number, { actor: CharacterActor; pose: ReturnType<typeof createCharacterPose>; }[]>>
		| null = null;
	let poseRequests = 0,
		poseEvaluations = 0,
		poseSharingHits = 0,
		poseCreations = 0,
		boneUploadBytes = 0,
		visibleActors = 0,
		frameGroups = 0;
	// CPU pose evaluations of owned evaluators that have since retired. The
	// stats add the live ones, so cpuEvaluations only grows: a measurement
	// window may subtract two readings even when peers leave or change model.
	let retiredCpuEvaluations = 0;
	// The culling sphere of the actor being tested, [x, y, z, radius]; reused.
	const sphere = new Float64Array( 4 );
	// The last main (non-preview, non-continuation) frame's culling inputs and
	// admitted actors, for the opt-in culling census only. One record, its
	// fields overwritten each main frame; disposal clears it.
	const cullFrame: {
		valid: boolean;
		frusta: Float64Array[];
		visible: CharacterActor[];
		chains: ReadonlyMap<number, readonly CharacterActor[]>;
		byGid: ReadonlyMap<number, CharacterActor>;
		origin: number;
	} = { valid: false, frusta: [], visible: [], chains: new Map(), byGid: new Map(), origin: 0 };
	/*
	================
	cullSphere

	The conservative sphere culling tests: the chain anchor's feet, with each
	model's all-clip radius scaled down the chain plus every attachment offset.
	Writes [x, y, z, radius] into out; false when the chain is not resident.
	================
	*/
	function cullSphere(
		actor: CharacterActor,
		chains: ReadonlyMap<number, readonly CharacterActor[]>,
		origin: number,
		out: Float64Array
	): boolean {
		const chain = chains.get( actor.gid );
		if ( !chain?.length || chain.some( value => !models.has( value.model ) ) ) return false;
		const anchor = chain[chain.length - 1]!;
		let radius = 0;
		for ( const value of chain ) {
			radius = (radius + models.get( value.model )!.radius) * value.scale * (value.bodyVolume ? 1.2 : 1) +
				(value.attachment ?
					hypot3( value.attachment.offset[0]!, value.attachment.offset[1]!, value.attachment.offset[2]! ) :
					0);
		}
		out[0] = anchor.pose.x + ((anchor.pose.regionId & 255) - (origin & 255)) * 1920;
		out[1] = anchor.pose.y;
		out[2] = anchor.pose.z + ((anchor.pose.regionId >>> 8) - (origin >>> 8)) * 1920;
		out[3] = radius;
		return true;
	}
	/*
	================
	posedHiddenBody

	Posed census only (stats(true, true)): whether every skinned vertex of the body's
	displayed pose, in world space, lies outside the frustum(s). null when the
	answer is unknown (no resident pose, or an unskinned primitive).
	================
	*/
	function posedHiddenBody(
		actor: CharacterActor,
		resource: { model: CharacterModel; },
		byGid: ReadonlyMap<number, CharacterActor>,
		origin: number,
		transforms: Map<number, Float32Array>,
		frusta: readonly Float64Array[]
	): boolean | null {
		// A pose retained from a replaced model cannot palette this model's primitives.
		const state = poses.get( actor.gid );
		const pose = state && state.model === actor.model ? state.pose : undefined;
		const world = transformFor( actor, byGid, origin, transforms );
		if ( !pose || !world || !frusta.length ) return null;
		const low = [ Infinity, Infinity, Infinity ], high = [ -Infinity, -Infinity, -Infinity ];
		for ( const primitive of resource.model.primitives ) {
			if ( primitive.emission ) continue;
			const geometry = primitive.geometry;
			if ( !geometry.joints || !geometry.weights ) return null;
			const palette = new Float32Array( primitive.joints.length * 16 );
			pose.palette( primitive, palette, 0 );
			const positions = geometry.positions;
			for ( let v = 0; v < positions.length / 3; v++ ) {
				const x = positions[v * 3]!, y = positions[v * 3 + 1]!, z = positions[v * 3 + 2]!;
				let sx = 0, sy = 0, sz = 0;
				for ( let k = 0; k < 4; k++ ) {
					const w = geometry.weights[v * 4 + k]!;
					if ( !w ) continue;
					const m = geometry.joints[v * 4 + k]! * 16;
					sx += w * (palette[m]! * x + palette[m + 4]! * y + palette[m + 8]! * z + palette[m + 12]!);
					sy += w * (palette[m + 1]! * x + palette[m + 5]! * y + palette[m + 9]! * z + palette[m + 13]!);
					sz += w * (palette[m + 2]! * x + palette[m + 6]! * y + palette[m + 10]! * z + palette[m + 14]!);
				}
				const wx = world[0]! * sx + world[4]! * sy + world[8]! * sz + world[12]!,
					wy = world[1]! * sx + world[5]! * sy + world[9]! * sz + world[13]!,
					wz = world[2]! * sx + world[6]! * sy + world[10]! * sz + world[14]!;
				low[0] = Math.min( low[0]!, wx );
				low[1] = Math.min( low[1]!, wy );
				low[2] = Math.min( low[2]!, wz );
				high[0] = Math.max( high[0]!, wx );
				high[1] = Math.max( high[1]!, wy );
				high[2] = Math.max( high[2]!, wz );
			}
		}
		if ( !Number.isFinite( low[0]! ) ) return null;
		// Visible when any frustum admits the box: each plane's farthest corner inside.
		return !frusta.some( planes => {
			for ( let i = 0; i < 30; i += 5 ) {
				const px = planes[i]! >= 0 ? high[0]! : low[0]!,
					py = planes[i + 1]! >= 0 ? high[1]! : low[1]!,
					pz = planes[i + 2]! >= 0 ? high[2]! : low[2]!;
				if ( planes[i]! * px + planes[i + 1]! * py + planes[i + 2]! * pz + planes[i + 3]! < 0 ) return false;
			}
			return true;
		} );
	}
	/*
	================
	cullCensus

	How much of each admitted actor's sphere lies outside the frustum: the
	distance its centre is outside the nearest violated plane, as a share of
	its radius (0 when the centre is inside). A large share means a tighter
	bound might reject it; this census proves no bound safe by itself.
	================
	*/
	function cullCensus( posed: boolean ) {
		if ( !cullFrame.valid ) return undefined;
		const { frusta, visible, chains, byGid, origin } = cullFrame;
		// The ceiling: the displayed pose's own skinned vertices, placed with the
		// renderer's transform. A body none of whose posed geometry box meets a
		// frustum is invisible in this frame. Unskinned parts or a missing pose
		// count the body as visible; cloth uses its skinned rest shape (flagged).
		let posedHidden = 0, posedUnknown = 0, posedCloth = 0;
		const transforms = new Map<number, Float32Array>();
		const outside = [ 0, 0, 0, 0 ];
		let bodies = 0,
			attachments = 0,
			centreInside = 0,
			radiusSum = 0,
			loneBodies = 0,
			activeRejected = 0,
			activeRadiusSum = 0;
		// Candidate only: the same conservative envelope restricted to the clips
		// a lone body plays now (its clip and every blend layer). Not yet a
		// safe bound: cloth displacement is not part of either radius here.
		const activeRadius = new Map<string, number>();
		for ( const actor of visible ) {
			if ( models.get( actor.model )?.plan.emission || !cullSphere( actor, chains, origin, sphere ) ) continue;
			if ( actor.attachment ) attachments++;
			else bodies++;
			radiusSum += sphere[3]!;
			const resource = models.get( actor.model )!, chain = chains.get( actor.gid )!;
			if ( chain.length === 1 ) {
				loneBodies++;
				const names = new Set( [ actor.clip, ...(actor.layers ?? []).map( layer => layer.clip ) ] );
				const key = actor.model + "|" + [ ...names ].sort().join( "|" );
				let radius = activeRadius.get( key );
				if ( radius === undefined ) {
					const clips = resource.model.clips.filter( clip => names.has( clip.name ) );
					radius = characterRadius( { ...resource.model, clips }, bounds );
					activeRadius.set( key, radius );
				}
				const scaled = radius * actor.scale * (actor.bodyVolume ? 1.2 : 1);
				activeRadiusSum += scaled;
				const admitted = !frusta.length ||
					frusta.some( planes => visibleFrustumSphere( planes, sphere[0]!, sphere[1]!, sphere[2]!, scaled ) );
				if ( !admitted ) activeRejected++;
				// Only the explicit posed census reads palettes (forcing CPU poses);
				// the ordinary details call stays non-evaluating.
				const hidden = posed ? posedHiddenBody( actor, resource, byGid, origin, transforms, frusta ) : false;
				if ( hidden === null ) posedUnknown++;
				else if ( hidden ) {
					posedHidden++;
					if ( resource.plan.cloth ) posedCloth++;
				}
			}
			// Visible when any frustum admits it: the smallest violation across frusta.
			let violation = frusta.length ? Infinity : 0;
			for ( const planes of frusta ) {
				let worst = 0;
				for ( let i = 0; i < 30; i += 5 ) {
					const distance =
						-(planes[i]! * sphere[0]! + planes[i + 1]! * sphere[1]! + planes[i + 2]! * sphere[2]! +
							planes[i + 3]!) /
						planes[i + 4]!;
					worst = Math.max( worst, distance );
				}
				violation = Math.min( violation, worst );
			}
			if ( violation <= 0 ) {
				centreInside++;
				continue;
			}
			outside[Math.min( 3, Math.floor( violation / sphere[3]! * 4 ) )]!++;
		}
		const admitted = bodies + attachments;
		return {
			admitted,
			bodies,
			attachments,
			centreInside,
			outsideShares: outside,
			meanRadius: admitted ? radiusSum / admitted : 0,
			activeRejected,
			// Mounted riders are bodies but not lone: average over the lone samples only.
			meanActiveRadius: loneBodies ? activeRadiusSum / loneBodies : 0,
			...(posed ? { posedHidden, posedUnknown, posedCloth } : {})
		};
	}
	/*
	================
	poseFor

	Reuse one actor pose evaluation across geometry, attachment and socket consumers.
	================
	*/
	function poseFor( actor: CharacterActor ) {
		poseRequests++;
		const resource = models.get( actor.model ), model = resource?.model;
		if ( !model ) return null;
		const share = framePoses && !resource!.plan.emission &&
			(!actor.animationLod || !actor.animationLod.crowded || actor.animationLod.fraction < .75);
		const candidates = share ? framePoses!.get( actor.model )?.get( actor.time ) : undefined;
		let shared: ReturnType<typeof createCharacterPose> | undefined;
		if ( candidates ) {
			for ( const candidate of candidates ) {
				if ( samePoseInputs( candidate.actor, actor ) ) {
					shared = candidate.pose;
					break;
				}
			}
		}
		if ( shared ) {
			poseSharingHits++;
			poses.set( actor.gid, { model: actor.model, pose: shared } );
			return shared;
		}
		let state = ownedPoses.get( actor.gid );
		if ( !state || state.model !== actor.model ) {
			if ( state ) retiredCpuEvaluations += state.pose.cpuEvaluations();
			state = {
				model: actor.model,
				pose: createCharacterPose( model ),
				lod: createPoseLod(),
				clips: model.clips
			};
			poseCreations++;
			probe?.characterCount( "pose-created" );
			ownedPoses.set( actor.gid, state );
		}
		// The current render plan reserves expanded pose storage before this
		// evaluator is needed. Catalog publication itself allocates no per-peer
		// scratch, including for actors deferred by the frame residency budget.
		const catalogChanged = state.clips !== model.clips;
		if ( catalogChanged ) {
			for ( let index = state.clips.length; index < model.clips.length; index++ ) {
				state.pose.admitClip( model.clips[index]! );
			}
			state.clips = model.clips;
		}
		poses.set( actor.gid, state );
		const optional = actor.animationLod?.optional && !actor.attachment && !actor.mountedOn &&
			!resource!.plan.emission;
		const mustSample = catalogChanged || state.sampled === undefined || state.clip !== actor.clip ||
			actor.animationLod?.optional === false || actor.layers?.some( layer => layer.lane === "event" ) ||
			!!actor.boneRotation;
		const lodAllowed = !actor.animationLod ||
			state.lod.sample( actor.animationLod.fraction, actor.animationLod.crowded, poseFrame );
		const interval = frameWork?.level() === 2 ? .1 : frameWork?.level() === 1 ? .05 : 0;
		if (
			optional && !mustSample && state.sampled !== undefined &&
			(frameWork?.remaining() === 0 || poseSeconds >= state.sampled && poseSeconds - state.sampled < interval)
		) {
			probe?.characterCount( "cosmetic-pose-deferred" );
			return state.pose;
		}
		// A cold evaluator contains only rest transforms. Resource replacement,
		// action entry and protected actors must initialize before any LOD gate.
		if ( !mustSample && !lodAllowed ) return state.pose;
		const started = optional && frameWork ? performance.now() : 0;
		state.pose.bodyVolume( actor.bodyVolume?.index, actor.bodyVolume?.female );
		state.pose.setBoneRotation( actor.boneRotation?.bone ?? "", actor.boneRotation?.rotation ?? null );
		if (
			state.pose.evaluate(
				actor.clip,
				actor.time,
				actor.loop,
				actor.layers,
				deferPoses && resource!.plan.sharedPalette
			)
		) poseEvaluations++;
		state.sampled = poseSeconds;
		state.clip = actor.clip;
		if ( optional && frameWork ) frameWork.spend( performance.now() - started );
		if ( share ) {
			let samples = framePoses!.get( actor.model );
			if ( !samples ) {
				samples = new Map();
				framePoses!.set( actor.model, samples );
			}
			const sameTime = samples.get( actor.time );
			if ( sameTime ) sameTime.push( { actor, pose: state.pose } );
			else samples.set( actor.time, [ { actor, pose: state.pose } ] );
		}
		return state.pose;
	}
	/*
	================
	facedMatrix

	The owner matrix with its rotation replaced by a yaw (placement's axes),
	keeping each basis column's length and the translation.
	================
	*/
	function facedMatrix( owner: Float32Array, yaw: import("@/engine/foundation/math/angles").Radians ): Float32Array {
		const faced = placement( 0, 0, owner[12]!, owner[13]!, owner[14]!, yaw );
		for ( let column = 0; column < 3; column++ ) {
			const at = column * 4, length = hypot3( owner[at]!, owner[at + 1]!, owner[at + 2]! );
			faced[at] = faced[at]! * length;
			faced[at + 1] = faced[at + 1]! * length;
			faced[at + 2] = faced[at + 2]! * length;
		}
		return faced;
	}
	/*
	================
	determinant3

	The determinant of a column-major 4x4 matrix's upper 3x3 (its basis).
	================
	*/
	function determinant3( m: ArrayLike<number> ) {
		return m[0]! * (m[5]! * m[10]! - m[9]! * m[6]!) - m[4]! * (m[1]! * m[10]! - m[9]! * m[2]!) +
			m[8]! * (m[1]! * m[6]! - m[5]! * m[2]!);
	}
	/*
	================
	holderSocket

	The actor an attached actor or rider follows, and that holder's socket
	(null when the bone is missing). Socket lookups evaluate the holder's pose.
	================
	*/
	function holderSocket(
		actor: CharacterActor,
		owner: CharacterActor,
		rows: ReadonlyMap<number, CharacterActor>
	): { holder: CharacterActor; socket: Float32Array | null; } {
		// 8D6880: a missing bone retries on the mount, otherwise the root
		// orientation is kept and the actor is still drawn. 8D4020 does
		// the same keep-the-owner-matrix step. CRTSocket_UpdateOrdinaryMatrices
		// (AB68C0) copies the parent matrix; it does not drop the child.
		let holder = owner;
		// A ground effect of a rider stands at the ride's root, not the
		// saddle; it follows the character again once dismounted.
		if ( actor.attachment?.ground && owner.mountedOn !== undefined ) {
			holder = rows.get( owner.mountedOn ) ?? owner;
		}
		let socket = actor.attachment?.root ?
			identity() :
			poseFor( holder )?.socket(
				actor.attachment?.bone ?? "saddle",
				actor.attachment?.basis === "compound"
			) ?? null;
		if ( !socket && actor.attachment && !actor.attachment.root && holder.mountedOn !== undefined ) {
			const mount = rows.get( holder.mountedOn );
			const mountSocket = mount ?
				poseFor( mount )?.socket( actor.attachment.bone, actor.attachment.basis === "compound" ) ?? null :
				null;
			if ( mount && mountSocket ) {
				holder = mount;
				socket = mountSocket;
			}
		}
		return { holder, socket };
	}
	/*
	================
	nativeAttachmentOffset

	Turn matrix (parent x socket) into the 8D6880 holder matrix in native
	space, then add the attachment offset rotated by the holder matrix.
	================
	*/
	function nativeAttachmentOffset(
		matrix: Float32Array,
		parent: Float32Array,
		attachment: NonNullable<CharacterActor["attachment"]>,
		ownerScale: number
	) {
		const [x, y, z] = attachment.offset;
		// The 8D6880 holder matrix in native space. An imported model's
		// space is Ry(PI) of native (exportGlb convPos mirrors Z, the
		// loader's __gltf_left_handed__ root adds Sx), so its root is
		// placement x Ry(PI) and a named socket is Ry(PI) x bone x
		// Sz: undo the Ry(PI) for a root, the trailing Sz for a bone.
		// An .efp program draws native coordinates through this
		// matrix as is; an imported mesh takes Ry(PI) once more after
		// the authored rotation (below).
		const columns = attachment.root ? [ 0, 2 ] : [ 2 ];
		for ( const c of columns ) {
			for ( let n = 0; n < 3; n++ ) matrix[c * 4 + n] = -matrix[c * 4 + n]!;
		}
		// 8D6AFF: binding +0x08 == 0 (an '@Bone' token) writes an
		// identity rotation over bone x root and keeps the position.
		if ( attachment.keepRotation === false && !attachment.root ) {
			for ( let c = 0; c < 3; c++ ) {
				for ( let n = 0; n < 3; n++ ) matrix[c * 4 + n] = c === n ? 1 : 0;
			}
		}
		// 8D6880 rotates the offset by the holder matrix, separately
		// from bone orientation, using character height scale C0.
		const axes = new Float32Array( 12 );
		for ( let c = 0; c < 3; c++ ) {
			const size = hypot3( parent[c * 4]!, parent[c * 4 + 1]!, parent[c * 4 + 2]! );
			if ( size ) {
				for ( let n = 0; n < 3; n++ ) axes[c * 4 + n] = parent[c * 4 + n]! / size * ownerScale;
			}
		}
		const delta = nativeModelOffset( axes, [ x, y, z ] );
		for ( let n = 0; n < 3; n++ ) matrix[12 + n]! += delta[n]!;
	}
	/*
	================
	attachedTransform

	The matrix of an actor that follows an owner: the owner's own transform,
	its socket, and the attachment's basis and offset. Null when the holder
	has no transform this frame.
	================
	*/
	function attachedTransform(
		actor: CharacterActor,
		owner: CharacterActor,
		rows: ReadonlyMap<number, CharacterActor>,
		origin: number,
		cache: Map<number, Float32Array>,
		chain: Set<number>
	): Float32Array | null {
		const { holder, socket: found } = holderSocket( actor, owner, rows );
		// A bare rider sits on the mount's saddle in the mount's resource
		// space, which carries the import adapter Sx(-1) (character.ts
		// __gltf_left_handed__); the rider's own body applies it again.
		// Cancel it once, as the native basis below does for named sockets,
		// or the rider is mirrored and culled inside out. A missing saddle
		// keeps the plain root (identity), which has no adapter to cancel.
		// Only an improper socket carries the adapter; a proper one (a model
		// built without the import root) has nothing to cancel.
		const saddled = !actor.attachment && !!found && determinant3( found ) < 0;
		const socket = found ?? identity();
		const owned = transformFor( holder, rows, origin, cache, chain );
		if ( !owned ) return null;
		// A root attachment with a fixed facing keeps its owner's position and
		// scale, not its rotation: 8D5440 copies the caster's matrix at spawn.
		const facing = actor.attachment?.root ? actor.attachment.facing : undefined;
		const parent = facing === undefined ? owned : facedMatrix( owned, facing );
		let matrix = new Float32Array( 16 );
		multiply( parent, socket, matrix );
		if ( saddled ) {
			for ( let n = 0; n < 3; n++ ) matrix[n] = -matrix[n]!;
		}
		if ( actor.attachment ) {
			const [x, y, z] = actor.attachment.offset;
			if ( actor.attachment.basis === "bsr" ) {
				matrix = bsrParticleAttachment(
					parent,
					socket,
					!!actor.attachment.root,
					actor.attachment.offset,
					owner.scale,
					actor.attachment.modelScale ?? owner.scale,
					actor.attachment.rotation
				);
			} else if ( actor.attachment.basis === "native" || actor.attachment.basis === "native-bsr" ) {
				nativeAttachmentOffset( matrix, parent, actor.attachment, owner.scale );
			} else {
				for ( let n = 0; n < 3; n++ ) {
					matrix[12 + n]! += matrix[n]! * x + matrix[4 + n]! * y + matrix[8 + n]! * z;
				}
			}
		}
		return matrix;
	}
	/*
	================
	rotatedEffect

	matrix x the producer's single-axis effect rotation, as a new matrix.
	================
	*/
	function rotatedEffect(
		matrix: Float32Array,
		rotation: NonNullable<CharacterActor["effectRotation"]>
	): Float32Array {
		const { axis, angle } = rotation,
			c = Math.fround( Math.cos( angle ) ),
			s = Math.fround( Math.sin( angle ) ),
			local = identity();
		if ( axis === "z" ) {
			local[0] = c;
			local[1] = s;
			local[4] = -s;
			local[5] = c;
		} else if ( axis === "y" ) {
			local[0] = c;
			local[2] = s;
			local[8] = -s;
			local[10] = c;
		} else {
			local[5] = c;
			local[6] = s;
			local[9] = -s;
			local[10] = c;
		}
		const out = new Float32Array( 16 );
		multiply( matrix, local, out );
		return out;
	}
	/*
	================
	transformFor

	Compose mount and attachment transforms before applying the actor placement.
	================
	*/
	function transformFor(
		actor: CharacterActor,
		rows: ReadonlyMap<number, CharacterActor>,
		origin: number,
		cache: Map<number, Float32Array>,
		chain?: Set<number>
	): Float32Array | null {
		const cached = cache.get( actor.gid );
		if ( cached ) return cached;
		if ( chain && (chain.has( actor.gid ) || chain.size >= MAX_ATTACHMENT_DEPTH) ) {
			throw new Error( "Cyclic or excessive character attachment" );
		}
		let matrix: Float32Array;
		const ownerId = actor.attachment?.gid ?? actor.mountedOn;
		// 777F60 binds a ride without resolving its vehicle, and 85E000 falls
		// back to the rider when 85D870 finds none: a rider whose vehicle has
		// no drawn row (not spawned yet, or its model still loading) stands
		// on its own placement instead of vanishing until the vehicle draws.
		const rider = !actor.attachment && actor.mountedOn !== undefined;
		if ( ownerId !== undefined && !(rider && !rows.has( ownerId )) ) {
			// The owner left this frame. Native deco and CRT updates run on a
			// live object; there is no matrix to inherit.
			const owner = rows.get( ownerId );
			if ( !owner ) return null;
			// Roots and frame-cache hits do not traverse a hierarchy. Allocate
			// cycle tracking only when this query actually follows an owner.
			chain ??= new Set<number>();
			chain.add( actor.gid );
			const attached = attachedTransform( actor, owner, rows, origin, cache, chain );
			if ( !attached ) return null;
			matrix = attached;
		} else {
			// An attached actor takes its owner's matrix; its own placement
			// would only be allocated, calculated and immediately discarded.
			matrix = placement( actor.pose.regionId, origin, actor.pose.x, actor.pose.y, actor.pose.z, actor.pose.yaw );
		}
		if ( actor.absoluteEffectScale && actor.attachment ) {
			for ( let c = 0; c < 3; c++ ) {
				const size = hypot3( matrix[c * 4]!, matrix[c * 4 + 1]!, matrix[c * 4 + 2]! );
				if ( size ) { for ( let n = 0; n < 3; n++ ) matrix[c * 4 + n]! /= size; }
			}
		}
		// The producer states the exact world basis for this model's own
		// space: native for an .efp program, importedModelBasis for a mesh.
		if ( actor.effectBasis ) {
			for ( let c = 0; c < 3; c++ ) {
				for ( let r = 0; r < 3; r++ ) matrix[c * 4 + r] = actor.effectBasis[c * 3 + r]!;
			}
		}
		if ( actor.effectRotation ) matrix = rotatedEffect( matrix, actor.effectRotation );
		// 8D9EC0 rotates a stage object in its native space (Rotation x
		// Attach). An imported mesh then leaves native space for its own
		// Ry(PI) model space: M = holder x rotation x Ry(PI).
		if ( actor.attachment?.basis === "native-bsr" ) {
			for ( const c of [ 0, 2 ] ) {
				for ( let n = 0; n < 3; n++ ) matrix[c * 4 + n] = -matrix[c * 4 + n]!;
			}
		}
		for ( let n = 0; n < 12; n++ ) matrix[n]! *= actor.scale;
		cache.set( actor.gid, matrix );
		return matrix;
	}
	// ============================================================================
	//
	// Frame preparation. prepare() runs these phases in order: sample the
	// frame's actors, retire residency, advance particle emitters, select the
	// visible actors, plan batches within the render budget, evaluate the
	// needed poses, then build each planned batch's draws.
	//
	// ============================================================================
	type ModelResource = NonNullable<ReturnType<typeof models.get>>;
	type CharacterBatch = NonNullable<ReturnType<typeof batches.get>>;
	type CharacterChains = ReturnType<typeof hierarchy.update>["chains"];
	/*
	================
	PrepareFrame

	One prepare() call's inputs and the frame-local state its phases share.
	================
	*/
	interface PrepareFrame {
		readonly geometry: GeometryCommands;
		readonly images: ImageCommands;
		readonly origin: number;
		readonly view: Float32Array | undefined;
		readonly preview: boolean;
		readonly seconds: number;
		readonly continuation: boolean;
		readonly dynamicAnimation: boolean;
		readonly frameActors: readonly CharacterActor[];
		// The emission cycle of each clocked actor (sampleFrameActors).
		readonly cycles: ReadonlyMap<number, number>;
		readonly byGid: ReadonlyMap<number, CharacterActor>;
		readonly chains: CharacterChains;
		// Frame-local actor matrices (transformFor's cache).
		readonly transforms: Map<number, Float32Array>;
		readonly opacity: ( actor: CharacterActor ) => number;
		readonly output: GeometryDraw[];
	}
	/*
	================
	GroupFrame

	One planned batch while its draws are built: its rows, model and storage.
	================
	*/
	interface GroupFrame {
		readonly id: string;
		readonly rows: CharacterActor[];
		readonly resource: ModelResource;
		readonly model: CharacterModel;
		readonly plan: ModelResource["plan"];
		readonly fading: boolean;
		readonly modifierClocks: ReturnType<typeof materialClocks.get> | undefined;
		readonly batch: CharacterBatch;
		readonly capacity: number;
		instancesChanged: boolean;
		// Borrow the renderer's scratch for this synchronous group only.
		// Undefined when the group never fades / carries no lights.
		streamsPrepared: boolean;
		opacities: Float32Array | undefined;
		pointLights: Float32Array | undefined;
	}
	/*
	================
	PrimitiveRows

	Per-row instance streams one primitive's draw consumes.
	================
	*/
	interface PrimitiveRows {
		readonly instances: Float32Array;
		readonly appearance: Float32Array | undefined;
		readonly pointLights: Float32Array | undefined;
	}
	/*
	================
	sampleFrameActors

	The actors this pass draws: deferred-particle samples applied, and clocked
	loops reduced to their cycle-local time.
	================
	*/
	function sampleFrameActors( continuation: boolean ) {
		// A stopped emitter receives its final cycle-local age. Preserve that
		// cycle's birth transforms while particles drain; changing loop=false
		// must not reseed the trail at the projectile's arrival point.
		// Ordinary skinned actors have no emitter/material cycle ownership.
		// Their unmodified animation time already drives pose evaluation.
		const cycles = new Map<number, number>();
		const sampledActors = hasDeferred ?
			actors.map( source => {
				const sample = source.deferredParticle ? deferred.sample( source.gid ) : undefined;
				return sample ?
					{
						...source,
						deferredParticle: !sample.deferred && sample.draw ? undefined : source.deferredParticle,
						time: sample.draw ? sample.time + deferred.pendingSeconds() : sample.time,
						opacity: (!sample.deferred || continuation) && sample.draw ?
							(source.opacity ?? 1) * sample.instanceAlpha / 255 :
							0
					} :
					source;
			} ) :
			actors;
		const frameActors = sampledActors.map( actor => {
			const plan = models.get( actor.model )?.plan;
			if ( plan?.sharedPalette && !plan.clocked ) return actor;
			const duration = plan?.continuousGraph ? undefined : plan?.clips.get( actor.clip )?.duration;
			if ( duration ) {
				cycles.set(
					actor.gid,
					actor.loop ? Math.floor( actor.time / duration ) : particleBirths.get( actor.gid )?.cycle ?? 0
				);
			}
			return actor.loop && duration && plan?.clocked ? { ...actor, time: actor.time % duration } : actor;
		} );
		return { frameActors, cycles };
	}
	/*
	================
	retireModels

	Drop the models and assemblies no retained, published or portrait actor
	uses. CPU bookkeeping only: their batches' draws wait in retiredDraws for
	the next full prepare pass, so this may run between frames.
	================
	*/
	function retireModels( inUse: readonly CharacterActor[], pending: readonly string[] = [] ) {
		if ( !retained ) return;
		const keep = residencyKeep( inUse, pending );
		for ( const [id, resource] of models ) {
			if ( keep.has( id ) ) continue;
			const batch = batches.get( id );
			if ( batch ) retiredDraws.push( ...batch.draws );
			batches.delete( id );
			forgetModel( id, resource );
		}
	}
	/*
	================
	residencyKeep

	The retained, published and portrait models, their dependencies, and
	the sources a caller is about to use.
	================
	*/
	function residencyKeep( inUse: readonly CharacterActor[], pending: readonly string[] = [] ) {
		const keep = new Set( [
			...(retained ?? []),
			...requestedAssemblies,
			...pending,
			...inUse.map( actor => actor.model ),
			...portraits.map( actor => actor.model )
		] );
		for ( const id of keep ) {
			for ( const dependency of models.get( id )?.dependencies ?? [] ) {
				keep.add( dependency );
			}
		}
		return keep;
	}
	/*
	================
	forgetModel

	Drop one model's CPU residency: owned images close and the counts fall.
	================
	*/
	function forgetModel( id: string, resource: NonNullable<ReturnType<typeof models.get>> ) {
		if ( resource.owned ) {
			for ( const image of resource.images ) {
				if ( !("kind" in image) ) image.close();
			}
			ownedModels--;
		}
		models.delete( id );
		residentBytes -= resource.bytes;
	}
	/*
	================
	relieveResidency

	A model or assembly at its budget first retires what nothing uses. A
	hidden tab runs no prepare pass (runtime.ts draws only while visible)
	yet keeps admitting the actors it presents, so without this the budget
	filled during a long background session and the runtime failed
	(BUG-070). The GPU half waits for the next visible frame. pending names
	the sources the caller is about to use, which this step's retain may not
	list yet.
	================
	*/
	function relieveResidency( pending: readonly string[] = [] ) {
		retireModels( actors, pending );
		residencyDirty = true;
	}
	/*
	================
	retireResidency

	Release models, batches and textures no retained or framed actor uses.
	================
	*/
	function retireResidency(
		geometry: GeometryCommands,
		images: ImageCommands,
		frameActors: readonly CharacterActor[]
	) {
		residencyPasses++;
		// A draw leaves the list only once released, so a failed release is
		// retried by the next pass.
		while ( retiredDraws.length ) {
			geometry.release( retiredDraws[retiredDraws.length - 1]! );
			retiredDraws.pop();
		}
		if ( retained ) {
			const keep = residencyKeep( frameActors );
			for ( const [id, resource] of models ) {
				if ( keep.has( id ) ) continue;
				// Release before forgetting the model, so a failed release
				// leaves it resident for the retry.
				for ( const draw of batches.get( id )?.draws ?? [] ) {
					geometry.release( draw );
				}
				batches.delete( id );
				forgetModel( id, resource );
			}
		}
		const liveImages = new Set( [ ...models.values() ].flatMap( resource => resource.images ) );
		for ( const [bitmap, draw] of textures ) {
			if ( !liveImages.has( bitmap ) ) {
				images.release( draw );
				textures.delete( bitmap );
			}
		}
		hasMaterialClocks = frameActors.some( actor => models.get( actor.model )?.plan.materialClocked );
		if ( !hasMaterialClocks ) materialClocks.reset();
		residencyDirty = false;
	}
	/*
	================
	actorOpacity

	The frame's opacity rule. A fade reaches the owner's model parts, not
	its effects (character-fade.ts).
	================
	*/
	function actorOpacity( byGid: ReadonlyMap<number, CharacterActor> ) {
		const opacity = ( actor: CharacterActor ): number => {
			const parent = actor.attachment ? byGid.get( actor.attachment.gid ) : undefined;
			if ( !parent ) return actor.opacity ?? 1;
			return attachedOpacity(
				actor.opacity ?? 1,
				opacity( parent ),
				// An effect's own emitters share its alpha; a model fade stops at the effect.
				!actor.effectEntity || !!parent.effectEntity
			);
		};
		return opacity;
	}
	/*
	================
	particleHistory

	The actor's retained emission history, restarted when its model, time or
	emission cycle moves backwards or changes.
	================
	*/
	function particleHistory( frame: PrepareFrame, actor: CharacterActor, model: CharacterModel, storage: number ) {
		let history = particleBirths.get( actor.gid );
		if (
			!history || history.model !== actor.model || actor.time < history.time ||
			history.cycle !== (frame.cycles.get( actor.gid ) ?? 0)
		) {
			history = {
				model: actor.model,
				time: actor.time,
				cycle: frame.cycles.get( actor.gid ) ?? 0,
				origin: frame.origin,
				bytes: storage,
				graph: model.particleGraph ?
					createParticleGraph( model.particleGraph, particleRandom.index ) :
					undefined,
				programs: model.primitives.map( () => [] ),
				matrices: model.primitives.map( p =>
					p.emission ?
						new Float32Array( (p.emission.capacity ?? p.emission.births.length) * 16 ).fill( NaN ) :
						undefined
				)
			};
			particleBirths.set( actor.gid, history );
		}
		return history;
	}
	/*
	================
	advanceGraphEmitter

	Advance a particle-graph emitter to the actor's time and rebuild its
	ribbon primitives' element frames.
	================
	*/
	function advanceGraphEmitter(
		actor: CharacterActor,
		model: CharacterModel,
		history: ParticleHistory & { graph?: ParticleGraphState; },
		transform: Float32Array,
		shift: readonly [number, number]
	) {
		const graph = history.graph!;
		advanceParticleGraph(
			graph,
			model.particleGraph!,
			actor.time,
			transform,
			particleRandom.table,
			actor.emissionEnd,
			shift[0],
			shift[1],
			actor.loop
		);
		particleRandom.index = graph.index;
		// Graph ribbons are strips through their elements' drawn
		// frames; other graph primitives are drawn from their tick
		// records by the GPU pass (particle-streams.ts).
		for ( let p = 0; p < model.primitives.length; p++ ) {
			const emitter = model.primitives[p]!.particleEmitter, matrices = history.matrices[p];
			if ( emitter === undefined || !matrices || !model.primitives[p]!.ribbon ) continue;
			const elements = graph.elements[emitter]!;
			matrices.fill( NaN );
			for ( let b = 0; b < elements.length; b++ ) {
				const element = elements[b];
				if ( !element?.alive ) continue;
				particleElementMatrix(
					element,
					matrices,
					b * 16,
					actor.time * PARTICLE_TICKS_PER_SECOND - graph.frame,
					rotationWork
				);
			}
		}
	}
	/*
	================
	advanceBirthEmitter

	Shift live birth transforms with the origin and record new births at the
	actor's transform, initializing their particle programs.
	================
	*/
	function advanceBirthEmitter(
		actor: CharacterActor,
		model: CharacterModel,
		history: ParticleHistory,
		transform: Float32Array,
		shift: readonly [number, number]
	) {
		const [dx, dz] = shift;
		for ( let p = 0; p < model.primitives.length; p++ ) {
			const emission = model.primitives[p]!.emission, matrices = history.matrices[p];
			if ( !emission || !matrices ) continue;
			for ( let b = 0; b < emission.births.length; b++ ) {
				if ( actor.emissionEnd !== undefined && emission.births[b]! >= actor.emissionEnd ) continue;
				const offset = b * 16;
				if ( Number.isFinite( matrices[offset + 15] ) ) {
					matrices[offset + 12]! += dx;
					matrices[offset + 14]! += dz;
				} else if (
					actor.time >= emission.births[b]! &&
					actor.time - emission.births[b]! < emission.lifetime
				) {
					matrices.set( transform, offset );
					const program = model.primitives[p]!.particleProgram;
					if ( program ) {
						const sample = initializeParticle(
							program,
							particleRandom.table,
							particleRandom.index,
							transform
						);
						particleRandom.index = sample.index;
						history.programs[p]![b] = sample.state;
					}
				}
				if ( emission.follow ) matrices.set( transform, offset );
			}
		}
	}
	/*
	================
	advanceParticles

	Admit emitters within the render budget and advance their histories.
	Returns the admitted emitters, the poses they need and their bytes.
	================
	*/
	function advanceParticles( frame: PrepareFrame ) {
		const { byGid, chains, origin, continuation } = frame;
		const particleNeeded = new Set<number>(), particleAccepted = new Set<number>();
		for ( const [gid, history] of particleBirths ) {
			if ( byGid.get( gid )?.model !== history.model || !models.has( history.model ) ) {
				particleBirths.delete( gid );
			}
		}
		let particleBytes = [ ...particleBirths.values() ].reduce( ( sum, h ) => sum + h.bytes, 0 );
		for ( const actor of frame.frameActors ) {
			if ( actor.deferredParticle && (!continuation || !deferred.sample( actor.gid )?.draw) ) continue;
			const resource = models.get( actor.model ), model = resource?.model, chain = chains.get( actor.gid )!;
			if (
				!model || !resource!.plan.emission || !chain.length || chain.some( a => !models.has( a.model ) )
			) continue;
			const storage = resource!.plan.particleBytes;
			const bytes = Math.max( 0, storage - (particleBirths.get( actor.gid )?.bytes ?? 0) ) +
				chain.reduce(
					( sum, a ) => sum + (particleNeeded.has( a.gid ) ? 0 : models.get( a.model )!.plan.poseBytes),
					0
				);
			if ( particleBytes + bytes > CHARACTER_RENDER_BYTES ) continue;
			particleBytes += bytes;
			particleAccepted.add( actor.gid );
			for ( const a of chain ) particleNeeded.add( a.gid );
			const history = particleHistory( frame, actor, model, storage );
			const transform = transformFor( actor, byGid, origin, frame.transforms );
			if ( !transform ) continue;
			const shift = [
				((history.origin & 255) - (origin & 255)) * 1920,
				((history.origin >>> 8) - (origin >>> 8)) * 1920
			] as const;
			if ( history.graph && model.particleGraph ) {
				advanceGraphEmitter( actor, model, history, transform, shift );
			} else {
				advanceBirthEmitter( actor, model, history, transform, shift );
			}
			history.origin = origin;
			history.time = actor.time;
		}
		// Deferred draws retain bounded simulation history; only actor retirement releases it.
		return { accepted: particleAccepted, needed: particleNeeded, bytes: particleBytes };
	}
	/*
	================
	selectVisible

	The actors with any opacity whose culling sphere meets a view, plus the
	admitted emitters. Records the census frame and the deferred set.
	================
	*/
	function selectVisible(
		frame: PrepareFrame,
		accepted: ReadonlySet<number>,
		reflectedView: Float32Array | undefined
	) {
		const { chains, origin, continuation } = frame, opacity = frame.opacity;
		const frusta = [ frame.view, reflectedView ].filter( ( v ): v is Float32Array => !!v ).map(
			prepareViewFrustum
		);
		const visible = frame.frameActors.filter( actor => {
			if ( opacity( actor ) <= 0 ) return false;
			if ( models.get( actor.model )?.plan.emission ) return accepted.has( actor.gid );
			if ( !cullSphere( actor, chains, origin, sphere ) ) return false;
			return !frusta.length ||
				frusta.some( frustum =>
					visibleFrustumSphere( frustum, sphere[0]!, sphere[1]!, sphere[2]!, sphere[3]! )
				);
		} );
		// The census (stats(true)) reads the last main frame's admission.
		if ( !continuation && !frame.preview ) {
			cullFrame.valid = true;
			cullFrame.frusta = frusta;
			cullFrame.visible = visible;
			cullFrame.chains = chains;
			cullFrame.byGid = frame.byGid;
			cullFrame.origin = origin;
		}
		if ( hasDeferred && !continuation ) deferredVisible = new Set( visible.map( actor => actor.gid ) );
		return visible;
	}
	/*
	================
	retainedDeferred

	Whether the first pass keeps this batch for the deferred continuation.
	================
	*/
	function retainedDeferred( frame: PrepareFrame, batch: { gids: readonly number[]; } ) {
		return !frame.continuation && hasDeferred &&
			batch.gids.some( gid => !!frame.byGid.get( gid )?.deferredParticle );
	}
	/*
	================
	batchKey

	The batch an actor joins: its model, plus a variant for every state that
	needs its own draws (cloth, deferral, fade, tint, light, glow, modifiers).
	================
	*/
	function batchKey( frame: PrepareFrame, actor: CharacterActor, plan: ModelResource["plan"] ) {
		const opacity = frame.opacity;
		const variant = (plan.cloth ?
			"\0cloth:" + actor.gid :
			"") +
			(actor.deferredParticle ? "\0deferred" : "") +
			(opacity( actor ) < 1 ? "\0fade" : "") + (actor.materialTint ? "\0tint" : "") +
			(actor.pointLight ? "\0light" : "") +
			(plan.equipmentGlow ?
				"\0glow:" + ((actor.animationLod?.fraction ?? 0) <= .5 && opacity( actor ) === 1) :
				"") +
			(hasMaterialClocks && materialClocks.get( actor ) ?
				"\0modifier:" + actor.gid + (plan.animationMaterial ?
					":" + (actor.modelAnimation?.revision ?? 0) + ":" +
					((actor.animationLod?.fraction ?? 0) > .5) :
					"") :
				"");
		if ( !variant ) return actor.model;
		let cachedKey = batchKeys.get( actor );
		if ( !cachedKey || cachedKey.model !== actor.model || cachedKey.variant !== variant ) {
			cachedKey = { model: actor.model, variant, key: actor.model + variant };
			batchKeys.set( actor, cachedKey );
		}
		return cachedKey.key;
	}
	/*
	================
	planBatches

	Plan the complete frame before creating poses, arrays, or GPU resources:
	group the visible actors by batch until the render budget is spent.
	================
	*/
	function planBatches(
		frame: PrepareFrame,
		visible: readonly CharacterActor[],
		particles: { needed: ReadonlySet<number>; bytes: number; }
	) {
		visibleActors = visible.length;
		// A rejected actor leaves capacity available for cheaper frameActors that follow it.
		const grouped = new Map<string, CharacterActor[]>(), needed = new Set<number>( particles.needed );
		renderBytes = particles.bytes + materialClocks.bytes();
		if ( !frame.continuation && hasDeferred ) {
			for ( const batch of batches.values() ) {
				if ( retainedDeferred( frame, batch ) ) {
					const actor = frame.byGid.get( batch.gids[0]! );
					if ( actor ) renderBytes += models.get( actor.model )!.plan.batchBytes( batch.gids.length );
				}
			}
		}
		deferredActors = 0;
		for ( const actor of visible ) {
			if ( actor.drawGeometry === false ) continue;
			const plan = models.get( actor.model )!.plan, dependencies = frame.chains.get( actor.gid )!;
			const key = batchKey( frame, actor, plan );
			const rows = grouped.get( key ) ?? [];
			const extra = plan.batchBytes( rows.length + 1 ) - plan.batchBytes( rows.length ) +
				dependencies.reduce(
					( bytes, value ) =>
						bytes + (needed.has( value.gid ) ? 0 : models.get( value.model )!.plan.poseBytes),
					0
				);
			if ( !Number.isSafeInteger( extra ) || extra < 0 || renderBytes + extra > CHARACTER_RENDER_BYTES ) {
				deferredActors++;
				continue;
			}
			renderBytes += extra;
			rows.push( actor );
			grouped.set( key, rows );
			for ( const value of dependencies ) {
				needed.add( value.gid );
			}
		}
		return { grouped, needed };
	}
	/*
	================
	retireUnplanned

	Retire all obsolete storage before allocating the replacement frame:
	batches the plan resized or dropped, and poses no planned row needs.
	================
	*/
	function retireUnplanned(
		frame: PrepareFrame,
		grouped: ReadonlyMap<string, CharacterActor[]>,
		needed: ReadonlySet<number>
	) {
		for ( const [id, batch] of batches ) {
			if ( retainedDeferred( frame, batch ) || frame.continuation && submitted.has( id ) ) continue;
			const rows = grouped.get( id );
			const capacity = rows ? models.get( rows[0]!.model )!.plan.capacity( rows.length ) : 0;
			if (
				!rows || batch.capacity !== capacity || !batch.signature.startsWith( String( frame.preview ) + ":" )
			) {
				for ( const draw of batch.draws ) {
					frame.geometry.release( draw );
				}
				batches.delete( id );
			}
		}
		for ( const gid of poses.keys() ) {
			if ( !needed.has( gid ) ) {
				poses.delete( gid );
			}
		}
		for ( const [gid, state] of ownedPoses ) {
			if ( !needed.has( gid ) ) {
				retiredCpuEvaluations += state.pose.cpuEvaluations();
				ownedPoses.delete( gid );
				probe?.characterCount( "pose-retired" );
			}
		}
	}
	/*
	================
	countPlan

	The plan's frame-probe counts.
	================
	*/
	function countPlan(
		frame: PrepareFrame,
		visible: readonly CharacterActor[],
		needed: ReadonlySet<number>,
		particleNeeded: ReadonlySet<number>
	) {
		probe?.characterMark( "character-plan" );
		// Visibility includes admitted emitters, whose particles can outlive an
		// off-screen source. Keep this distinct from requested pose storage.
		probe?.characterCount( "character-candidates", frame.frameActors.length );
		probe?.characterCount( "character-visible-candidates", visible.length );
		probe?.characterCount( "character-needed-poses", needed.size );
		probe?.characterCount( "character-particle-needed-poses", particleNeeded.size );
		if ( probe ) {
			let bodies = 0;
			for ( const actor of visible ) {
				if ( !models.get( actor.model )?.plan.emission ) bodies++;
			}
			probe?.characterCount( "character-visible-bodies", bodies );
		}
	}
	/*
	================
	evaluateNeededPoses

	Evaluate every needed actor's pose. Oldest cosmetic samples get the next
	budget slice. Admission and draw order remain unchanged; a busy crowd
	cannot starve its tail.
	================
	*/
	function evaluateNeededPoses( frameActors: readonly CharacterActor[], needed: ReadonlySet<number> ) {
		poseOrder.length = 0;
		poseOrder.push( ...frameActors );
		if ( frameWork ) {
			poseOrder.sort( ( a, b ) =>
				Number( !!a.animationLod?.optional ) - Number( !!b.animationLod?.optional ) ||
				(ownedPoses.get( a.gid )?.sampled ?? -1) - (ownedPoses.get( b.gid )?.sampled ?? -1)
			);
		}
		for ( const actor of poseOrder ) {
			if ( !needed.has( actor.gid ) ) {
				continue;
			}
			const model = models.get( actor.model )?.model;
			if ( !model ) {
				continue;
			}
			poseFor( actor );
		}
		probe?.characterMark( "character-poses" );
	}
	/*
	================
	actorTransform

	Resolve the frame-local actor matrix after hierarchy evaluation.
	================
	*/
	function actorTransform( frame: PrepareFrame, actor: CharacterActor ): Float32Array | null {
		// Null only when the owner actor is gone. A missing bone is
		// the root matrix, so it must not zero this instance.
		const matrix = transformFor( actor, frame.byGid, frame.origin, frame.transforms );
		if ( !matrix ) return null;
		if ( snapshots.index.get( actor.gid )?.deferredParticle ) {
			let row = particleSnapshots.get( actor.gid );
			if ( !row ) {
				row = { matrix: matrix.slice(), regionId: frame.origin };
				particleSnapshots.set( actor.gid, row );
			} else {
				row.matrix.set( matrix );
				row.regionId = frame.origin;
			}
		}
		return matrix;
	}
	/*
	================
	releaseUngrouped

	Release batches the plan left without rows, except the ones this frame
	still needs (retained deferred batches, and the first pass's submits).
	================
	*/
	function releaseUngrouped( frame: PrepareFrame, grouped: ReadonlyMap<string, CharacterActor[]> ) {
		for ( const [id, batch] of batches ) {
			if (
				!grouped.has( id ) && !retainedDeferred( frame, batch ) && !(frame.continuation && submitted.has( id ))
			) {
				for ( const draw of batch.draws ) {
					frame.geometry.release( draw );
				}
				batches.delete( id );
			}
		}
	}
	// ============================================================================
	//
	// Batch draws. prepareGroup builds one planned batch: its storage, its pose
	// key (an unchanged key reuses last frame's draws), its rows' instances and
	// palettes, then one draw per primitive.
	//
	// ============================================================================
	/*
	================
	updateModifiers

	Apply the batch's material clocks (glow, colors, texture motion, pulse)
	to one primitive's draw. initial forces every clocked value.
	================
	*/
	function updateModifiers(
		geometry: GeometryCommands,
		group: GroupFrame,
		draw: GeometryDraw,
		index: number,
		initial = false
	) {
		const clock = group.modifierClocks?.[index];
		if ( !clock ) return;
		const glow = group.model.primitives[index]!.equipmentGlow;
		if ( glow && clock.glow ) {
			geometry.updateEquipmentGlow(
				draw,
				clock.glow.color,
				clock.glow.uv,
				glow.gain,
				glow.alphaTest,
				!group.fading && (group.rows[0]!.animationLod?.fraction ?? 0) <= .5
			);
		}
		if ( clock.colors ) {
			for ( const color of clock.colors ) {
				geometry.updateMaterialColors( draw, color.rgb, color.flags );
			}
		}
		if ( clock.color && (initial || clock.colorChanged) ) {
			geometry.updateMaterialColors( draw, clock.color.rgb, clock.flags );
		}
		if ( clock.texture && (initial || clock.textureChanged) ) {
			geometry.updateTextureTransform( draw, clock.texture.matrix );
		}
		if ( clock.pulse && (initial || clock.pulseChanged) ) {
			geometry.updateTextureFactor( draw, clock.pulse.factor );
		}
	}
	/*
	================
	residentTextures

	Upload a model's images once; later batches share the draws.
	================
	*/
	function residentTextures( resource: ModelResource, images: ImageCommands ) {
		if ( resource.textures.length ) return;
		resource.textures = resource.images.map( image => {
			let draw = textures.get( image );
			if ( !draw ) {
				draw = images.upload( image );
				textures.set( image, draw );
			}
			return draw;
		} );
	}
	/*
	================
	admitBatch

	The batch's storage for these rows. Visibility changes active rows, not
	immutable mesh identity: geometry stays within a capacity band, the budget
	charges the padded storage, and GPU submission uses only live rows.
	================
	*/
	function admitBatch(
		frame: PrepareFrame,
		id: string,
		rows: readonly CharacterActor[],
		resource: ModelResource
	) {
		const { model, plan } = resource;
		const signature = String( frame.preview ) + ":" + rows.map( row => row.gid ).join( "," );
		const capacity = plan.capacity( rows.length );
		let batch = batches.get( id );
		const membershipChanged = batch?.signature !== signature;
		if (
			!batch || batch.capacity !== capacity || !batch.signature.startsWith( String( frame.preview ) + ":" )
		) {
			if ( batch ) {
				for ( const draw of batch.draws ) {
					frame.geometry.release( draw );
				}
			}
			const streams = plan.sharedPalette ?
				createPaletteStreams( model, capacity ) :
				undefined;
			batch = {
				signature,
				capacity,
				gids: rows.map( row => row.gid ),
				draws: [],
				instances: new Float32Array( capacity * 16 ),
				times: new Float64Array( capacity ).fill( NaN ),
				streams,
				palettes: streams ?
					streams.streams.map( s => s.data ) :
					model.primitives.map( p =>
						new Float32Array(
							capacity * (p.emission?.capacity ?? p.emission?.births.length ?? 1) *
								p.joints.length * 16
						)
					),
				particles: model.primitives.map( ( p, index ) =>
					p.emission && !p.ribbon ? createParticleStream( model, index, capacity ) : undefined
				),
				appearances: model.primitives.map( p =>
					!p.emission && (p.materialFrames || rows[0]!.materialTint) ?
						new Float32Array( capacity * 8 ) :
						undefined
				),
				ribbons: []
			};
			batches.set( id, batch );
		}
		if ( membershipChanged ) {
			batch.signature = signature;
			batch.gids = rows.map( row => row.gid );
			batch.poseKey = undefined;
		}
		return { batch, capacity, membershipChanged };
	}
	/*
	================
	batchPoseKey

	Everything the batch's draws depend on, serialized; undefined when it
	must rebuild anyway. A changing sampled time already proves the full key
	differs, so the actor and attachment graphs are not serialized to find it.
	Cloth advances on frame time even when its skeletal pose is unchanged.
	================
	*/
	function batchPoseKey( frame: PrepareFrame, group: GroupFrame ): string | undefined {
		const { rows, plan, batch } = group, { origin, preview, view } = frame, opacity = frame.opacity;
		const billboard = plan.billboard;
		if ( billboard && !view ) throw new Error( "Missing effect camera basis" );
		const clocked = plan.clocked;
		const timeChanged = batch.draws.length > 0 &&
			rows.some( ( actor, i ) =>
				actor.time !== batch.times[i] && (clocked || plan.clips.get( actor.clip )?.channels.length)
			);
		if ( timeChanged || plan.cloth ) return undefined;
		return JSON.stringify( [
			origin,
			preview,
			(billboard || preview) ? Array.from( view! ) : null,
			...rows.map(
				actor => [
					frame.cycles.get( actor.gid ),
					actor.pose,
					actor.drawGeometry,
					actor.effectBasis,
					actor.effectRotation,
					actor.scale,
					actor.bodyVolume,
					actor.absoluteEffectScale,
					actor.materialTint,
					actor.pointLight,
					opacity( actor ),
					actor.emissionEnd,
					actor.clip,
					clocked || plan.clips.get( actor.clip )?.channels.length ? actor.time : 0,
					actor.loop,
					actor.layers,
					actor.boneRotation,
					actor.attachment,
					frame.chains.get( actor.gid )!.slice( 1 )
				]
			)
		] );
	}
	/*
	================
	writeBatchRows

	Publish the rows' poses: shared palette streams, then each row's instance
	matrix and its own palettes. Sets group.instancesChanged when a row moved.
	================
	*/
	function writeBatchRows( frame: PrepareFrame, group: GroupFrame ) {
		const { rows, model, plan, batch } = group, geometry = frame.geometry, view = frame.view;
		batch.streams?.update(
			rows.map( actor => {
				const state = poses.get( actor.gid );
				if ( !state || state.model !== actor.model ) throw Error( "Missing prepared character pose" );
				return state.pose;
			} ),
			// Cloth reads the palette on the CPU. Keep canonical bindings shared
			// with the body, but never hand this storage to GPU-only sampling.
			plan.cloth ? undefined : geometry.prepareGpuBones,
			model
		);
		for ( let i = 0; i < rows.length; i++ ) {
			const actor = rows[i]!;
			// The needed-pose phase evaluates every admitted actor.
			// Upload consumes that result; it must not sample/validate
			// the same animation request a second time per actor.
			const state = poses.get( actor.gid );
			if ( !state || state.model !== actor.model ) throw Error( "Missing prepared character pose" );
			const transform = actorTransform( frame, actor );
			if ( !transform ) {
				batch.instances.fill( 0, i * 16, i * 16 + 16 );
				group.instancesChanged = true;
				continue;
			}
			let moved = false;
			for ( let n = 0; n < 16; n++ ) {
				if ( !Object.is( batch.instances[i * 16 + n], transform[n] ) ) {
					moved = true;
					break;
				}
			}
			if ( moved ) {
				batch.instances.set( transform, i * 16 );
				group.instancesChanged = true;
			}
			for ( let p = 0; p < model.primitives.length; p++ ) {
				const primitive = model.primitives[p]!, offset = i * primitive.joints.length * 16;
				if ( primitive.emission ) continue;
				if ( !batch.streams ) {
					state.pose.palette( primitive, batch.palettes[p]!, offset );
				}
				if ( primitive.billboard ) {
					faceEffectMesh(
						batch.palettes[p]!,
						offset,
						transform,
						view!,
						primitive.billboard,
						undefined,
						billboardAxes
					);
				}
			}
		}
	}
	/*
	================
	orderRibbonElements

	Newest first into ribbonOrder: live graph elements by birth (ties keep
	slot order), or the emission's births in reverse. Returns the count.
	================
	*/
	function orderRibbonElements(
		elements: readonly ({ alive: boolean; born: number; } | undefined)[] | undefined,
		births: number
	) {
		let count = 0;
		const total = elements ? elements.length : births;
		if ( ribbonOrder.length < total ) ribbonOrder = new Int32Array( total * 2 );
		for ( let i = 0; i < total; i++ ) {
			const b = elements ? i : total - 1 - i;
			if ( elements && !elements[b]?.alive ) continue;
			let at = count++;
			while (
				elements && at > 0 && elements[ribbonOrder[at - 1]!]!.born < elements[b]!.born
			) {
				ribbonOrder[at] = ribbonOrder[at - 1]!;
				at--;
			}
			ribbonOrder[at] = b;
		}
		return count;
	}
	/*
	================
	collectRibbonGroups

	One actor's ribbon points into ribbonGroups, a chain per element group.
	================
	*/
	function collectRibbonGroups(
		frame: PrepareFrame,
		group: GroupFrame,
		p: number,
		actor: CharacterActor
	) {
		const primitive = group.model.primitives[p]!, ribbon = primitive.ribbon!;
		const emission = primitive.emission!,
			history = particleBirths.get( actor.gid )!,
			matrices = history.matrices[p]!,
			material = primitive.materialFrames!,
			alpha = frame.opacity( actor );
		const elements = primitive.particleEmitter === undefined ?
			undefined :
			history.graph?.elements[primitive.particleEmitter];
		const count = orderRibbonElements( elements, emission.births.length );
		groupChains.clear();
		ribbonGroups.length = 0;
		for ( let k = 0; k < count; k++ ) {
			const b = ribbonOrder[k]!,
				element = elements?.[b],
				birth = element ? element.clockBirth / 20 : emission.births[b]!;
			if ( !elements && actor.emissionEnd !== undefined && birth >= actor.emissionEnd ) {
				continue;
			}
			// A group opens at its first element, drawn or not: the
			// groups keep the order the elements first name them.
			const key = element?.parent ?? 0;
			let points = groupChains.get( key );
			if ( !points ) {
				points = ribbonChains[ribbonGroups.length] ??= createRibbonChain();
				points.count = 0;
				groupChains.set( key, points );
				ribbonGroups.push( points );
			}
			const elapsed = actor.time - birth,
				age = emission.loop && elapsed >= 0 ? elapsed % emission.lifetime : elapsed;
			if (
				age < 0 || age >= emission.lifetime || !Number.isFinite( matrices[b * 16 + 15] )
			) continue;
			const frameIndex = Math.min(
					ribbon.widths.length - 1,
					Math.floor( age * ribbon.fps )
				),
				at = Math.min( material.colors.length / 4 - 1, Math.floor( age * material.fps ) );
			const scale = hypot3( matrices[b * 16]!, matrices[b * 16 + 1]!, matrices[b * 16 + 2]! ) *
				group.model.nodes[0]!.scale[0]!;
			pushRibbonPoint(
				points,
				matrices,
				b * 16 + 12,
				material.colors,
				at * 4,
				alpha,
				(ribbon.widths[frameIndex] ?? 1) * scale
			);
		}
	}
	/*
	================
	drawRibbon

	A ribbon primitive's strips for every row, written into the batch's reused
	vertex streams and uploaded as one dynamic draw.
	================
	*/
	function drawRibbon( frame: PrepareFrame, group: GroupFrame, p: number ): GeometryDraw {
		const geometry = frame.geometry, { preview, view } = frame, { rows, batch } = group;
		const primitive = group.model.primitives[p]!;
		const capacity = rows.length *
			Math.max(
				2,
				3 * ((primitive.emission?.capacity ?? primitive.emission?.births.length ?? 1) - 1) + 1
			) * 2;
		let ribbon = batch.ribbons[p];
		if ( ribbon?.capacity !== capacity ) {
			ribbon = {
				capacity,
				used: 0,
				positions: new Float32Array( capacity * 3 ),
				colors: new Float32Array( capacity * 4 ),
				uvs: new Float32Array( capacity * 2 ),
				indices: new Uint32Array( capacity * 3 )
			};
			batch.ribbons[p] = ribbon;
		}
		const { positions, colors, uvs, indices } = ribbon;
		let vertex = 0, index = 0;
		for ( const actor of rows ) {
			collectRibbonGroups( frame, group, p, actor );
			for ( const points of ribbonGroups ) {
				if ( primitive.ribbon!.spline ) ribbonSpline( points, ribbonDrawn, ribbonWork );
				else ribbonPolyline( points, ribbonDrawn );
				if ( !ribbonDrawn.count ) continue;
				ribbonStrip( ribbonDrawn, view!, ribbon, vertex, index, ribbonWork );
				vertex += ribbonDrawn.count * 2;
				index += (ribbonDrawn.count - 1) * 6;
			}
		}
		// The stream is reused: clear what the last frame wrote past this one.
		// Only vertices either frame wrote can differ from the uploaded stream.
		const touched = Math.max( vertex, ribbon.used );
		if ( vertex < ribbon.used ) {
			positions.fill( 0, vertex * 3, ribbon.used * 3 );
			colors.fill( 0, vertex * 4, ribbon.used * 4 );
			uvs.fill( 0, vertex * 2, ribbon.used * 2 );
		}
		ribbon.used = vertex;
		let draw = batch.draws[p];
		if ( !draw ) {
			draw = geometry.upload( {
				dynamicVertices: true,
				positions,
				colors,
				uvs,
				indices,
				transform: preview ? view! : identity(),
				world: !preview,
				instances: identity(),
				material: {
					...(group.modifierClocks?.[p]?.material ?? primitive.geometry.material!),
					...(rows[0]!.deferredParticle ? { deferredParticle: true } : {}),
					...(preview ? { fogDisabled: true } : {})
				}
			}, group.resource.textures[primitive.image] );
			batch.draws[p] = draw;
		} else {
			ribbonRange[0]![1] = touched;
			geometry.updatePositions( draw, positions, colors, uvs, touched ? ribbonRange : [] );
			if ( preview ) geometry.updateTransform( draw, view! );
		}
		geometry.updateIndices( draw, indices.subarray( 0, index ) );
		probe?.characterCount( "ribbon-vertices", vertex );
		return draw;
	}
	/*
	================
	uploadPrimitive

	The primitive's draw for this batch, with its material
	policy (blend, fade, tint, deferral) and textures.
	================
	*/
	function uploadPrimitive(
		frame: PrepareFrame,
		group: GroupFrame,
		p: number,
		instances: Float32Array,
		paletteOffsets?: Uint32Array
	) {
		const { preview, view } = frame, { rows, fading, resource } = group;
		const primitive = group.model.primitives[p]!;
		const authored = group.modifierClocks?.[p]?.material ?? primitive.geometry.material,
			base = authored!;
		return frame.geometry.upload(
			{
				...primitive.geometry,
				world: !preview,
				material: {
					...base,
					...(rows[0]!.deferredParticle ? { deferredParticle: true } : {}),
					instanceMaterialTint: !!rows[0]!.materialTint,
					// A fading opaque body becomes alpha blended but keeps writing
					// depth. An already blended material (every effect program) keeps
					// its own blend and never writes depth: a fading effect must not
					// hide what is behind it, such as a name board.
					...(fading ?
						authored?.blend ?
							{ instanceFade: true, depthWrite: false } :
							{ blend: true, instanceFade: true } :
						{}),
					...(preview ? { fogDisabled: true } : {})
				},
				instances,
				bones: group.batch.palettes[p],
				...(primitive.cloth ?
					{ joints: undefined, weights: undefined, bones: undefined, dynamicVertices: true } :
					{}),
				transform: preview ? view! : identity()
			},
			resource.textures[primitive.image],
			paletteOffsets,
			primitive.equipmentGlow && !fading && (rows[0]!.animationLod?.fraction ?? 0) <= .5 ?
				resource.textures[primitive.equipmentGlow.image] :
				primitive.environmentImage === undefined ?
				undefined :
				resource.textures[primitive.environmentImage]
		);
	}
	/*
	================
	drawParticles

	An emitted primitive's particle stream for every row. Ticks stay native
	(20 Hz) and the GPU pass draws them at the display rate (particle-streams.ts).
	================
	*/
	function drawParticles( frame: PrepareFrame, group: GroupFrame, p: number ): GeometryDraw {
		const { rows, batch } = group, particles = batch.particles[p]!;
		beginParticleFrame( particles, frame.view, rows.length );
		for ( let i = 0; i < rows.length; i++ ) {
			const actor = rows[i]!;
			particleRow.actor = actor;
			particleRow.history = particleBirths.get( actor.gid )!;
			particleRow.pose = poses.get( actor.gid )!.pose;
			particleRow.opacity = group.fading ? frame.opacity( actor ) : 1;
			particleRow.origin = frame.origin;
			writeParticleRow( particles, i, particleRow, particleRandom );
		}
		let draw = batch.draws[p];
		if ( !draw ) {
			draw = uploadPrimitive( frame, group, p, new Float32Array( particles.rows * particles.slots * 16 ) );
			batch.draws[p] = draw;
		} else if ( frame.preview ) frame.geometry.updateTransform( draw, frame.view! );
		frame.geometry.presentParticles( draw, particles );
		endParticleFrame( particles );
		probe?.characterCount( "particles", particles.live );
		updateModifiers( frame.geometry, group, draw, p, true );
		return draw;
	}
	/*
	================
	writeAppearance

	The rows' material-frame colors and windows, tinted; undefined when the
	primitive has no appearance stream.
	================
	*/
	function writeAppearance( group: GroupFrame, p: number ) {
		const { rows } = group, primitive = group.model.primitives[p]!, appearance = group.batch.appearances[p];
		if ( primitive.materialFrames && appearance ) {
			const frames = primitive.materialFrames, count = frames.colors.length / 4;
			for ( let i = 0; i < rows.length; i++ ) {
				const at = Math.max( 0, Math.min( count - 1, rows[i]!.time * frames.fps ) ),
					index = Math.floor( at ),
					next = Math.min( count - 1, index + 1 ),
					fraction = frames.sampling === "step" ? 0 : at - index;
				for ( let c = 0; c < 4; c++ ) {
					appearance[i * 8 + c] = frames.colors[index * 4 + c]! * (1 - fraction) +
						frames.colors[next * 4 + c]! * fraction;
				}
				appearance.set( frames.windows.subarray( index * 4, index * 4 + 4 ), i * 8 + 4 );
			}
		}
		if ( appearance ) {
			for ( let i = 0; i < rows.length; i++ ) {
				if ( !primitive.materialFrames ) {
					// Neutral appearance: white, opaque window, no offset.
					appearance.fill( 1, i * 8, i * 8 + 6 );
					appearance[i * 8 + 6] = appearance[i * 8 + 7] = 0;
				}
				const tint = rows[i]!.materialTint;
				if ( tint ) { for ( let c = 0; c < 3; c++ ) appearance[i * 8 + c]! *= tint[c]!; }
			}
		}
		return appearance;
	}
	/*
	================
	drawCloth

	A cloth primitive: its instance streams, then CPU cloth vertices from the
	batch palette.
	================
	*/
	function drawCloth( frame: PrepareFrame, group: GroupFrame, p: number, streams: PrimitiveRows ): GeometryDraw {
		const geometry = frame.geometry;
		const { preview, view } = frame, { rows, batch, fading } = group;
		const { instances, appearance, pointLights } = streams, primitive = group.model.primitives[p]!;
		batch.cloth ??= new Map();
		let cloth = batch.cloth.get( p );
		if ( !cloth ) {
			cloth = createClothVertices( primitive, clothRandom );
			batch.cloth.set( p, cloth );
		}
		let draw = batch.draws[p];
		const placementChanged = !draw || group.instancesChanged || fading || appearance || pointLights;
		if ( !draw ) draw = uploadPrimitive( frame, group, p, batch.instances );
		// Cloth vertices change independently of the actor placement. These
		// unskinned draws never enter the particle compute writer.
		if ( placementChanged ) {
			draw = geometry.updateInstances(
				draw,
				instances,
				group.opacities,
				appearance?.subarray( 0, instances.length / 2 ),
				pointLights
			);
		}
		batch.draws[p] = draw;
		geometry.writeVertices(
			draw,
			0,
			cloth.update(
				batch.palettes[p]!,
				frame.seconds,
				frame.dynamicAnimation && (rows[0]!.animationLod?.fraction ?? 0) < .25,
				// CIObject 853C40 initializes +C4 to zero; 85DEBB clears it each tick.
				{ direction: [ instances[8]!, instances[9]!, -instances[10]! ], speed: 0 }
			)
		);
		if ( preview ) geometry.updateTransform( draw, view! );
		updateModifiers( geometry, group, draw, p, true );
		return draw;
	}
	/*
	================
	drawSkinned

	An ordinary primitive: upload once, then refresh instances when anything
	per-row changed, and the bones from the batch palette or its stream.
	================
	*/
	function drawSkinned( frame: PrepareFrame, group: GroupFrame, p: number, streams: PrimitiveRows ): GeometryDraw {
		const geometry = frame.geometry;
		const { preview, view } = frame, { rows, batch, fading, capacity } = group;
		const { instances, appearance, pointLights } = streams, primitive = group.model.primitives[p]!;
		const stream = batch.streams?.streams[p],
			paletteOffsets = stream?.offsets.subarray( 0, rows.length );
		let draw = batch.draws[p];
		if ( !draw ) {
			draw = uploadPrimitive( frame, group, p, batch.instances, stream?.offsets );
			batch.draws[p] = draw;
			if ( capacity !== rows.length || fading || appearance || pointLights ) {
				draw = geometry.updateInstances(
					draw,
					instances,
					group.opacities,
					appearance?.subarray( 0, instances.length / 2 ),
					pointLights,
					paletteOffsets
				);
				batch.draws[p] = draw;
			}
		} else {
			if (
				group.instancesChanged || fading || appearance || pointLights || stream?.mappingChanged
			) {
				draw = geometry.updateInstances(
					draw,
					instances,
					group.opacities,
					appearance?.subarray( 0, instances.length / 2 ),
					pointLights,
					paletteOffsets
				);
			}
			batch.draws[p] = draw;
			if ( !stream ) {
				const upload = batch.palettes[p]!.subarray( 0, rows.length * primitive.joints.length * 16 );
				boneUploadBytes += upload.byteLength;
				geometry.updateBones( draw, upload );
			}
			if ( preview ) geometry.updateTransform( draw, view! );
		}
		if ( stream ) {
			boneUploadBytes +=
				geometry.updateBones( draw, stream.data.subarray( 0, stream.length ), stream.revision ) ??
					0;
		}
		updateModifiers( geometry, group, draw, p, true );
		return draw;
	}
	/*
	================
	drawPrimitive

	One primitive's draw for the batch, by kind: ribbon, particles, cloth, or
	an ordinary skinned or static mesh.
	================
	*/
	function drawPrimitive( frame: PrepareFrame, group: GroupFrame, p: number ): GeometryDraw {
		const primitive = group.model.primitives[p]!;
		if ( primitive.ribbon ) return drawRibbon( frame, group, p );
		if ( group.batch.particles[p] ) return drawParticles( frame, group, p );
		// Only mesh and cloth draws consume these streams. A cached group or
		// one containing only particles/ribbons needs no scratch writes.
		if ( !group.streamsPrepared ) {
			group.opacities = group.fading ? fillRowOpacities( rowStreams, group.rows, frame.opacity ) : undefined;
			group.pointLights = !frame.preview && group.rows.some( row => row.pointLight ) ?
				fillRowPointLights( rowStreams, group.rows, frame.origin ) :
				undefined;
			group.streamsPrepared = true;
		}
		const streams: PrimitiveRows = {
			appearance: writeAppearance( group, p ),
			instances: group.batch.instances.subarray( 0, group.rows.length * 16 ),
			pointLights: group.pointLights
		};
		if ( primitive.cloth ) return drawCloth( frame, group, p, streams );
		return drawSkinned( frame, group, p, streams );
	}
	/*
	================
	prepareGroup

	Build one planned batch's draws into frame.output.
	================
	*/
	function prepareGroup( frame: PrepareFrame, id: string, rows: CharacterActor[] ) {
		const { output } = frame;
		const outputStart = output.length;
		rows.sort( ( a, b ) => a.gid - b.gid );
		const resource = models.get( rows[0]!.model )!;
		residentTextures( resource, frame.images );
		const { batch, capacity, membershipChanged } = admitBatch( frame, id, rows, resource );
		const fading = frame.opacity( rows[0]! ) < 1;
		const group: GroupFrame = {
			id,
			rows,
			resource,
			model: resource.model,
			plan: resource.plan,
			fading,
			modifierClocks: hasMaterialClocks ? materialClocks.get( rows[0]! ) : undefined,
			batch,
			capacity,
			instancesChanged: membershipChanged,
			streamsPrepared: false,
			opacities: undefined,
			pointLights: undefined
		};
		const poseKey = batchPoseKey( frame, group );
		for ( let i = 0; i < rows.length; i++ ) batch.times[i] = rows[i]!.time;
		if ( poseKey !== undefined && batch.poseKey === poseKey ) {
			batch.draws.forEach( ( draw, index ) => updateModifiers( frame.geometry, group, draw, index ) );
			output.push( ...batch.draws );
			probe?.characterBatch?.(
				id.slice( rows[0]!.model.length ),
				rows.length,
				batch.draws.filter( draw => draw.indexCount > 0 && draw.instanceCount > 0 ).length
			);
			return;
		}
		batch.poseKey = poseKey;
		writeBatchRows( frame, group );
		for ( let p = 0; p < group.model.primitives.length; p++ ) {
			output.push( drawPrimitive( frame, group, p ) );
		}
		probe?.characterBatch?.(
			id.slice( rows[0]!.model.length ),
			rows.length,
			output.slice( outputStart ).filter( draw => draw.indexCount > 0 && draw.instanceCount > 0 ).length
		);
	}
	return {
		/*
		================
		frameWork
		================
		*/
		frameWork( work: import("@/engine/contracts/runtime").FrameWork ) {
			frameWork = work;
		},
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
		labelAnchors

		Project retained actor anchors through the same world view used by rendering.
		================
		*/
		labelAnchors( origin: number, view: Float32Array, width: number, height: number, gids: ReadonlySet<number> ) {
			const rows = snapshots.index,
				transforms = new Map<number, Float32Array>(),
				result = new Map<number, readonly [number, number, number]>();
			for ( const gid of gids ) {
				const actor = rows.get( gid );
				if ( !actor || actor.attachment || (actor.opacity ?? 1) <= 0 ) continue;
				// 85f57b: riding labels use the ride's height, not the rider's saddle.
				// 85f58e: without a resolvable ride (85D870 null) the rider's own
				// height and lift place it; a ride whose model is still loading
				// has no height yet either.
				const ride = actor.mountedOn !== undefined ? rows.get( actor.mountedOn ) : undefined,
					rideResource = ride && models.get( ride.model );
				const body = rideResource ? ride! : actor;
				const resource = rideResource || models.get( actor.model );
				if ( !resource ) continue;
				const bounds = bindBoundsOf( resource.model );
				const matrix = transformFor( body, rows, origin, transforms );
				if ( !matrix || !Number.isFinite( bounds[4] ) ) continue;
				const x = matrix[12]!,
					y = matrix[13]! +
						characterLabelHeight(
							{ height: body.height, scale: body.scale, groundItem: actor.groundItem },
							bounds[4],
							!!rideResource
						),
					z = matrix[14]!;
				const clip = [ 0, 0, 0, 0 ];
				for ( let n = 0; n < 4; n++ ) {
					clip[n] = view[n]! * x + view[4 + n]! * y + view[8 + n]! * z + view[12 + n]!;
				}
				const w = clip[3]!;
				if ( w <= 0 || clip[2]! < 0 || clip[2]! > w || Math.abs( clip[0]! ) > w || Math.abs( clip[1]! ) > w ) {
					continue;
				}
				result.set( gid, [ (clip[0]! / w + 1) * width / 2, (1 - clip[1]! / w) * height / 2, clip[2]! / w ] );
			}
			return result;
		},
		/*
		================
		particleSnapshot

		Expose the retained emitter snapshot without advancing its simulation clock.
		================
		*/
		particleSnapshot( gid: number ) {
			const row = particleSnapshots.get( gid );
			return row ? { matrix: row.matrix.slice(), regionId: row.regionId } : null;
		},
		/*
		================
		particleTime

		Report the emitter clock belonging to this actor lifetime.
		================
		*/
		particleTime( gid: number ) {
			return deferred.sample( gid )?.time;
		},
		/*
		================
		localMatrix

		Resolve a bone transform from the current actor pose.
		================
		*/
		localMatrix( rows: readonly CharacterActor[], gid: number, bone: string ) {
			const actor = rows.find( a => a.gid === gid );
			return actor ? poseFor( actor )?.socket( bone ) ?? null : null;
		},
		/*
		================
		matrix

		Resolve an actor placement through its mount and attachment hierarchy.
		================
		*/
		matrix( rows: readonly CharacterActor[], gid: number ) {
			const index = new Map( rows.map( a => [ a.gid, a ] ) ), actor = index.get( gid );
			return actor ? transformFor( actor, index, actor.pose.regionId, new Map() ) : null;
		},
		/*
		================
		socket

		Combine a bone position with a native model-space offset (8D6880). The
		offset turns with the holder's root, not with the bone, and is carried
		into imported space by nativeModelOffset. Effect queries use 8D6330's
		mount/root fallback; a null name is 8D6880's null bone, the holder's
		own root with no mount retry. Strict geometry queries keep a missing
		marker distinct from a real socket (for example, footstep contact).
		================
		*/
		socket(
			rows: readonly CharacterActor[],
			gid: number,
			bone: string | { name: string | null; fallback: "mount-root"; },
			offset: readonly [number, number, number]
		) {
			const byGid = new Map( rows.map( actor => [ actor.gid, actor ] ) ), actor = byGid.get( gid );
			if ( !actor ) return null;
			const fallback = typeof bone !== "string", name = fallback ? bone.name : bone;
			let holder = actor, pose = poseFor( holder );
			// A cold model is not evidence that an authored marker is absent.
			if ( !pose ) return null;
			let socket = name === null ? null : pose.socket( name );
			const visited = new Set<number>( [ holder.gid ] );
			while ( name !== null && !socket && fallback && holder.mountedOn !== undefined ) {
				const mount = byGid.get( holder.mountedOn );
				if ( !mount ) return null;
				if ( visited.has( mount.gid ) ) throw new Error( "Cyclic character socket mount" );
				visited.add( mount.gid );
				holder = mount;
				pose = poseFor( holder );
				if ( !pose ) return null;
				socket = pose.socket( name );
			}
			if ( name !== null && !socket && !fallback ) return null;
			const matrix = transformFor( holder, byGid, actor.pose.regionId, new Map() );
			if ( !matrix ) return null;
			const world = new Float32Array( 16 );
			// 8D64E7..8D656C: after the last mount misses, retain its root
			// matrix and still apply the offset through the holder's basis.
			if ( socket ) multiply( matrix, socket, world );
			else world.set( matrix );
			const delta = nativeModelOffset( matrix, offset );
			return { ...actor.pose, x: world[12]! + delta[0], y: world[13]! + delta[1], z: world[14]! + delta[2] };
		},
		/*
		================
		pickFrontend

		Pick only the supplied frontend actors against their authored volumes.
		================
		*/
		pickFrontend( ray: PickRay, ids: readonly number[] ) {
			const current = snapshots.index;
			// CPSCharacterSelect 0x73a220 tests visible models in roster order.
			for ( const gid of ids ) {
				const actor = current.get( gid );
				if ( !actor || actor.attachment || (actor.opacity ?? 1) <= 0 ) continue;
				const resource = models.get( actor.model );
				if ( !resource ) continue;
				for ( const batch of batches.values() ) {
					const index = batch.gids.indexOf( gid );
					if ( index < 0 ) continue;
					const bounds = pickVolumeOf( resource.model );
					if ( pickVolume( ray, bounds, batch.instances.subarray( index * 16, index * 16 + 16 ) ) ) {
						return gid;
					}
				}
			}
			return null;
		},
		/*
		================
		pick

		Resolve world selection against visible actor volumes in ray order.
		================
		*/
		pick( rays: readonly PickRay[], excluded: number, blindHeld = false, filtered = false ) {
			const current = snapshots.index, ride = current.get( excluded )?.mountedOn;
			// Native 856540 uses the aggregate transformed box, independent of
			// texture alpha and animated limb triangles. Preserve actor order.
			const drawn = new Map<number, Float32Array>();
			for ( const batch of batches.values() ) {
				for ( let i = 0; i < batch.gids.length; i++ ) {
					drawn.set( batch.gids[i]!, batch.instances.subarray( i * 16, i * 16 + 16 ) );
				}
			}
			const candidates: PickCandidate[] = [];
			for ( const actor of actors ) {
				const gid = actor.gid, matrix = drawn.get( gid ), resource = models.get( actor.model );
				if (
					blindHeld && actor.blindable || gid === excluded || gid === ride || actor.attachment ||
					(actor.opacity ?? 1) <= 0 || actor.pickable === false && !(filtered && actor.pickWhenFiltered) ||
					!matrix || !resource
				) continue;
				const bounds = pickVolumeOf( resource.model );
				const hits: { ray: number; depth: number; distance: number; }[] = [];
				for ( let r = 0; r < rays.length; r++ ) {
					const depth = pickVolumeDepth( rays[r]!, bounds, matrix );
					if ( depth !== null ) {
						hits.push( {
							ray: r,
							depth,
							distance: depth * hypot3( rays[r]!.delta[0]!, rays[r]!.delta[1]!, rays[r]!.delta[2]! )
						} );
					}
				}
				if ( hits.length ) candidates.push( { actor, matrix, model: resource.model, hits } );
			}
			// A box is far larger than a posed body (a T-posed giant's club spans
			// 70 units). A winner whose posed triangles meet none of the rays yields
			// to candidates that are confirmed: a drop, or a body actually under
			// the rays. Without a confirmed rival the native box winner stands, so
			// near-misses on a lone target still select it. Deliberate deviation
			// from 692680, which always keeps the nearest box hit.
			/*
			================
			confirmed
			================
			*/
			const confirmed = ( candidate: PickCandidate ) => {
				if ( candidate.actor.groundItem ) return true;
				let scratch = pickPoses.get( candidate.model );
				if ( !scratch ) {
					scratch = createCharacterPose( candidate.model );
					pickPoses.set( candidate.model, scratch );
				}
				return meshUnderRays( candidate, rays, scratch );
			};
			let result = selectPickCandidate( candidates );
			if ( result && candidates.length > 1 && !confirmed( result.candidate ) ) {
				const rivals = candidates.filter( candidate =>
					candidate !== result!.candidate && confirmed( candidate )
				);
				if ( rivals.length ) result = selectPickCandidate( rivals );
			}
			if ( !result ) return null;
			// 692680: a mounted winner answers with its rider, as does a linked ride.
			const rider = actors.find( actor => actor.mountedOn === result!.gid ),
				winner = actors.find( actor => actor.gid === result!.gid );
			return { gid: rider?.gid ?? winner?.pickOwner ?? result.gid, depth: result.depth, ray: result.ray };
		},
		/*
		================
		portraitSource

		Borrow the same retained model and textures used by the world actor.
		Only the character's own model parts (compound attachments: hair and
		equipment) follow it. Native portraits and the inventory doll own a
		separate CCObjCharacter built from the appearance
		(CIFQuickPartySlot_SetPortraitModel 5B9DF0, +0x3E0); skill and buff
		effects are decorations of the world entity and never reach it.
		================
		*/
		portraitSource( gid: number ): PortraitSource | null {
			const actor = portraitSnapshots.index.get( gid ) ?? actors.find( a => a.gid === gid ),
				resource = actor && models.get( actor.model );
			if ( !actor || !resource ) return null;
			const candidates = portraitSnapshots.index.has( gid ) ? [ ...portraitSnapshots.index.values() ] : actors;
			const children: PortraitPart[] = [];
			const parents = new Set( [ gid ] );
			// Preserve nested private-skeleton attachments without borrowing unrelated actors.
			for ( let previous = -1; previous !== parents.size; ) {
				previous = parents.size;
				for ( const child of candidates ) {
					if (
						parents.has( child.gid ) || child.attachment?.basis !== "compound" ||
						!parents.has( child.attachment.gid )
					) {
						continue;
					}
					const childResource = models.get( child.model );
					if ( !childResource ) continue;
					children.push( { actor: child, model: childResource.model, images: childResource.images } );
					parents.add( child.gid );
				}
			}
			return { actor, model: resource.model, images: resource.images, children };
		},
		/*
		================
		hasModel

		Whether a source with this id is resident (owned or borrowed).
		================
		*/
		hasModel( id: string ): boolean {
			return models.has( id );
		},
		/*
		================
		borrowModel

		Retain a borrowed source without taking ownership of its image lifetime.
		================
		*/
		borrowModel( id: string, model: CharacterModel, images: readonly WorldTexture[] ) {
			if ( disposed || models.has( id ) ) throw Error( "Invalid borrowed character resource" );
			models.set( id, {
				model,
				plan: createCharacterRenderPlan( model ),
				images: [ ...images ],
				textures: [],
				owned: false,
				bytes: 0,
				radius: characterRadius( model, bounds )
			} );
			residencyDirty = true;
		},
		/*
		================
		shadowCandidates

		Select shadow geometry from the existing visible draw submissions.
		================
		*/
		shadowCandidates( draws: readonly GeometryDraw[], eye: readonly number[], mode: number ) {
			if ( mode === 0 ) return [];
			const active = new Set( draws ),
				byId = new Map( actors.map( a => [ a.gid, a ] ) ),
				parts = new Map<number, { draw: GeometryDraw; instance: number; }[]>(),
				points = new Map<number, readonly [number, number, number]>();
			for ( const batch of batches.values() ) {
				for ( let i = 0; i < batch.gids.length; i++ ) {
					const actor = byId.get( batch.gids[i]! );
					if ( !actor || actor.attachment && !actor.shadowAttachment ) continue;
					let root = actor;
					const visited = new Set<number>();
					while ( root.attachment ) {
						if ( visited.has( root.gid ) ) break;
						visited.add( root.gid );
						const parent = byId.get( root.attachment.gid );
						if ( !parent ) break;
						root = parent;
					}
					if (
						root.shadowSize === undefined || (root.opacity ?? 1) <= 0 || root.drawGeometry === false
					) continue;
					const model = models.get( actor.model )?.model;
					if ( !model ) continue;
					const admitted = batch.draws.flatMap( ( draw, p ) =>
						active.has( draw ) && !model.primitives[p]?.emission && !model.primitives[p]?.particleProgram ?
							[ { draw, instance: i } ] :
							[]
					);
					if ( !admitted.length ) continue;
					const list = parts.get( root.gid ) ?? [];
					list.push( ...admitted );
					parts.set( root.gid, list );
					if ( root === actor ) {
						points.set( root.gid, [
							batch.instances[i * 16 + 12]!,
							batch.instances[i * 16 + 13]!,
							batch.instances[i * 16 + 14]!
						] );
					}
				}
			}
			return [ ...points ].map( ( [gid, point] ) => ({
				gid,
				point,
				distance: hypot3( point[0]! - eye[0]!, point[1]! - eye[1]!, point[2]! - eye[2]! )
			}) ).sort( ( a, b ) => a.distance - b.distance || a.gid - b.gid ).filter( r =>
				mode !== 2 || r.distance <= SHADOW_DISTANCE
			).slice( 0, SHADOW_LIMIT ).flatMap( ( { gid, point } ) => {
				const actor = byId.get( gid )!, resource = models.get( actor.model )!;
				const b = bindBoundsOf( resource.model );
				if ( mode === 1 && actor.shadowSize === 0 ) return [];
				return [ {
					projection: shadowProjection( point, (b[4] - b[1]) * actor.scale ),
					blobSize: mode === 1 ? actor.shadowSize! + 5 : undefined,
					parts: parts.get( gid )!
				} ];
			} );
		},
		/*
		================
		stats

		Report owned sources, assemblies and retained byte charges independently.
		================
		*/
		stats( details = false, posed = false ) {
			const cullSlack = details ? cullCensus( posed ) : undefined;
			let poseEligibility;
			if ( details ) {
				const unique = new Set<ReturnType<typeof createCharacterPose>>();
				let gpuSamples = 0,
					linearSamples = 0,
					sharedPaletteSamples = 0,
					clothSamples = 0,
					gpuPaletteSamples = 0;
				for ( const state of poses.values() ) {
					if ( unique.has( state.pose ) ) continue;
					unique.add( state.pose );
					const sample = state.pose.gpuSample();
					if ( !sample ) continue;
					gpuSamples++;
					if ( sample.clip.channels.every( channel => channel.interpolation === "LINEAR" ) ) linearSamples++;
					const plan = models.get( state.model )?.plan;
					if ( plan?.sharedPalette ) sharedPaletteSamples++;
					if ( plan?.cloth ) clothSamples++;
					if ( plan?.sharedPalette && !plan.cloth ) gpuPaletteSamples++;
				}
				// Sampling eligibility alone does not prove an affine playback clock.
				// Count retained evaluators without materializing CPU palettes or sockets.
				// Model restrictions overlap: cloth can use shared palettes, but its
				// CPU consumer excludes GPU-only storage. Device admission is separate.
				poseEligibility = {
					actors: poses.size,
					unique: unique.size,
					gpuSamples,
					linearSamples,
					sharedPaletteSamples,
					clothSamples,
					gpuPaletteSamples
				};
			}
			let liveOwnedCpuEvaluations = 0;
			for ( const state of ownedPoses.values() ) liveOwnedCpuEvaluations += state.pose.cpuEvaluations();
			return {
				poseEligibility,
				cullSlack,
				actors: actors.length,
				draws: [ ...batches.values() ].reduce( ( n, batch ) => n + batch.draws.length, 0 ),
				renderBytes,
				rowStreamBytes: (rowStreams.rowScratch?.byteLength ?? 0) + (rowStreams.lightScratch?.byteLength ?? 0),
				deferredActors,
				poseRequests,
				poseEvaluations,
				poseSharingHits,
				poseCreations,
				boneUploadBytes,
				visibleActors,
				frameGroups,
				hierarchyRebuilds: hierarchy.stats().rebuilds,
				actorRecordsCreated: snapshots.stats().created,
				residencyPasses,
				gpuAnimation: gpuAnimation?.(),
				liveOwnedCpuEvaluations,
				cpuEvaluations: retiredCpuEvaluations + liveOwnedCpuEvaluations
			};
		},
		/*
		================
		retain

		Record the sources and assemblies that must survive the next prepare pass.
		================
		*/
		retain( ids: readonly string[] ) {
			retainedScratch.clear();
			for ( const id of ids ) retainedScratch.add( id );
			if ( !retained || retained.size !== retainedScratch.size || ids.some( id => !retained!.has( id ) ) ) {
				const previous = retained;
				retained = retainedScratch;
				retainedScratch = previous ?? new Set();
				residencyDirty = true;
			}
		},
		/*
		================
		animation

		Admit a native clip and update the retained source charge before binding it.
		================
		*/
		animation( id: string, name: string, source: NativeClip ) {
			const base = models.get( id );
			if ( disposed || !base?.owned ) throw Error( "Animation body is not admitted" );
			if ( base.plan.clips.has( name ) ) return base.bytes;
			if ( base.model.clips.length >= 512 ) throw Error( "Character animation count budget" );
			const clip = bindNativeClip( structuredClone( source ), name, base.model.nodes ),
				model = { ...base.model, clips: [ ...base.model.clips, clip ] },
				bytes = characterBytes( model, base.images );
			// Hidden presentation can drop cache entries without a GPU prepare.
			// Reclaim those charges before rejecting growth of the active body.
			if ( residentBytes + bytes - base.bytes > CHARACTER_RESIDENT_BYTES ) relieveResidency( [ id ] );
			if ( residentBytes + bytes - base.bytes > CHARACTER_RESIDENT_BYTES ) {
				throw Error( "Character animation residency budget" );
			}
			residentBytes += bytes - base.bytes;
			base.bytes = bytes;
			for ( const [key, row] of models ) {
				if ( key === id || row.dependencies?.[0] === id ) {
					row.model = { ...row.model, clips: model.clips };
					row.plan = createCharacterRenderPlan( row.model );
					// Admission precedes visibility; include new root motion before
					// any actor or outgoing layer can sample the appended catalog.
					row.radius = Math.max( row.radius, characterRadius( row.model, bounds ) );
					framePoses?.delete( key );
				}
			}
			// A new action must not reset every standing peer's pose, cloth or
			// resident geometry. Skeleton bindings and buffer layouts are unchanged.
			return bytes;
		},
		/*
		================
		extendBorrowedAnimations

		Portraits own evaluators and draws but borrow the world's clip catalog.
		Refresh only an append-only catalog with identical source topology.
		================
		*/
		extendBorrowedAnimations( id: string, model: CharacterModel ) {
			const resource = models.get( id );
			if ( !resource || resource.owned || !isCharacterAnimationExtension( resource.model, model ) ) {
				throw Error( "Invalid borrowed animation extension" );
			}
			if ( resource.model === model ) return;
			// Borrowed catalogs follow the same envelope lifetime as owned ones.
			resource.radius = Math.max( resource.radius, characterRadius( model, bounds ) );
			resource.model = model;
			resource.plan = createCharacterRenderPlan( model );
			framePoses?.delete( id );
		},
		/*
		================
		model

		Admit source bytes once; every rejection releases only owned bitmap resources.
		================
		*/
		model( id: string, model: CharacterModel, images: WorldTexture[] ) {
			if ( disposed || models.has( id ) ) {
				for ( const image of images ) {
					if ( !("kind" in image) ) image.close();
				}
				if ( disposed ) {
					throw new Error( "Characters disposed" );
				}
				return;
			}
			try {
				const bytes = characterBytes( model, images );
				const full = () => ownedModels >= CHARACTER_MODELS || residentBytes + bytes > CHARACTER_RESIDENT_BYTES;
				if ( full() ) relieveResidency();
				if ( full() ) {
					throw new Error( "Character model residency exceeds budget" );
				}
				const admitted = structuredClone( model );
				models.set( id, {
					model: admitted,
					plan: createCharacterRenderPlan( admitted ),
					images: [ ...images ],
					textures: [],
					owned: true,
					bytes,
					radius: characterRadius( model, bounds )
				} );
				residentBytes += bytes;
				ownedModels++;
				residencyDirty = true;
			} catch ( error ) {
				for ( const image of images ) {
					if ( !("kind" in image) ) image.close();
				}
				throw error;
			}
		},
		/*
		================
		assembly

		Compose equipment from resident sources without duplicating their image ownership.
		================
		*/
		assembly(
			id: string,
			base: string,
			parts: readonly import("@/engine/contracts/character").CharacterAttachment[]
		) {
			if ( disposed ) {
				throw new Error( "Characters disposed" );
			}
			if ( models.has( id ) ) {
				requestedAssemblies.add( id );
				return;
			}
			if ( models.size - ownedModels >= CHARACTER_ASSEMBLIES ) {
				relieveResidency( [ base, ...parts.map( part => part.model ) ] );
			}
			if ( models.size - ownedModels >= CHARACTER_ASSEMBLIES ) {
				throw new Error( "Character assembly residency exceeds budget" );
			}
			const body = models.get( base );
			if ( !body ) {
				throw new Error( "Assembly body is not admitted" );
			}
			const hidden = new Set( parts.flatMap( part => [ ...part.covers ] ) ),
				primitives = body.model.primitives.filter( ( _, i ) => !hidden.has( i ) ),
				pixels = [ ...body.images ];
			const nodes = new Map( body.model.nodes.map( ( node, index ) => [ node.name, index ] ) );
			let assembledNodes = [ ...body.model.nodes ];
			for ( const part of parts ) {
				if ( part.branches ) {
					assembledNodes = appendEquipmentSockets(
						assembledNodes,
						part.branches.entries,
						part.branches.slot
					);
				}
				const resource = models.get( part.model );
				if ( !resource ) {
					throw new Error( "Assembly attachment is not admitted" );
				}
				const offset = pixels.length;
				pixels.push( ...resource.images );
				for ( const primitive of resource.model.primitives ) {
					if ( part.parts.includes( primitive.name.replace( /^part:/, "" ) ) ) {
						const joints = primitive.joints.map( joint => {
							const name = resource.model.nodes[joint]!.name, index = nodes.get( name );
							if ( index === undefined ) {
								throw new Error( `Attachment joint ${name} is absent from body` );
							}
							return index;
						} );
						const glow = part.equipment ?
							selectEquipmentGlow(
								resource.model.equipmentGlows?.[part.equipment.refObjId],
								part.equipment.plus
							) :
							undefined;
						primitives.push( {
							...primitive,
							equipmentGlow: glow ? { ...glow, image: glow.image + offset } : undefined,
							joints,
							environmentImage: primitive.environmentImage === undefined ?
								undefined :
								primitive.environmentImage + offset,
							image: primitive.image < 0 ? -1 : primitive.image + offset
						} );
					}
				}
			}
			const assembled = { ...body.model, nodes: assembledNodes, primitives };
			models.set( id, {
				model: assembled,
				plan: createCharacterRenderPlan( assembled ),
				images: pixels,
				textures: [],
				owned: false,
				radius: characterRadius( assembled, bounds ),
				dependencies: [ base, ...parts.map( part => part.model ) ],
				bytes: 0
			} );
			requestedAssemblies.add( id );
			residencyDirty = true;
		},
		/*
		================
		currentActors

		The retained actor snapshots, for read-only observation.
		================
		*/
		currentActors: (): readonly CharacterActor[] => actors,
		/*
		================
		actors

		Publish the frame actor lists used by poses, picking and rendering.
		================
		*/
		actors( value: readonly CharacterActor[], portraitActors: readonly CharacterActor[] = [] ) {
			const portraitRevision = portraitSnapshots.modelRevision();
			portraits = portraitSnapshots.update( portraitActors );
			if ( portraitSnapshots.modelRevision() !== portraitRevision ) residencyDirty = true;
			const revision = snapshots.modelRevision();
			actors = snapshots.update( value );
			requestedAssemblies.clear();
			for ( const gid of particleSnapshots.keys() ) {
				if ( !snapshots.index.has( gid ) ) particleSnapshots.delete( gid );
			}
			hasDeferred = actors.some( actor => !!actor.deferredParticle );
			if ( snapshots.modelRevision() !== revision ) residencyDirty = true;
		},
		/*
		================
		deferredPlan

		Build deferred effect queries from the current actor and particle state.
		================
		*/
		deferredPlan( origin: number, camera: readonly number[], enabled = true, night = true ) {
			if ( !hasDeferred ) return null;
			const ids: number[] = [],
				allowed = new Set<number>(),
				points = new Map<number, readonly number[]>(),
				transforms = new Map<number, Float32Array>();
			for ( const actor of actors ) {
				if (
					!actor.deferredParticle || !deferred.sample( actor.gid )?.deferred || !models.has( actor.model ) ||
					(actor.opacity ?? 1) <= 0 || actor.attachment && !deferredVisible.has( actor.attachment.gid )
				) continue;
				const matrix = transformFor( actor, snapshots.index, origin, transforms );
				if ( !matrix ) continue;
				ids.push( actor.gid );
				if ( enabled && (!actor.deferredParticle.nightOnly || night) ) {
					allowed.add( actor.gid );
					points.set(
						actor.gid,
						particleQueryPoint(
							Array.from( matrix.subarray( 12, 15 ) ),
							camera,
							actor.deferredParticle.offset
						)
					);
				}
			}
			if ( !ids.length ) return null;
			const query = deferred.plan( ids, allowed ),
				eligible = deferred.eligible(),
				input = new Float32Array( eligible.length * 4 );
			for ( let i = 0; i < eligible.length; i++ ) {
				input.set( points.get( eligible[i]! )!, i * 4 );
				input[i * 4 + 3] = 1;
			}
			return { query, points: input };
		},
		completeDeferred: deferred.complete,
		/*
		================
		prepare

		Retire unwanted resources, evaluate visible actors and reuse bounded GPU batches.
		================
		*/
		prepare(
			geometry: GeometryCommands,
			images: ImageCommands,
			origin: number,
			view?: Float32Array,
			preview = false,
			seconds = 0,
			continuation = false,
			deferredEnabled = true,
			night = true,
			dynamicAnimation = false,
			reflectedView?: Float32Array
		) {
			poseSeconds = seconds;
			probe?.characterBegin();
			if ( !continuation ) {
				poseFrame++;
				submitted.clear();
			}
			if ( !continuation ) deferred.begin( seconds, hasDeferred ? actors : [], deferredEnabled, night );
			deferPoses = !!geometry.prepareGpuBones;
			gpuAnimation = geometry.gpuAnimationStats;
			poseRequests =
				poseEvaluations =
				poseSharingHits =
				poseCreations =
				boneUploadBytes =
				visibleActors =
				frameGroups =
					0;
			framePoses = new Map();
			const { frameActors, cycles } = sampleFrameActors( continuation );
			// Residency retires draws and textures; the first pass's recorded
			// commands still use them, so the continuation leaves it for the next
			// full pass.
			if ( residencyDirty && !continuation ) retireResidency( geometry, images, frameActors );
			if ( hasMaterialClocks && !continuation ) {
				materialClocks.step( actors, seconds, path => models.get( path )?.model );
			}
			const { byGid, chains } = hierarchy.update( frameActors );
			const frame: PrepareFrame = {
				geometry,
				images,
				origin,
				view,
				preview,
				seconds,
				continuation,
				dynamicAnimation,
				frameActors,
				cycles,
				byGid,
				chains,
				transforms: new Map(),
				opacity: actorOpacity( byGid ),
				output: []
			};
			const particles = advanceParticles( frame );
			const visible = selectVisible( frame, particles.accepted, reflectedView );
			const { grouped, needed } = planBatches( frame, visible, particles );
			retireUnplanned( frame, grouped, needed );
			countPlan( frame, visible, needed, particles.needed );
			evaluateNeededPoses( frameActors, needed );
			releaseUngrouped( frame, grouped );
			for ( const [id, rows] of grouped ) {
				if ( !continuation ) submitted.add( id );
				// The first pass already submitted ordinary geometry. Keep its
				// admission/budget accounting, but do not rebuild or upload it
				// again when visibility completes the deferred pass.
				if ( continuation && !rows[0]!.deferredParticle ) continue;
				prepareGroup( frame, id, rows );
			}
			probe?.characterMark( "character-upload" );
			probe?.characterCount( "pose-evaluations", poseEvaluations );
			frameGroups = grouped.size;
			framePoses = null;
			return frame.output;
		},
		/*
		================
		invalidate

		Drop device-bound handles while retaining CPU sources for restoration.
		================
		*/
		invalidate() {
			rowStreams.rowScratch = rowStreams.lightScratch = undefined;
			batches.clear();
			retiredDraws = [];
			textures.clear();
			for ( const resource of models.values() ) {
				resource.textures = [];
			}
		},
		/*
		================
		dispose

		Retire GPU batches and owned images; borrowed sources remain with their owner.
		================
		*/
		dispose( geometry: GeometryCommands | null, images: ImageCommands | null ) {
			if ( disposed ) {
				return;
			}
			disposed = true;
			rowStreams.rowScratch = rowStreams.lightScratch = undefined;
			framePoses = null;
			residentBytes = 0;
			ownedModels = 0;
			for ( const batch of batches.values() ) {
				for ( const draw of batch.draws ) {
					geometry?.release( draw );
				}
			}
			for ( const draw of retiredDraws ) {
				geometry?.release( draw );
			}
			retiredDraws = [];
			for ( const resource of models.values() ) {
				if ( resource.owned ) {
					for ( const image of resource.images ) {
						if ( !("kind" in image) ) image.close();
					}
				}
			}
			for ( const draw of textures.values() ) {
				images?.release( draw );
			}
			textures.clear();
			batches.clear();
			models.clear();
			poses.clear();
			for ( const state of ownedPoses.values() ) retiredCpuEvaluations += state.pose.cpuEvaluations();
			ownedPoses.clear();
			cullFrame.valid = false;
			cullFrame.frusta = [];
			cullFrame.visible = [];
			cullFrame.chains = new Map();
			cullFrame.byGid = new Map();
			particleBirths.clear();
			materialClocks.reset();
			deferred.reset();
			particleSnapshots.clear();
			deferredVisible.clear();
			hasDeferred = false;
			hierarchy.reset();
			snapshots.reset();
			portraitSnapshots.reset();
			portraits = [];
			retained?.clear();
			retained = null;
			retainedScratch.clear();
			actors = [];
			requestedAssemblies.clear();
		}
	};
}
