/*
===========================================================================

actor-advance.ts - an actor's clocks, sounds and published actor

The part of the per-actor pass that runs on what actor-motion.ts decided
(MotionDecision): the dispatch advances each layer's installation by the
frame's animation delta and writes the shared time back, the sound lanes
advance over the dispatched ranges, the modifier layers and the model
animation follow, and the actor is written into frame.next with its foot
contacts and emission holders. Its statements are present's after
decide's, in present's order.

===========================================================================
*/
import { createModelAnimation } from "@/engine/foundation/animation/model-animation";
import { type AnimationDispatch, createAnimationDispatch } from "@/engine/foundation/animation/animation-dispatch";
import type { AnimationActivation } from "@/engine/foundation/animation/animation-activation";
import { blindableCharacter, blindableCompanion } from "@/engine/foundation/ui/name-visibility";
import { cosScale, monsterScale } from "@/engine/foundation/rendering/monster-scale";
import { characterHeadingYaw } from "@/engine/foundation/math/angles";
import type { EntityState } from "@/engine/contracts/world";
import type { CharacterActor } from "@/engine/contracts/character";
import type {
	ActorAppearance,
	ActorFrame,
	ActorOwner,
	ActorPass,
	KeptRecord,
	MotionDecision,
	RecordStamp,
	StampScratch
} from "./internal/presentation-contract";
import { worldAudioPosition } from "@/engine/foundation/world/world-space";
import { flightSoundDefinition } from "@/engine/foundation/animation/wing-flight";

const PROTECTED_ANIMATION_DISTANCE = 300;

