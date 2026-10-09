/*
===========================================================================

presentation-auxiliary.ts - presentation-owned hair, rides, booths and avatar children

Children follow the committed body selection. Their ids and locomotion
survive frame publication, and retirement can borrow their current actors.

===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";
import type { EntityState, WorldEvent } from "@/engine/contracts/world";
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { Renderer } from "@/engine/contracts/runtime";
import type { DressCatalog } from "@/engine/foundation/animation/equipment-appearance";
import { CHARACTER_ACTORS } from "@/engine/foundation/animation/character-budget";
import {
	changeLocomotion,
	locomotionLayers,
	type LocomotionBlend
} from "@/engine/foundation/animation/locomotion-blend";
import { movementGait } from "@/engine/foundation/gameplay/native-movement";
import { animationActivation, type AnimationActivation } from "@/engine/foundation/animation/animation-activation";
import { createModifierDelta } from "@/engine/foundation/rendering/modifier-delta";
import { createAnimationDispatch } from "@/engine/foundation/animation/animation-dispatch";
import {
	createModelAnimation,
	type ModelAnimationBinding,
	type ModifierSelector
} from "@/engine/foundation/animation/model-animation";
import type { AnimationParticleSet } from "@/engine/foundation/animation/animation-emission";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import type {
	Resource,
	LinkedRide,
	Auxiliary,
	SecondaryModel,
	ItemPresentation
} from "./internal/presentation-contract";
import { STALL_TITLE_MODE } from "@/engine/foundation/gameplay/interaction-approach";

// The ride transform modes 8602C0 reads from ride+0x29D (EffectSyntax_RotationType
// table CCDB10: none = 0, RT_FIXED = 1, RT_DUMMY = 2).
const RIDER_ON_SADDLE = 0;
const RIDE_COPIES_RIDER = 2;
const CHINESE_BOOTH = "res/item/china/item/cj_store.bsr";
const EUROPEAN_BOOTH = "res/item/europe/item/euro_streetstall01.bsr";

/*
================
BoothFrame
================
*/
interface BoothFrame {
	readonly entities: readonly EntityState[];
	readonly seconds: number;
	readonly next: Map<number, CharacterActor>;
	readonly animationHolders: { actor: CharacterActor; sets: readonly AnimationParticleSet[]; }[];
	readonly particleHolders: { actor: CharacterActor; particles: readonly ModelParticle[]; }[];
}

/*
================
BoothBindings
================
*/
interface BoothBindings {
	readonly boothModels: ReadonlyMap<string, SecondaryModel>;
	readonly items: Readonly<Record<string, ItemPresentation>>;
	readonly resources: {
		plan( paths: readonly string[] ): boolean;
		ready( path: string ): boolean;
		duration( path: string, clip: string ): number;
	};
	readonly renderer: Pick<Renderer, "setCharacterAssembly">;
}

/*
================
boothResourcePath

86A90E falls through to the race default only when the item lookup is null.
An existing item with no model does not silently substitute a default booth.
================
*/
function boothResourcePath( entity: EntityState, items: BoothBindings["items"] ): string | undefined {
	if ( (entity.titleId ?? 0) > 0 ) {
		const item = items[String( entity.titleId )];
		if ( item ) {
			const path = item.wornModelPath?.replaceAll( "\\", "/" ).toLowerCase();
			// Itemcommon names are relative to res; the NPC catalog keys full BSRs.
			return path ? (path.startsWith( "res/" ) ? path : "res/" + path) : undefined;
		}
	}
	return entity.countryByte9c === 0 ? CHINESE_BOOTH : entity.countryByte9c === 1 ? EUROPEAN_BOOTH : undefined;
}

/*
================
AuxiliaryFrame
================
*/
interface AuxiliaryFrame {
	readonly entities: readonly EntityState[];
	readonly seconds: number;
	readonly next: Map<number, CharacterActor>;
	readonly gameplay: GameplayState | null;
	readonly localMover: ( gid: number ) => boolean;
	readonly castByActor: ReadonlyMap<number, unknown>;
}

