/*
===========================================================================

characters.ts - admitted character models, world batches and separate portrait sources

===========================================================================
*/
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
import { createCharacterRenderPlan } from "@/engine/foundation/animation/character-render-plan";
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

/*
================
createCharacters

Own source resources separately from borrowed assemblies and per-frame draw batches.
================
*/
export function createCharacters() {
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
	const poses = new Map<number, {
		model: string;
		pose: ReturnType<typeof createCharacterPose>;
	}>();
	// Borrowing an identical frame result must not replace an actor's scratch.
	// At most one owned evaluator per needed actor, already charged by the
	// frame's characterPoseBytes reservation. Both maps retire together.
	const ownedPoses = new Map<
		number,
		{ model: string; pose: ReturnType<typeof createCharacterPose>; lod: ReturnType<typeof createPoseLod>; }
	>();
	let poseFrame = 0;
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
			state = { model: actor.model, pose: createCharacterPose( model ), lod: createPoseLod() };
			poseCreations++;
			probe?.characterCount( "pose-created" );
			ownedPoses.set( actor.gid, state );
		}
		poses.set( actor.gid, state );
		if (
			actor.animationLod &&
			!state.lod.sample( actor.animationLod.fraction, actor.animationLod.crowded, poseFrame )
		) return state.pose;
		state.pose.bodyVolume( actor.bodyVolume?.index, actor.bodyVolume?.female );
		if (
			state.pose.evaluate(
				actor.clip,
				actor.time,
				actor.loop,
				actor.layers,
				deferPoses && resource!.plan.sharedPalette
			)
		) poseEvaluations++;
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
	transformFor

	Compose mount and attachment transforms before applying the actor placement.
	================
	*/
	function transformFor(
		actor: CharacterActor,
		rows: ReadonlyMap<number, CharacterActor>,
		origin: number,
		cache: Map<number, Float32Array>,
		chain = new Set<number>()
	): Float32Array | null {
		const cached = cache.get( actor.gid );
		if ( cached ) return cached;
		if ( chain.has( actor.gid ) || chain.size >= 8 ) throw new Error( "Cyclic or excessive character attachment" );
		chain.add( actor.gid );
		let matrix = placement( actor.pose.regionId, origin, actor.pose.x, actor.pose.y, actor.pose.z, actor.pose.yaw );
		const ownerId = actor.attachment?.gid ?? actor.mountedOn;
		if ( ownerId !== undefined ) {
			// The owner left this frame. Native deco and CRT updates run on a
			// live object; there is no matrix to inherit.
			const owner = rows.get( ownerId );
			if ( !owner ) return null;
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
			// A bare rider sits on the mount's saddle in the mount's resource
			// space, which carries the import adapter Sx(-1) (character.ts
			// __gltf_left_handed__); the rider's own body applies it again.
			// Cancel it once, as the native basis below does for named sockets,
			// or the rider is mirrored and culled inside out. A missing saddle
			// keeps the plain root (identity), which has no adapter to cancel.
			// Only an improper socket carries the adapter; a proper one (a model
			// built without the import root) has nothing to cancel.
			const saddled = !actor.attachment && !!socket && determinant3( socket ) < 0;
			if ( !socket ) socket = identity();
			const owned = transformFor( holder, rows, origin, cache, chain );
			if ( !owned ) return null;
			// A root attachment with a fixed facing keeps its owner's position and
			// scale, not its rotation: 8D5440 copies the caster's matrix at spawn.
			const facing = actor.attachment?.root ? actor.attachment.facing : undefined;
			const parent = facing === undefined ? owned : facedMatrix( owned, facing );
			matrix = new Float32Array( 16 );
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
					// A894F0 returns the compound WORLD matrix, not a skeletal
					// root. Undo the imported body's Ry(PI) for a root effect;
					// a named imported socket instead has one remaining Sz(-1).
					// A compiled BSR mesh is itself Z-flipped, so it keeps one
					// more Sz(-1): its third column toggles back.
					const bsr = actor.attachment.basis === "native-bsr";
					const columns = actor.attachment.root ? (bsr ? [ 0 ] : [ 0, 2 ]) : (bsr ? [] : [ 2 ]);
					for ( const c of columns ) {
						for ( let n = 0; n < 3; n++ ) matrix[c * 4 + n] = -matrix[c * 4 + n]!;
					}
					// 8D6880 rotates the offset by the holder matrix, separately
					// from bone orientation, using character height scale C0.
					const offset = [ x, y, z ];
					for ( let c = 0; c < 3; c++ ) {
						const size = hypot3( parent[c * 4]!, parent[c * 4 + 1]!, parent[c * 4 + 2]! );
						if ( size ) {
							for ( let n = 0; n < 3; n++ ) {
								matrix[12 + n]! += parent[c * 4 + n]! / size * (c === 1 ? 1 : -1) * offset[c]! *
									owner.scale;
							}
						}
					}
				} else {for ( let n = 0; n < 3; n++ ) {
						matrix[12 + n]! += matrix[n]! * x + matrix[4 + n]! * y + matrix[8 + n]! * z;
					}}
			}
		}
		if ( actor.absoluteEffectScale && actor.attachment ) {
			for ( let c = 0; c < 3; c++ ) {
				const size = hypot3( matrix[c * 4]!, matrix[c * 4 + 1]!, matrix[c * 4 + 2]! );
				if ( size ) { for ( let n = 0; n < 3; n++ ) matrix[c * 4 + n]! /= size; }
			}
		}
		// Compiled BSR vertices are Z-flipped. Retail projectile bases are in
		// world coordinates, so undo the asset-local flip on their third column.
		if ( actor.effectBasis ) {
			for ( let c = 0; c < 3; c++ ) {
				for ( let r = 0; r < 3; r++ ) matrix[c * 4 + r] = actor.effectBasis[c * 3 + r]! * (c === 2 ? -1 : 1);
			}
		}
		if ( actor.effectRotation ) {
			const { axis, angle } = actor.effectRotation,
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
			matrix = out;
		}
		for ( let n = 0; n < 12; n++ ) matrix[n]! *= actor.scale;
		cache.set( actor.gid, matrix );
		return matrix;
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
				const body = actor.mountedOn !== undefined ? rows.get( actor.mountedOn ) : actor;
				if ( !body ) continue;
				const resource = models.get( body.model );
				if ( !resource ) continue;
				const bounds = bindBoundsOf( resource.model );
				const matrix = transformFor( body, rows, origin, transforms );
				if ( !matrix || !Number.isFinite( bounds[4] ) ) continue;
				const lift = actor.mountedOn !== undefined ? 7 : 2;
				const x = matrix[12]!,
					y = matrix[13]! + (actor.groundItem ? 5 : bounds[4] * body.scale + lift),
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

		Combine a local bone offset with the actor world transform. Effect queries
		use 8D6330's mount/root fallback; strict geometry queries keep a missing
		marker distinct from a real socket (for example, footstep contact).
		================
		*/
		socket(
			rows: readonly CharacterActor[],
			gid: number,
			bone: string | { name: string; fallback: "mount-root"; },
			offset: readonly [number, number, number]
		) {
			const byGid = new Map( rows.map( actor => [ actor.gid, actor ] ) ), actor = byGid.get( gid );
			if ( !actor ) return null;
			const fallback = typeof bone !== "string", name = fallback ? bone.name : bone;
			let holder = actor, pose = poseFor( holder );
			// A cold model is not evidence that an authored marker is absent.
			if ( !pose ) return null;
			let socket = pose.socket( name );
			const visited = new Set<number>( [ holder.gid ] );
			while ( !socket && fallback && holder.mountedOn !== undefined ) {
				const mount = byGid.get( holder.mountedOn );
				if ( !mount ) return null;
				if ( visited.has( mount.gid ) ) throw new Error( "Cyclic character socket mount" );
				visited.add( mount.gid );
				holder = mount;
				pose = poseFor( holder );
				if ( !pose ) return null;
				socket = pose.socket( name );
			}
			if ( !socket && !fallback ) return null;
			const matrix = transformFor( holder, byGid, actor.pose.regionId, new Map() );
			if ( !matrix ) return null;
			const world = new Float32Array( 16 );
			// 8D64E7..8D656C: after the last mount misses, retain its root
			// matrix and still apply the offset through the holder's basis.
			if ( socket ) multiply( matrix, socket, world );
			else world.set( matrix );
			const [x, y, z] = offset, point = [ 0, 0, 0 ];
			for ( let n = 0; n < 3; n++ ) {
				point[n] = world[12 + n]! + matrix[n]! * x + matrix[4 + n]! * y + matrix[8 + n]! * z;
			}
			return { ...actor.pose, x: point[0]!, y: point[1]!, z: point[2]! };
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
		pick( rays: readonly PickRay[], excluded: number, blindHeld = false ) {
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
					(actor.opacity ?? 1) <= 0 || actor.pickable === false || !matrix || !resource
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
			// 692680: a mounted winner answers with its rider.
			const rider = actors.find( actor => actor.mountedOn === result!.gid );
			return { gid: rider?.gid ?? result.gid, depth: result.depth, ray: result.ray };
		},
		/*
		================
		portraitSource

		Borrow the same retained model and textures used by the world actor.
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
					if ( parents.has( child.gid ) || !child.attachment || !parents.has( child.attachment.gid ) ) {
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
		stats() {
			return {
				actors: actors.length,
				draws: [ ...batches.values() ].reduce( ( n, batch ) => n + batch.draws.length, 0 ),
				renderBytes,
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
				liveOwnedCpuEvaluations: [ ...ownedPoses.values() ].reduce(
					( sum, state ) => sum + state.pose.cpuEvaluations(),
					0
				)
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
			if ( residentBytes + bytes - base.bytes > CHARACTER_RESIDENT_BYTES ) {
				throw Error( "Character animation residency budget" );
			}
			residentBytes += bytes - base.bytes;
			base.bytes = bytes;
			const changed = new Set<string>();
			for ( const [key, row] of models ) {
				if ( key === id || row.dependencies?.[0] === id ) {
					row.model = { ...row.model, clips: model.clips };
					row.plan = createCharacterRenderPlan( row.model );
					changed.add( key );
				}
			}
			// Topology/textures remain owned. Retire old palette streams through
			// the normal prepare/release path before their model identity changes.
			for ( const [gid, state] of ownedPoses ) {
				if ( changed.has( state.model ) ) {
					ownedPoses.delete( gid );
					poses.delete( gid );
				}
			}
			for ( const batch of batches.values() ) {
				if ( batch.gids.some( gid => actors.some( a => a.gid === gid && changed.has( a.model ) ) ) ) {
					batch.signature = "";
				}
			}
			return bytes;
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
				if (
					ownedModels >= CHARACTER_MODELS ||
					residentBytes + bytes > CHARACTER_RESIDENT_BYTES
				) {
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
				return;
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
			night = true
		) {
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
			// Residency retires draws and textures; the first pass's recorded
			// commands still use them, so the continuation leaves it for the next
			// full pass.
			if ( residencyDirty && !continuation ) {
				residencyPasses++;
				if ( retained ) {
					const keep = new Set( [
						...retained,
						...frameActors.map( actor => actor.model ),
						...portraits.map( actor => actor.model )
					] );
					for ( const id of keep ) {
						for ( const dependency of models.get( id )?.dependencies ?? [] ) {
							keep.add( dependency );
						}
					}
					for ( const [id, resource] of models ) {
						if ( !keep.has( id ) ) {
							const batch = batches.get( id );
							if ( batch ) {
								for ( const draw of batch.draws ) {
									geometry.release( draw );
								}
							}
							batches.delete( id );
							if ( resource.owned ) {
								for ( const image of resource.images ) {
									if ( !("kind" in image) ) image.close();
								}
								ownedModels--;
							}
							models.delete( id );
							residentBytes -= resource.bytes;
						}
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
			if ( hasMaterialClocks && !continuation ) {
				materialClocks.step( actors, seconds, path => models.get( path )?.model );
			}
			const { byGid, chains } = hierarchy.update( frameActors );
			const transforms = new Map<number, Float32Array>(),
				particleNeeded = new Set<number>(),
				particleAccepted = new Set<number>();
			for ( const [gid, history] of particleBirths ) {
				if ( byGid.get( gid )?.model !== history.model || !models.has( history.model ) ) {
					particleBirths.delete( gid );
				}
			}
			let particleBytes = [ ...particleBirths.values() ].reduce( ( sum, h ) => sum + h.bytes, 0 );
			for ( const actor of frameActors ) {
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
				let history = particleBirths.get( actor.gid );
				if (
					!history || history.model !== actor.model || actor.time < history.time ||
					history.cycle !== (cycles.get( actor.gid ) ?? 0)
				) {
					history = {
						model: actor.model,
						time: actor.time,
						cycle: cycles.get( actor.gid ) ?? 0,
						origin,
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
				const transform = transformFor( actor, byGid, origin, transforms );
				if ( !transform ) continue;
				const dx = ((history.origin & 255) - (origin & 255)) * 1920,
					dz = ((history.origin >>> 8) - (origin >>> 8)) * 1920;
				if ( history.graph && model.particleGraph ) {
					advanceParticleGraph(
						history.graph,
						model.particleGraph,
						actor.time,
						transform,
						particleRandom.table,
						actor.emissionEnd,
						dx,
						dz,
						actor.loop
					);
					particleRandom.index = history.graph.index;
					// Graph ribbons are strips through their elements' drawn
					// frames; other graph primitives are drawn from their tick
					// records by the GPU pass (particle-streams.ts).
					for ( let p = 0; p < model.primitives.length; p++ ) {
						const emitter = model.primitives[p]!.particleEmitter, matrices = history.matrices[p];
						if ( emitter === undefined || !matrices || !model.primitives[p]!.ribbon ) continue;
						const elements = history.graph.elements[emitter]!;
						matrices.fill( NaN );
						for ( let b = 0; b < elements.length; b++ ) {
							const element = elements[b];
							if ( !element?.alive ) continue;
							particleElementMatrix(
								element,
								matrices,
								b * 16,
								actor.time * PARTICLE_TICKS_PER_SECOND - history.graph.frame,
								rotationWork
							);
						}
					}
				} else {
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
				history.origin = origin;
				history.time = actor.time;
			}
			// Deferred draws retain bounded simulation history; only actor retirement releases it.
			// A fade reaches the owner's model parts, not its effects (character-fade.ts).
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
			const frustum = view ? prepareViewFrustum( view ) : undefined;
			const visible = frameActors.filter( actor => {
				if ( opacity( actor ) <= 0 ) return false;
				if ( models.get( actor.model )?.plan.emission ) return particleAccepted.has( actor.gid );
				const chain = chains.get( actor.gid )!;
				if ( !chain.length || chain.some( value => !models.has( value.model ) ) ) return false;
				const anchor = chain[chain.length - 1]!;
				let radius = 0;
				for ( const value of chain ) {
					radius = (radius + models.get( value.model )!.radius) * value.scale * (value.bodyVolume ? 1.2 : 1) +
						(value.attachment ?
							hypot3(
								value.attachment.offset[0]!,
								value.attachment.offset[1]!,
								value.attachment.offset[2]!
							) :
							0);
				}
				return !frustum ||
					visibleFrustumSphere(
						frustum,
						anchor.pose.x + ((anchor.pose.regionId & 255) - (origin & 255)) * 1920,
						anchor.pose.y,
						anchor.pose.z + ((anchor.pose.regionId >>> 8) - (origin >>> 8)) * 1920,
						radius
					);
			} );
			if ( hasDeferred && !continuation ) deferredVisible = new Set( visible.map( actor => actor.gid ) );
			// Plan the complete frame before creating poses, arrays, or GPU resources.
			visibleActors = visible.length;
			// A rejected actor leaves capacity available for cheaper frameActors that follow it.
			const grouped = new Map<string, CharacterActor[]>(), needed = new Set<number>( particleNeeded );
			renderBytes = particleBytes + materialClocks.bytes();
			const retainedDeferred = ( batch: { gids: readonly number[]; } ) =>
				!continuation && hasDeferred && batch.gids.some( gid => !!byGid.get( gid )?.deferredParticle );
			if ( !continuation && hasDeferred ) {
				for ( const batch of batches.values() ) {
					if ( retainedDeferred( batch ) ) {
						const actor = byGid.get( batch.gids[0]! );
						if ( actor ) renderBytes += models.get( actor.model )!.plan.batchBytes( batch.gids.length );
					}
				}
			}
			deferredActors = 0;
			for ( const actor of visible ) {
				if ( actor.drawGeometry === false ) continue;
				const plan = models.get( actor.model )!.plan, dependencies = chains.get( actor.gid )!;
				const key = actor.model + (actor.deferredParticle ? "\0deferred" : "") +
					(opacity( actor ) < 1 ? "\0fade" : "") + (actor.materialTint ? "\0tint" : "") +
					(actor.pointLight ? "\0light" : "") + (models.get( actor.model )!.model.primitives.some( p =>
							p.equipmentGlow
						) ?
						"\0glow:" + ((actor.animationLod?.fraction ?? 0) <= .5 && opacity( actor ) === 1) :
						"") +
					(hasMaterialClocks && materialClocks.get( actor ) ?
						"\0modifier:" + actor.gid + (models.get( actor.model )!.plan.animationMaterial ?
							":" + (actor.modelAnimation?.revision ?? 0) + ":" +
							((actor.animationLod?.fraction ?? 0) > .5) :
							"") :
						"");
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
			// Retire all obsolete storage before allocating the replacement frame.
			for ( const [id, batch] of batches ) {
				if ( retainedDeferred( batch ) || continuation && submitted.has( id ) ) continue;
				const rows = grouped.get( id );
				const capacity = rows ? models.get( rows[0]!.model )!.plan.capacity( rows.length ) : 0;
				if ( !rows || batch.capacity !== capacity || !batch.signature.startsWith( String( preview ) + ":" ) ) {
					for ( const draw of batch.draws ) {
						geometry.release( draw );
					}
					batches.delete( id );
				}
			}
			for ( const gid of poses.keys() ) {
				if ( !needed.has( gid ) ) {
					poses.delete( gid );
				}
			}
			for ( const gid of ownedPoses.keys() ) {
				if ( !needed.has( gid ) ) {
					ownedPoses.delete( gid );
					probe?.characterCount( "pose-retired" );
				}
			}
			probe?.characterMark( "character-plan" );
			for ( const actor of frameActors ) {
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
			/*
			================
			actorTransform

			Resolve the frame-local actor matrix after hierarchy evaluation.
			================
			*/
			function actorTransform( actor: CharacterActor ): Float32Array | null {
				// Null only when the owner actor is gone. A missing bone is
				// the root matrix, so it must not zero this instance.
				const matrix = transformFor( actor, byGid, origin, transforms );
				if ( !matrix ) return null;
				if ( snapshots.index.get( actor.gid )?.deferredParticle ) {
					let row = particleSnapshots.get( actor.gid );
					if ( !row ) {
						row = { matrix: matrix.slice(), regionId: origin };
						particleSnapshots.set( actor.gid, row );
					} else {
						row.matrix.set( matrix );
						row.regionId = origin;
					}
				}
				return matrix;
			}
			const output: GeometryDraw[] = [];
			for ( const [id, batch] of batches ) {
				if ( !grouped.has( id ) && !retainedDeferred( batch ) && !(continuation && submitted.has( id )) ) {
					for ( const draw of batch.draws ) {
						geometry.release( draw );
					}
					batches.delete( id );
				}
			}
			for ( const [id, rows] of grouped ) {
				if ( !continuation ) submitted.add( id );
				// The first pass already submitted ordinary geometry. Keep its
				// admission/budget accounting, but do not rebuild or upload it
				// again when visibility completes the deferred pass.
				if ( continuation && !rows[0]!.deferredParticle ) continue;
				rows.sort( ( a, b ) => a.gid - b.gid );
				const resource = models.get( rows[0]!.model )!, model = resource.model, plan = resource.plan;
				const fading = opacity( rows[0]! ) < 1;
				const modifierClocks = hasMaterialClocks ? materialClocks.get( rows[0]! ) : undefined;
				const updateModifiers = ( draw: GeometryDraw, index: number, initial = false ) => {
					const clock = modifierClocks?.[index];
					if ( !clock ) return;
					const glow = model.primitives[index]!.equipmentGlow;
					if ( glow && clock.glow ) {
						geometry.updateEquipmentGlow(
							draw,
							clock.glow.color,
							clock.glow.uv,
							glow.gain,
							glow.alphaTest,
							!fading && (rows[0]!.animationLod?.fraction ?? 0) <= .5
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
				};
				if ( !resource.textures.length ) {
					resource.textures = resource.images.map( image => {
						let draw = textures.get( image );
						if ( !draw ) {
							draw = images.upload( image );
							textures.set( image, draw );
						}
						return draw;
					} );
				}
				const signature = String( preview ) + ":" + rows.map( row => row.gid ).join( "," );
				// Visibility changes active rows, not immutable mesh identity.
				// Keep geometry within a capacity band; the budget charges the
				// padded storage and GPU submission below uses only live rows.
				const capacity = plan.capacity( rows.length );
				let batch = batches.get( id );
				const membershipChanged = batch?.signature !== signature;
				if ( !batch || batch.capacity !== capacity || !batch.signature.startsWith( String( preview ) + ":" ) ) {
					if ( batch ) {
						for ( const draw of batch.draws ) {
							geometry.release( draw );
						}
					}
					const streams = plan.sharedPalette ? createPaletteStreams( model, capacity ) : undefined;
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
							p.emission && !p.ribbon ? createParticleStream( model, index, rows.length ) : undefined
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
				const billboard = plan.billboard;
				if ( billboard && !view ) throw new Error( "Missing effect camera basis" );
				const clocked = plan.clocked;
				const timeChanged = batch.draws.length > 0 &&
					rows.some( ( actor, i ) =>
						actor.time !== batch!.times[i] && (clocked || plan.clips.get( actor.clip )?.channels.length)
					);
				// A changing sampled time already proves the full key differs.
				// Avoid serializing actor/attachment graphs just to discover it.
				const poseKey = timeChanged ?
					undefined :
					JSON.stringify( [
						origin,
						preview,
						(billboard || preview) ? Array.from( view! ) : null,
						...rows.map(
							actor => [
								cycles.get( actor.gid ),
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
								actor.attachment,
								chains.get( actor.gid )!.slice( 1 )
							]
						)
					] );
				for ( let i = 0; i < rows.length; i++ ) batch.times[i] = rows[i]!.time;
				if ( poseKey !== undefined && batch.poseKey === poseKey ) {
					batch.draws.forEach( ( draw, index ) => updateModifiers( draw, index ) );
					output.push( ...batch.draws );
					continue;
				}
				batch.poseKey = poseKey;
				batch.streams?.update(
					rows.map( actor => {
						const state = poses.get( actor.gid );
						if ( !state || state.model !== actor.model ) throw Error( "Missing prepared character pose" );
						return state.pose;
					} ),
					geometry.prepareGpuBones
				);
				let instancesChanged = membershipChanged;
				for ( let i = 0; i < rows.length; i++ ) {
					const actor = rows[i]!;
					// The needed-pose phase evaluates every admitted actor.
					// Upload consumes that result; it must not sample/validate
					// the same animation request a second time per actor.
					const state = poses.get( actor.gid );
					if ( !state || state.model !== actor.model ) throw Error( "Missing prepared character pose" );
					const transform = actorTransform( actor );
					if ( !transform ) {
						batch.instances.fill( 0, i * 16, i * 16 + 16 );
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
						instancesChanged = true;
					}
					for ( let p = 0; p < model.primitives.length; p++ ) {
						const primitive = model.primitives[p]!, offset = i * primitive.joints.length * 16;
						if ( primitive.emission ) continue;
						if ( !batch.streams ) state.pose.palette( primitive, batch.palettes[p]!, offset );
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
				for ( let p = 0; p < model.primitives.length; p++ ) {
					const primitive = model.primitives[p]!;
					if ( primitive.ribbon ) {
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
							const emission = primitive.emission!,
								history = particleBirths.get( actor.gid )!,
								matrices = history.matrices[p]!,
								material = primitive.materialFrames!,
								alpha = opacity( actor );
							const elements = primitive.particleEmitter === undefined ?
								undefined :
								history.graph?.elements[primitive.particleEmitter];
							// Newest first: live graph elements by birth (ties keep slot
							// order), or the emission's births in reverse.
							let count = 0;
							const total = elements ? elements.length : emission.births.length;
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
								const frame = Math.min(
										primitive.ribbon.widths.length - 1,
										Math.floor( age * primitive.ribbon.fps )
									),
									at = Math.min( material.colors.length / 4 - 1, Math.floor( age * material.fps ) );
								const scale =
									hypot3( matrices[b * 16]!, matrices[b * 16 + 1]!, matrices[b * 16 + 2]! ) *
									model.nodes[0]!.scale[0]!;
								pushRibbonPoint(
									points,
									matrices,
									b * 16 + 12,
									material.colors,
									at * 4,
									alpha,
									(primitive.ribbon.widths[frame] ?? 1) * scale
								);
							}
							for ( const points of ribbonGroups ) {
								if ( primitive.ribbon.spline ) ribbonSpline( points, ribbonDrawn, ribbonWork );
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
									...(modifierClocks?.[p]?.material ?? primitive.geometry.material!),
									...(rows[0]!.deferredParticle ? { deferredParticle: true } : {}),
									...(preview ? { fogDisabled: true } : {})
								}
							}, resource.textures[primitive.image] );
							batch.draws[p] = draw;
						} else {
							ribbonRange[0]![1] = touched;
							geometry.updatePositions( draw, positions, colors, uvs, touched ? ribbonRange : [] );
							if ( preview ) geometry.updateTransform( draw, view! );
						}
						geometry.updateIndices( draw, indices.subarray( 0, index ) );
						probe?.characterCount( "ribbon-vertices", vertex );
						output.push( draw );
						continue;
					}
					/*
					================
					uploadPrimitive

					The primitive's draw for this batch, with its material
					policy (blend, fade, tint, deferral) and textures.
					================
					*/
					const uploadPrimitive = ( instances: Float32Array, paletteOffsets?: Uint32Array ) => {
						const authored = modifierClocks?.[p]?.material ?? primitive.geometry.material,
							base = authored!;
						return geometry.upload(
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
								bones: batch.palettes[p],
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
					};
					const particles = batch.particles[p];
					if ( particles ) {
						// Ticks stay native (20 Hz) and the GPU pass draws them at
						// the display rate (particle-streams.ts).
						beginParticleFrame( particles, view );
						for ( let i = 0; i < rows.length; i++ ) {
							const actor = rows[i]!;
							particleRow.actor = actor;
							particleRow.history = particleBirths.get( actor.gid )!;
							particleRow.pose = poses.get( actor.gid )!.pose;
							particleRow.opacity = fading ? opacity( actor ) : 1;
							particleRow.origin = origin;
							writeParticleRow( particles, i, particleRow, particleRandom );
						}
						let draw = batch.draws[p];
						if ( !draw ) {
							draw = uploadPrimitive( new Float32Array( particles.rows * particles.slots * 16 ) );
							batch.draws[p] = draw;
						} else if ( preview ) geometry.updateTransform( draw, view! );
						geometry.presentParticles( draw, particles );
						endParticleFrame( particles );
						probe?.characterCount( "particles", particles.live );
						updateModifiers( draw, p, true );
						output.push( draw );
						continue;
					}
					const appearance = batch.appearances[p];
					const instances = batch.instances.subarray( 0, rows.length * 16 );
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
					const pointLights = !preview && rows.some( row => row.pointLight ) ?
						new Float32Array( rows.length * 12 ) :
						undefined;
					if ( pointLights ) {
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
							pointLights.set( [
								pos[12]!,
								pos[13]!,
								pos[14]!,
								light.attenuation,
								...light.ambient,
								0,
								...light.diffuse,
								0
							], i * 12 );
						}
					}
					const stream = batch.streams?.streams[p],
						paletteOffsets = stream?.offsets.subarray( 0, rows.length );
					let draw = batch.draws[p];
					if ( !draw ) {
						draw = uploadPrimitive( batch.instances, stream?.offsets );
						batch.draws[p] = draw;
						if ( capacity !== rows.length || fading || appearance || pointLights ) {
							draw = geometry.updateInstances(
								draw,
								instances,
								fading ? Float32Array.from( rows, opacity ) : undefined,
								appearance?.subarray( 0, instances.length / 2 ),
								pointLights,
								paletteOffsets
							);
							batch.draws[p] = draw;
						}
					} else {
						if (
							instancesChanged || fading || appearance || pointLights || stream?.mappingChanged
						) {
							draw = geometry.updateInstances(
								draw,
								instances,
								fading ? Float32Array.from( rows, opacity ) : undefined,
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
					updateModifiers( draw, p, true );
					output.push( draw );
				}
			}
			probe?.characterMark( "character-upload" );
			probe?.characterCount( "pose-evaluations", poseEvaluations );
			frameGroups = grouped.size;
			framePoses = null;
			return output;
		},
		/*
		================
		invalidate

		Drop device-bound handles while retaining CPU sources for restoration.
		================
		*/
		invalidate() {
			batches.clear();
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
			framePoses = null;
			residentBytes = 0;
			ownedModels = 0;
			for ( const batch of batches.values() ) {
				for ( const draw of batch.draws ) {
					geometry?.release( draw );
				}
			}
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
			ownedPoses.clear();
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
		}
	};
}