/*
================
createActorAdvance
================
*/
export function createActorAdvance( owner: ActorOwner ) {
	// The stamp advance fills each frame; an actor built again keeps a copy.
	const scratch = {} as StampScratch<RecordStamp>;
	/*
	================
	recordStamp

	The inputs of a steady decision's published actor beyond the decision
	and the resolved appearance (RecordStamp), read as the actor's own
	fields read them, into the owner's one scratch stamp. Pure reads.
	================
	*/
	function recordStamp(
		entity: EntityState,
		resolved: ActorAppearance,
		decision: MotionDecision,
		frame: ActorFrame
	): RecordStamp {
		const { effects, entityLod, published } = owner;
		const { gameplay, localMover, seconds } = frame;
		const { resource } = resolved, { state, renderPose, cast, dead } = decision, stamp = scratch;
		stamp.resolved = resolved;
		stamp.manifest = published.manifest;
		stamp.localGid = gameplay?.localGid;
		stamp.shadowSize = !entity.groundItem ? published.shadowSizes.get( entity.refObjId ) : undefined;
		stamp.bloodEffects = published.bloodEffects.get( resource.codename );
		stamp.appearance = effects.appearance( entity.gid );
		stamp.fraction = entityLod.fraction( entity.gid );
		stamp.crowded = entityLod.crowded();
		stamp.optional = !localMover( entity.gid ) && !entity.mountedOn && !cast && !dead &&
			entity.gid !== gameplay?.target && entity.gid !== gameplay?.targetPending &&
			entityLod.distance( entity.gid ) > PROTECTED_ANIMATION_DISTANCE &&
			!gameplay?.casts.some( cast => cast.target === entity.gid );
		stamp.regionId = renderPose.regionId;
		stamp.x = renderPose.x;
		stamp.y = renderPose.y;
		stamp.z = renderPose.z;
		stamp.yaw = characterHeadingYaw( renderPose.angle );
		stamp.height = published.heights.has( resource.codename ) ?
			published.heights.get( resource.codename )! *
			(state.actionHeight ?
				state.actionHeight.from +
				(state.actionHeight.to - state.actionHeight.from) *
					Math.min( 1, Math.max( 0, seconds - state.actionHeight.at ) ) :
				1) :
			undefined;
		stamp.petResolve = (entity.petResolve?.untilMs ?? 0) > seconds * 1000;
		stamp.modifierId = state.modifierId;
		return stamp;
	}
	return {
		/*
		================
		advance

		Advances the decided layers' clocks and sounds and publishes the
		actor into frame.next.
		================
		*/
		advance(
			entity: EntityState,
			resolved: ActorAppearance,
			decision: MotionDecision,
			frame: ActorFrame,
			pass: ActorPass,
			kept?: KeptRecord
		): Omit<KeptRecord, "retained"> | undefined {
			const {
				committedAuxiliary,
				displayedDependencies,
				effects,
				entityLod,
				footContact,
				published,
				renderer,
				resources,
				soundSurface,
				sounds
			} = owner;
			const {
				animationDeltaMs,
				gameplay,
				localMover,
				next,
				probe,
				sampleActorDetails,
				seconds,
				soundContext
			} = frame;
			const { animationHolders, particleHolders } = pass;
			const {
				resource,
				model,
				dependencies,
				auxiliaryCommit,
				particleCommit,
				defaultWearCommit
			} = resolved;
			const {
				state,
				nativePose,
				renderPose,
				layers,
				activationKeys,
				installations,
				cast,
				dead,
				authorityDead,
				seat,
				clip,
				looping,
				posture,
				armedIdle,
				actionMask
			} = decision;
			if ( sampleActorDetails ) probe?.detailBegin( "actor-sounds" );
			const metadata = resource.animationStates ?? published.animationStates.get( resource.codename );
			// Retain the native idle metadata through its outgoing blend.
			const motionMetadata = ( name: string ) =>
				state.stallMotion?.motion?.clip === name ?
					state.stallMotion.motion.definition :
					state.combatIdle?.motion?.clip === name ?
					state.combatIdle.motion.definition :
					metadata?.[name] ?? published.animationStates.get( resource.codename )?.[name];
			// Resolve equipment, surface and world position only when a cue
			// is due. Cursor advancement remains independent of visibility.
			const soundSource = () => {
				const context = soundContext( entity, cast?.skill, state.hitCritical );
				return {
					profile: resource.soundProfileName ?? published.soundProfiles.get( resource.codename ) ??
						resource.codename,
					position: worldAudioPosition( nativePose ),
					surface: context.player ? soundSurface( nativePose ) : undefined,
					context
				};
			};
			// ADD670 retains both outgoing and incoming installations. Clip
			// names and weights cannot identify cursors across a reinstallation.
			const retainedActivations = new Set<AnimationActivation>();
			const silentWingFlight = renderer.silentWingFlight?.() === true;
			const dispatch = (state.dispatch ??= createAnimationDispatch()).step(
				layers,
				animationDeltaMs,
				name =>
					motionMetadata( name )?.durationMs ??
						Math.round( resources.duration( resource.glb, name ) * 1000 )
			);
			// Most actors have one installation. Keep that row directly; blends
			// share one index for both pose and sound instead of copying two maps.
			// Dispatch omits zero-duration rows, so even the single row must match
			// by installation identity rather than by its position in the list.
			const single = dispatch.length === 1 ? dispatch[0] : undefined;
			const sampled = dispatch.length > 1 ? new Map<AnimationActivation, AnimationDispatch>() : undefined;
			if ( sampled ) {
				for ( const row of dispatch ) sampled.set( row.activation, row );
			}
			for ( let i = 0; i < layers.length; i++ ) {
				const layer = layers[i]!;
				const row = single?.activation === layer.activation ? single : sampled?.get( layer.activation! );
				layers[i] = row?.layer ?? layer;
			}
			for ( const layer of layers ) {
				if ( !layer.activation ) throw Error( "Missing animation installation" );
				retainedActivations.add( layer.activation );
				const row = single?.activation === layer.activation ? single : sampled?.get( layer.activation );
				sounds.advance(
					entity.gid,
					layer.clip,
					layer.activation.started,
					(row?.elapsedMs ?? 0) / 1000,
					layer.loop,
					flightSoundDefinition( layer.clip, motionMetadata( layer.clip ), silentWingFlight ),
					seconds,
					soundSource,
					layer.activation
				);
			}
			sounds.retainActivations( entity.gid, retainedActivations );
			for ( const key of installations.keys() ) {
				if ( !activationKeys.has( key ) ) installations.delete( key );
			}
			if ( sampleActorDetails ) {
				probe?.detailEnd( "actor-sounds" );
				probe?.detailBegin( "actor-record" );
			}
			// A steady decision's actor: kept for the next frame, and when this
			// frame reuses the decision that published it with the same
			// stamp, published again with only its clocks advanced. Every
			// other field is a function of the decision, the resolved
			// appearance, the entity row (all three the same) and the stamp.
			const stamp = decision.steady ? recordStamp( entity, resolved, decision, frame ) : undefined;
			let record: Omit<KeptRecord, "retained"> | undefined;
			if ( !owner.due.record( kept, stamp ) && kept && stamp ) {
				displayedDependencies.set( entity.gid, dependencies );
				if ( auxiliaryCommit !== undefined ) committedAuxiliary.set( entity.gid, auxiliaryCommit );
				state.modifierResource = resource;
				state.modifierLayers = layers;
				state.lastDrawnPose = { ...renderPose };
				const actor: CharacterActor = Object.freeze( {
					...kept.actor,
					modelAnimation: resource.modifierBindings?.length ?
						(state.modelAnimation ??= createModelAnimation()).step(
							dispatch,
							resource.modifierBindings,
							resource.modifierSelectors ?? []
						) :
						undefined,
					layers: posture || layers.length !== 1 || layers[0]!.clip !== clip ||
							layers[0]!.loop !== looping || layers[0]!.weight !== 1 || layers[0]!.lane === "event" ||
							state.locomotion!.outgoing.length ?
						layers :
						undefined,
					time: layers.length === 1 && layers[0]!.clip === clip ?
						layers[0]!.time :
						Math.max( 0, seconds - state.started )
				} );
				next.set( entity.gid, actor );
				if ( sampleActorDetails ) probe?.detailEnd( "actor-record" );
				record = { actor, stamp: kept.stamp };
			} else {
				displayedDependencies.set( entity.gid, dependencies );
				if ( auxiliaryCommit !== undefined ) committedAuxiliary.set( entity.gid, auxiliaryCommit );
				const appearance = effects.appearance( entity.gid ),
					baseScale = entity.kind === "monster" ?
						monsterScale( entity.rarity ?? 0, entity.tidWord ?? 0, resource.scalePercent ) :
						entity.kind === "cos" ?
						cosScale( resource.scalePercent ) :
						1;
				state.modifierResource = resource;
				state.modifierLayers = layers;
				state.lastDrawnPose = { ...renderPose };
				next.set(
					entity.gid,
					Object.freeze( {
						shadowSize: !entity.groundItem ? published.shadowSizes.get( entity.refObjId ) : undefined,
						modelAnimation: resource.modifierBindings?.length ?
							(state.modelAnimation ??= createModelAnimation()).step(
								dispatch,
								resource.modifierBindings,
								resource.modifierSelectors ?? []
							) :
							undefined,
						animationLod: {
							fraction: entityLod.fraction( entity.gid ),
							crowded: entityLod.crowded(),
							// Dispatch and gameplay run above even when a distant skeleton
							// reuses its last sample. Protect combat and the whole ride.
							optional: !localMover( entity.gid ) && !entity.mountedOn && !cast && !dead &&
								entity.gid !== gameplay?.target && entity.gid !== gameplay?.targetPending &&
								entityLod.distance( entity.gid ) > PROTECTED_ANIMATION_DISTANCE &&
								!gameplay?.casts.some( cast => cast.target === entity.gid )
						},
						blindable: blindableCharacter( entity, gameplay?.localGid ),
						blindableCompanion: blindableCompanion( entity, gameplay?.localGid ),
						groundItem: !!entity.groundItem,
						dungeonBlock: entity.dungeonBlock,
						previewClip: resource.clipFacts.names.has( armedIdle ) ? armedIdle : "stand",
						materialTint: appearance.materialTint,
						pointLight: appearance.pointLight,
						...(appearance.boneRotation ? { boneRotation: appearance.boneRotation } : {}),
						modifierId: state.modifierId,
						bloodEffects: published.bloodEffects.get( resource.codename ),
						effectBaseScale: baseScale,
						heightFactor: published.heightFactors.get( resource.codename ),
						effectAnchor: published.effectAnchors.get( resource.codename ),
						pickable: !authorityDead || entity.gid === gameplay?.localGid,
						// 692680: a filtered pick (SHIFT, or a dead local player) takes a
						// dead player; dead monsters and companions stay out of it.
						pickWhenFiltered: authorityDead && entity.kind === "player",
						height: published.heights.has( resource.codename ) ?
							published.heights.get( resource.codename )! *
							(state.actionHeight ?
								state.actionHeight.from +
								(state.actionHeight.to - state.actionHeight.from) *
									Math.min( 1, Math.max( 0, seconds - state.actionHeight.at ) ) :
								1) :
							undefined,
						mountedOn: seat,
						gid: entity.gid,
						model,
						pose: {
							regionId: renderPose.regionId,
							x: renderPose.x,
							y: renderPose.y,
							z: renderPose.z,
							yaw: characterHeadingYaw( renderPose.angle )
						},
						clip,
						// A single WAIT/SHOT layer can differ from the base clip. Only
						// collapse a full-weight timed layer that the base fields reproduce.
						// decide installs the locomotion blend before it returns.
						layers: posture || layers.length !== 1 || layers[0]!.clip !== clip ||
								layers[0]!.loop !== looping || layers[0]!.weight !== 1 || layers[0]!.lane === "event" ||
								state.locomotion!.outgoing.length ?
							layers :
							undefined,
						time: layers.length === 1 && layers[0]!.clip === clip ?
							layers[0]!.time :
							Math.max( 0, seconds - state.started ),
						loop: looping,
						scale: Math.fround(
							baseScale * appearance.scale *
								(1 +
									((entity.petResolve?.untilMs ?? 0) > seconds * 1000 ?
										entity.petResolve!.stacks * .012 :
										0))
						)
					} )
				);
				if ( sampleActorDetails ) probe?.detailEnd( "actor-record" );
				record = stamp && { actor: next.get( entity.gid )!, stamp: { ...stamp } };
			}
			// The record's tail, the same after either record above: foot
			// contacts over the dispatched ranges, the emission holders, the
			// committed default wear and equipment particles.
			if ( actionMask & 0x200 ) {
				const actor = next.get( entity.gid )!;
				for ( const row of dispatch ) {
					for ( const [from, to] of row.ranges ) {
						for ( const event of motionMetadata( row.layer.clip )?.trackEvents ?? [] ) {
							if ( event.eventCode === 2 && event.cursorMs > from && event.cursorMs <= to ) {
								footContact(
									entity,
									[ ...next.values() ],
									actor,
									nativePose,
									event.param0 !== 0,
									seconds
								);
							}
						}
					}
				}
			}
			if ( resource.animationParticles?.length ) {
				animationHolders.push( { actor: next.get( entity.gid )!, sets: resource.animationParticles } );
			}
			if ( defaultWearCommit !== undefined ) state.defaultWear = { resource, keys: defaultWearCommit };
			if ( particleCommit !== undefined ) state.equipmentParticles = particleCommit;
			if ( state.equipmentParticles?.length ) {
				particleHolders.push( { actor: next.get( entity.gid )!, particles: state.equipmentParticles } );
			}
			return record;
		}
	};
}
