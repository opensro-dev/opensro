/*
===========================================================================

actor-presentation.ts - each selected entity becomes the actor the renderer draws

The per-actor phase of the character presentation frame: skill objects and
ground items, then for each character its resource, worn appearance and
dress, and the animation layers of death, posture, actions, locomotion, hits
and idles, with their sounds. It writes every actor into frame.next and
returns the particle and animation-emission holders the frame finishes with.

It owns no state that outlives a frame. characters.ts owns the maps and
services in ActorOwner and calls present once per frame, between the
presentation-state phase and finalization.

===========================================================================
*/
import { fortressAppearance } from "@/engine/foundation/animation/fortress-appearance";
import { movementEntryRate, transitionActionStates } from "@/engine/foundation/animation/action-refresh";
import { defaultWearFrozen } from "@/engine/foundation/animation/default-wear-policy";
import { selectAvatarOverride, type AvatarOverrideSelection } from "@/engine/foundation/animation/avatar-override";
import { assembleEquipmentAppearance } from "@/engine/foundation/animation/equipment-appearance";
import type { AnimationParticleSet } from "@/engine/foundation/animation/animation-emission";
import { createModelAnimation } from "@/engine/foundation/animation/model-animation";
import { createAnimationDispatch } from "@/engine/foundation/animation/animation-dispatch";
import { animationActivation, type AnimationActivation } from "@/engine/foundation/animation/animation-activation";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import {
	groundVisualClock,
	advanceGroundVisual,
	type GroundVisualClock
} from "@/engine/foundation/animation/ground-visual";
import { referenceAppearanceItems, type AppearanceChoice } from "@/engine/foundation/animation/reference-appearance";
import { blindableCharacter } from "@/engine/foundation/ui/name-visibility";
import { monsterScale } from "@/engine/foundation/rendering/monster-scale";
import { postureLayers, type Posture } from "@/engine/foundation/animation/posture";
import { oneShotLayers } from "@/engine/foundation/animation/one-shot-layers";
import { changeLocomotion, stopLocomotion, locomotionLayers } from "@/engine/foundation/animation/locomotion-blend";
import { skillMotionResolveAnimation } from "@/engine/foundation/animation/skill-motion-resolve";
import { weaponAnimationSet, type AnimationMetadata } from "@/engine/foundation/animation/animation-metadata";
import { characterHeadingYaw } from "@/engine/foundation/math/angles";
import { movementGait } from "@/engine/foundation/gameplay/native-movement";
import type { DressCatalog } from "@/engine/foundation/animation/equipment-appearance";
import type { RandomIdle } from "@/engine/foundation/animation/random-idle";
import type { StatusView } from "@/engine/foundation/animation/status-presentation";
import type { CharacterSoundContext } from "@/engine/foundation/animation/sound-selectors";
import type { SkillLookup } from "@/engine/foundation/ui/buff-viewer";
import type { SkillMetadata } from "@/engine/foundation/gameplay/skill-catalog";
import type { AttachedEffect } from "@/engine/foundation/gameplay/attached-effects";
import type {
	CharacterActor,
	CharacterAttachment,
	CharacterLayer,
	CharacterPointLight
} from "@/engine/contracts/character";
import type { CastState, GameplayState, Pose, VitalState } from "@/engine/contracts/gameplay";
import type { EntityState, TransformSkin } from "@/engine/contracts/world";
import type {
	Auxiliary,
	CharacterPresentationState,
	DropModel,
	ItemPresentation,
	PresentationAppearance,
	PresentationOutput,
	PresentationIdleState,
	Resource
} from "./internal/presentation-contract";

const PROTECTED_ANIMATION_DISTANCE = 300;

