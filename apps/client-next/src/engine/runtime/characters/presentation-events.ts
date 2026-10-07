/*
===========================================================================

presentation-events.ts - effect advancement and ordered hit feedback

Consumes authored action triggers, advances effect flights and applies their
feedback before actor presentation. Owns floating damage text and the last
environmental damage sequence; the parent retains the original reset order.

===========================================================================
*/
import { appendDamageText, damageText } from "@/engine/foundation/ui/damage-text";
import { characterHeadingYaw } from "@/engine/foundation/math/angles";
import { hawkResult } from "@/engine/foundation/gameplay/attached-effects";
import { damageAnchor } from "@/engine/foundation/animation/damage-anchor";
import { combatStanceOnHit, COMBAT_STANCE_SECONDS } from "@/engine/foundation/animation/combat-stance";
import type { ActionSchedule } from "@/engine/foundation/animation/action-schedule";
import type { createReferenceAppearances } from "@/engine/foundation/animation/reference-appearance";
import type { CharacterSoundContext } from "@/engine/foundation/animation/sound-selectors";
import type { AttachedEffect } from "@/engine/foundation/gameplay/attached-effects";
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState, CastState, CastImpact, Pose } from "@/engine/contracts/gameplay";
import type { CharacterActor } from "@/engine/contracts/character";
import type { DamageText } from "@/engine/contracts/damage-text";
import type { EffectTrigger, EffectImpactEvent, HawkImpact, ImpactFeedback } from "@/engine/contracts/effects";
import type { Renderer } from "@/engine/contracts/runtime";
import type { Resource, PresentationOutput } from "./internal/presentation-contract";

/*
================
PresentationEventEffects

Synchronous effect operations borrowed from the existing effect owner.
================
*/
interface PresentationEventEffects {
	step(
		entities: readonly EntityState[],
		gameplay:
			| Pick<
				GameplayState,
				"localGid" | "pose" | "casts" | "vitals" | "questMarkers" | "inventory" | "attachedEffects"
			>
			| null,
		now: number,
		ready: ( path: string ) => boolean,
		duration: ( path: string, clip: string ) => number,
		triggers: readonly EffectTrigger[],
		socket: (
			gid: number,
			bone: string | null,
			offset: readonly [number, number, number],
			trigger: EffectTrigger
		) => CharacterActor["pose"] | null,
		presented: readonly CharacterActor[],
		effectDetail: number,
		bloodEnabled: boolean
	): CharacterActor[];
	attachedInstances(): readonly { key: string; gid: number; skill: number; stopped: boolean; }[];
	takeActivations(): readonly { gid: number; skill: number; at: number; }[];
	takeHawkImpacts(): readonly HawkImpact[];
	takeImpacts(): readonly EffectImpactEvent[];
	impactIndex( skill: number, phase: string, event: number ): number;
	hitFlash( localCaster: boolean, hwan: boolean, flags: number, atMs: number ): void;
	impactSource(
		caster: number | undefined,
		target: number,
		attached: readonly AttachedEffect[],
		cast: CastState
	): { gid: number; skill: number; defensive: boolean; };
	damage(
		gid: number,
		caster: number,
		kind: number,
		skill: number,
		defensive: boolean,
		projectile: boolean,
		secondary: boolean,
		pose: CharacterActor["pose"],
		basis: NonNullable<CharacterActor["effectBasis"]>,
		blood: readonly [string | null, string | null] | undefined,
		bloodEnabled: boolean,
		now: number,
		ready: ( path: string ) => boolean
	): CharacterActor[];
}

/*
================
PresentationEventCatalog
================
*/
interface PresentationEventCatalog {
	readonly catalog: ReadonlyMap<number, Resource>;
	readonly soundProfiles: ReadonlyMap<string, string>;
	readonly effectAnchors: ReadonlyMap<string, NonNullable<CharacterActor["effectAnchor"]>>;
	readonly riderModes: ReadonlyMap<string, number>;
	readonly bloodEffects: ReadonlyMap<string, readonly [string | null, string | null]>;
}

