/*
===========================================================================

presentation-actions.ts - cast action clocks and authored event preparation

Owns action clocks, prediction adoption and learned-motion warmup. The parent
builds delivered-state indices before this phase and receives the updated
gameplay snapshot before effect and hit feedback consume its events.

===========================================================================
*/
import { requestCastCancellation } from "@/engine/foundation/gameplay/cast-results";
import {
	advanceAction,
	actionLayers,
	reconcileActionInstallations,
	type ActionSchedule
} from "@/engine/foundation/animation/action-schedule";
import { advanceGroundVisual, type GroundVisualClock } from "@/engine/foundation/animation/ground-visual";
import { weaponSoundLabel, type CharacterSoundContext } from "@/engine/foundation/animation/sound-selectors";
import { weaponAnimationSet, type AnimationMetadata } from "@/engine/foundation/animation/animation-metadata";
import { skillMotionResolveAnimation } from "@/engine/foundation/animation/skill-motion-resolve";
import { combatStanceOnCast, COMBAT_STANCE_SECONDS } from "@/engine/foundation/animation/combat-stance";
import type { createReferenceAppearances } from "@/engine/foundation/animation/reference-appearance";
import type { StructureVisuals } from "@/engine/foundation/rendering/structure-stage";
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState, Pose, CastState } from "@/engine/contracts/gameplay";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import type { Resource, PresentationOutput } from "./internal/presentation-contract";

/*
================
PresentationActionCatalog
================
*/
interface PresentationActionCatalog {
	readonly catalog: ReadonlyMap<number, Resource>;
	readonly nativeMotionUrls: ReadonlyMap<string, ReadonlyMap<string, string>>;
	readonly animationStates: ReadonlyMap<string, Record<string, AnimationMetadata>>;
	readonly soundProfiles: ReadonlyMap<string, string>;
	readonly skillSounds: Pick<ReadonlyMap<number, readonly [string, string]>, "get">;
}

/*
================
PresentationActionFrame

Current frame values and narrow synchronous capabilities borrowed from owners.
================
*/
interface PresentationActionFrame {
	readonly entities: readonly EntityState[];
	readonly gameplay: GameplayState | null;
	readonly seconds: number;
	readonly simulationMs: number | undefined;
	readonly local: EntityState | undefined;
	readonly entitiesByGid: ReadonlyMap<number, EntityState>;
	readonly castTokens: ReadonlySet<number>;
	readonly vitalsByGid: ReadonlyMap<number, GameplayState["vitals"][number]>;
	readonly groundClocks: Map<number, GroundVisualClock & { duration: number; modifierId: number; }>;
	readonly combatStanceEnds: Map<number, number>;
	readonly resourceFor: ( entity: EntityState ) => Resource | undefined;
	readonly appearanceRef: ( entity: EntityState ) => number;
	readonly logicalPose: ( entity: EntityState ) => Pose;
	readonly wornEquipment: ( entity: EntityState, gameplay: GameplayState | null ) => readonly {
		readonly slot: number;
		readonly refObjId: number;
		readonly typeFlags: number;
		readonly plus: number;
	}[];
	readonly referenceAppearances: Pick<ReturnType<typeof createReferenceAppearances>, "get">;
	readonly random: Pick<PresentationRandom, "range">;
	readonly resources: { animation( path: string, role: string, url: string ): boolean; };
	readonly effects: {
		loaded(): boolean;
		phases( skill: number ): readonly (readonly string[])[] | undefined;
		structureShake( atMs: number ): void;
	};
	readonly feedback: {
		pendingDeaths(
			casts: readonly CastState[],
			currentResult?: ( gid: number, key: string ) => boolean
		): ReadonlySet<number>;
	};
	readonly health: {
		dead( gid: number ): boolean;
		currentResult( gid: number, key: string ): boolean;
	} | undefined;
	readonly structureVisuals: {
		step(
			rows: readonly { entity: EntityState; visuals: StructureVisuals; hp: number | undefined; }[],
			nowMs: number
		): readonly { gid: number; handle?: string; shake: boolean; }[];
	};
	readonly sounds: {
		emit(
			id: string,
			profile: string,
			cues: readonly string[],
			context: CharacterSoundContext,
			position: readonly [number, number, number],
			now: number
		): boolean;
	};
}

