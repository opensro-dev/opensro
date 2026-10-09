/*
===========================================================================

actor-motion.ts - an actor's animation layers, sounds and published actor

The second half of the per-actor pass: death, sitting and posture, action
states, locomotion, hits and idles become the actor's layers, its sounds
advance, and the actor is written into frame.next. Moved verbatim from the
per-actor loop.

===========================================================================
*/
import { movementEntryRate, transitionActionStates } from "@/engine/foundation/animation/action-refresh";
import { selectAvatarOverride } from "@/engine/foundation/animation/avatar-override";
import { createModelAnimation } from "@/engine/foundation/animation/model-animation";
import { type AnimationDispatch, createAnimationDispatch } from "@/engine/foundation/animation/animation-dispatch";
import { animationActivation, type AnimationActivation } from "@/engine/foundation/animation/animation-activation";
import { blindableCharacter } from "@/engine/foundation/ui/name-visibility";
import { monsterScale } from "@/engine/foundation/rendering/monster-scale";
import { postureLayers } from "@/engine/foundation/animation/posture";
import { oneShotLayers } from "@/engine/foundation/animation/one-shot-layers";
import { changeLocomotion, stopLocomotion, locomotionLayers } from "@/engine/foundation/animation/locomotion-blend";
import { skillMotionResolveAnimation } from "@/engine/foundation/animation/skill-motion-resolve";
import { weaponAnimationSet } from "@/engine/foundation/animation/animation-metadata";
import { characterHeadingYaw } from "@/engine/foundation/math/angles";
import { movementGait } from "@/engine/foundation/gameplay/native-movement";
import type { CharacterLayer } from "@/engine/contracts/character";
import { MOVEMENT_MODE_SEATED, type EntityState } from "@/engine/contracts/world";
import type { ActorAppearance, ActorFrame, ActorOwner, ActorPass } from "./internal/presentation-contract";

const PROTECTED_ANIMATION_DISTANCE = 300;

/*
================
seatedVehicle

The vehicle a rider sits on this frame: its mount, once that mount drew
last frame (BUG-063). Natively the vehicle's model loads with the vehicle,
so 85E000 always finds the seat (85D870) and the summoner is in the saddle,
in the air, as the horse fades in. Here the vehicle's GLB can still be
loading; the ride pose without its seat put the rider sitting on the
ground until the horse drew and lifted it. Until then the rider keeps its
own pose and placement and takes the saddle on the frame after the vehicle
first draws, while the vehicle's CIDecoAppear ramp is still at its start
(spawn-fades.ts). Only the seat waits: the mounted rules (no pickup, hidden
weapon) follow the simulation's link.
================
*/
function seatedVehicle( entity: EntityState, displayed: ReadonlyMap<number, unknown> ): number | undefined {
	return entity.mountedOn !== undefined && displayed.has( entity.mountedOn ) ? entity.mountedOn : undefined;
}