/*
================
PresentationEventFrame
================
*/
interface PresentationEventFrame {
	readonly entities: readonly EntityState[];
	readonly gameplay: GameplayState | null;
	readonly seconds: number;
	readonly simulationMs: number | undefined;
	readonly entitiesByGid: ReadonlyMap<number, EntityState>;
	readonly actionClocks: ReadonlyMap<number, ActionSchedule>;
	readonly combatStanceEnds: Map<number, number>;
	readonly triggers: readonly EffectTrigger[];
	readonly samples: Pick<ReadonlyMap<number, unknown>, "has">;
	readonly localMover: ( gid: number ) => boolean;
	readonly logicalPose: ( entity: EntityState ) => Pose;
	readonly appearanceRef: ( entity: EntityState ) => number;
	readonly soundContext: ( entity: EntityState, skill?: number, critical?: boolean ) => CharacterSoundContext;
	readonly posePresentation: { pose( gid: number, native: Pose, seconds: number ): Pose; };
	readonly referenceAppearances: Pick<ReturnType<typeof createReferenceAppearances>, "step">;
	readonly effects: PresentationEventEffects;
	readonly effectDetail: number;
	readonly bloodEnabled: boolean;
	readonly resources: {
		ready( path: string ): boolean;
		duration( path: string, clip: string ): number;
	};
	readonly renderer: Pick<Renderer, "characterSocket" | "characterMatrix" | "characterLocalMatrix">;
	readonly feedback: {
		take(
			casts: readonly CastState[],
			triggers: readonly EffectTrigger[],
			index: ( skill: number, phase: string, event: number ) => number,
			now: number,
			simulationMs: number | undefined,
			transfers: readonly EffectImpactEvent[]
		): readonly ImpactFeedback[];
		pendingDeaths(
			casts: readonly CastState[],
			currentResult?: ( gid: number, key: string ) => boolean
		): ReadonlySet<number>;
	};
	readonly health: {
		impact( gid: number, key: string, impact: CastImpact, now: number, source?: "cast" | "hawk" ): boolean;
		currentResult( gid: number, key: string ): boolean;
		dead( gid: number ): boolean;
		finishedCasts(): void;
	} | undefined;
	readonly sounds: {
		emit(
			id: string,
			profile: string,
			cues: readonly string[],
			context: CharacterSoundContext,
			position: readonly [number, number, number],
			now: number
		): boolean;
		impact(
			id: string,
			gid: number,
			profile: string,
			cues: readonly string[],
			context: CharacterSoundContext,
			at: number
		): void;
		flush( now: number, position: ( gid: number ) => readonly [number, number, number] | undefined ): void;
	};
}