/*
================
AuxiliaryBindings
================
*/
interface AuxiliaryBindings {
	readonly resourceFor: ( entity: EntityState ) => Resource | undefined;
	readonly dress: DressCatalog;
	readonly resources: { ready( path: string ): boolean; };
	readonly ridesByRider: ReadonlyMap<string, LinkedRide>;
	readonly riderModes: ReadonlyMap<string, number>;
	readonly committedAuxiliary: Map<number, readonly Auxiliary[]>;
}

/*
================
StageFrame
================
*/
interface StageFrame {
	readonly orbActors: readonly CharacterActor[];
	readonly effectActors: readonly CharacterActor[];
	readonly next: Map<number, CharacterActor>;
	readonly seconds: number;
	readonly animationHolders: { actor: CharacterActor; sets: readonly AnimationParticleSet[]; }[];
}

/*
================
StageBindings
================
*/
interface StageBindings {
	readonly renderer: Pick<Renderer, "setCharacterAssembly">;
	readonly resources: { duration( path: string, clip: string ): number; };
	readonly effects: {
		animationModel( path: string ): {
			bindings: readonly ModelAnimationBinding[];
			selectors: readonly ModifierSelector[];
			particles: readonly AnimationParticleSet[];
		} | undefined;
	};
}

/*
================
createAuxiliaryPresentation
================
*/
export function createAuxiliaryPresentation( allocateActor: () => number ) {
	const booths = new Map<number, {
		gid: number;
		resource: SecondaryModel;
		started?: number;
		activation?: AnimationActivation;
		delta: ReturnType<typeof createModifierDelta>;
		dispatch: ReturnType<typeof createAnimationDispatch>;
		selection: ReturnType<typeof createModelAnimation>;
	}>();
	const stageAnimations = new Map<
		number,
		{
			model: string;
			clip: string;
			activation: AnimationActivation;
			delta: ReturnType<typeof createModifierDelta>;
			dispatch: ReturnType<typeof createAnimationDispatch>;
			selection: ReturnType<typeof createModelAnimation>;
		}
	>();
	const hwanHairActors = new Map<number, { gid: number; started: number; }>();
	const linkedRides = new Map<number, number>();
	const auxiliaryActors = new Map<
		number,
		Map<number, { gid: number; model: string; motion: LocomotionBlend; }>
	>();
	return {
		linkedRides,
		auxiliaryActors,
		/*
		================
		receiveBooths

		A close or despawn can be followed by a reopen/spawn before the next
		frame. Retire at the event boundary so reused gids cannot inherit it.
		================
		*/
		receiveBooths( events: readonly WorldEvent[] ) {
			for ( const event of events ) {
				if ( event.kind === "reset" ) booths.clear();
				else if ( event.kind === "despawn" ) booths.delete( event.gid );
				else if (
					event.kind === "spawn" ||
					event.kind === "state" && event.entity.appearanceState?.[6] !== STALL_TITLE_MODE
				) {
					booths.delete( event.entity.gid );
				}
			}
		},
		/*
		================
		presentBooths

		The separate CCObjAnimation is cached until close (86A880). It never
		borrows the character's stall posture clip or animation clock.
		================
		*/
		presentBooths( frame: BoothFrame, bindings: BoothBindings ) {
			const { entities, seconds, next, animationHolders, particleHolders } = frame;
			const { boothModels, items, resources, renderer } = bindings;
			const alive = new Set<number>();
			for ( const entity of entities ) {
				if (
					(entity.kind !== "player" && entity.kind !== "local-player") ||
					entity.appearanceState?.[6] !== STALL_TITLE_MODE
				) continue;
				alive.add( entity.gid );
				const owner = next.get( entity.gid );
				if ( !owner ) continue;
				let state = booths.get( entity.gid );
				if ( !state ) {
					const path = boothResourcePath( entity, items ),
						resource = path ? boothModels.get( path ) : undefined;
					if ( !resource ) continue;
					state = {
						gid: allocateActor(),
						resource,
						delta: createModifierDelta(),
						dispatch: createAnimationDispatch(),
						selection: createModelAnimation()
					};
					booths.set( entity.gid, state );
				}
				const resource = state.resource;
				const paths = [
					resource.glb,
					...(resource.animationParticlePaths ?? []),
					...(resource.ambientParticles ?? []).map( particle =>
						"/assets/effects/programs.json#" + encodeURIComponent( particle.effectPath )
					)
				];
				// Start only after the model and particle programs are resident, so
				// admission cannot consume a time-zero animation modifier key.
				const ready = paths.map( path => resources.ready( path ) ).every( Boolean );
				if ( !ready || !resources.plan( paths ) || next.size >= CHARACTER_ACTORS ) continue;
				if ( state.started === undefined ) {
					state.started = seconds;
					state.activation = animationActivation( seconds );
				}
				const clip = resource.clips.includes( "stand" ) ? "stand" : "";
				const layers: NonNullable<CharacterActor["layers"]> = clip ?
					[ {
						clip,
						time: seconds - state.started,
						loop: true,
						weight: 1,
						lane: "timed",
						activation: state.activation
					} ] :
					[];
				const dispatch = state.dispatch.step(
					layers,
					state.delta( seconds ),
					name =>
						resource.animationStates?.[name]?.durationMs ??
							Math.trunc( resources.duration( resource.glb, name ) * 1000 )
				);
				const model = `booth:${state.gid}:${resource.glb}`;
				renderer.setCharacterAssembly( model, resource.glb, [] );
				// 86AB90 copies the body's complete geometry matrix. Root attachment
				// retains its scale; the renderer also inherits current body alpha.
				const actor: CharacterActor = {
					gid: state.gid,
					model,
					pose: owner.pose,
					clip,
					time: seconds - state.started,
					loop: true,
					layers,
					scale: 1,
					pickable: false,
					attachment: { gid: entity.gid, bone: "", root: true, offset: [ 0, 0, 0 ] },
					modelAnimation: state.selection.step(
						dispatch,
						resource.modifierBindings ?? [],
						resource.modifierSelectors ?? []
					)
				};
				next.set( actor.gid, actor );
				if ( resource.ambientParticles?.length ) {
					particleHolders.push( { actor, particles: resource.ambientParticles } );
				}
				if ( resource.animationParticles?.length ) {
					animationHolders.push( { actor, sets: resource.animationParticles } );
				}
			}
			for ( const gid of booths.keys() ) if ( !alive.has( gid ) ) booths.delete( gid );
		},
		/*
		================
		presentStages
		================
		*/
		presentStages( frame: StageFrame, bindings: StageBindings ) {
			const { orbActors, effectActors, next, seconds, animationHolders } = frame;
			const { renderer, resources, effects } = bindings;
			const stageAlive = new Set<number>();
			for ( const actor of [ ...orbActors, ...effectActors ] ) {
				if ( next.size >= CHARACTER_ACTORS ) continue;
				const model = `effect:${actor.gid}:${actor.model}`;
				renderer.setCharacterAssembly( model, actor.model, [] );
				const metadata = effects.animationModel( actor.model );
				let modelAnimation: CharacterActor["modelAnimation"];
				if ( metadata?.bindings.length ) {
					stageAlive.add( actor.gid );
					let holder = stageAnimations.get( actor.gid );
					if ( !holder || holder.model !== actor.model ) {
						holder = {
							model: actor.model,
							clip: actor.clip,
							activation: animationActivation( seconds ),
							delta: createModifierDelta(),
							dispatch: createAnimationDispatch(),
							selection: createModelAnimation()
						};
						stageAnimations.set( actor.gid, holder );
					}
					if ( holder.clip !== actor.clip ) {
						holder.clip = actor.clip;
						holder.activation = animationActivation( seconds );
					}
					const layers = actor.layers ??
						[ {
							clip: actor.clip,
							time: actor.time,
							weight: 1,
							loop: actor.loop,
							lane: actor.loop ? "timed" as const : "event" as const,
							activation: holder.activation
						} ];
					const dispatch = holder.dispatch.step(
						layers,
						holder.delta( seconds ),
						clip => Math.trunc( resources.duration( actor.model, clip ) * 1000 )
					);
					modelAnimation = holder.selection.step( dispatch, metadata.bindings, metadata.selectors );
				}
				next.set( actor.gid, { ...actor, model, modelAnimation, effectEntity: true } );
				if ( metadata?.particles.length ) {
					animationHolders.push( { actor: next.get( actor.gid )!, sets: metadata.particles } );
				}
			}
			for ( const gid of stageAnimations.keys() ) if ( !stageAlive.has( gid ) ) stageAnimations.delete( gid );
		},
		/*
		================
		resetStages
		================
		*/
		resetStages() {
			stageAnimations.clear();
			booths.clear();
		},
		/*
		================
		step
		================
		*/
		step( frame: AuxiliaryFrame, bindings: AuxiliaryBindings ) {
			const { entities, seconds, next, gameplay, localMover, castByActor } = frame;
			const { resourceFor, dress, resources, ridesByRider, riderModes, committedAuxiliary } = bindings;
			const hairOwners = new Set<number>();
			for ( const entity of entities ) {
				if ( entity.appearanceState?.[2] !== 1 ) continue;
				const resource = resourceFor( entity ), owner = next.get( entity.gid );
				if ( !resource?.codename.startsWith( "CHAR_CH_" ) || !owner ) continue;
				const hair = dress.hwan?.[resource.codename.includes( "_MAN_" ) ? "CH_M" : "CH_W"];
				if ( !hair || !resources.ready( hair.glb ) ) continue;
				hairOwners.add( entity.gid );
				let state = hwanHairActors.get( entity.gid );
				if ( !state ) {
					state = { gid: allocateActor(), started: seconds };
					hwanHairActors.set( entity.gid, state );
				}
				// AB5870 cancels the parent bind rotation and keeps its sampled position.
				// A missing hair marker retains the renderer's existing parent fallback.
				next.set( state.gid, {
					shadowAttachment: true,
					gid: state.gid,
					model: hair.glb,
					pose: owner.pose,
					clip: hair.clip,
					time: seconds - state.started,
					loop: true,
					scale: 1,
					pickable: false,
					attachment: { gid: entity.gid, bone: hair.bone, offset: [ 0, 0, 0 ], basis: "compound" }
				} );
			}
			for ( const gid of hwanHairActors.keys() ) if ( !hairOwners.has( gid ) ) hwanHairActors.delete( gid );
			// CICMonster_DeserializeSpawnPacket (861B00): a characterInfo ride BSR
			// becomes a second entity linked as the rider's ride (+0x2A0), its
			// transform mode (+0x29D) copied from the rider's Ride Type. Every motion
			// the rider plays is forwarded to it (CICharactor_PlayAnimationByMotionId
			// 85ED80; CCObjCharacter_PlayAnimationWithFallback keeps a missing clip on
			// "stand"), and it leaves with the rider (CICharactor_DespawnWithFade
			// 855B00). CICUser_SubmitBodyRideAndAttachments (8602C0) composes them:
			// mode 0 seats the rider on the ride's animated "saddle", mode 2 copies the
			// rider's world matrix onto the ride, mode 1 (RT_FIXED) links neither. The
			// ride carries no scale of its own (861B00 never calls SetModelScale on it).
			const rideOwners = new Set<number>();
			for ( const entity of entities ) {
				const resource = resourceFor( entity ), owner = next.get( entity.gid );
				const ride = resource ? ridesByRider.get( resource.codename ) : undefined;
				if ( !owner || !ride || !resources.ready( ride.glb ) || next.size >= CHARACTER_ACTORS ) continue;
				rideOwners.add( entity.gid );
				let gid = linkedRides.get( entity.gid );
				if ( gid === undefined ) {
					gid = allocateActor();
					linkedRides.set( entity.gid, gid );
				}
				const mode = riderModes.get( resource!.codename ) ?? 0;
				const motion = ( clip: string ) => ride.clips.includes( clip ) ? clip : "stand";
				next.set( gid, {
					gid,
					model: ride.glb,
					pose: owner.pose,
					clip: motion( owner.clip ),
					layers: owner.layers?.map( layer => ({ ...layer, clip: motion( layer.clip ) }) ),
					time: owner.time,
					loop: owner.loop,
					scale: 1,
					opacity: owner.opacity,
					height: owner.height,
					// World_PickEntityAtScreenPoint (692680): a ride answers with its rider.
					pickable: owner.pickable,
					pickOwner: entity.gid,
					...(mode === RIDE_COPIES_RIDER ?
						{ attachment: { gid: entity.gid, bone: "", root: true, offset: [ 0, 0, 0 ] as const } } :
						{})
				} );
				if ( mode === RIDER_ON_SADDLE ) next.set( entity.gid, { ...owner, mountedOn: gid } );
			}
			for ( const gid of linkedRides.keys() ) if ( !rideOwners.has( gid ) ) linkedRides.delete( gid );
			// 8E9DD0 / 8EA7A0: auxiliary resource is the item's second animated
			// handle. Its lifetime follows the COMMITTED body selection, including
			// cold replacement fallback, not the newest unready inventory plan.
			for ( const entity of entities ) {
				const owner = next.get( entity.gid );
				if ( !owner ) continue;
				const entries = committedAuxiliary.get( entity.gid ) ?? [];
				if ( !entries.length ) {
					auxiliaryActors.delete( entity.gid );
					continue;
				}
				let children = auxiliaryActors.get( entity.gid );
				if ( !children ) {
					children = new Map();
					auxiliaryActors.set( entity.gid, children );
				}
				const alive = new Set<number>();
				let moving = (localMover( entity.gid ) ? gameplay!.moving : entity.moving) ?? false;
				const requested = entity.mountedOn !== undefined || entity.appearanceState?.[0] === 2 ||
						castByActor.has( entity.gid ) ?
					"stand" :
					moving ?
					movementGait( entity.movementMode ) :
					"stand";
				for ( const { id, entry } of entries ) {
					alive.add( id );
					let child = children.get( id );
					if ( !child || child.model !== entry.glb ) {
						child = {
							gid: allocateActor(),
							model: entry.glb,
							motion: { ...changeLocomotion( undefined, "stand", true, seconds, "stand" ), enter: 0 }
						};
						children.set( id, child );
					}
					// 8E8340 retains the existing track on a missing lookup. Wing
					// BSRs have state 0 and 7, but no fabricated walk animation.
					if ( entry.clips.includes( requested ) && child.motion.clip !== requested ) {
						child.motion = {
							...changeLocomotion( child.motion, requested, true, seconds, requested ),
							enter: 0
						};
					}
					const layers = locomotionLayers( child.motion, seconds );
					if ( next.size >= CHARACTER_ACTORS ) continue;
					// Auxiliary BSR skeletons use the same AB5870 attach-root frame as Hwan hair.
					next.set( child.gid, {
						shadowAttachment: true,
						gid: child.gid,
						model: entry.glb,
						pose: owner.pose,
						clip: child.motion.clip,
						time: seconds - child.motion.started,
						loop: true,
						layers,
						scale: 1,
						pickable: false,
						attachment: { gid: entity.gid, bone: entry.bone, offset: [ 0, 0, 0 ], basis: "compound" }
					} );
				}
				for ( const id of children.keys() ) if ( !alive.has( id ) ) children.delete( id );
			}
			for ( const gid of committedAuxiliary.keys() ) {
				if ( !next.has( gid ) ) {
					committedAuxiliary.delete( gid );
					auxiliaryActors.delete( gid );
				}
			}
			for ( const gid of auxiliaryActors.keys() ) if ( !next.has( gid ) ) auxiliaryActors.delete( gid );
		},
		/*
		================
		resetHair

		The caller retains the original reset order between independent owners.
		================
		*/
		resetHair() {
			hwanHairActors.clear();
		},
		/*
		================
		resetChildren

		Drop every character's auxiliary children. linkedRides is not cleared
		here: no teardown ever cleared it.
		================
		*/
		resetChildren() {
			auxiliaryActors.clear();
		}
	};
}
