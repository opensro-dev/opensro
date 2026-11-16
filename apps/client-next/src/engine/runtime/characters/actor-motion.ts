/*
===========================================================================

actor-motion.ts - what an actor plays this frame

The second half of the per-actor pass, cut in two. decide reads the
actor's inputs (its entity row, vitals, cast, hit, posture, effects, the
catalogue) and turns death, sitting and posture, action states,
locomotion, hits and idles into this frame's layers and the choices its
published actor is built from. advance (actor-advance.ts) then advances
their clocks and sounds and publishes the actor. present runs both, in
that order, for every actor every frame.

===========================================================================
*/
import { movementEntryRate, transitionActionStates } from "@/engine/foundation/animation/action-refresh";
import { selectAvatarOverride } from "@/engine/foundation/animation/avatar-override";
import { animationActivation, type AnimationActivation } from "@/engine/foundation/animation/animation-activation";
import { postureLayers } from "@/engine/foundation/animation/posture";
import { oneShotLayers } from "@/engine/foundation/animation/one-shot-layers";
import { changeLocomotion, stopLocomotion, locomotionLayers } from "@/engine/foundation/animation/locomotion-blend";
import { skillMotionResolveAnimation } from "@/engine/foundation/animation/skill-motion-resolve";
import { weaponAnimationSet } from "@/engine/foundation/animation/animation-metadata";
import { movementGait } from "@/engine/foundation/gameplay/native-movement";
import { approachHeading, castFacing } from "@/engine/foundation/gameplay/cast-facing";
import type { CharacterLayer } from "@/engine/contracts/character";
import { MOVEMENT_MODE_SEATED, type EntityState } from "@/engine/contracts/world";
import type { AvatarOverrideSelection } from "@/engine/foundation/animation/avatar-override";
import type {
	ActorAppearance,
	ActorFrame,
	ActorOwner,
	ActorPass,
	MotionDecision,
	MotionRetained,
	MotionStamp,
	StampScratch
} from "./internal/presentation-contract";
import { createActorAdvance } from "./actor-advance";
import { STALL_TITLE_MODE } from "@/engine/foundation/gameplay/interaction-approach";

const STALL_ACTION_MASK = 0x8000;
const STALL_MOTION = 80;
// 86CBA0's yaw rate in heading words: 3*pi rad/s (turn in
// pose-presentation.ts), the rate a body turns back at when a cast
// facing leaves its yaw behind.
const CAST_YAW_RELEASE_WORDS_PER_SECOND = 98304;
// The stamp's one value for every empty avatar commit: the appearance phase
// hands a fresh empty list each frame to bodies without an assembly.
const EMPTY_COMMIT: readonly number[] = Object.freeze( [] );