/*
================
ActorOwner

The owners and maps present borrows. Every member keeps its identity for the
presenter's lifetime: the catalogue (published) and the outputs (output) are
mutated in place, never replaced, so reading them each frame is current. Each
service is typed by the members this phase calls.
================
*/
export interface ActorOwner {
	readonly activeSkin: ( entity: EntityState ) => TransformSkin | undefined;
	readonly allocateActor: () => number;
	readonly appearances: Map<number, PresentationAppearance>;
	readonly avatarOverrides: Map<number, AvatarOverrideSelection>;
	readonly committedAuxiliary: Map<number, readonly Auxiliary[]>;
	readonly concealmentSkills: ( catalog: readonly SkillMetadata[] | undefined ) => SkillLookup;
	readonly displayedDependencies: Map<number, readonly string[]>;
	readonly effects: {
		appearance( gid: number ): {
			weaponHidden: boolean;
			materialTint: readonly [number, number, number] | undefined;
			scale: number;
			pointLight?: CharacterPointLight;
		};
		hostMotions(
			gid: number
		): { key: string; started: number; stoppedAt: number | undefined; set: string; id: number; }[];
	};
	readonly entityLod: {
		crowded(): boolean;
		distance( gid: number ): number;
		fraction( gid: number ): number;
	};
	readonly footContact: (
		entity: EntityState,
		actors: readonly CharacterActor[],
		actor: CharacterActor,
		pose: Pose,
		right: boolean,
		seconds: number
	) => void;
	readonly groundClocks: Map<number, GroundVisualClock & { duration: number; modifierId: number; }>;
	readonly health: { dead( gid: number ): boolean; } | undefined;
	readonly output: PresentationOutput;
	readonly posePresentation: {
		moving( gid: number ): boolean;
		pose( gid: number, target: Pose, now: number, settledTranslation?: boolean ): Pose;
	};
	readonly published: {
		readonly animationStates: ReadonlyMap<string, Record<string, AnimationMetadata>>;
		readonly bloodEffects: ReadonlyMap<string, readonly [string | null, string | null]>;
		readonly dress: DressCatalog;
		readonly dropModels: Record<string, DropModel>;
		readonly effectAnchors: ReadonlyMap<
			string,
			{ readonly bone: string | null; readonly offset: readonly [number, number, number]; }
		>;
		readonly heightFactors: ReadonlyMap<string, number>;
		readonly heights: ReadonlyMap<string, number>;
		readonly itemIds: ReadonlyMap<string, number>;
		readonly items: Record<string, ItemPresentation>;
		readonly manifest: number;
		readonly manifests: readonly string[];
		readonly nativeMotionUrls: ReadonlyMap<string, ReadonlyMap<string, string>>;
		readonly shadowSizes: ReadonlyMap<number, number>;
		readonly soundProfiles: ReadonlyMap<string, string>;
	};
	readonly referenceAppearances: { get( gid: number ): AppearanceChoice | undefined; };
	readonly renderer: {
		setCharacterAssembly( id: string, base: string, parts: readonly CharacterAttachment[] ): void;
	};
	readonly resourceFor: ( entity: EntityState ) => Resource | undefined;
	readonly resources: {
		animation( body: string, name: string, path: string ): boolean;
		duration( path: string, clip: string ): number;
		plan( paths: readonly string[] ): boolean;
		ready( path: string ): boolean;
	};
	readonly skillObjects: {
		frame(
			entity: EntityState,
			seconds: number,
			resources: { ready( path: string ): boolean; plan( paths: readonly string[] ): boolean; },
			viewer?: {
				localGid: number;
				effects: readonly AttachedEffect[];
				skill: ( id: number ) => SkillMetadata | undefined;
			}
		): { actor: CharacterActor; paths: string[]; particles: readonly ModelParticle[]; } | null;
	};
	readonly soundSurface: ( pose: Pose ) => string | undefined;
	readonly sounds: {
		advance(
			gid: number,
			clip: string,
			started: number,
			time: number,
			loop: boolean,
			definition: AnimationMetadata | undefined,
			now: number,
			source: () => {
				profile: string;
				position: readonly [number, number, number];
				surface?: string;
				context: CharacterSoundContext;
			},
			lane?: string | AnimationActivation
		): void;
		retainActivations( gid: number, active: ReadonlySet<AnimationActivation> ): void;
	};
	readonly states: Map<number, CharacterPresentationState>;
	readonly statusOwner: { view( gid: number, mask: number, bodyVisual: number ): StatusView; };
	readonly wornEquipment: (
		entity: EntityState,
		gameplay: GameplayState | null
	) => readonly {
		readonly slot: number;
		readonly refObjId: number;
		readonly typeFlags: number;
		readonly plus: number;
	}[];
	readonly presentationState: {
		readonly idleStates: ReadonlyMap<number, PresentationIdleState>;
		readonly combatStanceEnds: ReadonlyMap<number, number>;
	};
}

