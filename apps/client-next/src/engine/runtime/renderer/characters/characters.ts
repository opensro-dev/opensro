/*
===========================================================================

characters.ts - characters.ts - admitted character models, world batches and separate portrait sources

===========================================================================
*/
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
import { bsrParticleAttachment } from "@/engine/foundation/animation/bsr-particle-transform";
import { createPaletteStreams } from "./palette-streams";
import { createCharacterRenderPlan } from "@/engine/foundation/animation/character-render-plan";
import { createCharacterHierarchy } from "@/engine/foundation/animation/character-hierarchy";
import { characterPickVolume } from "@/engine/foundation/animation/character-pick-volume";
import { pickVolume, pickVolumeDepth } from "@/engine/foundation/rendering/pick-volume";
import { ribbonSpline, ribbonStrip, type RibbonPoint } from "@/engine/foundation/rendering/particle-ribbon";
import {
	particleRandomTable,
	initializeParticle,
	advanceParticle,
	placeParticle,
	type ParticleInstance
} from "@/engine/foundation/animation/particle-program";
import { type PickBounds, type PickRay } from "@/engine/foundation/rendering/picking";
import { faceEffectPlate, faceEffectMesh } from "@/engine/foundation/rendering/effect-billboard";
import { characterRadius } from "@/engine/foundation/animation/character-bounds";
import {
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
/*
================
createCharacters
================
*/
export function createCharacters(
	animationProbe?: import("@/engine/foundation/animation/animation-pose").AnimationPoseProbe
) {
	let probe: import("@/engine/contracts/runtime").RenderFrameProbe | undefined;
	const models = new Map<string, {
		model: CharacterModel;
		plan: ReturnType<typeof createCharacterRenderPlan>;
		images: ImageBitmap[];
		textures: ImageDraw[];
		owned: boolean;
		bytes: number;
		radius: number;
		dependencies?: readonly string[];
	}>();
	const materialClocks = createModelMaterialClocks(), deferred = createDeferredParticles();
	const particleSnapshots = new Map<number, { matrix: Float32Array; regionId: number; }>();
	let hasDeferred = false, deferredVisible = new Set<number>();
	const hierarchy = createCharacterHierarchy(), snapshots = createActorSnapshots();
	const volumes = new WeakMap<CharacterModel, PickBounds>();
	const textures = new Map<ImageBitmap, ImageDraw>();
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
	const particleRandom = particleRandomTable();
	let particleRandomIndex = 0;
	const particleBirths = new Map<
		number,
		{
			model: string;
			time: number;
			cycle: number;
			origin: number;
			bytes: number;
			graph?: ParticleGraphState;
			matrices: (Float32Array | undefined)[];
			programs: (ParticleInstance | undefined)[][];
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
		particleInstances: (Float32Array | undefined)[];
		appearances: (Float32Array | undefined)[];
		poseKey?: string;
	}>();
	let residentBytes = 0, renderBytes = 0, deferredActors = 0;
	let retained: Set<string> | null = null, retainedScratch = new Set<string>();
	// Residency depends on model membership, not interpolated poses or clocks.
	// Snapshot the model ids: actor snapshots are mutated by the next publication.
	let residencyDirty = true, residencyPasses = 0, hasMaterialClocks = false;
	let actors: readonly CharacterActor[] = [], disposed = false;
	const portraitSnapshots = createActorSnapshots();
	let portraits: readonly CharacterActor[] = [];
	let gpuAnimation: GeometryCommands["gpuAnimationStats"], deferPoses = false;
	// A frame may contain many instances requesting exactly the same model pose.
	// Share only identical inputs; particles retain independent mutable age samples.
	let framePoses: Map<string, Map<string, ReturnType<typeof createCharacterPose>>> | null = null;
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
================
	*/
	function poseFor( actor: CharacterActor ) {
		poseRequests++;
		const resource = models.get( actor.model ), model = resource?.model;
		if ( !model ) return null;
		const share = framePoses && !resource!.plan.emission &&
			(!actor.animationLod || !actor.animationLod.crowded || actor.animationLod.fraction < .75);
		const key = share ?
			JSON.stringify( [ actor.clip, actor.time, actor.loop, actor.layers ?? null, actor.bodyVolume ?? null ] ) :
			"";
		const shared = share ? framePoses!.get( actor.model )?.get( key ) : undefined;
		if ( shared ) {
			poseSharingHits++;
			poses.set( actor.gid, { model: actor.model, pose: shared } );
			return shared;
		}
		let state = ownedPoses.get( actor.gid );
		if ( !state || state.model !== actor.model ) {
			state = { model: actor.model, pose: createCharacterPose( model, animationProbe ), lod: createPoseLod() };
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
			samples.set( key, state.pose );
		}
		return state.pose;
	}
	/*
================
transformFor
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
			let socket = actor.attachment?.root ?
				identity() :
				poseFor( holder )?.socket( actor.attachment?.bone ?? "saddle" ) ?? null;
			if ( !socket && actor.attachment && !actor.attachment.root && holder.mountedOn !== undefined ) {
				const mount = rows.get( holder.mountedOn );
				const mountSocket = mount ? poseFor( mount )?.socket( actor.attachment.bone ) ?? null : null;
				if ( mount && mountSocket ) {
					holder = mount;
					socket = mountSocket;
				}
			}
			if ( !socket ) socket = identity();
			const parent = transformFor( holder, rows, origin, cache, chain );
			if ( !parent ) return null;
			matrix = new Float32Array( 16 );
			multiply( parent, socket, matrix );
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
				} else if ( actor.attachment.basis === "native" ) {
					// A894F0 returns the compound WORLD matrix, not a skeletal
					// root. Undo the imported body's Ry(PI) for a root effect;
					// a named imported socket instead has one remaining Sz(-1).
					for ( const c of actor.attachment.root ? [ 0, 2 ] : [ 2 ] ) {
						for ( let n = 0; n < 3; n++ ) matrix[c * 4 + n] = -matrix[c * 4 + n]!;
					}
					// 8D6880 rotates the offset by the holder matrix, separately
					// from bone orientation, using character height scale C0.
					const offset = [ x, y, z ];
					for ( let c = 0; c < 3; c++ ) {
						const size = Math.hypot( parent[c * 4]!, parent[c * 4 + 1]!, parent[c * 4 + 2]! );
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
				const size = Math.hypot( matrix[c * 4]!, matrix[c * 4 + 1]!, matrix[c * 4 + 2]! );
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
labelAnchors
================
		*/
		profile( value: import("@/engine/contracts/runtime").RenderFrameProbe | undefined ) {
			probe = value;
		},
		/*
================
labelAnchors
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
				let bounds = volumes.get( resource.model );
				if ( !bounds ) {
					bounds = characterPickVolume( resource.model );
					volumes.set( resource.model, bounds );
				}
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
================
		*/
		particleSnapshot( gid: number ) {
			const row = particleSnapshots.get( gid );
			return row ? { matrix: row.matrix.slice(), regionId: row.regionId } : null;
		},
		/*
================
particleTime
================
		*/
		particleTime( gid: number ) {
			return deferred.sample( gid )?.time;
		},
		/*
================
localMatrix
================
		*/
		localMatrix( rows: readonly CharacterActor[], gid: number, bone: string ) {
			const actor = rows.find( a => a.gid === gid );
			return actor ? poseFor( actor )?.socket( bone ) ?? null : null;
		},
		/*
================
matrix
================
		*/
		matrix( rows: readonly CharacterActor[], gid: number ) {
			const index = new Map( rows.map( a => [ a.gid, a ] ) ), actor = index.get( gid );
			return actor ? transformFor( actor, index, actor.pose.regionId, new Map() ) : null;
		},
		/*
================
socket
================
		*/
		socket(
			rows: readonly CharacterActor[],
			gid: number,
			bone: string,
			offset: readonly [number, number, number]
		) {
			const byGid = new Map( rows.map( actor => [ actor.gid, actor ] ) ), actor = byGid.get( gid );
			if ( !actor ) return null;
			const socket = poseFor( actor )?.socket( bone ),
				matrix = transformFor( actor, byGid, actor.pose.regionId, new Map() );
			if ( !socket || !matrix ) return null;
			const world = new Float32Array( 16 );
			multiply( matrix, socket, world );
			const [x, y, z] = offset, point = [ 0, 0, 0 ];
			for ( let n = 0; n < 3; n++ ) {
				point[n] = world[12 + n]! + matrix[n]! * x + matrix[4 + n]! * y + matrix[8 + n]! * z;
			}
			return { ...actor.pose, x: point[0]!, y: point[1]!, z: point[2]! };
		},
		/*
================
pickFrontend
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
					let bounds = volumes.get( resource.model );
					if ( !bounds ) {
						bounds = characterPickVolume( resource.model );
						volumes.set( resource.model, bounds );
					}
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
================
		*/
		pick( rays: readonly PickRay[], excluded: number, blindHeld = false ) {
			let result: { gid: number; depth: number; ray: number; } | null = null, bestDistance = Infinity;
			const current = snapshots.index, ride = current.get( excluded )?.mountedOn;
			// Native 856540 uses the aggregate transformed box, independent of
			// texture alpha and animated limb triangles. Preserve actor order.
			const drawn = new Map<number, Float32Array>();
			for ( const batch of batches.values() ) {
				for ( let i = 0; i < batch.gids.length; i++ ) {
					drawn.set( batch.gids[i]!, batch.instances.subarray( i * 16, i * 16 + 16 ) );
				}
			}
			for ( const actor of actors ) {
				const gid = actor.gid, matrix = drawn.get( gid ), resource = models.get( actor.model );
				if (
					blindHeld && actor.blindable || gid === excluded || gid === ride || actor.attachment ||
					(actor.opacity ?? 1) <= 0 || actor.pickable === false || !matrix || !resource
				) continue;
				let bounds = volumes.get( resource.model );
				if ( !bounds ) {
					bounds = characterPickVolume( resource.model );
					volumes.set( resource.model, bounds );
				}
				for ( let r = 0; r < rays.length; r++ ) {
					const ray = rays[r]!, depth = pickVolumeDepth( ray, bounds, matrix );
					if ( depth === null ) continue;
					const distance = depth * Math.hypot( ...ray.delta );
					// 69282b: a center hit may replace an off-center winner;
					// a closer subsequent hit still wins, including off-center.
					if ( distance < bestDistance || (result?.ray !== 4 && r === 4) ) {
						bestDistance = distance;
						result = { gid, depth, ray: r };
					}
				}
			}
			if ( result ) {
				const rider = actors.find( actor => actor.mountedOn === result!.gid );
				if ( rider ) result = { ...result, gid: rider.gid };
			}
			return result;
		},
		/*
================
portraitSource
================
		*/
		portraitSource( gid: number ) {
			const actor = portraitSnapshots.index.get( gid ) ?? actors.find( a => a.gid === gid ),
				resource = actor && models.get( actor.model );
			return actor && resource ? { actor, model: resource.model, images: resource.images } : null;
		},
		/*
================
borrowModel
================
		*/
		borrowModel( id: string, model: CharacterModel, images: readonly ImageBitmap[] ) {
			if ( disposed || models.has( id ) ) throw Error( "Invalid borrowed character resource" );
			models.set( id, {
				model,
				plan: createCharacterRenderPlan( model ),
				images: [ ...images ],
				textures: [],
				owned: false,
				bytes: 0,
				radius: characterRadius( model )
			} );
			residencyDirty = true;
		},
		/*
================
shadowCandidates
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
				distance: Math.hypot( ...point.map( ( v, i ) => v - eye[i]! ) )
			}) ).sort( ( a, b ) => a.distance - b.distance || a.gid - b.gid ).filter( r =>
				mode !== 2 || r.distance <= SHADOW_DISTANCE
			).slice( 0, SHADOW_LIMIT ).flatMap( ( { gid, point } ) => {
				const actor = byId.get( gid )!, resource = models.get( actor.model )!;
				let b = volumes.get( resource.model );
				if ( !b ) {
					b = characterPickVolume( resource.model );
					volumes.set( resource.model, b );
				}
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
================
		*/
		model( id: string, model: CharacterModel, images: ImageBitmap[] ) {
			if ( disposed || models.has( id ) ) {
				for ( const image of images ) {
					image.close();
				}
				if ( disposed ) {
					throw new Error( "Characters disposed" );
				}
				return;
			}
			try {
				const bytes = characterBytes( model, images );
				if (
					[ ...models.values() ].filter( resource => resource.owned ).length >= CHARACTER_MODELS ||
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
					radius: characterRadius( model )
				} );
				residentBytes += bytes;
				residencyDirty = true;
			} catch ( error ) {
				for ( const image of images ) {
					image.close();
				}
				throw error;
			}
		},
		/*
================
assembly
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
			if ( [ ...models.values() ].filter( resource => !resource.owned ).length >= 1024 ) {
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
				radius: characterRadius( assembled ),
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
			if ( !continuation ) poseFrame++;
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
							time: sample.time,
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
			if ( residencyDirty ) {
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
									image.close();
								}
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
							createParticleGraph( model.particleGraph, particleRandomIndex ) :
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
						particleRandom,
						actor.emissionEnd,
						dx,
						dz,
						actor.loop
					);
					particleRandomIndex = history.graph.index;
					for ( let p = 0; p < model.primitives.length; p++ ) {
						const emitter = model.primitives[p]!.particleEmitter, matrices = history.matrices[p];
						if ( emitter === undefined || !matrices ) continue;
						const elements = history.graph.elements[emitter]!;
						matrices.fill( NaN );
						for ( let b = 0; b < elements.length; b++ ) {
							const element = elements[b];
							if ( !element?.alive ) continue;
							particleElementMatrix( element, matrices, b * 16, actor.time * 20 - history.graph.frame );
							history.programs[p]![b] = element.state;
						}
					}
				} else {for ( let p = 0; p < model.primitives.length; p++ ) {
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
										particleRandom,
										particleRandomIndex,
										transform
									);
									particleRandomIndex = sample.index;
									history.programs[p]![b] = sample.state;
								}
							}
							if ( emission.follow ) matrices.set( transform, offset );
						}
					}}
				history.origin = origin;
				history.time = actor.time;
			}
			// Deferred draws retain bounded simulation history; only actor retirement releases it.
			const opacity = ( actor: CharacterActor ): number => {
				const parent = actor.attachment ? byGid.get( actor.attachment.gid ) : undefined;
				return (actor.opacity ?? 1) * (parent ? opacity( parent ) : 1);
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
						(value.attachment ? Math.hypot( ...value.attachment.offset ) : 0);
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
				if ( retainedDeferred( batch ) ) continue;
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
				if ( !grouped.has( id ) && !retainedDeferred( batch ) ) {
					for ( const draw of batch.draws ) {
						geometry.release( draw );
					}
					batches.delete( id );
				}
			}
			for ( const [id, rows] of grouped ) {
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
						particleInstances: model.primitives.map( p =>
							p.emission ?
								new Float32Array(
									rows.length * (p.emission.capacity ?? p.emission.births.length) * 16
								) :
								undefined
						),
						appearances: model.primitives.map( p =>
							p.materialFrames || rows[0]!.materialTint ?
								new Float32Array(
									capacity * (p.emission?.capacity ?? p.emission?.births.length ?? 1) * 8
								) :
								undefined
						)
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
							faceEffectMesh( batch.palettes[p]!, offset, transform, view!, primitive.billboard );
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
						const positions = new Float32Array( capacity * 3 ),
							colors = new Float32Array( capacity * 4 ),
							uvs = new Float32Array( capacity * 2 ),
							indices = new Uint32Array( capacity * 3 );
						let vertex = 0, index = 0;
						for ( const actor of rows ) {
							const groups = new Map<unknown, RibbonPoint[]>(),
								emission = primitive.emission!,
								history = particleBirths.get( actor.gid )!,
								matrices = history.matrices[p]!;
							const elements = primitive.particleEmitter === undefined ?
								undefined :
								history.graph?.elements[primitive.particleEmitter];
							const order = elements ?
								elements.map( ( _, i ) => i ).filter( i => elements[i]?.alive ).sort( ( a, b ) =>
									elements[b]!.born - elements[a]!.born
								) :
								emission.births.map( ( _, i ) => i ).reverse();
							for ( const b of order ) {
								const element = elements?.[b],
									birth = element ? element.clockBirth / 20 : emission.births[b]!;
								if ( !elements && actor.emissionEnd !== undefined && birth >= actor.emissionEnd ) {
									continue;
								}
								const group = element?.parent ?? 0, points = groups.get( group ) ?? [];
								groups.set( group, points );
								const elapsed = actor.time - birth,
									age = emission.loop && elapsed >= 0 ? elapsed % emission.lifetime : elapsed;
								if (
									age < 0 || age >= emission.lifetime || !Number.isFinite( matrices[b * 16 + 15] )
								) continue;
								const frame = Math.min(
										primitive.ribbon.widths.length - 1,
										Math.floor( age * primitive.ribbon.fps )
									),
									material = primitive.materialFrames!,
									at = Math.min( material.colors.length / 4 - 1, Math.floor( age * material.fps ) );
								const scale =
									Math.hypot( matrices[b * 16]!, matrices[b * 16 + 1]!, matrices[b * 16 + 2]! ) *
									model.nodes[0]!.scale[0]!;
								const color = Array.from( material.colors.subarray( at * 4, at * 4 + 4 ) );
								color[3]! *= opacity( actor );
								points.push( {
									position: Array.from( matrices.subarray( b * 16 + 12, b * 16 + 15 ) ),
									color,
									width: (primitive.ribbon.widths[frame] ?? 1) * scale
								} );
							}
							for ( const group of groups.values() ) {
								const strip = ribbonStrip( ribbonSpline( group ), view! );
								positions.set( strip.positions, vertex * 3 );
								colors.set( strip.colors, vertex * 4 );
								uvs.set( strip.uvs, vertex * 2 );
								for ( const value of strip.indices ) indices[index++] = value + vertex;
								vertex += strip.positions.length / 3;
							}
						}
						let draw = batch.draws[p];
						if ( !draw ) {
							draw = geometry.upload( {
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
							geometry.updatePositions( draw, positions, colors, uvs );
							if ( preview ) geometry.updateTransform( draw, view! );
						}
						geometry.updateIndices( draw, indices.subarray( 0, index ) );
						output.push( draw );
						continue;
					}
					const appearance = batch.appearances[p];
					const emitted = batch.particleInstances[p];
					let instances = batch.instances.subarray( 0, rows.length * 16 );
					// Graph particles share this immutable bind scale. Copy it
					// into each palette before applying that particle's scale.
					const graphPalette = primitive.emission && model.particleGraph ? identity() : undefined;
					if ( graphPalette ) {
						for ( let axis = 0; axis < 3; axis++ ) graphPalette[axis * 5] = model.nodes[0]!.scale[axis]!;
					}
					const particleTimes: number[] = [], particleOpacities: number[] = [];
					if ( primitive.emission && emitted ) {
						let count = 0;
						for ( let i = 0; i < rows.length; i++ ) {
							const actor = rows[i]!,
								state = poses.get( actor.gid )!,
								birthMatrices = particleBirths.get( actor.gid )!.matrices[p]!;
							const elements = primitive.particleEmitter === undefined ?
								undefined :
								particleBirths.get( actor.gid )!.graph?.elements[primitive.particleEmitter];
							for ( let b = 0; b < (elements?.length ?? primitive.emission.births.length); b++ ) {
								const element = elements?.[b];
								if ( elements && !element?.alive ) continue;
								const birth = element ? element.clockBirth / 20 : primitive.emission.births[b]!,
									transform = birthMatrices.subarray( b * 16, b * 16 + 16 );
								const elapsed = actor.time - birth,
									age = primitive.emission.loop && elapsed >= 0 ?
										elapsed % primitive.emission.lifetime :
										elapsed;
								if (
									age < 0 || age >= primitive.emission.lifetime ||
									!Number.isFinite( transform[15] ) ||
									!elements && actor.emissionEnd !== undefined && birth >= actor.emissionEnd
								) continue;
								emitted.set( transform, count * 16 );
								const offset = count * primitive.joints.length * 16;
								if ( graphPalette ) batch.palettes[p]!.set( graphPalette, offset );
								else state.pose.palette( primitive, batch.palettes[p]!, offset );
								const particle = particleBirths.get( actor.gid )!.programs[p]![b];
								if ( particle ) {
									if ( model.particleGraph ) {
										for ( let axis = 0; axis < 3; axis++ ) {
											for ( let row = 0; row < 3; row++ ) {
												batch.palettes[p]![offset + axis * 4 + row]! *= particle.scale[axis]!;
											}
										}
									} else if ( primitive.particleProgram ) {
										particleRandomIndex = advanceParticle(
											particle,
											primitive.particleProgram,
											Math.floor( age * 20 ),
											particleRandom,
											particleRandomIndex
										);
										placeParticle( particle, batch.palettes[p]!, offset );
									}
								}
								if ( primitive.billboard ) {
									faceEffectMesh( batch.palettes[p]!, offset, transform, view!, primitive.billboard );
								}
								if ( particle ) placeParticle( particle, batch.palettes[p]!, offset, true );
								particleTimes.push( age );
								if ( fading ) particleOpacities.push( opacity( actor ) );
								count++;
							}
						}
						instances = emitted.subarray( 0, count * 16 );
					}
					if ( primitive.materialFrames && appearance ) {
						const frames = primitive.materialFrames, count = frames.colors.length / 4;
						for ( let i = 0; i < (emitted ? particleTimes.length : rows.length); i++ ) {
							const at = Math.max(
									0,
									Math.min( count - 1, (emitted ? particleTimes[i]! : rows[i]!.time) * frames.fps )
								),
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
					if ( appearance && !emitted ) {
						for ( let i = 0; i < rows.length; i++ ) {
							if ( !primitive.materialFrames ) appearance.set( [ 1, 1, 1, 1, 1, 1, 0, 0 ], i * 8 );
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
						draw = geometry.upload(
							{
								...primitive.geometry,
								world: !preview,
								material: {
									...(modifierClocks?.[p]?.material ?? primitive.geometry.material!),
									...(rows[0]!.deferredParticle ? { deferredParticle: true } : {}),
									instanceMaterialTint: !!rows[0]!.materialTint,
									...(fading ? { blend: true, instanceFade: true } : {}),
									...(preview ? { fogDisabled: true } : {})
								},
								instances: emitted ?? batch.instances,
								bones: batch.palettes[p],
								transform: preview ? view! : identity()
							},
							resource.textures[primitive.image],
							stream?.offsets,
							primitive.equipmentGlow && !fading && (rows[0]!.animationLod?.fraction ?? 0) <= .5 ?
								resource.textures[primitive.equipmentGlow.image] :
								primitive.environmentImage === undefined ?
								undefined :
								resource.textures[primitive.environmentImage]
						);
						batch.draws[p] = draw;
						if ( capacity !== rows.length || fading || appearance || emitted || pointLights ) {
							draw = geometry.updateInstances(
								draw,
								instances,
								fading ?
									Float32Array.from( emitted ? particleOpacities : rows.map( opacity ) ) :
									undefined,
								appearance?.subarray( 0, instances.length / 2 ),
								pointLights,
								paletteOffsets
							);
							batch.draws[p] = draw;
						}
					} else {
						if (
							instancesChanged || fading || appearance || emitted || pointLights || stream?.mappingChanged
						) {
							draw = geometry.updateInstances(
								draw,
								instances,
								fading ?
									Float32Array.from( emitted ? particleOpacities : rows.map( opacity ) ) :
									undefined,
								appearance?.subarray( 0, instances.length / 2 ),
								pointLights,
								paletteOffsets
							);
						}
						batch.draws[p] = draw;
						if ( !stream ) {
							const upload = emitted ?
								batch.palettes[p]! :
								batch.palettes[p]!.subarray( 0, rows.length * primitive.joints.length * 16 );
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
================
		*/
		dispose( geometry: GeometryCommands | null, images: ImageCommands | null ) {
			if ( disposed ) {
				return;
			}
			disposed = true;
			framePoses = null;
			residentBytes = 0;
			for ( const batch of batches.values() ) {
				for ( const draw of batch.draws ) {
					geometry?.release( draw );
				}
			}
			for ( const resource of models.values() ) {
				if ( resource.owned ) {
					for ( const image of resource.images ) {
						image.close();
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
