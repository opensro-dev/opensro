/*
===========================================================================

presentation-state.ts - posture, idle timing and combat stance lifetime

Advances entity presentation state in admission order before camera selection
is rendered. The parent supplies current frame inputs and owns publication.

===========================================================================
*/
import { emoteRoute, emoteAttachments } from "@/engine/foundation/animation/emote";
import { transitionPosture } from "@/engine/foundation/animation/posture";
import { advanceRandomIdle } from "@/engine/foundation/animation/random-idle";
import type { AnimationMetadata } from "@/engine/foundation/animation/animation-metadata";
import { MOVEMENT_MODE_SEATED, type EntityState } from "@/engine/contracts/world";
import type { Pose } from "@/engine/contracts/gameplay";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import type { DeathModel, Resource, PresentationIdleState, PresentationOutput } from "./internal/presentation-contract";

/*
================
PresentationStateFrame

Borrowed capabilities are the small surfaces this phase actually uses.
================
*/
export interface PresentationStateFrame {
	readonly entities: readonly EntityState[];
	readonly seconds: number;
	readonly simulationMs: number | undefined;
	readonly logicalPose: ( entity: EntityState ) => Pose;
	readonly appearanceRef: ( entity: EntityState ) => number;
	readonly states: ReadonlyMap<
		number,
		{ readonly postureClip?: string; readonly pickupStarted?: number; readonly actionMask?: number; }
	>;
	readonly health: { dead( gid: number ): boolean; } | undefined;
	readonly deadGids: ReadonlySet<number>;
	readonly hitByActor: ReadonlyMap<number, { readonly downAt?: number; }>;
	readonly castByActor: ReadonlyMap<number, unknown>;
	readonly resources: { duration( path: string, role: string ): number; };
	readonly random: Pick<PresentationRandom, "range">;
	readonly active: ReadonlySet<number>;
	// Deaths whose killing hit has not landed yet: presentation has not
	// entered action state 1 for them (actor-motion's death criterion).
	readonly pendingDeaths: ReadonlySet<number>;
	// GameConfig +0x12E for the login shard (uncensoredShard).
	readonly uncensored: boolean;
}

/*
================
PresentationStateCatalog
================
*/
export interface PresentationStateCatalog {
	readonly catalog: ReadonlyMap<number, Resource>;
	readonly recoveryByCodename: ReadonlyMap<string, number>;
	readonly animationStates: ReadonlyMap<string, Record<string, AnimationMetadata>>;
	readonly deathModels: ReadonlyMap<string, DeathModel>;
}

// Action-state mask bits CICharactor_EnterActionState (857830) hands an
// entry callback: the states active before it (bit = 1 << state).
const MASK_CAST = 1 << 2;
const MASK_BASE = 1 << 3;

/*
================
enterDeath

CICharactor_Action_KnockdownDie (8E64F0), the entry of action state 1, run
once when presentation enters death. From state 4 (down, bit 0x10) it plays
downdie and keeps the body. Otherwise a characterInfo death model (+0x28)
replaces the mesh when GameConfig +0x12E is set or the body has no deathLoop
track (0x24, CCObjCharacter_HasMotionTrack); deathLoop then installs and the
death one-shot (motion 4) plays over it only when state 2 or 3 was active.
================
*/
function enterDeath(
	entry: PresentationIdleState,
	dead: boolean,
	input: { previousMask: number; deathModel: boolean; deathLoop: boolean; uncensored: boolean; }
) {
	if ( !dead ) {
		entry.deathEntered = false;
		entry.deathModel = false;
		entry.deathAction = false;
		return;
	}
	if ( entry.deathEntered ) return;
	entry.deathEntered = true;
	const down = entry.downDeath === true;
	entry.deathModel = !down && input.deathModel && (input.uncensored || !input.deathLoop);
	entry.deathAction = !down && (input.previousMask & (MASK_CAST | MASK_BASE)) !== 0;
}