/*
================
createPresentationActions
================
*/
export function createPresentationActions() {
	const actionClocks = new Map<number, ActionSchedule>();
	// Action events of a predicted cast (cast-prediction.ts), held until the
	// server's cast adopts its clock: effects and sounds belong to that cast.
	const predictedEvents = new Map<number, ReturnType<typeof advanceAction>["events"]>();
	const deathFinalizes = new Map<number, number>();
	const warm = {
		warmSkills: undefined as readonly number[] | undefined,
		warmBody: undefined as string | undefined,
		warmMotions: [] as { role: string; url: string; }[]
	};
	return {
		actionClocks,
		predictedEvents,
		deathFinalizes,
		warm,
		/*
		================
		step

		Death cancellation may copy gameplay.casts, changing only each cast's
		cancellationRequestedAtMs and cancelledAtMs. Pose and movement fields,
		vitals, cast order, token, caster, skill, target and resultOnly stay intact.
		Earlier movement samples and state indices therefore remain valid: later
		castByActor consumers read membership or skill, while cancellation timing
		is read from the returned gameplay and the action clocks advanced here.
		Keep that field contract if this phase gains another snapshot adjustment.
		================
		*/
		step(
			frame: PresentationActionFrame,
			output: Pick<PresentationOutput, "failure">,
			published: PresentationActionCatalog
		) {
			const {
				entities,
				seconds,
				simulationMs,
				local,
				entitiesByGid,
				castTokens,
				vitalsByGid,
				groundClocks,
				combatStanceEnds,
				resourceFor,
				appearanceRef,
				logicalPose,
				wornEquipment,
				referenceAppearances,
				random,
				resources,
				effects,
				feedback,
				health,
				structureVisuals,
				sounds
			} = frame;
			let { gameplay } = frame;
			// Native death motion clears attack bit 2 (CCC A44 = FFCF),
			// invoking 8E6120 -> 8DCF40. Retire the action, flushing committed
			// results; independently launched flights retain their own results.
			for ( const token of deathFinalizes.keys() ) if ( !castTokens.has( token ) ) deathFinalizes.delete( token );
			const waitingForDeathHit = feedback.pendingDeaths( gameplay?.casts ?? [], health?.currentResult );
			if ( gameplay?.casts?.length ) {
				const casts = gameplay.casts.map( cast => {
					const caster = entitiesByGid.get( cast.caster ), dead = caster?.appearanceState?.[0] === 2;
					if ( dead && !waitingForDeathHit.has( cast.caster ) && !deathFinalizes.has( cast.token ) ) {
						deathFinalizes.set( cast.token, simulationMs ?? seconds * 1000 );
					}
					const ended = deathFinalizes.get( cast.token );
					return !cast.resultOnly && ended !== undefined &&
							(cast.cancelledAtMs === undefined || ended < cast.cancelledAtMs) ?
						requestCastCancellation( cast, ended ) :
						cast;
				} );
				if ( casts.some( ( cast, i ) => cast !== gameplay!.casts![i] ) ) gameplay = { ...gameplay, casts };
			}

			const actionLayersByActor = new Map<number, import("@/engine/contracts/character").CharacterLayer[]>();
			const waitingActors = new Set<number>();
			// Warm learned motions through the character resource owner. BANs
			// stay shared by body/role; no per-cast mesh or texture rebuild.
			if ( local ) {
				const body = resourceFor( local ), urls = body && published.nativeMotionUrls.get( body.codename );
				if ( body && urls && effects.loaded() ) {
					if ( warm.warmSkills !== gameplay?.skills || warm.warmBody !== body.glb ) {
						warm.warmSkills = gameplay?.skills;
						warm.warmBody = body.glb;
						const roles = new Set(
							(warm.warmSkills ?? []).flatMap( skill => (effects.phases( skill ) ?? []).flat() )
						);
						warm.warmMotions = [ ...roles ].flatMap( role => {
							const url = urls.get( role );
							return url ? [ { role, url } ] : [];
						} );
					}
					if ( warm.warmMotions.length ) {
						warm.warmMotions = warm.warmMotions.filter( ( { role, url } ) =>
							!resources.animation( body.glb, role, url )
						);
					}
				}
			}
			const triggers: import("@/engine/contracts/effects").EffectTrigger[] = [];
			// A prediction's clock lives while it is published and until the
			// server's cast that adopts it takes it over below.
			const predictionToken = gameplay?.castPrediction?.token;
			const adopting = new Set( (gameplay?.casts ?? []).map( cast => cast.predictedToken ) );
			for ( const [token, clock] of actionClocks ) {
				if ( castTokens.has( token ) || token === predictionToken || adopting.has( token ) ) continue;
				const entity = clock.caster === undefined ? undefined : entitiesByGid.get( clock.caster );
				if ( entity && entity.appearanceState?.[0] !== 2 && !health?.dead( entity.gid ) ) {
					// Model-owned installations outlive the skill decoration. Stop
					// its WAIT/SHOT once, retaining READY's native natural exit.
					advanceAction( clock, seconds, undefined, clock.cancelledAt ?? seconds );
					const layers = actionLayers( clock, seconds );
					if ( layers.length ) {
						continue;
					}
				}
				actionClocks.delete( token );
			}
			for ( const token of predictedEvents.keys() ) {
				if ( token !== predictionToken && !adopting.has( token ) ) predictedEvents.delete( token );
			}
			for ( const [gid, clock] of groundClocks ) {
				const entity = entitiesByGid.get( gid );
				if ( !entity ) groundClocks.delete( gid );
				else advanceGroundVisual( clock, seconds, !!entity.groundItem?.claimantGid, clock.duration );
			}
			/*
			================
			soundContext
			================
			*/
			function soundContext( entity: EntityState, skill = 0, critical = false ) {
				const player = entity.kind === "player" || entity.kind === "local-player",
					equipment = wornEquipment( entity, gameplay ),
					weapon = equipment.find( item => item.slot === 6 );
				const disguise = referenceAppearances.get( entity.gid );
				return {
					player,
					weapon: disguise ?
						weaponSoundLabel( disguise.weapon << 11 ) :
						weapon ?
						weaponSoundLabel( weapon.typeFlags ) :
						"PUNCH",
					skill: published.skillSounds.get( skill )?.[player ? 1 : 0],
					critical,
					berserk: entity.appearanceState?.[2] === 1
				};
			}
			// 4F7CC0: every staged structure re-evaluates on its own one-second
			// timer; a stage reached by rising damage plays its sound (4F78A0).
			for (
				const event of structureVisuals.step(
					entities.flatMap( entity => {
						const staged = entity.kind === "structure" ?
							published.catalog.get( appearanceRef( entity ) )?.structureVisuals :
							undefined;
						return staged ? [ { entity, visuals: staged, hp: vitalsByGid.get( entity.gid )?.hp } ] : [];
					} ),
					simulationMs ?? seconds * 1000
				)
			) {
				// Camera scripts run on the presentation clock, like the skill shakes.
				if ( event.shake ) effects.structureShake( seconds * 1000 );
				const entity = entitiesByGid.get( event.gid ),
					resource = entity ? published.catalog.get( appearanceRef( entity ) ) : undefined;
				if ( !entity || !resource || !event.handle ) continue;
				const pose = logicalPose( entity );
				sounds.emit(
					`structure:${event.gid}:${event.handle}:${seconds}`,
					resource.soundProfileName ?? published.soundProfiles.get( resource.codename ) ?? resource.codename,
					[ event.handle ],
					soundContext( entity ),
					[ (pose.regionId & 255) * 1920 + pose.x, pose.y, (pose.regionId >>> 8) * 1920 + pose.z ],
					seconds
				);
			}
			// The local press's prediction animates beside the server's casts.
			const animated = gameplay?.castPrediction ?
				[ ...(gameplay.casts ?? []), gameplay.castPrediction ] :
				gameplay?.casts ?? [];
			for ( const cast of animated ) {
				if ( cast.resultOnly ) continue;
				const entity = entitiesByGid.get( cast.caster ), resource = entity ? resourceFor( entity ) : undefined;
				if ( !entity || !resource ) continue;
				let clock = actionClocks.get( cast.token );
				// The server's cast takes over the prediction's running action and
				// fires the events it held back, so nothing restarts.
				let adopted: ReturnType<typeof advanceAction>["events"] = [];
				if ( !clock && cast.predictedToken !== undefined ) {
					clock = actionClocks.get( cast.predictedToken );
					if ( clock ) {
						actionClocks.delete( cast.predictedToken );
						actionClocks.set( cast.token, clock );
						adopted = predictedEvents.get( cast.predictedToken ) ?? [];
						predictedEvents.delete( cast.predictedToken );
					}
				}
				if ( !clock ) {
					const tables = effects.phases( cast.skill );
					if ( !tables ) continue;
					const inventory = wornEquipment( entity, gameplay );
					const weapon = inventory.find( item => item.slot === 6 );
					let loading = false;
					const alternatives = tables.map( table =>
						table.map( role => {
							if ( role.startsWith( "native:" ) ) {
								const resolved = skillMotionResolveAnimation( {
									role,
									clips: resource.clips,
									bodyStates: resource.animationStates,
									catalogStates: published.animationStates.get( resource.codename ),
									motionUrls: published.nativeMotionUrls.get( resource.codename )
								} );
								if ( !resolved ) return undefined;
								if (
									resolved.banUrl &&
									!resources.animation( resource.glb, resolved.clip, resolved.banUrl )
								) loading = true;
								return { clip: resolved.clip, definition: resolved.definition };
							}
							const disguise = referenceAppearances.get( entity.gid ),
								set = disguise ?
									weaponAnimationSet( disguise.weapon << 11 ) :
									weapon ?
									weaponAnimationSet( weapon.typeFlags ) :
									undefined;
							const armed = set ? `${role}-${set}` : "";
							const clip = resource.clips.includes( armed ) ?
								armed :
								resource.clips.includes( role ) ?
								role :
								"";
							const definition =
								(resource.animationStates ?? published.animationStates.get( resource.codename ))
									?.[clip];
							return definition ? { clip, definition } : undefined;
						} )
					);
					if ( alternatives.some( table => table.some( phase => phase === undefined ) ) ) {
						output.failure = `Missing action phase timeline ${resource.codename}`;
						continue;
					}
					if ( loading ) continue;
					// 8E0440 stores the low byte of rand(); 8E06E0 shares it
					// across READY/WAIT/SHOT, reducing by each authored count.
					const choice = random.range( 0, 32768 ) & 255;
					const phases = alternatives.map( table => table.length ? table[choice % table.length]! : null );
					const age = simulationMs !== undefined && cast.receivedAtMs !== undefined ?
						Math.max( 0, simulationMs - cast.receivedAtMs ) / 1000 :
						0;
					clock = {
						caster: cast.caster,
						started: seconds - age,
						previous: 0,
						phases: phases as ActionSchedule["phases"],
						phase: 0,
						entered: false
					};
					actionClocks.set( cast.token, clock );
					if (
						entity.appearanceState?.[0] !== 2 && !health?.dead( entity.gid ) &&
						combatStanceOnCast( entity, phases.some( phase => phase !== null ), !!phases[1] )
					) {
						combatStanceEnds.set(
							entity.gid,
							Math.max(
								combatStanceEnds.get( entity.gid ) ?? -Infinity,
								clock.started + COMBAT_STANCE_SECONDS
							)
						);
					}
				}
				const shotAt = cast.shotAtMs !== undefined && simulationMs !== undefined ?
					seconds + (cast.shotAtMs - simulationMs) / 1000 :
					undefined;
				// Death exits the character action even if pmhp defers destruction of
				// its skill deco. A network-only deferred request does not.
				const deathAt = deathFinalizes.get( cast.token );
				const stopAt = deathAt === undefined ?
					cast.cancelledAtMs :
					Math.min( deathAt, cast.cancelledAtMs ?? Infinity );
				const cancelledAt = stopAt !== undefined ?
					seconds + (stopAt - (simulationMs ?? seconds * 1000)) / 1000 :
					undefined;
				clock.animationRate = entity.animationRate ?? 1;
				const events = [
					...adopted.map( event => ({ ...event, adopted: true }) ),
					...advanceAction( clock, seconds, shotAt, cancelledAt ).events
				];
				const attackKind = clock.phases[2]?.clip.startsWith( "native:" ) ?
					Number( clock.phases[2].clip.split( ":" )[2] ) :
					({ attack1: 2, attack2: 5, attack3: 16, attack4: 17 } as Record<string, number>)[
						clock.phases[2]?.clip.split( "-" )[0] ?? ""
					] ?? 0;
				let presented = events;
				if ( cast.token === predictionToken ) {
					// The windup (READY, WAIT) presents at the press, sound and all;
					// the release and its impacts wait for the server's answer, which
					// adopts the prediction's visuals (effects.ts adoptCast).
					presented = events.filter( event => event.phase === "READY" || event.phase === "WAIT" );
					predictedEvents.set( cast.token, [
						...(predictedEvents.get( cast.token ) ?? []),
						...events.filter( event => event.phase !== "READY" && event.phase !== "WAIT" )
					] );
				}
				for ( const event of presented ) triggers.push( { cast, ...event, attackKind } );
			}
			reconcileActionInstallations( actionClocks.values() );
			for ( const clock of actionClocks.values() ) {
				if ( clock.caster === undefined ) continue;
				// 8E60C0 leaves action state 2 at the last motion callback and
				// rejects callbacks from a replaced installation. A retained skill
				// decoration does not own that state after its motion ends. Check
				// after reconciliation so a replaced WAIT cannot suppress idle.
				if (
					clock.phases[1] && clock.cancelledAt === undefined &&
					(clock.wait || clock.phase < 3 && clock.phase !== 1 && !clock.phaseSuperseded)
				) waitingActors.add( clock.caster );
				actionLayersByActor.set( clock.caster, [
					...actionLayers( clock, seconds ),
					...(actionLayersByActor.get( clock.caster ) ?? [])
				] );
			}
			return { gameplay, actionLayersByActor, waitingActors, triggers, soundContext };
		},
		/*
		================
		warmed

		True once every learned motion the warm-up planned has been fetched.
		================
		*/
		warmed() {
			return warm.warmMotions.length === 0;
		},
		/*
		================
		resetWarm

		The presentation teardown clears the warm-up early and the action
		clocks near its end (resetClocks); the two keep those positions.
		================
		*/
		resetWarm() {
			warm.warmSkills = undefined;
			warm.warmBody = undefined;
			warm.warmMotions = [];
		},
		/*
		================
		resetClocks
		================
		*/
		resetClocks() {
			actionClocks.clear();
			predictedEvents.clear();
			deathFinalizes.clear();
		}
	};
}