/*
================
sameSelection

Two avatar override selections with the same order and choice: a commit
selected again over its own result returns an equal one.
================
*/
function sameSelection( a: AvatarOverrideSelection | undefined, b: AvatarOverrideSelection | undefined ) {
	if ( !a || !b ) return a === b;
	if ( a.selected !== b.selected || a.order.length !== b.order.length ) return false;
	for ( let i = 0; i < a.order.length; i++ ) if ( a.order[i] !== b.order[i] ) return false;
	return true;
}

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
	const actorAdvance = createActorAdvance( owner );
	// Names present built for every actor every frame, made once per set,
	// on which alone they depend: a weapon set's motion set and preview
	// idle, a motion set's native state roles. The same string each frame
	// also spares the catalogue lookups keyed by it a fresh hash. The sets
	// come from finite tables (weaponAnimationSet, the dress catalogue's
	// overrides), so the maps stay small.
	const weaponNames = new Map<string | undefined, { motionSet: string | undefined; previewIdle: string; }>();
	const nativeRoles = new Map<string, string[]>();
	/*
	================
	weaponNamesOf
	================
	*/
	function weaponNamesOf( weaponSet: string | undefined ) {
		let names = weaponNames.get( weaponSet );
		if ( !names ) {
			names = { motionSet: weaponSet?.replaceAll( "-", "_" ), previewIdle: "preview-state0-" + weaponSet };
			weaponNames.set( weaponSet, names );
		}
		return names;
	}
	/*
	================
	nativeRole

	The native state role `native:<set>:<state>`.
	================
	*/
	function nativeRole( set: string, stateId: number ): string {
		let roles = nativeRoles.get( set );
		if ( !roles ) {
			roles = [];
			nativeRoles.set( set, roles );
		}
		return roles[stateId] ??= `native:${set}:${stateId}`;
	}
	/*
	================
	decide

	The actor's state, layers and published choices this frame (MotionDecision),
	from its inputs: present's statements before advance's, in its order.
	================
	*/
	function decide(
		entity: EntityState,
		resolved: ActorAppearance,
		frame: ActorFrame,
		pass: ActorPass
	): MotionDecision {
		const {
			allocateActor,
			avatarOverrides,
			effects,
			footContact,
			health,
			output,
			posePresentation,
			published,
			referenceAppearances,
			resources,
			states,
			statusOwner,
			wornEquipment,
			presentationState
		} = owner;
		const {
			actionLayersByActor,
			castByActor,
			drawnPoses,
			entitiesByGid,
			gameplay,
			hitByActor,
			localMover,
			logicalPose,
			pendingDeaths,
			probe,
			sampleActorDetails,
			seconds,
			vitalsByGid,
			waitingActors
		} = frame;
		const { resource, model, overrideCommit } = resolved;
		if ( sampleActorDetails ) probe?.detailBegin( "actor-motion" );
		const nativePose = logicalPose( entity ), facts = resource.clipFacts, clips = facts.names;

		let state = states.get( entity.gid );
		// steady: this decide changes no retained state and decides nothing
		// from the clock but the locomotion layers (MotionDecision.steady).
		// Each statement below that would change state, or whose choice the
		// clock alone can change, clears it. A rider waits on its mount's
		// first draw (seatedVehicle), which no input of its own shows.
		let steady = !!state?.modifierResource && state.modifierResource.glb === resource.glb &&
			state.activations !== undefined && !localMover( entity.gid ) && entity.gid !== gameplay?.localGid &&
			!entity.mountedOn;
		if ( !state ) {
			state = { modifierId: allocateActor(), clip: "", started: seconds };
			states.set( entity.gid, state );
		} else if ( state.modifierResource && state.modifierResource.glb !== resource.glb ) {
			// 8E64F0 (death model) reloads the visual mesh from the corpse BSR
			// (LoadVisualMeshFromBSR), replacing its CCObjAnimation; in the port,
			// any base-mesh GLB change on the same GID (death model or 85C060
			// transform skin) swaps the clip catalog and must discard prior
			// mesh installations.
			state.locomotion = undefined;
			state.postureClip = undefined;
			state.postureStarted = undefined;
			state.pickupStarted = undefined;
			state.combatIdle = undefined;
			state.stallMotion = undefined;
			state.dispatch?.reset();
			state.activations?.clear();
			const idle = presentationState.idleStates.get( entity.gid )?.idle;
			if ( idle?.clip && !clips.has( idle.clip ) ) idle.clip = undefined;
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
		if ( overrideCommit !== undefined && !sameSelection( previousOverride, selectedOverride ) ) steady = false;
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
		// 8DC440 turns a live caster toward its target every frame. A rider's
		// yaw belongs to its vehicle in this port, so riders keep theirs.
		const castTarget = cast && !cast.cancelledAtMs && !cast.cancellationRequestedAtMs &&
				cast.target !== entity.gid && !entity.mountedOn ?
			entitiesByGid.get( cast.target ) :
			undefined;
		if ( state.castFacing || state.castYawRelease ) steady = false;
		state.castFacing = castFacing( state.castFacing, {
			caster: nativePose,
			target: castTarget && logicalPose( castTarget ),
			tracking: !!castTarget && !dead,
			moving: !!(localMover( entity.gid ) ? gameplay!.moving : entity.moving),
			revision: (localMover( entity.gid ) ? gameplay!.movementRevision : entity.movementRevision) ?? 0,
			seconds
		} );
		if ( state.castFacing ) steady = false;
		// The frame's drawn pose, resolved once in presentPoses
		// (#1442c); the tracked cast yaw (8DC440) still turns its
		// angle on top, as it already did over a held pose.
		let renderPose = drawnPoses.get( entity.gid ) ?? nativePose;
		if ( state.castFacing ) {
			renderPose = { ...renderPose, angle: Math.round( state.castFacing.angle ) % 65536 };
			// 8DC440 wrote this yaw (CICharactor_SetYaw): it outlives
			// the facing, and the body turns back from it when the
			// facing ends instead of snapping to the drawn angle.
			state.castYawRelease = { angle: state.castFacing.angle, at: seconds };
		} else if ( state.castYawRelease ) {
			const release = state.castYawRelease, target = renderPose.angle;
			const turned = approachHeading(
				release.angle,
				target,
				Math.max( 0, seconds - release.at ) * CAST_YAW_RELEASE_WORDS_PER_SECOND
			);
			renderPose = { ...renderPose, angle: Math.round( turned ) % 65536 };
			state.castYawRelease = turned === target ? undefined : { angle: turned, at: seconds };
		}
		if ( hit && hit.damage > 0 && state.hitToken !== hit.token ) {
			steady = false;
			state.hitToken = hit.token;
			state.hitStarted = hit.at;
			state.hitCritical = hit.critical;
		}
		const deathEntry = presentationState.idleStates.get( entity.gid ),
			downDeath = deathEntry?.downDeath,
			quickDeath = facts.quickDeath;
		const stall = !dead && entity.appearanceState?.[6] === STALL_TITLE_MODE;
		const sitting = !stall && entity.movementMode === MOVEMENT_MODE_SEATED && !entity.mountedOn;
		const seat = seatedVehicle( entity, output.displayed );
		const sittingClip = facts.sitting;
		if ( state.dead !== dead || state.sitting !== sitting || stall ) steady = false;
		if ( state.dead !== undefined && state.dead !== dead ) {
			// 8E64F0: downdie from state 4; otherwise the motion-4 one-shot only
			// when state 2 or 3 was active, over the deathLoop base.
			state.postureClip = dead ?
				(downDeath ? quickDeath : deathEntry?.deathAction ? "death" : undefined) :
				undefined;
			state.postureStarted = seconds;
		} else if ( stall ) {
			state.postureClip = undefined;
		} else if ( !dead && state.sitting !== undefined && state.sitting !== sitting ) {
			state.postureClip = sitting ? facts.sitDown : facts.standUp;
			state.postureStarted = seconds;
		}
		const heightTarget = sitting ? .5 : 1;
		if ( stall && !state.actionInput?.stall ) state.actionHeight = { from: 1, to: 1, at: seconds };
		if ( state.sitting !== sitting ) {
			const old = state.actionHeight,
				current = old ?
					old.from + (old.to - old.from) * Math.min( 1, Math.max( 0, seconds - old.at ) ) :
					heightTarget;
			state.actionHeight = { from: current, to: heightTarget, at: seconds };
		}
		state.dead = dead;
		state.sitting = sitting;
		const deadLoop = facts.deathLoop;
		let moving = (localMover( entity.gid ) ? gameplay!.moving : entity.moving) ?? false;
		const statusMask = vitalsByGid.get( entity.gid )?.abnormal ?? 0;
		const statusView = statusOwner.view( entity.gid, statusMask, entity.appearanceState?.[2] ?? 0 );
		// 85C590 writes +0xB9 from the FZ bit with no hp test.
		// Death selects its own clip; it does not clear the lock.
		const poseFrozen = statusView.poseLocked;
		if ( poseFrozen || stall ) moving = false;
		if ( poseFrozen || statusView.rate !== 1 ) steady = false;
		const requestedMoving = moving;
		const movementRevision = (localMover( entity.gid ) ? gameplay!.movementRevision : entity.movementRevision) ?? 0;
		const displaced = (localMover( entity.gid ) ? gameplay!.movementPath : entity.movementPath)?.displacement ===
				true ||
			(localMover( entity.gid ) ? gameplay!.movementTransition : entity.movementTransition)?.reason ===
				"displacement";
		const waiting = !dead && waitingActors.has( entity.gid );
		const activeCast = !!cast && cast.cancelledAtMs === undefined &&
			(actionLayersByActor.get( entity.gid )?.length ?? 0) > 0;
		// The navigation hold itself resolved in presentPoses
		// (#1442c); a surviving hold still stops the walk's gait.
		if ( state.navigationHold ) moving = false;
		const activePosture = presentationState.idleStates.get( entity.gid )?.posture;
		const derivedMask = dead ?
			2 :
			stall ?
			STALL_ACTION_MASK :
			sitting ?
			0x40 :
			activePosture?.kind === "down" ?
			0x10 :
			(waiting ? 0 : 8 | (moving ? 0x200 : 0x100)) | (cast ? 4 : 0);
		const previousInput = state.actionInput;
		const inputChanged = !previousInput || previousInput.dead !== dead ||
			previousInput.stall !== stall || previousInput.sitting !== sitting ||
			previousInput.mountedOn !== (entity.mountedOn ?? 0) ||
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
			else if ( stall ) commands.push( { kind: "enter", state: 15 } );
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
		if (
			inputChanged || commands.length || refresh.effects.length || refresh.mask !== state.actionMask ||
			state.actionRevision !== movementRevision || state.actionMode !== entity.movementMode || waiting || cast
		) steady = false;
		state.actionRevision = movementRevision;
		state.actionMask = refresh.mask;
		if ( inputChanged ) {
			state.actionInput = {
				dead,
				sitting,
				stall,
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
		// A cast that stops the walk leaves state 9. When the stop and
		// the cast arrive in one server burst (0xB2F5 with 0xB245), this
		// frame already reports moving=false, so requiring
		// requestedMoving missed every real cast and the body then
		// glided across the correction under its cast animation
		// (BR-261015-0827-9B2B). The hold pose is the last drawn one:
		// this frame's pose() has already spent the correction. A cast
		// record alone is not enough: gameplay.casts retains completed
		// (phase 3, projectile still flying) and cancelled casts, and
		// latching a hold for those would freeze an ordinary walk stop.
		if (
			(requestedMoving || waiting || activeCast) &&
			refresh.effects.some( e => e.kind === "leave" && e.state === 9 ) &&
			!refresh.navigation
		) {
			state.navigationHold = displaced ? undefined : {
				revision: movementRevision,
				pose: requestedMoving ? renderPose : state.lastDrawnPose ?? renderPose,
				logical: { ...nativePose },
				mode: entity.movementMode
			};
			if ( state.navigationHold ) {
				renderPose = { ...state.navigationHold.pose, angle: renderPose.angle };
			}
			moving = false;
		}
		if ( state.navigationHold ) steady = false;
		// 8EADF0 tries the selected item override (+0x118, 8E7210 direct
		// without DEFAULT fallback) before the weapon animation set
		// (CCObjCharacter_ResolveWeaponAnimationPrefix 8E83F0 stores it at
		// +0x114; 8E7150 tries the weapon set then DEFAULT), per state and
		// only when unmounted. A partial override set such as avatar_wing
		// (which defines only state 7 run) therefore plays its run clip while
		// stand (0), walk (1), combat stance (6) and stall (80, 8EA820) fall
		// through to the weapon set before DEFAULT.
		const motionDisguise = referenceAppearances.get( entity.gid ),
			weapon = wornEquipment( entity, gameplay ).find( item => item.slot === 6 ),
			weaponSet = motionDisguise ?
				weaponAnimationSet( motionDisguise.weapon << 11 ) :
				weapon ?
				weaponAnimationSet( weapon.typeFlags ) :
				undefined,
			overrideSet = override?.animation,
			weaponNamed = weaponNamesOf( weaponSet ),
			weaponMotionSet = weaponNamed.motionSet,
			motionSet = overrideSet || weaponMotionSet,
			motionSetKey = overrideSet && weaponMotionSet ?
				`${overrideSet}:${weaponMotionSet}` :
				motionSet || "default";
		let combatIdle: ReturnType<typeof skillMotionResolveAnimation>;
		if ( (presentationState.combatStanceEnds.get( entity.gid ) ?? -Infinity) > seconds ) {
			steady = false;
			const metadata = published.animationStates.get( resource.codename ),
				stanceSet = !entity.mountedOn && motionSet || "default",
				stanceKey = !entity.mountedOn ? motionSetKey : "default";
			if (
				state.combatIdle?.body !== resource || state.combatIdle.metadata !== metadata ||
				state.combatIdle.set !== stanceKey
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
					set: stanceKey,
					motion: skillMotionResolveAnimation( {
						role: `native:${stanceSet}:6`,
						fallbackSet: !entity.mountedOn && overrideSet ? weaponMotionSet : undefined,
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
		// 8E6B50 installs motion 80 with a 200 ms blend and full actor height.
		// The character virtual dispatch (8EA820) retains its 3-tier
		// override -> weapon -> DEFAULT lookup.
		if ( stall ) {
			const metadata = published.animationStates.get( resource.codename ), set = motionSet || "default";
			if (
				state.stallMotion?.body !== resource || state.stallMotion.metadata !== metadata ||
				state.stallMotion.set !== motionSetKey
			) {
				state.stallMotion = {
					body: resource,
					metadata,
					set: motionSetKey,
					motion: skillMotionResolveAnimation( {
						role: `native:${set}:${STALL_MOTION}`,
						fallbackSet: overrideSet ? weaponMotionSet : undefined,
						clips: resource.clips,
						bodyStates: resource.animationStates,
						catalogStates: metadata,
						motionUrls: published.nativeMotionUrls.get( resource.codename )
					} )
				};
			}
		}
		const stallMotion = stall ? state.stallMotion?.motion : undefined;
		const stallClip = stallMotion && (!stallMotion.banUrl ||
				resources.animation( resource.glb, stallMotion.clip, stallMotion.banUrl )) ?
			stallMotion.clip :
			undefined;
		const baseRole = dead ?
			(downDeath ? quickDeath : deadLoop) :
			stall ?
			stallClip ?? "stand" :
			seat !== undefined ?
			"ride" :
			sitting ?
			sittingClip :
			(moving || !state.navigationHold && posePresentation.moving( entity.gid )) ?
			movementGait( entity.movementMode ) :
			combatIdle?.clip ?? "stand";
		let clip = baseRole === stallClip || baseRole === combatIdle?.clip || clips.has( baseRole ) ?
			baseRole :
			facts.fallback;
		if (
			!dead && !sitting && !stall && !entity.mountedOn &&
			(baseRole === "run" || baseRole === "walk" || baseRole === "stand")
		) {
			const stateId = baseRole === "run" ? 7 : baseRole === "walk" ? 1 : 0;
			for ( const set of [ overrideSet, weaponMotionSet ] ) {
				if ( !set ) continue;
				const candidate = nativeRole( set, stateId ),
					definition = published.animationStates.get( resource.codename )?.[candidate] ??
						resource.animationStates?.[candidate],
					url = published.nativeMotionUrls.get( resource.codename )?.get( candidate );
				if ( !definition ) continue;
				if ( clips.has( candidate ) || url && resources.animation( resource.glb, candidate, url ) ) {
					clip = candidate;
					break;
				}
				// A motion still loading is asked for again every frame until
				// it lands (resources.ts keeps only the loads a frame wants).
				if ( url ) steady = false;
			}
		}
		if ( baseRole !== "stand" || state.clip !== clip ) steady = false;
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
			if (
				state.frozenSample !== undefined || state.rateSample !== undefined || state.rateClock !== undefined
			) steady = false;
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
					if ( effect.state === 8 || effect.state === 9 || effect.state === 6 || effect.state === 15 ) {
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
						let selected = clips.has( role ) || role === combatIdle?.clip ?
							role :
							"stand";
						const definition = committed ? override : previousDefinition;
						if (
							!entity.mountedOn &&
							(role === "run" || role === "walk" || role === "stand")
						) {
							const stateId = role === "run" ? 7 : role === "walk" ? 1 : 0;
							for ( const set of [ definition?.animation, weaponMotionSet ] ) {
								if ( !set ) continue;
								const candidate = nativeRole( set, stateId ),
									stateDef = published.animationStates.get( resource.codename )?.[candidate] ??
										resource.animationStates?.[candidate],
									url = published.nativeMotionUrls.get( resource.codename )?.get( candidate );
								if (
									stateDef &&
									(clips.has( candidate ) ||
										url && resources.animation( resource.glb, candidate, url ))
								) {
									selected = candidate;
									break;
								}
							}
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
			if ( stallClip && clip === stallClip ) state.locomotion.enter = .2;
			state.locomotion.rate = baseRole === "run" || baseRole === "walk" ? entryRate : 1;
			// 777F60 mounts and 85E930 dismounts with PlayAnimation(0,0,0,0,1,1):
			// no blend, so the rider snaps into and out of the ride pose.
			if ( (previousLocomotion?.clip === "ride") !== (clip === "ride") ) {
				state.locomotion = { ...state.locomotion, enter: 0, outgoing: [] };
			}
		}
		if ( previousLocomotion !== state.locomotion || previousLocomotion.clip === "" ) steady = false;
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
			state.postureClip && clips.has( state.postureClip ) &&
			state.postureStarted !== undefined
		) {
			steady = false;
			const elapsed = Math.max( 0, seconds - state.postureStarted ),
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
				steady = false;
				const role = `attached-${motion.set.replaceAll( "_", "-" )}-${motion.id}`,
					definition = (resource.animationStates ?? published.animationStates.get( resource.codename ))
						?.[role];
				if ( !definition || !clips.has( role ) ) continue;
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
		if ( actionLayersByActor.get( entity.gid )?.length ) steady = false;
		if ( !dead ) layers.unshift( ...(actionLayersByActor.get( entity.gid ) ?? []) );
		// Normal hit clips contain sparse tracks. They must leave untouched bones
		// on the timed lane instead of resetting the entire skeleton to bind pose.
		const hitClip = facts.hit;
		const posture = presentationState.idleStates.get( entity.gid )?.posture;
		if ( !dead && posture ) {
			steady = false;
			const role = posture.kind === "emote" ?
				posture.clip :
				posture.kind === "recover" ?
				"wakeup" :
				"down";
			const duration = resources.duration( resource.glb, role );
			const projected = bindLayers(
				postureLayers( posture, seconds, duration ),
				"posture",
				posture.started
			).filter( layer =>
				clips.has( layer.clip ) ||
				((layer.clip === "pet-roar" || layer.clip === "pet-howl") && role === layer.clip && duration > 0)
			);
			if ( posture.kind === "down" ) layers.splice( 0, layers.length, ...projected );
			else layers.unshift( ...projected );
		}
		const reaction = posture?.kind === "down" ? "downdamage" : hitClip;
		if (
			!dead && reaction && clips.has( reaction ) && state.hitStarted !== undefined &&
			seconds - state.hitStarted < resources.duration( resource.glb, reaction ) + .2
		) {
			steady = false;
			const age = Math.max( 0, seconds - state.hitStarted ),
				duration = resources.duration( resource.glb, reaction );
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
			steady = false;
			state.pickupRevision = entity.pickupRevision;
			state.pickupStarted = seconds;
		}
		const idle = presentationState.idleStates.get( entity.gid )?.idle;
		if ( !stall && idle?.clip && clips.has( idle.clip ) ) {
			steady = false;
			const idleClip = idle.clip, age = Math.max( 0, seconds - idle.started );
			const idleLayers = bindLayers(
				oneShotLayers( idleClip, clip, age, resources.duration( resource.glb, idleClip ), .2 ),
				"idle",
				idle.started
			).map( layer =>
				layer.lane === "event" ?
					{ ...layer, weight: Math.max( 0, Math.min( layer.weight, age / .4 ) ) } :
					layer
			);
			layers.splice( 0, layers.length, ...idleLayers );
		}
		if (
			!dead && !entity.mountedOn && state.pickupStarted !== undefined &&
			clips.has( "pick" ) &&
			seconds - state.pickupStarted < resources.duration( resource.glb, "pick" )
		) {
			steady = false;
			layers.splice( 0, layers.length - 1, {
				clip: "pick",
				time: Math.max( 0, seconds - state.pickupStarted ),
				loop: false,
				weight: 1,
				lane: "event",
				activation: activation( "pickup", state.pickupStarted )
			} );
		}
		if ( sampleActorDetails ) probe?.detailEnd( "actor-motion" );
		return {
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
			// The disguise and weapon the motion set was read from above.
			armedIdle: motionDisguise || weapon ? weaponNamed.previewIdle : "stand",
			actionMask: refresh.mask,
			steady,
			reused: false
		};
	}
	// The stamp present fills each frame; a due entity's is copied before
	// decide, and only a steady decide retains the copy.
	const scratch = {} as StampScratch<MotionStamp>;
	/*
	================
	motionStamp

	Every input a steady decide reads, read as decide reads it
	(MotionStamp), into the owner's one scratch stamp. Pure reads: decide's
	own calls stay where they are.
	================
	*/
	function motionStamp( entity: EntityState, resolved: ActorAppearance, frame: ActorFrame ): MotionStamp {
		const { effects, health, posePresentation, published, referenceAppearances, wornEquipment, presentationState } =
			owner;
		const {
			actionLayersByActor,
			castByActor,
			gameplay,
			hitByActor,
			localMover,
			pendingDeaths,
			seconds,
			vitalsByGid,
			waitingActors
		} = frame;
		const { resource, overrideCommit } = resolved, gid = entity.gid;
		const vital = vitalsByGid.get( gid ),
			hit = hitByActor.get( gid ),
			entry = presentationState.idleStates.get( gid );
		const disguise = referenceAppearances.get( gid ),
			weapon = wornEquipment( entity, gameplay ).find( item => item.slot === 6 );
		const stamp = scratch;
		stamp.entity = entity;
		stamp.glb = resource.glb;
		stamp.codename = resource.codename;
		stamp.clips = resource.clips;
		stamp.clipFacts = resource.clipFacts;
		stamp.animationStates = resource.animationStates;
		stamp.commit = overrideCommit?.length === 0 ? EMPTY_COMMIT : overrideCommit;
		stamp.manifest = published.manifest;
		stamp.hpZero = vital?.hp === 0;
		stamp.abnormal = vital?.abnormal ?? 0;
		stamp.healthDead = !!health?.dead( gid );
		stamp.pendingDeath = pendingDeaths.has( gid );
		stamp.hitToken = hit && hit.damage > 0 ? hit.token : undefined;
		stamp.cast = castByActor.get( gid );
		stamp.waiting = waitingActors.has( gid );
		stamp.actionLayers = actionLayersByActor.get( gid )?.length ?? 0;
		stamp.hostMotions = effects.hostMotions( gid ).length;
		stamp.stance = (presentationState.combatStanceEnds.get( gid ) ?? -Infinity) > seconds;
		stamp.posture = entry?.posture;
		stamp.idle = !!entry?.idle.clip && resource.clipFacts.names.has( entry.idle.clip );
		stamp.poseMoving = posePresentation.moving( gid );
		stamp.weaponSet = disguise ?
			weaponAnimationSet( disguise.weapon << 11 ) :
			weapon ?
			weaponAnimationSet( weapon.typeFlags ) :
			undefined;
		stamp.armed = !!(disguise || weapon);
		stamp.local = localMover( gid ) || gid === gameplay?.localGid;
		return stamp;
	}
	/*
	================
	reuse

	The retained steady decision for this frame: its choices stand, and
	what the clock and the drawn pose move is taken again as decide takes
	it on a steady frame (no cast facing, yaw release or navigation hold
	turns the drawn pose; the locomotion layers at this frame's time).
	================
	*/
	function reuse( retained: MotionRetained, entity: EntityState, frame: ActorFrame ): MotionDecision {
		const { drawnPoses, logicalPose, seconds } = frame;
		const decision = retained.decision, nativePose = logicalPose( entity );
		return {
			...decision,
			nativePose,
			renderPose: drawnPoses.get( entity.gid ) ?? nativePose,
			layers: locomotionLayers( decision.state.locomotion!, seconds ),
			reused: true
		};
	}
	// The presentation frame present last ran in, and its count: a retained
	// decision stands only on the frame after the one it was presented at.
	let presentedFrame: ActorFrame | undefined, frameIndex = 0;
	return {
		/*
		================
		present

		Builds the character's animation layers, advances its sounds and
		publishes its actor into frame.next: decide, then advance. When the
		rule finds the entity's previous steady decision still due to repeat,
		advance runs on it instead of a new decide.
		================
		*/
		present( entity: EntityState, resolved: ActorAppearance, frame: ActorFrame, pass: ActorPass ) {
			if ( frame !== presentedFrame ) {
				presentedFrame = frame;
				frameIndex++;
			}
			const retained = owner.states.get( entity.gid )?.motion,
				previous = retained?.frame === frameIndex - 1 ? retained : undefined,
				stamp = previous && motionStamp( entity, resolved, frame );
			// The rule sees every presented entity; one with nothing retained
			// is always due.
			const reused = !owner.due.motion( previous, stamp ) && !!previous;
			// A steady decide reads what the stamp reads and changes none of
			// it, so the stamp taken before it stands for it.
			const taken = !reused && stamp ? { ...stamp } : undefined;
			const decision = reused ? reuse( previous!, entity, frame ) : decide( entity, resolved, frame, pass );
			const retain = !reused && decision.steady ?
				taken ?? { ...motionStamp( entity, resolved, frame ) } :
				undefined;
			// The actor the retained decision published last frame, when this
			// frame reuses that decision: advance may publish it again.
			const kept = reused && decision.state.record?.retained === previous ? decision.state.record : undefined;
			const record = actorAdvance.advance( entity, resolved, decision, frame, pass, kept );
			// Retained only once advance has published: a throw leaves the
			// last retained frame behind, and the next frame decides.
			if ( reused ) previous!.frame = frameIndex;
			else decision.state.motion = retain && { decision, stamp: retain, frame: frameIndex };
			const motion = decision.state.motion;
			decision.state.record = record && motion && { ...record, retained: motion };
		}
	};
}