/*
================
createPresentationState
================
*/
export function createPresentationState() {
	const idleStates = new Map<number, PresentationIdleState>();
	// Presentation-time deadlines survive cast retirement and visibility selection.
	const combatStanceEnds = new Map<number, number>();
	return {
		idleStates,
		combatStanceEnds,
		/*
		================
		step

		Keep admission order: advanceRandomIdle consumes the shared RNG stream.
		================
		*/
		step(
			frame: PresentationStateFrame,
			output: Pick<PresentationOutput, "failure">,
			published: PresentationStateCatalog
		) {
			const {
				entities,
				seconds,
				simulationMs,
				logicalPose,
				appearanceRef,
				states,
				health,
				deadGids,
				hitByActor,
				castByActor,
				resources,
				random,
				active,
				pendingDeaths,
				uncensored
			} = frame;
			const { catalog, recoveryByCodename, animationStates, deathModels } = published;
			for ( const entity of entities ) {
				if ( entity.groundItem ) continue;
				const resource = catalog.get( appearanceRef( entity ) );
				if ( !resource ) continue;
				const pose = logicalPose( entity );
				let entry = idleStates.get( entity.gid );
				if ( !entry ) {
					entry = {
						x: pose.x,
						z: pose.z,
						region: pose.regionId,
						idle: { remaining: 15, previous: seconds, started: seconds }
					};
					idleStates.set( entity.gid, entry );
				}
				const state = states.get( entity.gid ),
					moving = entry.x !== pose.x || entry.z !== pose.z || entry.region !== pose.regionId;
				const dead = health?.dead( entity.gid ) ||
					(entity.appearanceState?.[0] !== undefined ?
						entity.appearanceState[0] === 2 :
						deadGids.has( entity.gid ));
				const hit = hitByActor.get( entity.gid );
				const isPlayer = entity.kind === "local-player" || entity.kind === "player" ||
					/^CHAR_/.test( resource.codename );
				const previousEmote = entry.posture?.kind === "emote" ? entry.posture.clip : undefined;
				// The death the actor shows: the authority's, once its killing hit
				// has played. Natively LIFE dead enters state 1 straight from the
				// down state 4 (8E64F0 plays downdie), so a knocked-down monster
				// stays down until then; cancelling its posture at the authority's
				// death stood it up for the length of the held hit.
				const shownDead = dead && !pendingDeaths.has( entity.gid );
				if ( shownDead && entry.posture?.kind === "down" ) entry.downDeath = true;
				else if ( !dead ) entry.downDeath = false;
				enterDeath( entry, shownDead, {
					previousMask: state?.actionMask ?? 0,
					deathModel: deathModels.has( resource.codename ),
					deathLoop: resource.clips.includes( "deathLoop" ) || resource.clips.includes( "deathloop" ),
					uncensored
				} );
				if ( shownDead || entity.mountedOn || entity.movementMode === MOVEMENT_MODE_SEATED ) {
					entry.posture = transitionPosture( entry.posture, { kind: "cancel" } );
				} else {
					if ( hit?.downAt !== undefined ) {
						const recoveryMs = recoveryByCodename.get( resource.codename );
						if ( recoveryMs === undefined ) {
							output.failure = `Missing native recovery duration ${resource.codename}`;
						} else {entry.posture = transitionPosture( entry.posture, {
								kind: "down",
								at: hit.downAt,
								recoveryMs
							} );}
					} else if ( entry.posture?.kind === "emote" && (moving || castByActor.has( entity.gid ) || hit) ) {
						entry.posture = transitionPosture( entry.posture, { kind: "cancel" } );
					}
					if ( entity.emote && entry.emoteRevision !== entity.emote.revision ) {
						const action = emoteRoute(
							isPlayer,
							entity.kind === "cos",
							entity.tidWord ?? 0,
							entity.emote.action
						);
						if (
							action !== null && !moving && !castByActor.has( entity.gid ) &&
							resource.clips.includes( `emote${action}` )
						) {
							entry.posture = transitionPosture( entry.posture, {
								kind: "emote",
								action,
								at: simulationMs === undefined ?
									seconds :
									seconds + (entity.emote.atMs - simulationMs) / 1000
							} );
						} else if ( action === null && !moving ) entry.idle.remaining = 0;
					}
					// A monster the authority killed never wakes: it waits down for its
					// shown death, which then enters from state 4 as downdie.
					if ( entry.posture && !(dead && entry.posture.kind === "down") ) {
						const role = entry.posture.kind === "emote" ?
							entry.posture.clip :
							entry.posture.kind === "recover" ?
							"wakeup" :
							"down";
						entry.posture = transitionPosture( entry.posture, {
							kind: "tick",
							at: seconds,
							duration: resources.duration( resource.glb, role ) ||
								((resource.animationStates ?? animationStates.get( resource.codename ))?.[role]
										?.durationMs ?? 0) / 1000
						} );
					}
				}
				entry.attachmentsHidden = emoteAttachments(
					entry.attachmentsHidden ?? false,
					previousEmote,
					entry.posture?.kind === "emote" ? entry.posture.clip : undefined,
					isPlayer,
					!!entity.mountedOn
				);
				entry.emoteRevision = entity.emote?.revision;
				if ( dead ) combatStanceEnds.delete( entity.gid );
				// 85DE06 precedes countdown subtraction. The expiry frame also
				// resets the fidget timer to 15s; it does not consume that frame.
				const suppressIdle = (combatStanceEnds.get( entity.gid ) ?? -Infinity) > entry.idle.previous;
				const eligible = !suppressIdle && !dead && !moving && !entity.mountedOn &&
					entity.movementMode !== MOVEMENT_MODE_SEATED &&
					!entry.posture && !castByActor.has( entity.gid ) && !hit && !state?.postureClip &&
					!(state?.pickupStarted !== undefined &&
						seconds - state.pickupStarted < resources.duration( resource.glb, "pick" ));
				advanceRandomIdle(
					entry.idle,
					seconds,
					eligible,
					resource.clips,
					random.range,
					role =>
						resources.duration( resource.glb, role ) ||
						((resource.animationStates ?? animationStates.get( resource.codename ))?.[role]?.durationMs ??
								0) / 1000
				);
				entry.x = pose.x;
				entry.z = pose.z;
				entry.region = pose.regionId;
			}
			for ( const gid of idleStates.keys() ) if ( !active.has( gid ) ) idleStates.delete( gid );
			for ( const [gid, end] of combatStanceEnds ) {
				if ( !active.has( gid ) || end <= seconds ) combatStanceEnds.delete( gid );
			}
		},
		/*
		================
		forgetStance

		A spawn or despawn ends the gid's combat stance.
		================
		*/
		forgetStance( gid: number ) {
			combatStanceEnds.delete( gid );
		},
		/*
		================
		clearStances

		A world reset (and its replay) ends every combat stance but keeps the
		idle states; the presentation teardown clears both (reset).
		================
		*/
		clearStances() {
			combatStanceEnds.clear();
		},
		/*
		================
		reset
		================
		*/
		reset() {
			idleStates.clear();
			combatStanceEnds.clear();
		}
	};
}