/*
================
ActorFrame

The current frame's inputs: the step's arguments, what earlier phases built
this frame, and the frame probe (assigned after construction).
================
*/
export interface ActorFrame {
	readonly actionLayersByActor: ReadonlyMap<number, CharacterLayer[]>;
	readonly active: Set<number>;
	readonly animationDeltaMs: number;
	readonly castByActor: ReadonlyMap<number, CastState>;
	readonly entities: readonly EntityState[];
	readonly gameplay: GameplayState | null;
	readonly hitByActor: ReadonlyMap<
		number,
		{ token: string; at: number; damage: number; critical: boolean; downAt?: number; }
	>;
	readonly localMover: ( gid: number ) => boolean;
	readonly logicalPose: ( entity: EntityState ) => Pose;
	readonly nativeServerName: string | undefined;
	readonly next: Map<number, CharacterActor>;
	readonly normalFortressClothes: boolean;
	readonly pendingDeaths: ReadonlySet<number>;
	readonly probe: { detailBegin( stage: string ): void; detailEnd( stage: string ): void; } | undefined;
	readonly sampleActorDetails: boolean | undefined;
	readonly seconds: number;
	readonly selected: readonly EntityState[];
	readonly soundContext: (
		entity: EntityState,
		skill?: number,
		critical?: boolean
	) => CharacterSoundContext;
	readonly vitalsByGid: ReadonlyMap<number, VitalState>;
	readonly waitingActors: ReadonlySet<number>;
}