/*
================
createPresentationEvents
================
*/
export function createPresentationEvents() {
	const state = { damageTexts: [] as DamageText[], environmentalSequence: 0 };
	return {
		state,
		/*
		================
		step
		================
		*/
		step(
			frame: PresentationEventFrame,
			output: Pick<PresentationOutput, "displayed">,
			published: PresentationEventCatalog
		) {
			const {
				entities,
				gameplay,
				seconds,
				simulationMs,
				entitiesByGid,
				actionClocks,
				combatStanceEnds,
				triggers,
				samples,
				localMover,
				logicalPose,
				appearanceRef,
				soundContext,
				posePresentation,
				referenceAppearances,
				effects,
				effectDetail,
				bloodEnabled,
				resources,
				renderer,
				feedback,
				health,
				sounds
			} = frame;
			if ( state.damageTexts.length ) {
				state.damageTexts = state.damageTexts.filter( row => seconds - row.started <= 3 );
			}
			for ( const event of gameplay?.environmentalDamage ?? [] ) {
				if ( event.sequence <= state.environmentalSequence ) continue;
				state.environmentalSequence = event.sequence;
				const target = entitiesByGid.get( event.gid ),
					at = simulationMs === undefined ? seconds : seconds + (event.atMs - simulationMs) / 1000;
				if ( !target || seconds - at >= 1 ) continue;
				if ( state.damageTexts.length >= 2048 ) throw Error( "Damage text capacity exceeded" );
				const native = logicalPose( target );
				const anchor = posePresentation.pose( target.gid, native, seconds );
				// 77A080 -> 8E2840 -> 8D4DD0: environmental feedback is a
				// victim label, with no attack animation or invented impact sound.
				state.damageTexts = appendDamageText(
					state.damageTexts,
					damageText(
						{ ...target, ...anchor },
						{ type: 0, flags: 1, damage: event.damage, fatal: false, secondaryAmount: 0 },
						at,
						true
					),
					seconds
				);
			}
			const hitByActor = new Map<
				number,
				{ token: string; at: number; damage: number; critical: boolean; downAt?: number; }
			>();
			// Effects anchor to what is drawn: a walking character's frame-clock pose,
			// not the worker's latest sample, or entity-attached effects jitter
			// against the body. pose() is idempotent within one frame time.
			let drawnLocal: import("@/engine/contracts/gameplay").Pose | null = null;
			const effectEntities = entities.map( entity => {
				const local = entity.gid === gameplay?.localGid && !!gameplay.pose;
				if ( !samples.has( entity.gid ) || !localMover( entity.gid ) && !entity.moving ) return entity;
				const drawn = posePresentation.pose( entity.gid, logicalPose( entity ), seconds );
				if ( local ) {
					drawnLocal = { ...gameplay!.pose!, regionId: drawn.regionId, x: drawn.x, y: drawn.y, z: drawn.z };
				}
				return { ...entity, regionId: drawn.regionId, x: drawn.x, y: drawn.y, z: drawn.z };
			} );
			// Sample sockets from the admitted models at the current mechanical pose
			// and authored callback cursor; flight ownership precedes hit feedback.
			// The press's prediction is a cast of its own until the server adopts it.
			const effectGameplay = gameplay && (drawnLocal || gameplay.castPrediction) ?
				{
					localGid: gameplay.localGid,
					pose: drawnLocal ?? gameplay.pose,
					casts: gameplay.castPrediction ? [ ...gameplay.casts, gameplay.castPrediction ] : gameplay.casts,
					vitals: gameplay.vitals,
					questMarkers: gameplay.questMarkers,
					inventory: gameplay.inventory,
					attachedEffects: gameplay.attachedEffects
				} :
				gameplay;
			const effectActors = effects.step(
				effectEntities,
				effectGameplay,
				seconds,
				resources.ready,
				resources.duration,
				triggers,
				( gid, bone, offset, trigger ) => {
					const phaseIndex = trigger.phase === "READY" ? 0 : trigger.phase === "WAIT" ? 1 : 2;
					const phase = actionClocks.get( trigger.cast.token )?.phases[phaseIndex];
					const cursor = trigger.event === 0 ?
						0 :
						phase?.definition.trackEvents.filter( row => row.eventCode === 1 )[trigger.event - 1]?.cursorMs;
					const rows = [ ...output.displayed.values() ].map( actor => {
						const entity = entitiesByGid.get( actor.gid ),
							native = entity ? logicalPose( entity ) : undefined;
						const sampled = native ? posePresentation.pose( actor.gid, native, seconds ) : undefined;
						const currentPose = sampled ?
							{
								regionId: sampled.regionId,
								x: sampled.x,
								y: sampled.y,
								z: sampled.z,
								yaw: characterHeadingYaw( sampled.angle )
							} :
							actor.pose;
						const shifted = {
							...actor,
							pose: currentPose,
							time: Math.max( 0, actor.time + trigger.at - seconds ),
							layers: actor.layers?.map( layer => ({
								...layer,
								time: Math.max( 0, layer.time + trigger.at - seconds )
							}) )
						};
						if (
							!trigger.sampleCurrent && actor.gid === gid && gid === trigger.cast.caster && phase &&
							cursor !== undefined
						) {
							shifted.layers = [ {
								clip: phase.clip,
								time: cursor / 1000,
								loop: phaseIndex === 1,
								weight: 1,
								lane: "event"
							}, ...(shifted.layers ?? []).filter( layer => layer.lane === "timed" ) ];
						}
						return shifted;
					} );
					return renderer.characterSocket( rows, gid, { name: bone, fallback: "mount-root" }, offset );
				},
				[ ...output.displayed.values() ],
				effectDetail,
				bloodEnabled
			);
			// 008DD6F0 calls snd_activate once after constructing the effect,
			// including restored instances and immediate-stop instances.
			referenceAppearances.step( effects.attachedInstances() );
			for ( const event of effects.takeActivations() ) {
				const entity = entitiesByGid.get( event.gid ),
					resource = entity ? published.catalog.get( appearanceRef( entity ) ) : undefined;
				if ( entity && resource ) {
					const pose = logicalPose( entity );
					sounds.emit(
						`activate:${event.gid}:${event.skill}:${event.at}`,
						resource.soundProfileName ?? published.soundProfiles.get( resource.codename ) ??
							resource.codename,
						[ "SND_ACTIVATE" ],
						soundContext( entity, event.skill ),
						[ (pose.regionId & 255) * 1920 + pose.x, pose.y, (pose.regionId >>> 8) * 1920 + pose.z ],
						seconds
					);
				}
			}
			const hawkHits = effects.takeHawkImpacts().map( event => ({
				cast: {
					token: event.id,
					caster: event.holder,
					target: event.target,
					skill: event.skill,
					damage: event.damage & 0x7fff,
					fatal: false
				},
				target: event.target,
				impact: hawkResult( event.damage ),
				key: event.resultKey,
				at: event.at,
				source: "hawk" as const,
				soundSkill: event.skill
			}) );
			const hits = [
				...feedback.take(
					gameplay?.casts ?? [],
					triggers,
					effects.impactIndex,
					seconds,
					simulationMs,
					effects.takeImpacts()
				),
				...hawkHits
			];
			for ( const hit of hits ) {
				const { cast, impact, at, key } = hit,
					target = entitiesByGid.get( hit.target ),
					caster = entitiesByGid.get( cast.caster );
				if ( !target ) continue;
				// Retired results must not restart hit/death motion, sounds or
				// floating damage on a revived actor. Admission belongs to the
				// ordered HP owner, not a second LIFE cache in this renderer.
				if (
					health &&
					!health.impact(
						hit.target,
						key,
						impact,
						simulationMs ?? seconds * 1000,
						hit.source === "hawk" ? "hawk" : "cast"
					)
				) continue;
				// Refresh at damage application, not receipt or distance admission.
				const alive = health ? !health.dead( target.gid ) : target.appearanceState?.[0] !== 2;
				if ( combatStanceOnHit( target, alive, impact ) ) {
					combatStanceEnds.set(
						target.gid,
						Math.max(
							combatStanceEnds.get( target.gid ) ?? -Infinity,
							at + COMBAT_STANCE_SECONDS
						)
					);
				}
				effects.hitFlash(
					hit.source !== "hawk" && caster !== undefined && caster.gid === gameplay?.localGid,
					caster?.appearanceState?.[2] === 1,
					impact.flags,
					Math.trunc( seconds * 1000 )
				);
				const observer = gameplay?.pose;
				// Local movement owns gameplay.pose; the entity row can still
				// contain the spawn position. Admission and text placement must
				// use the same current victim position.
				const native = logicalPose( target );
				const dx = observer ?
					native.x - observer.x + ((native.regionId & 255) - (observer.regionId & 255)) * 1920 :
					Infinity;
				const dy = observer ? native.y - observer.y : Infinity;
				const dz = observer ?
					native.z - observer.z + ((native.regionId >>> 8) - (observer.regionId >>> 8)) * 1920 :
					Infinity;
				if ( cast.caster === gameplay?.localGid || dx * dx + dy * dy + dz * dz < 40000 ) {
					if ( state.damageTexts.length >= 2048 ) throw Error( "Damage text capacity exceeded" );
					const anchor = posePresentation.pose( target.gid, native, seconds );
					state.damageTexts = appendDamageText(
						state.damageTexts,
						damageText( { ...target, ...anchor }, impact, at, !caster ),
						seconds
					);
					if ( impact.type === 2 && seconds - at <= .25 ) {
						const resource = published.catalog.get( target.refObjId );
						if ( resource ) {
							sounds.emit(
								"block:" + key,
								resource.soundProfileName ?? published.soundProfiles.get( resource.codename ) ??
									resource.codename,
								[ "SND_BLOCKING" ],
								soundContext( target, 0, !!(impact.flags & 2) ),
								[
									(anchor.regionId & 255) * 1920 + anchor.x,
									anchor.y,
									(anchor.regionId >>> 8) * 1920 + anchor.z
								],
								at
							);
						}
					}
				}
				// 7756D0 captures the trap position before destruction. Result
				// presentation must not depend on its model or entity surviving.
				if ( effectDetail && "effectPosition" in cast && cast.effectPosition ) {
					const position = cast.effectPosition;
					effectActors.push( ...effects.damage(
						target.gid,
						cast.caster,
						impact.type ?? 0,
						cast.skill,
						false,
						false,
						true,
						{ ...position, yaw: characterHeadingYaw( position.angle ) },
						[ 1, 0, 0, 0, 1, 0, 0, 0, 1 ],
						undefined,
						bloodEnabled,
						seconds,
						resources.ready
					) );
					continue;
				}
				const route = hit.source === "flush" ?
					effects.impactSource( caster?.gid, target.gid, gameplay?.attachedEffects ?? [], cast ) :
					{ gid: cast.caster, skill: hit.soundSkill ?? 0, defensive: false };
				if ( effectDetail && caster && hit.source !== "hawk" ) {
					const shown = [ ...output.displayed.values() ].map( actor => {
							const entity = entitiesByGid.get( actor.gid );
							if ( !entity ) return actor;
							const pose = posePresentation.pose( actor.gid, logicalPose( entity ), seconds );
							return {
								...actor,
								pose: {
									regionId: pose.regionId,
									x: pose.x,
									y: pose.y,
									z: pose.z,
									yaw: characterHeadingYaw( pose.angle )
								}
							};
						} ),
						source = shown.find( a => a.gid === caster.gid ),
						victim = shown.find( a => a.gid === target.gid ),
						matrix = source ? renderer.characterMatrix?.( shown, caster.gid ) : null;
					if ( source && victim && matrix ) {
						const targetResource = published.catalog.get( appearanceRef( target ) ),
							anchor = targetResource ?
								published.effectAnchors.get( targetResource.codename ) :
								undefined;
						const bone = anchor?.bone ?
							renderer.characterLocalMatrix( shown, target.gid, anchor.bone ) :
							null;
						const ride = target.mountedOn ? entitiesByGid.get( target.mountedOn ) : undefined,
							rideResource = ride ? published.catalog.get( ride.refObjId ) : undefined;
						const saddle =
							anchor?.bone && ride && rideResource && !published.riderModes.get( rideResource.codename ) ?
								renderer.characterLocalMatrix( shown, ride.gid, "saddle" ) :
								null;
						const point = hit.source === "cast" ?
							(hit.position ?? victim.pose) :
							anchor ?
							damageAnchor( victim.pose, source.pose, anchor.offset, bone, saddle ) :
							victim.pose;
						// 8D5440 copies the caster's native world matrix: an imported
						// body's placement x Ry(PI). The program draws native space.
						const basis = Array.from(
							{ length: 9 },
							( _, i ) => matrix[Math.floor( i / 3 ) * 4 + i % 3]! * (i >= 3 && i < 6 ? 1 : -1)
						) as unknown as NonNullable<CharacterActor["effectBasis"]>;
						effectActors.push(
							...effects.damage(
								target.gid,
								caster.gid,
								impact.type ?? 0,
								route.skill,
								route.defensive,
								hit.source === "cast",
								hit.source === "flush" || (hit.secondary ?? false),
								point,
								basis,
								targetResource ? published.bloodEffects.get( targetResource.codename ) : undefined,
								bloodEnabled,
								seconds,
								resources.ready
							)
						);
					}
				}
				const emitter = entitiesByGid.get( route.gid ),
					resource = emitter ? published.catalog.get( emitter.refObjId ) : undefined;
				// 8E3800 passes a null action owner to 8D5440. Its hawk hit
				// does not dispatch the holder's SND_DMG/critical cues.
				if (
					hit.source !== "hawk" && impact.type !== 2 && impact.type !== 7 && emitter && resource &&
					seconds - at <= .25
				) {
					const context = soundContext( emitter, route.skill, !!(impact.flags & 2) );
					sounds.impact(
						"impact:" + key,
						emitter.gid,
						resource.soundProfileName ?? published.soundProfiles.get( resource.codename ) ??
							resource.codename,
						route.defensive ?
							[ "SND_DDMG" ] :
							context.critical ?
							[ "SND_CRIDMG", "SND_DMG" ] :
							[ "SND_DMG" ],
						context,
						at
					);
				}
				if (
					impact.fatal || impact.type === 2 || impact.type === 7 || !(impact.damage > 0 || impact.type === 4)
				) continue;
				const previous = hitByActor.get( hit.target );
				hitByActor.set( hit.target, {
					token: key,
					at,
					damage: impact.damage,
					critical: !!(impact.flags & 2),
					downAt: previous?.downAt ?? (impact.type === 4 ? at : undefined)
				} );
			}
			health?.finishedCasts();
			const pendingDeaths = feedback.pendingDeaths( gameplay?.casts ?? [], health?.currentResult );
			sounds.flush( seconds, gid => {
				const pose = gid === gameplay?.localGid ? gameplay.pose : entitiesByGid.get( gid );
				return pose ?
					[ (pose.regionId & 255) * 1920 + pose.x, pose.y, (pose.regionId >>> 8) * 1920 + pose.z ] :
					undefined;
			} );
			return { hitByActor, effectActors, pendingDeaths };
		},
		/*
		================
		resetSequence

		Forget the last environmental damage sequence. The teardown resets it
		right after damage feedback, apart from the damage texts.
		================
		*/
		resetSequence() {
			state.environmentalSequence = 0;
		},
		/*
		================
		resetDamageTexts

		Drop the live damage texts. A replayed world reset (eventRain) clears
		only these, never the environmental sequence.
		================
		*/
		resetDamageTexts() {
			state.damageTexts = [];
		}
	};
}
