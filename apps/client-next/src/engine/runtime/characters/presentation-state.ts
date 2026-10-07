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
import type { EntityState } from "@/engine/contracts/world";
import type { Pose } from "@/engine/contracts/gameplay";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import type { Resource, PresentationIdleState, PresentationOutput } from "./internal/presentation-contract";

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
	readonly states: ReadonlyMap<number, { readonly postureClip?: string; readonly pickupStarted?: number; }>;
	readonly health: { dead( gid: number ): boolean; } | undefined;
	readonly deadGids: ReadonlySet<number>;
	readonly hitByActor: ReadonlyMap<number, { readonly downAt?: number; }>;
	readonly castByActor: ReadonlyMap<number, unknown>;
	readonly resources: { duration( path: string, role: string ): number; };
	readonly random: Pick<PresentationRandom, "range">;
	readonly active: ReadonlySet<number>;
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
				active
			} = frame;
			const { catalog, recoveryByCodename, animationStates } = published;
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
				if ( dead && entry.posture?.kind === "down" ) entry.downDeath = true;
				else if ( !dead ) entry.downDeath = false;
				if ( dead || entity.mountedOn || entity.movementMode === 4 ) {
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
					if ( entry.posture ) {
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
				const eligible = !suppressIdle && !dead && !moving && !entity.mountedOn && entity.movementMode !== 4 &&
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