/*
================
createActorPresentation
================
*/
export function createActorPresentation( owner: ActorOwner ) {
	/*
	================
	wornSignature

	The slot, item and plus of every worn visual slot (0..8), as the appearance
	signature compares them. Built every frame, because an in-place edit of
	the list must still change the signature, but with one string and no
	intermediate arrays.
	================
	*/
	function wornSignature( equipment: readonly { slot: number; refObjId: number; plus: number; }[] ) {
		let text = "";
		for ( const item of equipment ) {
			// The original positive test: a NaN slot is not a visual slot.
			if ( !(item.slot >= 0 && item.slot < 9) ) continue;
			if ( text ) text += ";";
			text += item.slot + "," + item.refObjId + "," + item.plus;
		}
		return text;
	}
	/*
	================
	avatarSignature

	The avatar item ids, built like wornSignature.
	================
	*/
	function avatarSignature( avatars: readonly { refObjId: number; }[] ) {
		let text = "";
		for ( let index = 0; index < avatars.length; index++ ) {
			text += (index ? ";" : "") + avatars[index]!.refObjId;
		}
		return text;
	}
	return {
		/*
		================
		present

		The original per-actor presentation pass, moved verbatim: names keep
		their meaning through the destructuring below.
		================
		*/
		present( frame: ActorFrame ) {
			const {
				activeSkin,
				allocateActor,
				appearances,
				avatarOverrides,
				committedAuxiliary,
				concealmentSkills,
				displayedDependencies,
				effects,
				entityLod,
				footContact,
				groundClocks,
				health,
				output,
				posePresentation,
				published,
				referenceAppearances,
				renderer,
				resourceFor,
				resources,
				skillObjects,
				soundSurface,
				sounds,
				states,
				statusOwner,
				wornEquipment,
				presentationState
			} = owner;
			const {
				actionLayersByActor,
				active,
				animationDeltaMs,
				castByActor,
				entities,
				gameplay,
				hitByActor,
				localMover,
				logicalPose,
				nativeServerName,
				next,
				normalFortressClothes,
				pendingDeaths,
				probe,
				sampleActorDetails,
				seconds,
				selected,
				soundContext,
				vitalsByGid,
				waitingActors
			} = frame;
			const appearanceActive = new Set( selected.map( entity => entity.gid ) );
			for ( const gid of appearances.keys() ) if ( !appearanceActive.has( gid ) ) appearances.delete( gid );
			const animationHolders: { actor: CharacterActor; sets: readonly AnimationParticleSet[]; }[] = [];
			const particleHolders: { actor: CharacterActor; particles: readonly ModelParticle[]; }[] = [];
			// Fortress clothing compares every player with the local one; find it
			// once per frame, not once per actor (a linear scan each, so O(n^2).
			const localEntity = entities.find( e => e.gid === gameplay?.localGid );
			for ( const entity of selected ) {
				active.add( entity.gid );
				try {
					if ( entity.skillObject ) {
						const visual = skillObjects.frame( entity, seconds, resources, {
							localGid: gameplay?.localGid ?? 0,
							effects: gameplay?.attachedEffects ?? [],
							skill: concealmentSkills( gameplay?.skillCatalog )
						} );
						if ( visual ) {
							next.set( entity.gid, visual.actor );
							displayedDependencies.set( entity.gid, visual.paths );
							if ( visual.particles.length ) {
								particleHolders.push( { actor: visual.actor, particles: visual.particles } );
							}
						}
						continue;
					}
					if ( entity.groundItem ) {
						const item = published.items[String( entity.refObjId )],
							drop = published.dropModels[item?.dropModelPath ?? ""];
						if ( !drop ) {
							if ( published.manifest === published.manifests.length ) {
								throw Error( "Missing authored drop model for item " + entity.refObjId );
							}
							continue;
						}
						const gold = (entity.groundItem.typeFlags & 0x60) === 0x60 &&
							(entity.groundItem.typeFlags & 0x780) === 0x280;
						const fanfare = gold && entity.groundItem.appear !== undefined ?
							published.dropModels["item/etc/drop_ch_money_ing.bsr"] :
							undefined;
						if ( gold && entity.groundItem.appear !== undefined && !fanfare ) {
							throw Error( "Missing native gold fanfare model" );
						}
						const paths = fanfare ? [ fanfare.glb, drop.glb ] : [ drop.glb ];
						const ready = paths.map( path => resources.ready( path ) ).every( Boolean );
						if ( !ready || !resources.plan( paths ) ) continue;
						let clock = groundClocks.get( entity.gid );
						if ( !clock ) {
							clock = {
								...groundVisualClock( seconds, !!fanfare ),
								modifierId: allocateActor(),
								duration: fanfare ? resources.duration( fanfare.glb, "stand" ) : 0
							};
							groundClocks.set( entity.gid, clock );
							advanceGroundVisual( clock, seconds, !!entity.groundItem.claimantGid, clock.duration );
						}
						if ( entity.groundItem.claimantGid ) continue;
						// 86DB40 event 100 schedules state 1 for the next 1-ms timer.
						const model = fanfare && clock.pendingModel ? fanfare : drop;
						next.set( entity.gid, {
							groundItem: true,
							modifierId: clock.modifierId,
							gid: entity.gid,
							model: model.glb,
							clip: model.clips.includes( "stand" ) ? "stand" : "",
							time: clock.time,
							loop: model.clipLoop,
							scale: 1,
							pose: {
								regionId: entity.regionId,
								x: entity.x,
								y: entity.y,
								z: entity.z,
								yaw: characterHeadingYaw( entity.heading )
							}
						} );
						displayedDependencies.set( entity.gid, paths );
						if ( model.ambientParticles?.length ) {
							particleHolders.push( {
								actor: next.get( entity.gid )!,
								particles: model.ambientParticles
							} );
						}
						continue;
					}
					// Character-info supplies native height, hit anchors and audio.
					// A retried manifest can leave models resident first; do not
					// publish incomplete bodies to next frame's effect owner.
					if ( published.manifest < published.manifests.length ) continue;
					const resource = resourceFor( entity );
					if ( !resource ) continue;
					if ( !resources.ready( resource.glb ) ) {
						const previous = output.displayed.get( entity.gid ),
							paths = displayedDependencies.get( entity.gid );
						if ( previous && paths && resources.plan( paths ) ) {
							next.set( entity.gid, previous );
							displayedDependencies.set( entity.gid, paths );
						}
						continue;
					}
					// Resolve authored EFP dependencies before starting the holder clock.
					// A cold decoder must not consume and lose a time-zero BAN key.
					let particlesReady = true;
					for ( const path of resource.animationParticlePaths ?? [] ) {
						if ( !resources.ready( path ) ) particlesReady = false;
					}
					if ( !particlesReady ) continue;
					let model = resource.glb;
					let dependencies: readonly string[] = [ resource.glb ];
					let auxiliaryCommit: readonly Auxiliary[] | undefined = [];
					let particleCommit: readonly ModelParticle[] | undefined = resource.ambientParticles ?? [];
					let overrideCommit: readonly number[] | undefined = [];
					let defaultWearCommit: readonly string[] | undefined = [];
					const fallback = () => {
						const previous = output.displayed.get( entity.gid ),
							paths = displayedDependencies.get( entity.gid );
						if ( previous && paths && resources.plan( paths ) ) {
							auxiliaryCommit = undefined;
							overrideCommit = undefined;
							particleCommit = undefined;
							defaultWearCommit = undefined;
							dependencies = paths;
							return previous.model;
						}
						return resource.glb;
					};
					try {
						const disguise = referenceAppearances.get( entity.gid );
						const skin = activeSkin( entity ), avatars = skin ? [] : entity.avatars ?? [];
						if ( skin && !skin.player ) {
							// 85C060: a monster skin is its own body; the wearer's items are set aside.
						} else if ( disguise ) {
							// CICharactor_EquipReferenceAppearance: the random look's items
							// go through the ordinary slot visuals and compound refresh.
							const parts = assembleEquipmentAppearance( {
									resource,
									dress: published.dress,
									equipment: referenceAppearanceItems(
										disguise,
										resource.codename.includes( "_MAN_" ),
										published.itemIds
									),
									avatars: [],
									hwanHair: false,
									mounted: entity.mountedOn !== undefined,
									weaponHidden: false,
									attachmentsHidden: false,
									fortressIndex: -1,
									player: entity.kind === "player" || entity.kind === "local-player",
									ownerless: false,
									committedWear: [],
									freezeWear: nativeServerName !== undefined &&
										defaultWearFrozen( published.dress.defaultWearLanguage ?? 4, nativeServerName )
								} ).parts,
								paths = [ resource.glb, ...parts.map( p => p.model ) ];
							if ( resources.plan( paths ) ) {
								model = `assembly:disguise:${resource.glb}:${JSON.stringify( parts )}`;
								renderer.setCharacterAssembly( model, resource.glb, parts );
								dependencies = paths;
							} else model = fallback();
						} else if (
							(skin || entity.gid === gameplay?.localGid || entity.equipment || avatars.length) &&
							published.manifest >= 3
						) {
							const equipment = wornEquipment( entity, gameplay );
							const weaponHidden = effects.appearance( entity.gid ).weaponHidden;
							const hwanHair = entity.appearanceState?.[2] === 1 &&
								resource.codename.startsWith( "CHAR_CH_" );
							const freezeWear = defaultWearFrozen(
								published.dress.defaultWearLanguage ?? 4,
								nativeServerName
							);
							const player = entity.kind === "player" || entity.kind === "local-player",
								local = localEntity;
							const fortressIndex = player && local ?
								fortressAppearance(
									states.get( entity.gid )?.fortressIndex ?? -1,
									local.arenaTeam ?? 255,
									entity.arenaTeam ?? 255,
									normalFortressClothes,
									gameplay?.fortress,
									gameplay?.social?.guild?.id ?? 0,
									entity.gid === gameplay?.localGid ?
										gameplay?.social?.guild?.id ?? 0 :
										entity.guildId ?? 0,
									gameplay?.social?.alliances?.map( a => a.id ) ?? []
								) :
								-1;
							const visualState = states.get( entity.gid );
							if ( visualState ) visualState.fortressIndex = fortressIndex;
							const signature = fortressIndex + ":" + Number( freezeWear ) + ":" +
								Number( entity.mountedOn !== undefined ) + ":" + Number( hwanHair ) + ":" +
								Number( weaponHidden ) + ":" +
								Number( !!presentationState.idleStates.get( entity.gid )?.attachmentsHidden ) + ":" +
								wornSignature( equipment ) + "|" + avatarSignature( avatars );
							let appearance = appearances.get( entity.gid );
							if (
								!appearance || appearance.resource !== resource ||
								appearance.dress !== published.dress ||
								appearance.items !== published.items || appearance.signature !== signature
							) {
								const committedWear = states.get( entity.gid )?.defaultWear;
								const assembly = assembleEquipmentAppearance( {
									resource,
									dress: published.dress,
									equipment,
									avatars,
									hwanHair,
									mounted: entity.mountedOn !== undefined,
									weaponHidden,
									attachmentsHidden: !!presentationState.idleStates.get( entity.gid )
										?.attachmentsHidden,
									fortressIndex,
									player,
									ownerless: false,
									committedWear: committedWear?.resource === resource ? committedWear.keys : [],
									freezeWear
								} );
								const { parts, auxiliary, defaultWear } = assembly;
								const particles: ModelParticle[] = [
									...(resource.ambientParticles ?? []),
									...assembly.particles
								];
								appearance = {
									resource,
									dress: published.dress,
									items: published.items,
									signature,
									defaultWear,
									particles,
									parts,
									auxiliary,
									avatarIds: assembly.avatarIds,
									model: `assembly:${resource.glb}:${JSON.stringify( parts )}`,
									dependencies: [ resource.glb, ...parts.map( part => part.model ) ]
								};
								appearances.set( entity.gid, appearance );
							}
							const parts = appearance.parts;
							let ready = true;
							for ( const part of parts ) {
								if ( !resources.ready( part.model ) ) {
									ready = false;
								}
							}
							if ( !ready ) {
								model = fallback();
							} else {
								auxiliaryCommit = appearance.auxiliary;
								overrideCommit = appearance.avatarIds;
								particleCommit = appearance.particles;
								defaultWearCommit = appearance.defaultWear;
							}
							if ( ready && parts.length ) {
								model = appearance.model;
								renderer.setCharacterAssembly( model, resource.glb, parts );
								dependencies = appearance.dependencies;
							}
						}
					} catch ( error ) {
						output.failure = String( error );
						model = fallback();
					}
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
					const downDeath = presentationState.idleStates.get( entity.gid )?.downDeath,
						quickDeath = resource.clips.includes( "deathquick" ) ?
							"deathquick" :
							resource.clips.includes( "downdie" ) ?
							"downdie" :
							"death";
					const sitting = entity.movementMode === 4 && !entity.mountedOn;
					const sittingClip = resource.clips.includes( "sit" ) ? "sit" : "charselect-state14";
					if ( state.dead !== undefined && state.dead !== dead ) {
						state.postureClip = dead ? (downDeath ? quickDeath : "death") : undefined;
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
						entity.mountedOn ?
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
						state.rateSample +=
							(state.rateClock === undefined ? 0 : Math.max( 0, seconds - state.rateClock )) *
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
										const metadata =
												published.animationStates.get( resource.codename )?.[candidate] ??
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
					const layers: import("@/engine/contracts/character").CharacterLayer[] = locomotionLayers(
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
						rows: readonly import("@/engine/contracts/character").CharacterLayer[],
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
								definition =
									(resource.animationStates ?? published.animationStates.get( resource.codename ))
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
					const elapsed = new Map( dispatch.map( row => [ row.activation, row.elapsedMs ] ) );
					const sampled = new Map( dispatch.map( row => [ row.activation, row.layer ] ) );
					for ( let i = 0; i < layers.length; i++ ) {
						layers[i] = sampled.get( layers[i]!.activation! ) ?? layers[i]!;
					}
					for ( const layer of layers ) {
						if ( !layer.activation ) throw Error( "Missing animation installation" );
						retainedActivations.add( layer.activation );
						sounds.advance(
							entity.gid,
							layer.clip,
							layer.activation.started,
							(elapsed.get( layer.activation ) ?? 0) / 1000,
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
						height: published.heights.has( resource.codename ) ?
							published.heights.get( resource.codename )! *
							(state.actionHeight ?
								state.actionHeight.from +
								(state.actionHeight.to - state.actionHeight.from) *
									Math.min( 1, Math.max( 0, seconds - state.actionHeight.at ) ) :
								1) :
							undefined,
						mountedOn: entity.mountedOn,
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
				} catch ( error ) {
					output.failure = String( error );
				}
			}
			return { animationHolders, particleHolders };
		}
	};
}