/*
================
createActorMotion
================
*/
export function createActorMotion( owner: ActorOwner ) {
	return {
		/*
		================
		present

		Builds the character's animation layers, advances its sounds and
		publishes its actor into frame.next.
		================
		*/
		present( entity: EntityState, resolved: ActorAppearance, frame: ActorFrame, pass: ActorPass ) {
			const {
				allocateActor,
				avatarOverrides,
				committedAuxiliary,
				displayedDependencies,
				effects,
				entityLod,
				footContact,
				health,
				output,
				posePresentation,
				published,
				referenceAppearances,
				resources,
				soundSurface,
				sounds,
				states,
				statusOwner,
				wornEquipment,
				presentationState
			} = owner;
			const {
				actionLayersByActor,
				animationDeltaMs,
				castByActor,
				gameplay,
				hitByActor,
				localMover,
				logicalPose,
				next,
				pendingDeaths,
				probe,
				sampleActorDetails,
				seconds,
				soundContext,
				vitalsByGid,
				waitingActors
			} = frame;
			const { animationHolders, particleHolders } = pass;
			const {
				resource,
				model,
				dependencies,
				auxiliaryCommit,
				particleCommit,
				overrideCommit,
				defaultWearCommit
			} = resolved;
			if ( sampleActorDetails ) probe?.detailBegin( "actor-motion" );
			const nativePose = logicalPose( entity );

			let state = states.get( entity.gid );
			if ( !state ) {
				state = { modifierId: allocateActor(), clip: "", started: seconds };
				states.set( entity.gid, state );
			}
			const previousOverride = avatarOverrides.get( entity.gid );
			const selectedOverride = overrideCommit === undefined ?
				previousOverride :
				selectAvatarOverride(
					previousOverride,
					overrideCommit,
					published.dress?.avatarVisualOverrides ?? {}
				);
			if ( selectedOverride ) avatarOverrides.set( entity.gid, selectedOverride );
			const overrideChanged = previousOverride?.selected !== selectedOverride?.selected;
			const override = selectedOverride?.selected === undefined ?
				undefined :
				published.dress?.avatarVisualOverrides?.[selectedOverride.selected];
			const authorityDead = entity.appearanceState?.[0] !== undefined ?
				entity.appearanceState[0] === 2 :
				vitalsByGid.get( entity.gid )?.hp === 0;
			// Wire death retires targeting immediately; the native result
			// vector starts death presentation only when its hit is applied.
			const dead = (authorityDead || !!health?.dead( entity.gid )) && !pendingDeaths.has( entity.gid ),
				cast = castByActor.get( entity.gid ),
				hit = hitByActor.get( entity.gid );
			let renderPose = posePresentation.pose(
				entity.gid,
				nativePose,
				seconds,
				dead && entity.moving === false
			);
			if ( hit && hit.damage > 0 && state.hitToken !== hit.token ) {
				state.hitToken = hit.token;
				state.hitStarted = hit.at;
				state.hitCritical = hit.critical;
			}
			const deathEntry = presentationState.idleStates.get( entity.gid ),
				downDeath = deathEntry?.downDeath,
				quickDeath = resource.clips.includes( "deathquick" ) ?
					"deathquick" :
					resource.clips.includes( "downdie" ) ?
					"downdie" :
					"death";
			const sitting = entity.movementMode === MOVEMENT_MODE_SEATED && !entity.mountedOn;
			const seat = seatedVehicle( entity, output.displayed );
			const sittingClip = resource.clips.includes( "sit" ) ? "sit" : "charselect-state14";
			if ( state.dead !== undefined && state.dead !== dead ) {
				// 8E64F0: downdie from state 4; otherwise the motion-4 one-shot only
				// when state 2 or 3 was active, over the deathLoop base.
				state.postureClip = dead ?
					(downDeath ? quickDeath : deathEntry?.deathAction ? "death" : undefined) :
					undefined;
				state.postureStarted = seconds;
			} else if ( !dead && state.sitting !== undefined && state.sitting !== sitting ) {
				state.postureClip = sitting ?
					(resource.clips.includes( "sitdown" ) ? "sitdown" : "charselect-state13") :
					(resource.clips.includes( "standup" ) ? "standup" : "charselect-state15");
				state.postureStarted = seconds;
			}
			const heightTarget = sitting ? .5 : 1;
			if ( state.sitting !== sitting ) {
				const old = state.actionHeight,
					current = old ?
						old.from + (old.to - old.from) * Math.min( 1, Math.max( 0, seconds - old.at ) ) :
						heightTarget;
				state.actionHeight = { from: current, to: heightTarget, at: seconds };
			}
			state.dead = dead;
			state.sitting = sitting;
			const deadLoop = resource.clips.includes( "deathLoop" ) ?
				"deathLoop" :
				resource.clips.includes( "deathloop" ) ?
				"deathloop" :
				"death";
			let moving = (localMover( entity.gid ) ? gameplay!.moving : entity.moving) ?? false;
			const statusMask = vitalsByGid.get( entity.gid )?.abnormal ?? 0;
			const statusView = statusOwner.view( entity.gid, statusMask, entity.appearanceState?.[2] ?? 0 );
			// 85C590 writes +0xB9 from the FZ bit with no hp test.
			// Death selects its own clip; it does not clear the lock.
			const poseFrozen = statusView.poseLocked;
			if ( poseFrozen ) moving = false;
			const requestedMoving = moving;
			const movementRevision =
				(localMover( entity.gid ) ? gameplay!.movementRevision : entity.movementRevision) ?? 0;
			const displaced =
				(localMover( entity.gid ) ? gameplay!.movementPath : entity.movementPath)?.displacement ===
					true;
			// 8DD550 drives skill travel through 8797C0 and 86D5C0 even
			// after state 9 exits. Its position owner is not the walk hold.
			if (
				state.navigationHold &&
				(state.navigationHold.revision !== movementRevision || displaced || dead || entity.mountedOn)
			) state.navigationHold = undefined;
			if ( state.navigationHold ) {
				renderPose = state.navigationHold.pose;
				moving = false;
			}
			const activePosture = presentationState.idleStates.get( entity.gid )?.posture;
			const waiting = !dead && waitingActors.has( entity.gid );
			const derivedMask = dead ?
				2 :
				sitting ?
				0x40 :
				activePosture?.kind === "down" ?
				0x10 :
				(waiting ? 0 : 8 | (moving ? 0x200 : 0x100)) | (cast ? 4 : 0);
			const previousInput = state.actionInput;
			const inputChanged = !previousInput || previousInput.dead !== dead ||
				previousInput.sitting !== sitting || previousInput.mountedOn !== (entity.mountedOn ?? 0) ||
				previousInput.movementMode !== entity.movementMode ||
				previousInput.requestedMoving !== requestedMoving ||
				previousInput.movementRevision !== movementRevision ||
				previousInput.posture !== (activePosture?.kind ?? "") ||
				previousInput.waiting !== waiting || previousInput.casting !== !!cast;
			const commands: Parameters<typeof transitionActionStates>[2][number][] = [];
			let mask = state.actionMask ?? derivedMask;
			if ( !dead ) mask &= ~2;
			mask = (mask & ~4) | (cast ? 4 : 0);
			if ( previousInput && inputChanged ) {
				if ( dead || activePosture?.kind === "down" || entity.mountedOn ) mask = derivedMask;
				else if ( sitting ) commands.push( { kind: "enter", state: 6 } );
				else if ( waiting ) commands.push( { kind: "leave", state: 3 } );
				else {
					if ( !(mask & 8) ) commands.push( { kind: "enter", state: 3 } );
					if ( moving ) {
						if ( state.actionMode !== entity.movementMode && mask & 0x200 ) {
							commands.push( { kind: "leave", state: 9 } );
						}
						commands.push( { kind: "enter", state: 9 } );
					} else if ( mask & 0x200 ) commands.push( { kind: "leave", state: 9 } );
				}
			}
			if ( poseFrozen && (mask & 0x200) ) commands.push( { kind: "leave", state: 9 } );
			if ( overrideChanged && state.actionMask !== undefined ) commands.push( { kind: "refresh" } );
			const refresh = transitionActionStates( mask, moving, commands );
			const entryRate = movementEntryRate(
				renderPose,
				nativePose,
				localMover( entity.gid ) ? gameplay!.movementPath : entity.movementPath,
				state.actionRevision !== movementRevision
			);
			state.actionRevision = movementRevision;
			state.actionMask = refresh.mask;
			if ( inputChanged ) {
				state.actionInput = {
					dead,
					sitting,
					mountedOn: entity.mountedOn ?? 0,
					movementMode: entity.movementMode,
					requestedMoving,
					movementRevision,
					posture: activePosture?.kind ?? "",
					waiting,
					casting: !!cast
				};
			}
			state.actionMode = entity.movementMode;
			if (
				requestedMoving && refresh.effects.some( e => e.kind === "leave" && e.state === 9 ) &&
				!refresh.navigation
			) {
				state.navigationHold = displaced ? undefined : {
					revision: movementRevision,
					pose: renderPose,
					mode: entity.movementMode
				};
				moving = false;
			}
			// 8EADF0 tries the selected item override before the ordinary
			// body track, only when unmounted. Without one the character plays
			// its weapon's animation set (CCObjCharacter_ResolveWeaponAnimationPrefix
			// 8E83F0 stores it at +0x114): a spear runs two-handed. A set that
			// lacks the state keeps the default selection, as native falls back.
			const motionDisguise = referenceAppearances.get( entity.gid ),
				weapon = wornEquipment( entity, gameplay ).find( item => item.slot === 6 ),
				weaponSet = motionDisguise ?
					weaponAnimationSet( motionDisguise.weapon << 11 ) :
					weapon ?
					weaponAnimationSet( weapon.typeFlags ) :
					undefined,
				motionSet = override?.animation || weaponSet?.replaceAll( "-", "_" );
			let combatIdle: ReturnType<typeof skillMotionResolveAnimation>;
			if ( (presentationState.combatStanceEnds.get( entity.gid ) ?? -Infinity) > seconds ) {
				const metadata = published.animationStates.get( resource.codename ),
					stanceSet = !entity.mountedOn && motionSet || "default";
				if (
					state.combatIdle?.body !== resource || state.combatIdle.metadata !== metadata ||
					state.combatIdle.set !== stanceSet
				) {
					// CICharactor_UpdateCombatStanceAnimation (8E5ADE) plays state 6
					// through the model's slot +0C. On a character model that is
					// CCObjCharacter_PlayAnimation (8EADF0, CCObjCharacter vtable
					// C15290), not the CCObjAnimation base's DEFAULT-only 8E7470: the
					// item override, then the weapon set (a crossbow's ready stance),
					// then DEFAULT (skillMotionResolveAnimation retries it).
					state.combatIdle = {
						body: resource,
						metadata,
						set: stanceSet,
						motion: skillMotionResolveAnimation( {
							role: `native:${stanceSet}:6`,
							clips: resource.clips,
							bodyStates: resource.animationStates,
							catalogStates: metadata,
							motionUrls: published.nativeMotionUrls.get( resource.codename )
						} )
					};
				}
				const motion = state.combatIdle.motion;
				if (
					motion &&
					(!motion.banUrl || resources.animation( resource.glb, motion.clip, motion.banUrl ))
				) combatIdle = motion;
			}
			const baseRole = dead ?
				(downDeath ? quickDeath : deadLoop) :
				seat !== undefined ?
				"ride" :
				sitting ?
				sittingClip :
				(moving || !state.navigationHold && posePresentation.moving( entity.gid )) ?
				movementGait( entity.movementMode ) :
				combatIdle?.clip ?? "stand";
			let clip = baseRole === combatIdle?.clip || resource.clips.includes( baseRole ) ?
				baseRole :
				resource.clips.includes( "stand" ) ?
				"stand" :
				resource.clips[0] ?? "";
			if (
				motionSet && !entity.mountedOn && !dead && !sitting &&
				(baseRole === "run" || baseRole === "walk" || baseRole === "stand")
			) {
				const role = `native:${motionSet}:${baseRole === "run" ? 7 : baseRole === "walk" ? 1 : 0}`;
				const definition = published.animationStates.get( resource.codename )?.[role] ??
						resource.animationStates?.[role],
					url = published.nativeMotionUrls.get( resource.codename )?.get( role );
				if (
					definition &&
					(resource.clips.includes( role ) || url && resources.animation( resource.glb, role, url ))
				) clip = role;
			}
			if ( state.clip !== clip ) {
				state.clip = clip;
				state.started = seconds;
				state.feedbackSettled = false;
			}
			if ( poseFrozen ) {
				state.frozenSample ??= Math.max( 0, seconds - state.started );
				state.started = seconds - state.frozenSample;
				state.rateSample = undefined;
				state.rateClock = undefined;
			} else if ( statusView.rate !== 1 ) {
				state.rateSample ??= Math.max( 0, seconds - (state.rateClock ?? state.started) );
				state.rateSample += (state.rateClock === undefined ? 0 : Math.max( 0, seconds - state.rateClock )) *
					statusView.rate;
				state.rateClock = seconds;
				state.started = seconds - state.rateSample;
				state.frozenSample = undefined;
			} else {
				state.frozenSample = undefined;
				state.rateSample = undefined;
				state.rateClock = undefined;
			}
			const looping = dead ? !downDeath && clip !== "death" : true;
			if ( refresh.effects.length ) {
				let committed = false;
				const previousDefinition = previousOverride?.selected === undefined ?
					undefined :
					published.dress?.avatarVisualOverrides?.[previousOverride.selected];
				for ( const effect of refresh.effects ) {
					if ( effect.kind === "commit" ) {
						committed = true;
						continue;
					}
					if ( effect.kind === "navigation" ) continue;
					if ( effect.kind === "feet" ) {
						const actor = output.displayed.get( entity.gid );
						if ( actor ) {
							for ( const right of [ false, true ] ) {
								footContact(
									entity,
									[ ...output.displayed.values() ],
									actor,
									nativePose,
									right,
									seconds
								);
							}
						}
						continue;
					}
					if ( effect.kind === "leave" ) {
						if ( effect.state === 8 || effect.state === 9 || effect.state === 6 ) {
							state.locomotion = stopLocomotion( state.locomotion, seconds );
						}
						if ( effect.state === 8 || effect.state === 7 ) {
							const idle = presentationState.idleStates.get( entity.gid )?.idle;
							if ( idle ) idle.clip = undefined;
						}
					} else {
						if ( effect.state === 3 || effect.state === 6 ) {
							const target = effect.state === 6 ? .5 : 1,
								old = state.actionHeight,
								current = old ?
									old.from +
									(old.to - old.from) * Math.min( 1, Math.max( 0, seconds - old.at ) ) :
									target;
							const interpolate = effect.state === 6 ?
								!!(effect.previous & 8) :
								!!(effect.previous & 0x50);
							state.actionHeight = {
								from: interpolate ? current : target,
								to: target,
								at: seconds
							};
						}
						if ( effect.state === 8 || effect.state === 6 || effect.state === 9 ) {
							if ( effect.state === 8 ) {
								const idle = presentationState.idleStates.get( entity.gid )?.idle;
								if ( idle ) idle.clip = undefined;
							}
							const role = effect.state === 6 ?
								sittingClip :
								effect.state === 9 ?
								movementGait( entity.movementMode ) :
								combatIdle?.clip ?? "stand";
							let selected = resource.clips.includes( role ) || role === combatIdle?.clip ?
								role :
								"stand";
							const definition = committed ? override : previousDefinition;
							if (
								definition && !entity.mountedOn &&
								(role === "stand" || role === "run" || role === "walk")
							) {
								const candidate = `native:${definition.animation}:${
									role === "run" ? 7 : role === "walk" ? 1 : 0
								}`;
								const metadata = published.animationStates.get( resource.codename )?.[candidate] ??
										resource.animationStates?.[candidate],
									url = published.nativeMotionUrls.get( resource.codename )?.get( candidate );
								if (
									metadata &&
									(resource.clips.includes( candidate ) ||
										url && resources.animation( resource.glb, candidate, url ))
								) selected = candidate;
							}
							state.locomotion = changeLocomotion(
								state.locomotion,
								selected,
								true,
								seconds,
								role,
								true
							);
							state.locomotion.enter = effect.state === 8 ?
								0 :
								effect.state === 6 ?
								.2 :
								role === "run" ?
								.1 :
								.2;
							state.locomotion.rate = effect.state === 9 ? entryRate : 1;
						}
					}
				}
			}
			const previousLocomotion = state.locomotion;
			// 8E06E0 leaves base state 3 when WAIT is installed; 8E5B80
			// then exits idle/movement. Keep their existing exit envelopes.
			state.locomotion = waiting ?
				state.locomotion ?? changeLocomotion( undefined, "", true, seconds, baseRole ) :
				changeLocomotion( state.locomotion, clip, looping, seconds, baseRole );
			if ( !waiting && previousLocomotion?.clip === "" ) state.locomotion.enter = .2;
			if ( previousLocomotion !== state.locomotion ) {
				state.locomotion.rate = baseRole === "run" || baseRole === "walk" ? entryRate : 1;
				// 777F60 mounts and 85E930 dismounts with PlayAnimation(0,0,0,0,1,1):
				// no blend, so the rider snaps into and out of the ride pose.
				if ( (previousLocomotion?.clip === "ride") !== (clip === "ride") ) {
					state.locomotion = { ...state.locomotion, enter: 0, outgoing: [] };
				}
			}
			const layers: CharacterLayer[] = locomotionLayers(
				state.locomotion,
				seconds
			);
			const activationKeys = new Set<string>(),
				installations = state.activations ??= new Map<string, AnimationActivation>();
			const activation = ( producer: string, started: number ) => {
				const key = producer + ":" + started;
				activationKeys.add( key );
				let value = installations.get( key );
				if ( !value ) {
					value = animationActivation( started );
					installations.set( key, value );
				}
				return value;
			};
			const bindLayers = (
				rows: readonly CharacterLayer[],
				producer: string,
				started: number
			) => rows.map( layer => ({
				...layer,
				activation: activation( producer + ":" + layer.lane, started )
			}) );
			if (
				state.postureClip && resource.clips.includes( state.postureClip ) &&
				state.postureStarted !== undefined
			) {
				const elapsed = seconds - state.postureStarted,
					duration = resources.duration( resource.glb, state.postureClip );
				if ( elapsed < duration + .2 ) {
					layers.splice(
						0,
						layers.length,
						...bindLayers(
							oneShotLayers( state.postureClip, clip, elapsed, duration, .2 ),
							"transition",
							state.postureStarted
						)
					);
				} else state.postureClip = undefined;
			}
			if ( !dead ) {
				for ( const motion of effects.hostMotions( entity.gid ) ) {
					const role = `attached-${motion.set.replaceAll( "_", "-" )}-${motion.id}`,
						definition = (resource.animationStates ?? published.animationStates.get( resource.codename ))
							?.[role];
					if ( !definition || !resource.clips.includes( role ) ) continue;
					const age = Math.max( 0, seconds - motion.started ),
						end = motion.stoppedAt ??
							(definition.loop ? Infinity : motion.started + definition.durationMs / 1000),
						weight = Math.min( 1, Math.max( 0, Math.min( seconds, end ) - motion.started ) / .2 ) *
							Math.max( 0, Math.min( 1, 1 - (seconds - end) / .2 ) );
					if ( weight > 0 ) {
						layers.unshift( {
							clip: role,
							time: definition.loop ? age : Math.min( age, definition.durationMs / 1000 ),
							loop: definition.loop ?? false,
							weight,
							lane: definition.loop ? "timed" : "event",
							activation: activation( "attached:" + motion.key, motion.started )
						} );
					}
				}
			}
			if ( !dead ) layers.unshift( ...(actionLayersByActor.get( entity.gid ) ?? []) );
			// Normal hit clips contain sparse tracks. They must leave untouched bones
			// on the timed lane instead of resetting the entire skeleton to bind pose.
			const hitClip = resource.clips.includes( "hit1" ) ?
				"hit1" :
				resource.clips.includes( "hit" ) ?
				"hit" :
				"";
			const posture = presentationState.idleStates.get( entity.gid )?.posture;
			if ( !dead && posture ) {
				const role = posture.kind === "emote" ?
					posture.clip :
					posture.kind === "recover" ?
					"wakeup" :
					"down";
				const projected = bindLayers(
					postureLayers( posture, seconds, resources.duration( resource.glb, role ) ),
					"posture",
					posture.started
				).filter( layer => resource.clips.includes( layer.clip ) );
				if ( posture.kind === "down" ) layers.splice( 0, layers.length, ...projected );
				else layers.unshift( ...projected );
			}
			const reaction = posture?.kind === "down" ? "downdamage" : hitClip;
			if (
				!dead && reaction && resource.clips.includes( reaction ) && state.hitStarted !== undefined &&
				seconds - state.hitStarted < resources.duration( resource.glb, reaction ) + .2
			) {
				const age = seconds - state.hitStarted, duration = resources.duration( resource.glb, reaction );
				layers.unshift( {
					clip: reaction,
					time: Math.min( age, duration ),
					loop: false,
					weight: Math.min( 1, Math.max( 0, 1 - (age - duration) / .2 ) ),
					lane: "event",
					activation: activation( "hit", state.hitStarted )
				} );
			}
			if ( entity.pickupRevision !== undefined && entity.pickupRevision !== state.pickupRevision ) {
				state.pickupRevision = entity.pickupRevision;
				state.pickupStarted = seconds;
			}
			const idle = presentationState.idleStates.get( entity.gid )?.idle;
			if ( idle?.clip ) {
				const idleClip = idle.clip, age = seconds - idle.started;
				const idleLayers = bindLayers(
					oneShotLayers( idleClip, clip, age, resources.duration( resource.glb, idleClip ), .2 ),
					"idle",
					idle.started
				).map( layer =>
					layer.lane === "event" ? { ...layer, weight: Math.min( layer.weight, age / .4 ) } : layer
				);
				layers.splice( 0, layers.length, ...idleLayers );
			}
			if (
				!dead && !entity.mountedOn && state.pickupStarted !== undefined &&
				resource.clips.includes( "pick" ) &&
				seconds - state.pickupStarted < resources.duration( resource.glb, "pick" )
			) {
				layers.splice( 0, layers.length - 1, {
					clip: "pick",
					time: seconds - state.pickupStarted,
					loop: false,
					weight: 1,
					lane: "event",
					activation: activation( "pickup", state.pickupStarted )
				} );
			}
			if ( sampleActorDetails ) {
				probe?.detailEnd( "actor-motion" );
				probe?.detailBegin( "actor-sounds" );
			}
			const metadata = resource.animationStates ?? published.animationStates.get( resource.codename );
			// Retain the native idle metadata through its outgoing blend.
			const motionMetadata = ( name: string ) =>
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
					position: [
						(nativePose.regionId & 255) * 1920 + nativePose.x,
						nativePose.y,
						(nativePose.regionId >>> 8) * 1920 + nativePose.z
					] as const,
					surface: context.player ? soundSurface( nativePose ) : undefined,
					context
				};
			};
			// ADD670 retains both outgoing and incoming installations. Clip
			// names and weights cannot identify cursors across a reinstallation.
			const retainedActivations = new Set<AnimationActivation>();
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
					motionMetadata( layer.clip ),
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
			displayedDependencies.set( entity.gid, dependencies );
			if ( auxiliaryCommit !== undefined ) committedAuxiliary.set( entity.gid, auxiliaryCommit );
			const appearance = effects.appearance( entity.gid ),
				baseScale = entity.kind === "monster" ?
					monsterScale( entity.rarity ?? 0, entity.tidWord ?? 0, resource.scalePercent ) :
					1;
			const previewWeapon = wornEquipment( entity, gameplay ).find( item => item.slot === 6 ),
				disguise = referenceAppearances.get( entity.gid ),
				armedIdle = disguise ?
					"preview-state0-" + weaponAnimationSet( disguise.weapon << 11 ) :
					previewWeapon ?
					"preview-state0-" + weaponAnimationSet( previewWeapon.typeFlags ) :
					"stand";
			state.modifierResource = resource;
			state.modifierLayers = layers;
			next.set( entity.gid, {
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
				groundItem: !!entity.groundItem,
				previewClip: resource.clips.includes( armedIdle ) ? armedIdle : "stand",
				materialTint: appearance.materialTint,
				pointLight: appearance.pointLight,
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
				layers: posture || layers.length !== 1 || layers[0]!.clip !== clip ||
						layers[0]!.loop !== looping || layers[0]!.weight !== 1 || layers[0]!.lane === "event" ||
						state.locomotion.outgoing.length ?
					layers :
					undefined,
				time: layers.length === 1 && layers[0]!.clip === clip ?
					layers[0]!.time :
					seconds - state.started,
				loop: looping,
				scale: Math.fround( baseScale * appearance.scale )
			} );
			if ( sampleActorDetails ) probe?.detailEnd( "actor-record" );
			if ( refresh.mask & 0x200 ) {
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
		}
	};
}
