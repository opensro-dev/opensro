/*
===========================================================================

characters.ts - character assemblies and their presentation lifetime

Owns resource admission, actor assembly, animation and effects for roster
previews and world entities. Readiness belongs to each required assembly;
scenery and auxiliary actors cannot satisfy a player readiness barrier.

===========================================================================
*/

import { partyMembers, partyPortraitGid } from "@/engine/foundation/ui/party-overlay";
import { fortressAppearance } from "@/engine/foundation/animation/fortress-appearance";
import { movementEntryRate, transitionActionStates } from "@/engine/foundation/animation/action-refresh";
import { createStatusOwner } from "@/engine/foundation/animation/status-presentation";
import { defaultWearFrozen, refreshDefaultWear } from "@/engine/foundation/animation/default-wear-policy";
import { validateEquipmentBranches, equipmentSocket } from "@/engine/foundation/animation/equipment-sockets";
import { validateEquipmentParticles } from "@/engine/foundation/animation/equipment-particles";
import { selectAvatarOverride, type AvatarOverrideSelection } from "@/engine/foundation/animation/avatar-override";
import {
	assembleEquipmentAppearance,
	createItemCodenameIndex,
	type DressCatalog,
	type SetEntry,
	wornItemsFromList
} from "@/engine/foundation/animation/equipment-appearance";
import { requestCastCancellation } from "@/engine/foundation/gameplay/cast-results";
import {
	createAnimationEmission,
	modelAnimationParticles,
	type AnimationParticleSet
} from "@/engine/foundation/animation/animation-emission";
import {
	COMBAT_STANCE_SECONDS,
	combatStanceOnCast,
	combatStanceOnHit
} from "@/engine/foundation/animation/combat-stance";
import {
	createModelAnimation,
	modelAnimationBindings,
	modelModifierSets,
	type ModifierSelector,
	type ModelAnimationBinding
} from "@/engine/foundation/animation/model-animation";
import { createEntityLod } from "@/engine/foundation/animation/entity-lod";
import { createAnimationDispatch } from "@/engine/foundation/animation/animation-dispatch";
import { createModifierDelta } from "@/engine/foundation/rendering/modifier-delta";
import { animationActivation, type AnimationActivation } from "@/engine/foundation/animation/animation-activation";
import { createPresentationIds } from "@/engine/foundation/animation/presentation-ids";
import { createModelEmission, modelAmbientParticles } from "@/engine/foundation/animation/model-emission";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import {
	groundVisualClock,
	advanceGroundVisual,
	type GroundVisualClock
} from "@/engine/foundation/animation/ground-visual";
import { advanceBodyShape, bodyVolumeIndex, type BodyShapeBlend } from "@/engine/foundation/animation/body-shape";
import { createSceneryEmission } from "@/engine/foundation/animation/scenery-emission";
import { damageAnchor } from "@/engine/foundation/animation/damage-anchor";
import { decodeTooltipSkills } from "@/engine/foundation/ui/skill-tooltip-catalog";
import {
	createReferenceAppearances,
	referenceAppearanceItems
} from "@/engine/foundation/animation/reference-appearance";
import { hawkResult } from "@/engine/foundation/gameplay/attached-effects";
import { createDamageFeedback } from "./damage-feedback";
import { hiddenSilkCos, blindableCharacter } from "@/engine/foundation/ui/name-visibility";
import { concealmentState, concealmentAlpha, seenAlpha } from "@/engine/foundation/gameplay/concealment";
import { skillLookup, type SkillLookup } from "@/engine/foundation/ui/buff-viewer";
import { createPosePresentation } from "./pose-presentation";
import { createCharacterStateIndex } from "./state-index";
import { createSkillObjects, SKILL_OBJECT_MANIFESTS } from "./skill-objects";
import { monsterScale, monsterMaterialSlot } from "@/engine/foundation/rendering/monster-scale";
import { skillSoundRoots, weaponSoundLabel } from "@/engine/foundation/animation/sound-selectors";
import { emoteRoute, emoteAttachments } from "@/engine/foundation/animation/emote";
import { transitionPosture, postureLayers, type Posture } from "@/engine/foundation/animation/posture";
import { disappearActor, type Disappear } from "@/engine/foundation/animation/disappear";
import { appendDamageText, damageText } from "@/engine/foundation/ui/damage-text";
import { oneShotLayers } from "@/engine/foundation/animation/one-shot-layers";
import { advanceRandomIdle, type RandomIdle } from "@/engine/foundation/animation/random-idle";
import {
	changeLocomotion,
	stopLocomotion,
	locomotionLayers,
	type LocomotionBlend
} from "@/engine/foundation/animation/locomotion-blend";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import { createOrbs } from "./orbs/orbs";
import { advanceCharacterFade, type CharacterFade } from "@/engine/foundation/animation/character-fade";
import { advanceAction, actionLayers, type ActionSchedule } from "@/engine/foundation/animation/action-schedule";
import { skillMotionResolveAnimation } from "@/engine/foundation/animation/skill-motion-resolve";
import { CHARACTER_ACTORS } from "@/engine/foundation/animation/character-budget";
import { createCharacterSelection } from "@/engine/foundation/animation/character-selection";
import {
	animationMetadata,
	weaponAnimationSet,
	type AnimationMetadata
} from "@/engine/foundation/animation/animation-metadata";
import { createCharacterEffects } from "./effects/effects";
import { createCharacterSounds, type SoundRule } from "./sounds/sounds";
import { createCharacterResources } from "./resources/resources";
import { characterHeadingYaw } from "@/engine/foundation/math/angles";
import { radians, previewYaw } from "@/engine/foundation/math/angles";
import { creationLoadout, creationRange } from "@/engine/foundation/ui/character-create";
import { dockSlot } from "@/engine/foundation/rendering/dock-slots";
import { previewIdle } from "@/engine/foundation/animation/preview-idle";
import type { CharacterRecord } from "@/engine/contracts/session";
import { movementGait } from "@/engine/foundation/gameplay/native-movement";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Renderer } from "@/engine/contracts/runtime";
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { CharacterActor } from "@/engine/contracts/character";
/*
================
Resource
================
*/
interface Resource {
	particleModifiers?: unknown;
	animationParticles?: readonly AnimationParticleSet[];
	animationParticlePaths?: readonly string[];
	animationBindings?: unknown;
	modifierSets?: unknown;
	modifierSelectors?: readonly ModifierSelector[];
	modifierBindings?: readonly ModelAnimationBinding[];
	ambientParticles?: readonly ModelParticle[];
	materialKind?: number;
	materialVariants?: Readonly<Record<string, string>>;
	scalePercent?: number;
	eventRain?: boolean;
	soundProfileName?: string;
	animationStates?: Record<string, AnimationMetadata>;
	codename: string;
	cover?: Record<string, number>;
	refObjId: number;
	glb: string;
	clips: readonly string[];
	previewGlb?: string;
	previewClips?: readonly string[];
}
/*
================
CharacterFrameProbe
Optional measurements supplied by the runtime; presentation never discovers globals.
================
*/
export interface CharacterFrameProbe {
	detailBegin( stage: string ): void;
	detailEnd( stage: string ): void;
	sampleDetails(): boolean;
}

const GOLD_DROP_MODELS = [
	"item/etc/drop_ch_money_ing.bsr",
	"item/etc/drop_ch_money_small.bsr",
	"item/etc/drop_ch_money_normal.bsr",
	"item/etc/drop_ch_money_large.bsr"
] as const;

/*
================
createCharacterPresentation
================
*/
/*
================
createCharacterPresentation
================
*/
export function createCharacterPresentation(
	assets: AssetOwner,
	renderer: Renderer,
	origin: string,
	play: ( event: import("@/engine/contracts/audio").SoundEvent ) => void,
	random: PresentationRandom,
	soundSurface: ( pose: import("@/engine/contracts/gameplay").Pose ) => string | undefined = () => undefined,
	health?: {
		impact(
			gid: number,
			key: string,
			impact: import("@/engine/contracts/gameplay").CastImpact,
			now: number,
			source?: "cast" | "hawk"
		): boolean;
		currentResult( gid: number, key: string ): boolean;
		dead( gid: number ): boolean;
		release( key: string, now: number ): void;
		finishedCasts(): void;
	}
) {
	let probe: CharacterFrameProbe | undefined;
	// One id index per published catalogue, as GlobalDataManager keeps it.
	let concealmentCatalog: readonly import("@/engine/foundation/gameplay/skill-catalog").SkillMetadata[] | undefined,
		concealmentLookup: SkillLookup = skillLookup( undefined );
	const concealmentSkills = ( catalog: typeof concealmentCatalog ): SkillLookup => {
		if ( catalog !== concealmentCatalog ) {
			concealmentCatalog = catalog;
			concealmentLookup = skillLookup( catalog );
		}
		return concealmentLookup;
	};
	const entityLod = createEntityLod();
	let animationDelta = createModifierDelta();
	let footprints: import("@/engine/contracts/footprint").Footprint[] = [], footprintSequence = 0;
	/*
================
footContact
================
	*/
	function footContact(
		entity: EntityState,
		actors: readonly CharacterActor[],
		actor: CharacterActor,
		pose: import("@/engine/contracts/gameplay").Pose,
		right: boolean,
		seconds: number
	) {
		if ( (entity.kind !== "player" && entity.kind !== "local-player") || entity.movementMode === 4 ) return;
		const surface = soundSurface( pose );
		if ( surface !== "SAND" && surface !== "SNOW" ) return;
		const socket = renderer.characterSocket( actors, entity.gid, right ? "Bip01 R Toe0" : "Bip01 L Toe0", [
			0,
			0,
			0
		] );
		if ( socket ) {
			footprints.push( {
				id: ++footprintSequence,
				pose: socket,
				yaw: Math.fround( Math.PI - actor.pose.yaw + (right ? -.07853981852531433 : .07853981852531433) ),
				right,
				surface,
				started: seconds
			} );
		}
	}
	/*
================
clearFootprints
================
	*/
	function clearFootprints() {
		if ( footprints.length ) {
			footprints = [];
			renderer.setFootprints( footprints );
		}
		footprintSequence = 0;
	}
	const groundClocks = new Map<number, GroundVisualClock & { duration: number; modifierId: number; }>();
	const referenceAppearances = createReferenceAppearances( () => random.range( 0, 32768 ) );
	const idleStates = new Map<
		number,
		{
			x: number;
			z: number;
			region: number;
			idle: RandomIdle;
			posture?: Posture;
			emoteRevision?: number;
			downDeath?: boolean;
			attachmentsHidden?: boolean;
		}
	>();
	// Presentation-time deadlines survive cast retirement and visibility selection.
	const combatStanceEnds = new Map<number, number>();
	const recoveryByCodename = new Map<string, number>();
	let damageTexts: import("@/engine/contracts/damage-text").DamageText[] = [];
	let environmentalSequence = 0;
	const feedback = createDamageFeedback( ( key, at ) => health?.release( key, at ) );
	const retiring = new Set<number>(),
		disappearing = new Map<
			number,
			Disappear & {
				children?: readonly CharacterActor[];
				particles: readonly ModelParticle[];
				animation?: {
					resource: Resource;
					dispatch: ReturnType<typeof createAnimationDispatch>;
					selection: ReturnType<typeof createModelAnimation>;
				};
			}
		>();
	const allocateActor = createPresentationIds(), lizardGid = allocateActor();
	let previewReady = false, dockReady = false;
	let rainEventActive = false;
	const rainEventEntities = new Map<number, number>();
	const rainEvents:
		({ kind: "spawn" | "state"; gid: number; refObjId: number; } | { kind: "despawn"; gid: number; } | {
			kind: "reset";
		})[] = [];
	const catalog = new Map<number, Resource>(),
		states = new Map<number, {
			modifierId: number;
			feedbackSettled?: boolean;
			modifierResource?: Resource;
			modifierLayers?: readonly import("@/engine/contracts/character").CharacterLayer[];
			activations?: Map<string, AnimationActivation>;
			dispatch?: ReturnType<typeof createAnimationDispatch>;
			modelAnimation?: ReturnType<typeof createModelAnimation>;
			navigationHold?: {
				revision: number;
				pose: import("@/engine/contracts/gameplay").Pose;
				mode: number | undefined;
			};
			actionRevision?: number;
			actionMode?: number;
			actionMask?: number;
			actionInput?: string;
			actionHeight?: { from: number; to: number; at: number; };
			dead?: boolean;
			sitting?: boolean;
			postureClip?: string;
			postureStarted?: number;
			combatIdle?: {
				body: Resource;
				metadata: Record<string, AnimationMetadata> | undefined;
				motion: ReturnType<typeof skillMotionResolveAnimation>;
			};
			clip: string;
			started: number;
			frozenSample?: number;
			rateSample?: number;
			rateClock?: number;
			locomotion?: LocomotionBlend;
			hitToken?: string;
			hitStarted?: number;
			hitCritical?: boolean;
			pickupRevision?: number;
			pickupStarted?: number;
			equipmentParticles?: readonly ModelParticle[];
			defaultWear?: { resource: Resource; keys: readonly string[]; };
			fortressIndex?: number;
		}>();
	let dress: DressCatalog = {},
		itemIds: ReadonlyMap<string, number> = new Map(),
		items: Record<string, {
			codename: string;
			dropModelPath?: string;
			wornModelPath?: string | null;
		}> = {};
	let commonReady = false;
	let dropModels: Record<
		string,
		{
			glb: string;
			clips: readonly string[];
			clipLoop: boolean;
			particleModifiers?: unknown;
			ambientParticles?: readonly ModelParticle[];
		}
	> = {};
	const animationEmission = createAnimationEmission( allocateActor );
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
	const modelEmission = createModelEmission( allocateActor );
	const orbs = createOrbs( play, random, allocateActor ), scenery = createSceneryEmission( allocateActor );
	const statusOwner = createStatusOwner();
	const skillObjects = createSkillObjects( allocateActor );
	const resources = createCharacterResources( assets, renderer, origin ),
		sounds = createCharacterSounds( play, random.range ),
		effects = createCharacterEffects(
			assets,
			origin,
			play,
			random,
			id => {
				const item = items[String( id )];
				return item ? { ...item, model: dropModels[item.dropModelPath ?? ""] } : undefined;
			},
			allocateActor,
			statusOwner
		);
	const animationStates = new Map<string, Record<string, AnimationMetadata>>(),
		soundProfiles = new Map<string, string>();
	let skillSounds = skillSoundRoots( [] );
	const manifests = [
		"/assets/char/roster.json",
		"/assets/npc/manifest.json",
		"/assets/data/missionPresentation.json",
		"/assets/audio/effectsound.json",
		"/assets/anim/manifest.json",
		"/assets/itemdrop/manifest.json",
		"/assets/data/skillData.json"
	];
	const shadowSizes = new Map<number, number>();
	const heights = new Map<string, number>(), heightFactors = new Map<string, number>();
	const bloodEffects = new Map<string, readonly [string | null, string | null]>(),
		riderModes = new Map<string, number>();
	const effectAnchors = new Map<string, NonNullable<CharacterActor["effectAnchor"]>>();
	const posePresentation = createPosePresentation();
	const selection = createCharacterSelection( CHARACTER_ACTORS );
	const stateIndex = createCharacterStateIndex();
	let cameraFade: ({ gid: number; time: number; } & CharacterFade) | null = null;
	let cameraTarget: import("@/engine/contracts/scene").FollowCameraTarget | null = null;
	const actionClocks = new Map<number, ActionSchedule>();
	const deathFinalizes = new Map<number, number>();
	const nativeMotionUrls = new Map<string, ReadonlyMap<string, string>>();
	let warmSkills: readonly number[] | undefined, warmBody: string | undefined;
	let warmMotions: { role: string; url: string; }[] = [];
	// A mask's skin (msch 1) replaces the model outright; an msch 3 disguise
	// keeps the body and redresses it.
	/*
================
transformSkinRef
================
	*/
	function transformSkinRef( entity: EntityState ) {
		return referenceAppearances.skin( entity.gid, entity.transformSkin );
	}
	/*
================
appearanceRef
================
	*/
	function appearanceRef( entity: EntityState ) {
		return transformSkinRef( entity ) ?? referenceAppearances.get( entity.gid )?.model ?? entity.refObjId;
	}
	// The skin in force: a Duplicate (player skin) wears the copied player's
	// items; a mask wears nothing of the player's (85C060).
	/*
================
activeSkin
================
	*/
	function activeSkin( entity: EntityState ) {
		return transformSkinRef( entity ) !== undefined ? entity.transformSkin : undefined;
	}
	/*
================
wornEquipment
================
	*/
	function wornEquipment(
		entity: EntityState,
		gameplay: GameplayState | null
	): readonly {
		readonly slot: number;
		readonly refObjId: number;
		readonly typeFlags: number;
		readonly plus: number;
	}[] {
		const skin = activeSkin( entity );
		if ( skin ) return skin.equipment;
		return entity.gid === gameplay?.localGid ? gameplay.inventory : entity.equipment ?? [];
	}
	/*
================
resourceFor
================
	*/
	function resourceFor( entity: EntityState ): Resource | undefined {
		const resource = catalog.get( appearanceRef( entity ) );
		const variant = resource && entity.kind === "monster" ?
			resource.materialVariants
				?.[String( monsterMaterialSlot( entity.rarity ?? 0, entity.tidWord ?? 0, resource.materialKind ) )] :
			undefined;
		return resource && variant ? { ...resource, glb: variant } : resource;
	}
	const previewWear = new Map<number, { resource: Resource; keys: readonly string[]; }>();
	const dockStates = new Map<
		number,
		{ deleted: boolean; started: number; transition: string | null; previous?: CharacterActor; }
	>();
	let lizardStarted: number | null = null;
	let previewShape: BodyShapeBlend | null = null;
	let previewDisplay: { actor: CharacterActor; paths: string[]; } | null = null;
	const displayedDependencies = new Map<number, readonly string[]>();
	// Appearance topology is independent of pose time. Revalidate resource
	// readiness each frame, but derive garment/cosmetic parts only on change.
	const hwanHairActors = new Map<number, { gid: number; started: number; }>();
	/*
================
Auxiliary
================
	*/
	type Auxiliary = { id: number; entry: SetEntry & { bone: string; clips: readonly string[]; }; };
	const avatarOverrides = new Map<number, AvatarOverrideSelection>();
	const committedAuxiliary = new Map<number, readonly Auxiliary[]>();
	const auxiliaryActors = new Map<
		number,
		Map<number, { gid: number; model: string; motion: ReturnType<typeof changeLocomotion>; }>
	>();
	const appearances = new Map<
		number,
		{
			resource: Resource;
			dress: typeof dress;
			items: typeof items;
			signature: string;
			defaultWear: readonly string[];
			particles: readonly ModelParticle[];
			parts: import("@/engine/contracts/character").CharacterAttachment[];
			auxiliary: readonly Auxiliary[];
			avatarIds: readonly number[];
			model: string;
			dependencies: readonly string[];
		}
	>();
	let manifest = 0, failure: string | null = null, displayed = new Map<number, CharacterActor>();
	let hideSilkCos = false;
	return {
		/*
================
options
================
		*/
		options( value: import("@/engine/foundation/gameplay/game-options").GameOptions ) {
			hideSilkCos = value.hideSilkCos;
		},
		// 856480 sets a global bit on admission; 85FA80 clears it on removal.
		// It is not a count: a surviving actor cannot re-enable a cleared bit.
		/*
================
receiveLifecycle
================
		*/
		receiveLifecycle( events: readonly import("@/engine/contracts/world").WorldEvent[] ) {
			entityLod.receive( events );
			const next: typeof rainEvents = [];
			for ( const event of events ) {
				if ( event.kind === "reset" ) {
					combatStanceEnds.clear();
					modelEmission.reset();
					animationEmission.reset();
					stageAnimations.clear();
					groundClocks.clear();
					retiring.clear();
					disappearing.clear();
					next.length = 0;
					next.push( { kind: "reset" } );
				} else if ( event.kind === "spawn" || event.kind === "state" ) {
					if ( event.kind === "spawn" ) combatStanceEnds.delete( event.entity.gid );
					next.push( { kind: event.kind, gid: event.entity.gid, refObjId: event.entity.refObjId } );
				} else if ( event.kind === "despawn" ) {
					combatStanceEnds.delete( event.gid );
					retiring.add( event.gid );
					next.push( { kind: "despawn", gid: event.gid } );
				}
			}
			if ( next[0]?.kind === "reset" ) rainEvents.length = 0;
			if ( rainEvents.length + next.length > 65536 ) throw Error( "Weather lifecycle journal overflow" );
			for ( const event of next ) rainEvents.push( event );
		},
		/*
================
eventRain
================
		*/
		eventRain() {
			// Preserve delivery order while the character catalogs load.
			if ( manifest < 2 ) return rainEventActive;
			for ( const event of rainEvents ) {
				if ( event.kind === "reset" ) {
					combatStanceEnds.clear();
					modelEmission.reset();
					animationEmission.reset();
					stageAnimations.clear();
					groundClocks.clear();
					retiring.clear();
					disappearing.clear();
					damageTexts = [];
					rainEventActive = false;
					rainEventEntities.clear();
				} else if ( event.kind === "despawn" ) {
					if ( catalog.get( rainEventEntities.get( event.gid ) ?? 0 )?.eventRain ) rainEventActive = false;
					rainEventEntities.delete( event.gid );
				} else {
					if (
						rainEventEntities.get( event.gid ) !== event.refObjId &&
						catalog.get( event.refObjId )?.eventRain
					) rainEventActive = true;
					rainEventEntities.set( event.gid, event.refObjId );
				}
			}
			rainEvents.length = 0;
			return rainEventActive;
		},
		/*
================
receiveFeedback
================
		*/
		receiveFeedback(
			events: readonly import("@/engine/contracts/orb").VisualFeedback[],
			entities: readonly EntityState[]
		) {
			for ( const event of events ) {
				if ( event.kind === "item-effect" ) effects.item( event );
				else if ( event.kind === "level-up" ) {
					const entity = entities.find( e => e.gid === event.gid );
					if ( entity ) effects.system( event.gid, entity.kind === "cos" ? -2147483614 : -2147483642 );
				} else orbs.receive( [ event ], entities );
			}
		},
		/*
================
step
================
		*/
		step(
			entities: readonly EntityState[],
			gameplay: GameplayState | null,
			seconds: number,
			simulationMs?: number,
			cameraPitch = Math.PI / 18,
			dock?: readonly CharacterRecord[],
			preview?: import("@/engine/contracts/frontend").CreationSnapshot | null,
			lizard = false,
			effectDetail = 2,
			bloodEnabled = true,
			blindHeld = false,
			nativeServerName?: string,
			normalFortressClothes = false
		) {
			entityLod.step(
				entities,
				gameplay?.localGid,
				renderer.presentationCamera?.() ?? null,
				Math.trunc( seconds * 1000 )
			);
			const animationDeltaMs = animationDelta( seconds );
			skillObjects.retain( entities );
			resources.begin( seconds );
			failure = null;
			const result = resources.poll();
			const skillObjectResult = result && SKILL_OBJECT_MANIFESTS.some( path => path === result.path );
			if ( result && skillObjectResult ) {
				try {
					skillObjects.catalog(
						result.path,
						JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) )
					);
					resources.accepted( result.path );
				} catch ( error ) {
					resources.rejected( result.path, error );
				}
			}
			if ( result && !skillObjectResult ) {
				try {
					const decoded = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) );
					const value = decoded as {
						effectAppearanceStores?: number[][];
						skillAudioRows?: string[];
						recoveryByCodename?: Record<string, number>;
						models?: Resource[] | Record<string, Resource>;
						dress?: typeof dress;
						itemsByRefObjId?: typeof items;
						rules?: SoundRule[];
						characterShadowSizes?: readonly (readonly [number, number])[];
						characterActionEffectRows?: {
							codename: string;
							soundProfileName: string;
							bloodEffects?: readonly [string | null, string | null];
							riderTransformMode?: number;
							heightFactor?: number;
							anchorSocketName?: string | null;
							anchorOffset?: { x: number; y: number; z: number; };
						}[];
					};
					if ( value.characterShadowSizes ) {
						for ( const [id, size] of value.characterShadowSizes ) {
							if (
								!Number.isSafeInteger( id ) || id <= 0 || !Number.isInteger( size ) || size < 0
							) throw Error( "Invalid character shadow reference size" );
							shadowSizes.set( id, size );
						}
					}
					const nextHeights = new Map( heights ),
						nextHeightFactors = new Map( heightFactors ),
						nextEffectAnchors = new Map( effectAnchors );
					const nextSkillSounds = value.skillAudioRows ?
						skillSoundRoots( value.skillAudioRows ) :
						skillSounds;
					const nextCatalog = new Map( catalog ),
						nextAnimations = new Map( animationStates ),
						nextProfiles = new Map( soundProfiles ),
						nextMotionUrls = new Map( nativeMotionUrls );
					const rows = Object.values( value.models ?? {} ).filter( row => row.refObjId !== undefined );
					for ( const row of rows ) {
						if (
							(row.scalePercent !== undefined &&
								(!Number.isFinite( row.scalePercent ) || row.scalePercent <= 0)) ||
							(row.eventRain !== undefined && typeof row.eventRain !== "boolean") ||
							!Number.isInteger( row.refObjId ) || typeof row.codename !== "string" ||
							typeof row.glb !== "string" || !Array.isArray( row.clips ) || row.clips.some( clip =>
								typeof clip !== "string"
							)
						) {
							throw new Error( "Invalid character manifest" );
						}
					}
					for ( const row of rows ) {
						if (
							row.materialKind !== undefined &&
							(!Number.isInteger( row.materialKind ) || row.materialKind < 0 || row.materialKind > 255)
						) throw Error( "Invalid monster material kind" );
						if (
							row.materialVariants !== undefined &&
							(typeof row.materialVariants !== "object" || row.materialVariants === null ||
								Array.isArray( row.materialVariants ) ||
								Object.entries( row.materialVariants ).some( ( [slot, path] ) =>
									!/^([1-4])$/.test( slot ) || typeof path !== "string" ||
									!path.startsWith( "/assets/npc/" ) || path.includes( ".." ) ||
									!path.endsWith( ".glb" )
								))
						) throw Error( "Invalid monster material variants" );
						nextCatalog.set( row.refObjId, {
							...row,
							ambientParticles: modelAmbientParticles( row.particleModifiers ),
							animationParticles: modelAnimationParticles( row.particleModifiers ),
							animationParticlePaths: [
								...new Set(
									modelAnimationParticles( row.particleModifiers ).flatMap( s =>
										s.particles.map( p =>
											"/assets/effects/programs.json#" + encodeURIComponent( p.effectPath )
										)
									)
								)
							],
							modifierBindings: modelAnimationBindings( row.animationBindings ),
							modifierSelectors: modelModifierSets( row.modifierSets ),
							animationStates: row.animationStates ? animationMetadata( row.animationStates ) : undefined
						} );
					}

					if ( result.path === "/assets/anim/manifest.json" ) {
						for (
							const [name, entry] of Object.entries(
								(decoded as {
									models: Record<string, Record<string, unknown>>;
								}).models ?? {}
							)
						) {
							const { soundProfileName, animationSets, ...clips } = entry;
							const metadata = animationMetadata( clips );
							if ( metadata.hit && !metadata.hit1 ) metadata.hit1 = metadata.hit;
							if (
								animationSets && typeof animationSets === "object" && !Array.isArray( animationSets )
							) {
								for ( const [set, states] of Object.entries( animationSets ) ) {
									if (
										!states || typeof states !== "object" || Array.isArray( states )
									) throw new Error( "Invalid animation set" );
									const rows = states as Record<string, unknown>;
									const urls = new Map( nextMotionUrls.get( name ) );
									for ( const [state, value] of Object.entries( rows ) ) {
										const role = `native:${set}:${state}`, url = (value as { url?: unknown; }).url;
										if (
											!/^\d+$/.test( state ) || typeof url !== "string" ||
											!url.startsWith( "/assets/anim/" ) || !url.endsWith( ".ban" ) ||
											url.includes( ".." )
										) throw Error( "Invalid native animation publication" );
										Object.assign( metadata, animationMetadata( { [role]: value } ) );
										urls.set( role, url );
									}
									nextMotionUrls.set( name, urls );
									for (
										const [role, id] of Object.entries( {
											attack1: 2,
											attack2: 5,
											attack3: 16,
											attack4: 17
										} )
									) {
										if ( rows[String( id )] ) {
											Object.assign(
												metadata,
												animationMetadata( {
													[`${role}-${set.replaceAll( "_", "-" )}`]: rows[String( id )]
												} )
											);
										}
									}
								}
							}
							nextAnimations.set( name, metadata );
							if ( typeof soundProfileName === "string" ) {
								nextProfiles.set( name, soundProfileName );
							}
						}
					}
					for ( const row of value.characterActionEffectRows ?? [] ) {
						if ( !row || typeof row.codename !== "string" || typeof row.soundProfileName !== "string" ) {
							throw new Error( "Invalid sound profile" );
						}
						nextProfiles.set( row.codename, row.soundProfileName );
						if ( row.bloodEffects ) {
							if (
								row.bloodEffects.length !== 2 ||
								row.bloodEffects.some( p =>
									p !== null && (typeof p !== "string" || !p.endsWith( ".efp" ) || p.includes( ".." ))
								)
							) throw Error( "Invalid character blood resources" );
							bloodEffects.set( row.codename, row.bloodEffects );
						}
						if ( row.riderTransformMode !== undefined ) {
							riderModes.set( row.codename, row.riderTransformMode );
						}
						if ( row.anchorOffset ) {
							const a = row.anchorOffset;
							if ( ![ a.x, a.y, a.z ].every( Number.isFinite ) ) {
								throw Error( "Invalid action effect anchor" );
							}
							nextEffectAnchors.set( row.codename, {
								bone: row.anchorSocketName ?? null,
								offset: [ a.x, a.y, a.z ]
							} );
						}
						if ( row.heightFactor !== undefined ) {
							if ( !Number.isFinite( row.heightFactor ) ) throw new Error( "Invalid character height" );
							nextHeights.set( row.codename, Math.fround( Math.fround( row.heightFactor ) * 20 ) );
							nextHeightFactors.set( row.codename, Math.fround( row.heightFactor ) );
						}
					}
					if ( value.dress ) {
						validateEquipmentParticles( value.dress.specialGlows );
						if (
							value.dress.defaultWearLanguage !== undefined &&
							(!Number.isInteger( value.dress.defaultWearLanguage ) ||
								value.dress.defaultWearLanguage < 0 || value.dress.defaultWearLanguage > 5)
						) throw Error( "Invalid native clothing language" );
						for ( const [id, row] of Object.entries( value.dress.avatarVisualOverrides ?? {} ) ) {
							if (
								!/^\d+$/.test( id ) || !row || typeof row.animation !== "string" ||
								!Number.isInteger( row.priority ) || row.priority < 0 || row.priority > 255 ||
								typeof row.additionalBsr !== "string" ||
								row.additionalBsr !== "" &&
									(!row.additionalBsr.startsWith( "res/" ) || !row.additionalBsr.endsWith( ".bsr" ) ||
										row.additionalBsr.includes( ".." ))
							) throw Error( "Invalid avatar visual override" );
						}
						const equipmentEntries: Record<string, SetEntry> = {};
						for ( const [id, row] of Object.entries( value.dress.equipment ?? {} ) ) {
							if (
								!row || !/^\d+$/.test( id ) ||
								row.slot !== null && (!Number.isInteger( row.slot ) || row.slot < 0 || row.slot > 8) ||
								!row.bodies || typeof row.bodies !== "object"
							) throw Error( "Invalid native equipment reference" );
							for ( const [body, entry] of Object.entries( row.bodies ) ) {
								if ( entry !== null ) equipmentEntries[id + body] = entry;
							}
						}
						for ( const entry of Object.values( value.dress.avatarAuxiliary ?? {} ) ) {
							if (
								!entry || typeof entry.bone !== "string" || !entry.bone ||
								!Array.isArray( entry.clips ) || !entry.clips.includes( "stand" ) ||
								entry.clips.some( clip => typeof clip !== "string" )
							) throw Error( "Invalid auxiliary avatar" );
						}
						for (
							const entries of [
								value.dress.hwan ?? {},
								value.dress.defaultWear ?? {},
								value.dress.fortressWear ?? {},
								value.dress.avatarAuxiliary ?? {},
								equipmentEntries
							]
						) {
							if ( !entries || typeof entries !== "object" || Array.isArray( entries ) ) {
								throw new Error( "Invalid equipment catalog" );
							}
							for ( const entry of Object.values( entries ) ) {
								if (
									!entry || typeof entry.glb !== "string" || !entry.glb.startsWith( "/assets/" ) ||
									entry.glb.includes( ".." ) || !Array.isArray( entry.parts ) ||
									entry.parts.some( part => typeof part !== "string" )
								) throw new Error( "Invalid equipment entry" );
								validateEquipmentBranches( entry.branches );
								if ( entry.covers ) {
									for ( const indices of Object.values( entry.covers ) ) {
										if (
											!Array.isArray( indices ) ||
											indices.some( index => !Number.isInteger( index ) || index < 0 )
										) throw new Error( "Invalid equipment coverage" );
									}
								}
							}
						}
					}
					if ( value.itemsByRefObjId ) {
						for ( const entry of Object.values( value.itemsByRefObjId ) ) {
							if ( !entry || typeof entry.codename !== "string" ) {
								throw new Error( "Invalid item presentation" );
							}
						}
					}
					let nextDrops = dropModels;
					if ( result.path === "/assets/itemdrop/manifest.json" ) {
						if (
							decoded.format !== "sro-mission-itemdrop-models" || !decoded.models ||
							typeof decoded.models !== "object" || Array.isArray( decoded.models )
						) throw new Error( "Invalid drop model catalog" );
						for (
							const row of Object.values( decoded.models ) as {
								glb: string;
								clips: string[];
								clipLoop: boolean;
							}[]
						) {
							if (
								!row || typeof row.glb !== "string" || !row.glb.startsWith( "/assets/itemdrop/" ) ||
								row.glb.includes( ".." ) || !Array.isArray( row.clips ) || row.clips.some( clip =>
									typeof clip !== "string"
								) || typeof row.clipLoop !== "boolean"
							) throw new Error( "Invalid drop model entry" );
						}
						nextDrops = Object.fromEntries(
							Object.entries( decoded.models as typeof dropModels ).map( (
								[key, row]
							) => [ key, { ...row, ambientParticles: modelAmbientParticles( row.particleModifiers ) } ] )
						);
					}
					// No live state changes until every projection has validated.
					if ( value.recoveryByCodename ) {
						for ( const [name, period] of Object.entries( value.recoveryByCodename ) ) {
							if (
								!name || !Number.isInteger( period ) || period < 0 || period > 0x7fffffff - 500
							) {
								throw Error( "Invalid native recovery duration" );
							}
						}
					}
					if ( value.recoveryByCodename ) {
						recoveryByCodename.clear();
						for ( const [name, period] of Object.entries( value.recoveryByCodename ) ) {
							recoveryByCodename.set( name, period );
						}
					}
					if ( value.rules ) sounds.catalog( value.rules );
					if ( value.effectAppearanceStores ) {
						const pools = value.effectAppearanceStores;
						if (
							pools.length !== 2 ||
							pools.some( p => !Array.isArray( p ) || p.some( id => !Number.isInteger( id ) || id <= 0 ) )
						) throw Error( "Invalid native appearance stores" );
						const refs = new Map<number, { type: number; cap: number; }>();
						for ( const row of decodeTooltipSkills( value ).values() ) {
							const block = row.directTooltipParams.nativeParamBlocks.slice().reverse().find( b =>
								b.offset === 0x268
							);
							if ( block ) refs.set( row.id, { type: block.values[0]!, cap: block.values[1]! } );
						}
						referenceAppearances.setReferences( refs, pools );
					}
					skillSounds = nextSkillSounds;
					effectAnchors.clear();
					for ( const [key, anchor] of nextEffectAnchors ) effectAnchors.set( key, anchor );
					heights.clear();
					for ( const [key, height] of nextHeights ) heights.set( key, height );
					heightFactors.clear();
					for ( const [key, factor] of nextHeightFactors ) heightFactors.set( key, factor );
					catalog.clear();
					for ( const [key, row] of nextCatalog ) catalog.set( key, row );
					animationStates.clear();
					for ( const [key, row] of nextAnimations ) animationStates.set( key, row );
					nativeMotionUrls.clear();
					for ( const [key, row] of nextMotionUrls ) nativeMotionUrls.set( key, row );
					soundProfiles.clear();
					for ( const [key, row] of nextProfiles ) soundProfiles.set( key, row );
					if ( value.dress ) {
						dress = value.dress;
						itemIds = createItemCodenameIndex( dress );
					}
					if ( value.itemsByRefObjId ) items = value.itemsByRefObjId;
					dropModels = nextDrops;
					manifest++;
					resources.accepted( result.path );
				} catch ( error ) {
					resources.rejected( result.path, error );
				}
			}
			if ( (entities.length || dock?.length || preview) && manifest < (dock || preview ? 1 : manifests.length) ) {
				resources.manifest( manifests[manifest]! );
			}
			if ( manifest === manifests.length ) {
				const path = skillObjects.nextManifest( entities );
				if ( path ) resources.manifest( path );
			}
			previewReady = false;
			dockReady = false;
			if ( !preview ) {
				previewDisplay = null;
				previewShape = null;
			}
			if ( dock || preview ) {
				if ( previewDisplay ) resources.plan( previewDisplay.paths );
				const actors: CharacterActor[] = [];
				// Dock and creation actors are dressed through the item catalog
				// (roster.json, manifest 0); none exists before it is resident.
				// The gecko needs no catalog and is admitted meanwhile.
				const catalogResident = manifest > 0;
				const rows = !catalogResident ? [] : preview ?
					[ {
						id: 0,
						name: preview.selection.name,
						deletePending: false,
						visualLoadout: creationLoadout( preview.selection, itemIds )
					} ] :
					dock!.slice( 0, 4 );
				for ( const [index, row] of rows.entries() ) {
					try {
						const resource = [ ...catalog.values() ].find( model =>
							model.codename === row.visualLoadout.modelCodename
						);
						if ( !resource ) continue;
						// SCharacterInfo_BuildDisplayActor: the dock is dressed from the row's
						// items through the same slot visuals as the world. Previews are ownerless.
						const oldWear = previewWear.get( row.id ),
							freeze = nativeServerName !== undefined &&
								defaultWearFrozen( dress.defaultWearLanguage ?? 4, nativeServerName );
						const assembly = assembleEquipmentAppearance( {
							resource,
							dress,
							equipment: wornItemsFromList( row.visualLoadout.items, dress ),
							avatars: row.visualLoadout.avatars,
							hwanHair: false,
							mounted: false,
							weaponHidden: false,
							attachmentsHidden: false,
							fortressIndex: -1,
							player: true,
							ownerless: true,
							committedWear: oldWear?.resource === resource ? oldWear.keys : [],
							freezeWear: freeze
						} );
						const parts = assembly.parts, wear = assembly.defaultWear;
						if ( !resource.previewGlb || !resource.previewClips ) {
							throw Error( "Missing native dock preview " + resource.codename );
						}
						const paths = [ resource.previewGlb, ...parts.map( part => part.model ) ];
						// Admit unknown parts incrementally: reserving the maximum
						// for an entire equipped actor at once can never fit.
						if ( !paths.map( path => resources.ready( path ) ).every( Boolean ) ) continue;
						const { heightScale, volumeScale, ...assemblyLoadout } = row.visualLoadout;
						previewWear.set( row.id, { resource, keys: wear } );
						const model = (preview ? "creation:" : "dock:") + row.id + ":" +
							JSON.stringify( assemblyLoadout ) + (wear.length ? ":" + JSON.stringify( wear ) : "");
						renderer.setCharacterAssembly( model, resource.previewGlb, parts );
						let clip = previewIdle(
								resource.previewClips,
								row.visualLoadout.animationSetName,
								row.deletePending
							),
							time = seconds,
							loop = true;
						if ( !preview ) {
							let state = dockStates.get( row.id );
							if ( !state ) {
								state = { deleted: row.deletePending, started: seconds, transition: null };
								dockStates.set( row.id, state );
							} else if ( state.deleted !== row.deletePending ) {
								state = {
									deleted: row.deletePending,
									started: seconds,
									transition: row.deletePending ? "charselect-state13" : "charselect-state15",
									previous: displayed.get( row.id )
								};
								dockStates.set( row.id, state );
							}
							if ( state.transition ) {
								const duration = resources.duration( resource.previewGlb, state.transition );
								if ( duration <= 0 ) {
									throw Error( "Missing native deletion transition " + state.transition );
								}
								if ( seconds - state.started < duration ) {
									clip = state.transition;
									time = seconds - state.started;
									loop = false;
								} else state.transition = null;
							}
						}
						const transition = dockStates.get( row.id ),
							elapsed = transition ? Math.max( 0, seconds - transition.started ) : 1;
						const volume = preview ?
							preview.selection.volume :
							bodyVolumeIndex( dock![index]!.bodyShapeByte, dock![index]!.volumeIndex );
						if ( preview ) {
							previewShape = advanceBodyShape(
								previewShape,
								resource.codename,
								row.visualLoadout.heightScale,
								volume,
								seconds
							);
						}
						const opacity = preview ?
							1 :
							transition?.previous ?
							(transition.previous.opacity ?? 1) +
							((row.deletePending ? .8 : 1) - (transition.previous.opacity ?? 1)) *
								Math.min( 1, elapsed ) :
							row.deletePending ?
							.8 :
							1;
						actors.push( {
							gid: row.id,
							model,
							opacity,
							layers: transition?.previous && elapsed < .1 ?
								[ {
									clip: transition.previous.clip,
									time: transition.previous.time + elapsed,
									loop: transition.previous.loop,
									weight: 1 - elapsed / .1,
									lane: "event"
								}, { clip, time, loop, weight: 1, lane: "timed" } ] :
								undefined,
							pose: preview ?
								{ regionId: 0, x: 3, y: .5, z: 0, yaw: previewYaw( preview.yaw ) } :
								dockSlot( index, dock!.length ),
							clip,
							time,
							loop,
							scale: preview ? previewShape!.height : row.visualLoadout.heightScale,
							bodyVolume: {
								index: preview ? previewShape!.volume : volume,
								female: resource.codename.includes( "_WOMAN_" )
							}
						} );
						if ( preview ) previewDisplay = { actor: actors.at( -1 )!, paths };
					} catch ( error ) {
						failure = String( error );
					}
				}
				for ( const id of previewWear.keys() ) {
					if ( !rows.some( row => row.id === id ) ) previewWear.delete( id );
				}
				for ( const id of dockStates.keys() ) {
					if ( preview || !rows.some( row => row.id === id ) ) dockStates.delete( id );
				}
				// Count only complete roster assemblies before auxiliary actors enter.
				dockReady = catalogResident && !preview && actors.length === rows.length;
				if ( lizard && !preview ) {
					const path = "/assets/character-select/interface_lizard.glb";
					if ( resources.ready( path ) ) {
						if ( lizardStarted === null ) lizardStarted = seconds;
						const elapsed = seconds - lizardStarted, duration = resources.duration( path, "move" );
						// 73a207: PlayAnimation(1,0,200,0,1,1); the animation set
						// has no enter fade and a 200 ms EXIT fade. Root motion stays in the skeleton.
						actors.push( {
							gid: lizardGid,
							model: path,
							pose: {
								regionId: 0x6951,
								x: 155.600006,
								y: -20,
								z: 651.599976,
								yaw: radians( Math.PI - 3 )
							},
							clip: elapsed < duration + .2 ? "move" : "stand",
							time: elapsed,
							loop: elapsed >= duration + .2,
							scale: 1,
							layers: oneShotLayers( "move", "stand", elapsed, duration, .2 )
						} );
					}
				} else lizardStarted = null;
				if ( preview && catalogResident ) {
					// Customization admits the whole selectable wardrobe. Waiting
					// for only the initial outfit makes the first equipment click
					// a network operation after the screen has already been revealed.
					const paths = new Set<string>();
					for ( const gender of [ 0, 1 ] as const ) {
						const selection = { ...preview.selection, gender };
						const [firstFigure, lastFigure] = creationRange( selection, "figure" );
						for ( let figure = firstFigure; figure <= lastFigure; figure++ ) {
							const codename = creationLoadout( { ...selection, figure }, itemIds ).modelCodename;
							const model = [ ...catalog.values() ].find( row => row.codename === codename );
							if ( model?.previewGlb ) paths.add( model.previewGlb );
						}
						const [firstWeapon, lastWeapon] = creationRange( selection, "weapon" );
						for ( let weapon = firstWeapon; weapon <= lastWeapon; weapon++ ) {
							const equipped = { ...selection, weapon };
							const [firstProtector, lastProtector] = creationRange( equipped, "protector" );
							for ( let protector = firstProtector; protector <= lastProtector; protector++ ) {
								const loadout = creationLoadout( { ...equipped, protector }, itemIds );
								const body = (selection.race === 0 ? "EU" : "CH") + "_" + (gender === 0 ? "M" : "W");
								for ( const item of loadout.items ) {
									const entry = dress.equipment?.[String( item.refObjId )]?.bodies[body];
									if ( entry ) paths.add( entry.glb );
								}
							}
						}
						const prefix = (selection.race === 0 ? "EU" : "CH") + "_" + (gender === 0 ? "M" : "W") + "_";
						for ( const [key, entry] of Object.entries( dress.defaultWear ?? {} ) ) {
							if ( key.startsWith( prefix ) ) paths.add( entry.glb );
						}
					}
					previewReady = actors.length === 1;
					for ( const path of paths ) {
						// Do not short-circuit: ready owns bounded incremental admission.
						const ready = resources.ready( path );
						previewReady = ready && previewReady;
					}
				}
				if ( preview && !actors.length && previewDisplay ) {
					actors.push( {
						...previewDisplay.actor,
						pose: { ...previewDisplay.actor.pose, yaw: previewYaw( preview.yaw ) },
						time: seconds
					} );
				}
				displayed = new Map( actors.map( actor => [ actor.gid, actor ] ) );
				actors.push(
					...scenery.step(
						renderer.scenery?.() ?? null,
						seconds,
						resources.ready,
						CHARACTER_ACTORS - actors.length
					)
				);
				cameraTarget = null;
				renderer.setCharacterActors( actors );
				resources.retainWanted( actors.map( actor => actor.model ) );
				return;
			}
			for ( const gid of retiring ) {
				const actor = displayed.get( gid );
				if ( actor ) {
					const id = allocateActor(), state = states.get( gid );
					const animation = state?.modifierResource && state.dispatch && state.modelAnimation ?
						{
							resource: state.modifierResource,
							dispatch: state.dispatch,
							selection: state.modelAnimation
						} :
						undefined;
					disappearing.set( id, {
						actor: {
							...actor,
							layers: state?.modifierLayers ?? actor.layers,
							gid: id,
							mountedOn: undefined,
							attachment: undefined,
							pickable: false
						},
						started: seconds,
						particles: modelEmission.transfer( gid, id ),
						animation,
						children: [ ...(auxiliaryActors.get( gid )?.values() ?? []) ].flatMap( child => {
							const actor = displayed.get( child.gid );
							return actor ? [ actor ] : [];
						} ).map( child => ({ ...child, attachment: { ...child.attachment!, gid: id } }) )
					} );
				}
			}
			for ( const gid of retiring ) {
				avatarOverrides.delete( gid );
				committedAuxiliary.delete( gid );
				auxiliaryActors.delete( gid );
			}
			retiring.clear();
			for ( const [id, row] of disappearing ) if ( seconds - row.started >= 1.5 ) disappearing.delete( id );
			// Choose presentation work before requesting assets. Keep the local player
			// and its mount, then nearest entities with a stable identity tie-break.
			const sampleActorDetails = probe?.sampleDetails();
			probe?.detailBegin( "presentation-selection" );
			const anchor = gameplay?.pose;
			const local = entities.find( entity => entity.gid === gameplay?.localGid );
			const priority = ( entity: EntityState ) =>
				entity.gid === local?.gid ? 0 : entity.gid === local?.mountedOn ? 1 : 2;
			const selected = selection.select( entities, anchor ?? undefined, local?.gid, local?.mountedOn );
			// Resident actor assemblies own their dependencies before transient
			// effects compete for the frame's resource budget. A cold hit effect
			// must never evict the fighter or strip its already admitted clothing.
			for ( const entity of selected ) {
				const paths = displayedDependencies.get( entity.gid );
				if ( paths ) resources.plan( paths );
				else if ( priority( entity ) < 2 ) {
					const resource = catalog.get( appearanceRef( entity ) );
					if ( resource ) resources.plan( [ resource.glb ] );
				}
			}
			// Baseline drops own residency before one-shot combat effects compete.
			commonReady = manifest === manifests.length && !!local && displayed.has( local.gid );
			if ( gameplay?.localGid && commonReady ) {
				for ( const key of GOLD_DROP_MODELS ) {
					const model = dropModels[key];
					if ( model && !resources.ready( model.glb ) ) commonReady = false;
				}
			}
			probe?.detailEnd( "presentation-selection" );
			probe?.detailBegin( "presentation-events" );
			const { castByActor, castTokens, vitalsByGid, entitiesByGid } = stateIndex.update( entities, gameplay );
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
			// Warm learned motions through the character resource owner. BANs
			// stay shared by body/role; no per-cast mesh or texture rebuild.
			if ( local ) {
				const body = resourceFor( local ), urls = body && nativeMotionUrls.get( body.codename );
				if ( body && urls && effects.loaded() ) {
					if ( warmSkills !== gameplay?.skills || warmBody !== body.glb ) {
						warmSkills = gameplay?.skills;
						warmBody = body.glb;
						const roles = new Set(
							(warmSkills ?? []).flatMap( skill => (effects.phases( skill ) ?? []).flat() )
						);
						warmMotions = [ ...roles ].flatMap( role => {
							const url = urls.get( role );
							return url ? [ { role, url } ] : [];
						} );
					}
					if ( warmMotions.length ) {
						warmMotions = warmMotions.filter( ( { role, url } ) =>
							!resources.animation( body.glb, role, url )
						);
					}
				}
			}
			const triggers: import("@/engine/contracts/effects").EffectTrigger[] = [];
			for ( const token of actionClocks.keys() ) if ( !castTokens.has( token ) ) actionClocks.delete( token );
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
					skill: skillSounds.get( skill )?.[player ? 1 : 0],
					critical,
					berserk: entity.appearanceState?.[2] === 1
				};
			}
			for ( const cast of gameplay?.casts ?? [] ) {
				if ( cast.resultOnly ) continue;
				const entity = entitiesByGid.get( cast.caster ), resource = entity ? resourceFor( entity ) : undefined;
				if ( !entity || !resource ) continue;
				let clock = actionClocks.get( cast.token );
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
									catalogStates: animationStates.get( resource.codename ),
									motionUrls: nativeMotionUrls.get( resource.codename )
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
							const definition = (resource.animationStates ?? animationStates.get( resource.codename ))
								?.[clip];
							return definition ? { clip, definition } : undefined;
						} )
					);
					if ( alternatives.some( table => table.some( phase => phase === undefined ) ) ) {
						failure = `Missing action phase timeline ${resource.codename}`;
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
				for ( const event of advanceAction( clock, seconds, shotAt, cancelledAt ).events ) {
					triggers.push( {
						cast,
						...event,
						attackKind: clock.phases[2]?.clip.startsWith( "native:" ) ?
							Number( clock.phases[2].clip.split( ":" )[2] ) :
							({ attack1: 2, attack2: 5, attack3: 16, attack4: 17 } as Record<string, number>)[
								clock.phases[2]?.clip.split( "-" )[0] ?? ""
							] ?? 0
					} );
				}
				actionLayersByActor.set( cast.caster, [
					...actionLayers( clock, seconds ),
					...(actionLayersByActor.get( cast.caster ) ?? [])
				] );
			}
			if ( damageTexts.length ) damageTexts = damageTexts.filter( row => seconds - row.started <= 3 );
			for ( const event of gameplay?.environmentalDamage ?? [] ) {
				if ( event.sequence <= environmentalSequence ) continue;
				environmentalSequence = event.sequence;
				const target = entitiesByGid.get( event.gid ),
					at = simulationMs === undefined ? seconds : seconds + (event.atMs - simulationMs) / 1000;
				if ( !target || seconds - at >= 1 ) continue;
				if ( damageTexts.length >= 2048 ) throw Error( "Damage text capacity exceeded" );
				const native = target.gid === gameplay?.localGid && gameplay.pose ?
					gameplay.pose :
					{ regionId: target.regionId, x: target.x, y: target.y, z: target.z, angle: target.heading };
				const anchor = posePresentation.pose( target.gid, native, seconds );
				// 77A080 -> 8E2840 -> 8D4DD0: environmental feedback is a
				// victim label, with no attack animation or invented impact sound.
				damageTexts = appendDamageText(
					damageTexts,
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
			// Sample sockets from the admitted models at the current mechanical pose
			// and authored callback cursor; flight ownership precedes hit feedback.
			const effectActors = effects.step(
				entities,
				gameplay,
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
					const rows = [ ...displayed.values() ].map( actor => {
						const entity = entitiesByGid.get( actor.gid ),
							native = entity ?
								(entity.gid === gameplay?.localGid && gameplay.pose ?
									gameplay.pose :
									{
										regionId: entity.regionId,
										x: entity.x,
										y: entity.y,
										z: entity.z,
										angle: entity.heading
									}) :
								undefined;
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
					return renderer.characterSocket( rows, gid, bone, offset );
				},
				[ ...displayed.values() ],
				effectDetail,
				bloodEnabled
			);
			// 008DD6F0 calls snd_activate once after constructing the effect,
			// including restored instances and immediate-stop instances.
			referenceAppearances.step( effects.attachedInstances() );
			for ( const event of effects.takeActivations() ) {
				const entity = entitiesByGid.get( event.gid ),
					resource = entity ? catalog.get( appearanceRef( entity ) ) : undefined;
				if ( entity && resource ) {
					const pose = entity.gid === gameplay?.localGid && gameplay.pose ? gameplay.pose : entity;
					sounds.emit(
						`activate:${event.gid}:${event.skill}:${event.at}`,
						resource.soundProfileName ?? soundProfiles.get( resource.codename ) ?? resource.codename,
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
						Math.max( combatStanceEnds.get( target.gid ) ?? -Infinity, at + COMBAT_STANCE_SECONDS )
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
				const native = target.gid === gameplay?.localGid && observer ?
					observer :
					{ regionId: target.regionId, x: target.x, y: target.y, z: target.z, angle: target.heading };
				const dx = observer ?
					native.x - observer.x + ((native.regionId & 255) - (observer.regionId & 255)) * 1920 :
					Infinity;
				const dy = observer ? native.y - observer.y : Infinity;
				const dz = observer ?
					native.z - observer.z + ((native.regionId >>> 8) - (observer.regionId >>> 8)) * 1920 :
					Infinity;
				if ( cast.caster === gameplay?.localGid || dx * dx + dy * dy + dz * dz < 40000 ) {
					if ( damageTexts.length >= 2048 ) throw Error( "Damage text capacity exceeded" );
					const anchor = posePresentation.pose( target.gid, native, seconds );
					damageTexts = appendDamageText(
						damageTexts,
						damageText( { ...target, ...anchor }, impact, at, !caster ),
						seconds
					);
					if ( impact.type === 2 && seconds - at <= .25 ) {
						const resource = catalog.get( target.refObjId );
						if ( resource ) {
							sounds.emit(
								"block:" + key,
								resource.soundProfileName ?? soundProfiles.get( resource.codename ) ??
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
				const route = hit.source === "flush" ?
					effects.impactSource( caster?.gid, target.gid, gameplay?.attachedEffects ?? [], cast ) :
					{ gid: cast.caster, skill: hit.soundSkill ?? 0, defensive: false };
				if ( effectDetail && caster && hit.source !== "hawk" ) {
					const shown = [ ...displayed.values() ].map( actor => {
							const entity = entitiesByGid.get( actor.gid );
							if ( !entity ) return actor;
							const native = entity.gid === gameplay?.localGid && gameplay.pose ?
									gameplay.pose :
									{
										regionId: entity.regionId,
										x: entity.x,
										y: entity.y,
										z: entity.z,
										angle: entity.heading
									},
								pose = posePresentation.pose( actor.gid, native, seconds );
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
						const targetResource = catalog.get( appearanceRef( target ) ),
							anchor = targetResource ? effectAnchors.get( targetResource.codename ) : undefined;
						const bone = anchor?.bone ?
							renderer.characterLocalMatrix( shown, target.gid, anchor.bone ) :
							null;
						const ride = target.mountedOn ? entitiesByGid.get( target.mountedOn ) : undefined,
							rideResource = ride ? catalog.get( ride.refObjId ) : undefined;
						const saddle =
							anchor?.bone && ride && rideResource && !riderModes.get( rideResource.codename ) ?
								renderer.characterLocalMatrix( shown, ride.gid, "saddle" ) :
								null;
						const point = hit.source === "cast" ?
							(hit.position ?? victim.pose) :
							anchor ?
							damageAnchor( victim.pose, source.pose, anchor.offset, bone, saddle ) :
							victim.pose;
						const basis = Array.from(
							{ length: 9 },
							( _, i ) => matrix[Math.floor( i / 3 ) * 4 + i % 3]! * (i >= 6 ? -1 : 1)
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
								targetResource ? bloodEffects.get( targetResource.codename ) : undefined,
								bloodEnabled,
								seconds,
								resources.ready
							)
						);
					}
				}
				const emitter = entitiesByGid.get( route.gid ),
					resource = emitter ? catalog.get( emitter.refObjId ) : undefined;
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
						resource.soundProfileName ?? soundProfiles.get( resource.codename ) ?? resource.codename,
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
			const active = new Set( entities.map( entity => entity.gid ) ), next = new Map<number, CharacterActor>();
			// Admission order owns idle RNG, before distance/camera presentation selection.
			const deadGids = new Set( gameplay?.vitals.filter( v => v.hp === 0 ).map( v => v.gid ) ?? [] );
			probe?.detailEnd( "presentation-events" );
			probe?.detailBegin( "presentation-state" );
			for ( const entity of entities ) {
				if ( entity.groundItem ) continue;
				const resource = catalog.get( appearanceRef( entity ) );
				if ( !resource ) continue;
				const pose = entity.gid === gameplay?.localGid && gameplay.pose ? gameplay.pose : entity;
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
							failure = `Missing native recovery duration ${resource.codename}`;
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
			probe?.detailEnd( "presentation-state" );
			probe?.detailBegin( "presentation-actors" );
			const appearanceActive = new Set( selected.map( entity => entity.gid ) );
			for ( const gid of appearances.keys() ) if ( !appearanceActive.has( gid ) ) appearances.delete( gid );
			const animationHolders: { actor: CharacterActor; sets: readonly AnimationParticleSet[]; }[] = [];
			const particleHolders: { actor: CharacterActor; particles: readonly ModelParticle[]; }[] = [];
			for ( const entity of selected ) {
				active.add( entity.gid );
				try {
					if ( entity.skillObject ) {
						const visual = skillObjects.frame( entity, seconds, resources );
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
						const item = items[String( entity.refObjId )], drop = dropModels[item?.dropModelPath ?? ""];
						if ( !drop ) {
							if ( manifest === manifests.length ) {
								throw Error( "Missing authored drop model for item " + entity.refObjId );
							}
							continue;
						}
						const gold = (entity.groundItem.typeFlags & 0x60) === 0x60 &&
							(entity.groundItem.typeFlags & 0x780) === 0x280;
						const fanfare = gold && entity.groundItem.appear !== undefined ?
							dropModels["item/etc/drop_ch_money_ing.bsr"] :
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
					if ( manifest < manifests.length ) continue;
					const resource = resourceFor( entity );
					if ( !resource ) continue;
					if ( !resources.ready( resource.glb ) ) {
						const previous = displayed.get( entity.gid ), paths = displayedDependencies.get( entity.gid );
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
						const previous = displayed.get( entity.gid ), paths = displayedDependencies.get( entity.gid );
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
									dress,
									equipment: referenceAppearanceItems(
										disguise,
										resource.codename.includes( "_MAN_" ),
										itemIds
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
										defaultWearFrozen( dress.defaultWearLanguage ?? 4, nativeServerName )
								} ).parts,
								paths = [ resource.glb, ...parts.map( p => p.model ) ];
							if ( resources.plan( paths ) ) {
								model = `assembly:disguise:${resource.glb}:${JSON.stringify( parts )}`;
								renderer.setCharacterAssembly( model, resource.glb, parts );
								dependencies = paths;
							} else model = fallback();
						} else if (
							(skin || entity.gid === gameplay?.localGid || entity.equipment || avatars.length) &&
							manifest >= 3
						) {
							const equipment = wornEquipment( entity, gameplay );
							const weaponHidden = effects.appearance( entity.gid ).weaponHidden;
							const hwanHair = entity.appearanceState?.[2] === 1 &&
								resource.codename.startsWith( "CHAR_CH_" );
							const freezeWear = defaultWearFrozen( dress.defaultWearLanguage ?? 4, nativeServerName );
							const player = entity.kind === "player" || entity.kind === "local-player",
								local = entities.find( e => e.gid === gameplay?.localGid );
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
								Number( !!idleStates.get( entity.gid )?.attachmentsHidden ) + ":" +
								equipment.filter( item => item.slot >= 0 && item.slot < 9 ).map( item =>
									item.slot + "," + item.refObjId + "," + item.plus
								).join( ";" ) + "|" + avatars.map( item => item.refObjId ).join( ";" );
							let appearance = appearances.get( entity.gid );
							if (
								!appearance || appearance.resource !== resource || appearance.dress !== dress ||
								appearance.items !== items || appearance.signature !== signature
							) {
								const committedWear = states.get( entity.gid )?.defaultWear;
								const assembly = assembleEquipmentAppearance( {
									resource,
									dress,
									equipment,
									avatars,
									hwanHair,
									mounted: entity.mountedOn !== undefined,
									weaponHidden,
									attachmentsHidden: !!idleStates.get( entity.gid )?.attachmentsHidden,
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
									dress,
									items,
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
						failure = String( error );
						model = fallback();
					}
					if ( sampleActorDetails ) probe?.detailBegin( "actor-motion" );
					const nativePose = entity.gid === gameplay?.localGid && gameplay.pose ?
						gameplay.pose :
						{ regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };

					let state = states.get( entity.gid );
					if ( !state ) {
						state = { modifierId: allocateActor(), clip: "", started: seconds };
						states.set( entity.gid, state );
					}
					const previousOverride = avatarOverrides.get( entity.gid );
					const selectedOverride = overrideCommit === undefined ?
						previousOverride :
						selectAvatarOverride( previousOverride, overrideCommit, dress?.avatarVisualOverrides ?? {} );
					if ( selectedOverride ) avatarOverrides.set( entity.gid, selectedOverride );
					const overrideChanged = previousOverride?.selected !== selectedOverride?.selected;
					const override = selectedOverride?.selected === undefined ?
						undefined :
						dress?.avatarVisualOverrides?.[selectedOverride.selected];
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
					const downDeath = idleStates.get( entity.gid )?.downDeath,
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
					let moving = (entity.gid === gameplay?.localGid ? gameplay.moving : entity.moving) ?? false;
					const statusMask = vitalsByGid.get( entity.gid )?.abnormal ?? 0;
					const statusView = statusOwner.view( entity.gid, statusMask, entity.appearanceState?.[2] ?? 0 );
					// 85C590 writes +0xB9 from the FZ bit with no hp test.
					// Death selects its own clip; it does not clear the lock.
					const poseFrozen = statusView.poseLocked;
					if ( poseFrozen ) moving = false;
					const requestedMoving = moving;
					const movementRevision =
						(entity.gid === gameplay?.localGid ? gameplay.movementRevision : entity.movementRevision) ?? 0;
					if (
						state.navigationHold &&
						(state.navigationHold.revision !== movementRevision || dead || entity.mountedOn)
					) state.navigationHold = undefined;
					if ( state.navigationHold ) {
						renderPose = state.navigationHold.pose;
						moving = false;
					}
					const activePosture = idleStates.get( entity.gid )?.posture;
					const derivedMask = dead ?
						2 :
						sitting ?
						0x40 :
						activePosture?.kind === "down" ?
						0x10 :
						8 | (moving ? 0x200 : 0x100) | (cast ? 4 : 0);
					const input = [
						dead,
						sitting,
						entity.mountedOn ?? 0,
						entity.movementMode,
						requestedMoving,
						movementRevision,
						activePosture?.kind ?? "",
						!!cast
					].join( ":" );
					const commands: Parameters<typeof transitionActionStates>[2][number][] = [];
					let mask = state.actionMask ?? derivedMask;
					if ( !dead ) mask &= ~2;
					mask = (mask & ~4) | (cast ? 4 : 0);
					if ( state.actionInput !== undefined && state.actionInput !== input ) {
						if ( dead || activePosture?.kind === "down" || entity.mountedOn ) mask = derivedMask;
						else if ( sitting ) commands.push( { kind: "enter", state: 6 } );
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
						entity.gid === gameplay?.localGid ? gameplay.movementPath : entity.movementPath,
						state.actionRevision !== movementRevision
					);
					state.actionRevision = movementRevision;
					state.actionMask = refresh.mask;
					state.actionInput = input;
					state.actionMode = entity.movementMode;
					if (
						requestedMoving && refresh.effects.some( e => e.kind === "leave" && e.state === 9 ) &&
						!refresh.navigation
					) {
						state.navigationHold = {
							revision: movementRevision,
							pose: renderPose,
							mode: entity.movementMode
						};
						moving = false;
					}
					let combatIdle: ReturnType<typeof skillMotionResolveAnimation>;
					if ( (combatStanceEnds.get( entity.gid ) ?? -Infinity) > seconds ) {
						const metadata = animationStates.get( resource.codename );
						if ( state.combatIdle?.body !== resource || state.combatIdle.metadata !== metadata ) {
							// 8E5ADE -> model vtable C15160+0C -> 8E7470:
							// DEFAULT state 6, NOT weapon-set preview state 0.
							state.combatIdle = {
								body: resource,
								metadata,
								motion: skillMotionResolveAnimation( {
									role: "native:default:6",
									clips: resource.clips,
									bodyStates: resource.animationStates,
									catalogStates: metadata,
									motionUrls: nativeMotionUrls.get( resource.codename )
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
					// 8EADF0 tries the selected item override before the ordinary
					// body track, only when unmounted. Missing states keep the normal
					// selection; published cold BANs use the existing resource owner.
					if (
						override && !entity.mountedOn && !dead && !sitting &&
						(baseRole === "run" || baseRole === "walk" || baseRole === "stand")
					) {
						const role = `native:${override.animation}:${
							baseRole === "run" ? 7 : baseRole === "walk" ? 1 : 0
						}`;
						const definition = animationStates.get( resource.codename )?.[role] ??
								resource.animationStates?.[role],
							url = nativeMotionUrls.get( resource.codename )?.get( role );
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
							dress?.avatarVisualOverrides?.[previousOverride.selected];
						for ( const effect of refresh.effects ) {
							if ( effect.kind === "commit" ) {
								committed = true;
								continue;
							}
							if ( effect.kind === "navigation" ) continue;
							if ( effect.kind === "feet" ) {
								const actor = displayed.get( entity.gid );
								if ( actor ) {
									for ( const right of [ false, true ] ) {
										footContact(
											entity,
											[ ...displayed.values() ],
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
									const idle = idleStates.get( entity.gid )?.idle;
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
										const idle = idleStates.get( entity.gid )?.idle;
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
										const metadata = animationStates.get( resource.codename )?.[candidate] ??
												resource.animationStates?.[candidate],
											url = nativeMotionUrls.get( resource.codename )?.get( candidate );
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
					state.locomotion = changeLocomotion( state.locomotion, clip, looping, seconds, baseRole );
					if ( previousLocomotion !== state.locomotion ) {
						state.locomotion.rate = baseRole === "run" || baseRole === "walk" ? entryRate : 1;
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
								definition = (resource.animationStates ?? animationStates.get( resource.codename ))
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
					const posture = idleStates.get( entity.gid )?.posture;
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
					const idle = idleStates.get( entity.gid )?.idle;
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
					const metadata = resource.animationStates ?? animationStates.get( resource.codename );
					// Retain the native idle metadata through its outgoing blend.
					const motionMetadata = ( name: string ) =>
						state.combatIdle?.motion?.clip === name ?
							state.combatIdle.motion.definition :
							metadata?.[name] ?? animationStates.get( resource.codename )?.[name];
					// Resolve equipment, surface and world position only when a cue
					// is due. Cursor advancement remains independent of visibility.
					const soundSource = () => {
						const context = soundContext( entity, cast?.skill, state.hitCritical );
						return {
							profile: resource.soundProfileName ?? soundProfiles.get( resource.codename ) ??
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
						shadowSize: !entity.groundItem ? shadowSizes.get( entity.refObjId ) : undefined,
						modelAnimation: resource.modifierBindings?.length ?
							(state.modelAnimation ??= createModelAnimation()).step(
								dispatch,
								resource.modifierBindings,
								resource.modifierSelectors ?? []
							) :
							undefined,
						animationLod: { fraction: entityLod.fraction( entity.gid ), crowded: entityLod.crowded() },
						blindable: blindableCharacter( entity, gameplay?.localGid ),
						groundItem: !!entity.groundItem,
						previewClip: resource.clips.includes( armedIdle ) ? armedIdle : "stand",
						materialTint: appearance.materialTint,
						pointLight: appearance.pointLight,
						modifierId: state.modifierId,
						bloodEffects: bloodEffects.get( resource.codename ),
						effectBaseScale: baseScale,
						heightFactor: heightFactors.get( resource.codename ),
						effectAnchor: effectAnchors.get( resource.codename ),
						pickable: !authorityDead || entity.gid === gameplay?.localGid,
						height: heights.has( resource.codename ) ?
							heights.get( resource.codename )! *
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
						layers: posture || layers.length > 1 || state.locomotion.outgoing.length ? layers : undefined,
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
					failure = String( error );
				}
			}
			probe?.detailEnd( "presentation-actors" );
			probe?.detailBegin( "presentation-finalize" );
			for ( const gid of states.keys() ) {
				if ( !active.has( gid ) ) {
					states.delete( gid );
					displayedDependencies.delete( gid );
				}
			}
			if ( footprints.length ) {
				footprints = footprints.filter( row => seconds < row.started + 20 );
				renderer.setFootprints( footprints );
			}
			const settled = new Set<number>();
			for ( const entity of entities ) {
				const state = states.get( entity.gid ), resource = catalog.get( appearanceRef( entity ) );
				if ( state?.dead && resource ) {
					const clip = state.postureClip ??
							(state.clip === "deathquick" || state.clip === "downdie" ? state.clip : "death"),
						start = state.postureStarted ?? state.started;
					if ( seconds - start >= resources.duration( resource.glb, clip ) ) {
						state.feedbackSettled = true;
						settled.add( entity.gid );
					}
				}
			}
			const orbActors = orbs.step(
				entities,
				seconds,
				settled,
				( gid, bone ) => renderer.characterSocket( [ ...next.values() ], gid, bone, [ 0, 0, 0 ] ),
				resources.ready,
				resources.duration,
				3,
				resources.failed
			);
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
				next.set( actor.gid, { ...actor, model, modelAnimation } );
				if ( metadata?.particles.length ) {
					animationHolders.push( { actor: next.get( actor.gid )!, sets: metadata.particles } );
				}
			}
			for ( const gid of stageAnimations.keys() ) if ( !stageAlive.has( gid ) ) stageAnimations.delete( gid );
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
				let moving = (entity.gid === gameplay?.localGid ? gameplay.moving : entity.moving) ?? false;
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
			for ( const gid of avatarOverrides.keys() ) if ( !active.has( gid ) ) avatarOverrides.delete( gid );
			sounds.retain( active );
			posePresentation.retain( active );
			cameraTarget = null;
			if ( local && gameplay?.pose ) {
				const height = heights.get( catalog.get( local.refObjId )?.codename ?? "" );
				const mount = local.mountedOn ? entitiesByGid.get( local.mountedOn ) : undefined;
				const mountHeight = mount ? heights.get( catalog.get( mount.refObjId )?.codename ?? "" ) : undefined;
				const rendered = next.get( mount?.gid ?? local.gid )?.pose;
				if ( rendered && height !== undefined && (!local.mountedOn || mount && mountHeight !== undefined) ) {
					cameraTarget = {
						height,
						mounted: !!mount,
						pose: {
							regionId: rendered.regionId,
							x: rendered.x,
							y: mount ? Math.fround( Math.fround( rendered.y + mountHeight! ) - 13 ) : rendered.y,
							z: rendered.z,
							angle: mount?.heading ?? gameplay.pose.angle
						}
					};
				}
			}
			// 85EC00 sets body state 4's alpha 0x50 on the model and its mount,
			// but 85D890 runs on every update of every character except the
			// local player (CICUser/CICCos_OnUpdate) and restores 0xFF unless
			// the concealment rule hides it. Body 4 therefore stays only on the
			// local player's own model, and 6/7 follow concealment.ts.
			for ( const entity of entities ) {
				const actor = next.get( entity.gid );
				if ( actor?.opacity !== undefined ) next.set( entity.gid, { ...actor, opacity: undefined } );
			}
			if ( local?.appearanceState?.[2] === 4 ) {
				const actor = next.get( local.gid );
				if ( actor ) next.set( local.gid, { ...actor, opacity: seenAlpha() } );
			}
			if (
				entities.some( e =>
					e.gid !== local?.gid && (e.appearanceState?.[2] === 6 || e.appearanceState?.[2] === 7)
				)
			) {
				const lookup = concealmentSkills( gameplay?.skillCatalog ),
					byGid = new Map<number, import("@/engine/foundation/gameplay/attached-effects").AttachedEffect[]>();
				for ( const effect of gameplay?.attachedEffects ?? [] ) {
					let list = byGid.get( effect.gid );
					if ( !list ) byGid.set( effect.gid, list = [] );
					list.push( effect );
				}
				const viewer = local ? byGid.get( local.gid ) ?? [] : [],
					party = new Set( gameplay?.social?.members.map( m => m.name ) ?? [] );
				for ( const entity of entities ) {
					const body = entity.appearanceState?.[2] ?? 0;
					if ( entity.gid === local?.gid || body !== 6 && body !== 7 ) continue;
					const state = concealmentState(
						body,
						byGid.get( entity.gid ) ?? [],
						viewer,
						entityLod.distance( entity.gid ),
						lookup
					);
					const alpha = concealmentAlpha( state, party.has( entity.name ) );
					const actor = next.get( entity.gid );
					if ( actor && alpha !== undefined ) next.set( entity.gid, { ...actor, opacity: alpha } );
				}
			}
			if ( local ) {
				if ( cameraFade?.gid !== local.gid ) {
					cameraFade = { gid: local.gid, time: seconds, mode: false, current: 255, start: 255, progress: 1 };
				}
				const hidden = cameraPitch < -0.8999999761581421,
					transition = cameraFade.mode !== hidden || cameraFade.progress < 1;
				const alpha = advanceCharacterFade( cameraFade, hidden, Math.max( 0, seconds - cameraFade.time ) );
				cameraFade.time = seconds;
				// 866B90 applies camera interpolation first while it is live;
				// after it finishes, the body-4 branch restores alpha 0x50.
				const actor = next.get( local.gid );
				if ( actor && (transition || local.appearanceState?.[2] !== 4) ) {
					next.set( local.gid, { ...actor, opacity: alpha } );
				}
			} else cameraFade = null;
			for ( const row of disappearing.values() ) {
				let actor = disappearActor( row, seconds );
				if ( actor && next.size < CHARACTER_ACTORS ) {
					if ( row.animation && actor.layers ) {
						const { resource, dispatch, selection } = row.animation;
						const ranges = dispatch.step(
							actor.layers,
							animationDeltaMs,
							name =>
								resource.animationStates?.[name]?.durationMs ??
									Math.round( resources.duration( resource.glb, name ) * 1000 )
						);
						actor = {
							...actor,
							modelAnimation: selection.step(
								ranges,
								resource.modifierBindings ?? [],
								resource.modifierSelectors ?? []
							)
						};
						if ( resource.animationParticles?.length ) {
							animationHolders.push( { actor, sets: resource.animationParticles } );
						}
					}
					next.set( actor.gid, actor );
					for ( const child of row.children ?? [] ) {
						if ( next.size >= CHARACTER_ACTORS ) break;
						const age = seconds - row.started;
						next.set( child.gid, {
							...child,
							time: child.time + age,
							layers: child.layers?.map( layer => ({ ...layer, time: layer.time + age }) )
						} );
					}
					if ( row.particles.length ) particleHolders.push( { actor, particles: row.particles } );
				}
			}
			for ( const entity of entities ) {
				if ( hiddenSilkCos( entity, hideSilkCos ) ) {
					const actor = next.get( entity.gid );
					if ( actor ) next.set( entity.gid, { ...actor, opacity: 0 } );
				}
			}
			for ( const holder of particleHolders ) holder.actor = next.get( holder.actor.gid )!;
			for (
				const actor of modelEmission.step(
					particleHolders,
					seconds,
					resources.ready,
					CHARACTER_ACTORS - next.size,
					gid => entityLod.fraction( gid )
				)
			) next.set( actor.gid, actor );
			for ( const holder of animationHolders ) holder.actor = next.get( holder.actor.gid )!;
			for (
				const actor of animationEmission.step(
					animationHolders,
					seconds,
					resources.ready,
					CHARACTER_ACTORS - next.size,
					renderer.presentationNight?.() ?? true,
					( gid, actor ) => {
						if ( !actor ) return renderer.characterParticleSnapshot( gid );
						const matrix = renderer.characterMatrix( [ ...next.values(), actor ], gid );
						return matrix ? { matrix, regionId: actor.pose.regionId } : null;
					},
					gid => renderer.characterParticleTime?.( gid ),
					path => resources.duration( path, "effect" )
				)
			) next.set( actor.gid, actor );
			for (
				const actor of scenery.step(
					renderer.scenery?.() ?? null,
					seconds,
					resources.ready,
					CHARACTER_ACTORS - next.size
				)
			) next.set( actor.gid, actor );
			// 5BAF70 -> 5B9DF0 builds a slot-owned preview from the roster model.
			// It remains admitted even when no world entity exists for that member.
			const portraits: CharacterActor[] = [];
			for ( const member of gameplay ? partyMembers( gameplay ) : [] ) {
				const resource = catalog.get( member.model );
				if ( !resource || !resources.ready( resource.glb ) ) continue;
				portraits.push( {
					gid: partyPortraitGid( member.id ),
					model: resource.glb,
					pose: { regionId: 0, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
					clip: "stand",
					time: 0,
					loop: true,
					scale: 1
				} );
			}
			displayed = next;
			renderer.setCharacterActors(
				[ ...next.values() ].map( actor =>
					blindHeld && actor.blindable ? { ...actor, opacity: 0, pickable: false } : actor
				),
				portraits
			);
			resources.retainWanted( [ ...next.values(), ...portraits ].map( actor => actor.model ) );
			probe?.detailEnd( "presentation-finalize" );
		},
		ready: ( gid: number ) => displayed.has( gid ),
		/*
 ================
 entryReady

 Keep first-use baseline work behind world entry without spawning fake drops.
 ================
 */
		entryReady: () => commonReady && warmMotions.length === 0 && effects.loaded(),
		previewReady: () => previewReady,
		dockReady: () => dockReady,
		/*
================
profile
================
		*/
		profile( value: CharacterFrameProbe | undefined ) {
			probe = value;
		},
		cameraTarget: () => cameraTarget,
		takeCameraScripts: () => effects.takeCameraScripts(),
		orbGauge: () => orbs.gauge(),
		damageText: () => damageTexts as readonly import("@/engine/contracts/damage-text").DamageText[],
		error: () => failure ?? resources.error() ?? effects.error(),
		/*
================
reset
================
		*/
		reset() {
			skillObjects.reset();
			clearFootprints();
			animationDelta = createModifierDelta();
			warmSkills = undefined;
			warmBody = undefined;
			warmMotions = [];
			commonReady = false;
			scenery.reset();
			entityLod.reset();
			modelEmission.reset();
			animationEmission.reset();
			stageAnimations.clear();
			groundClocks.clear();
			selection.reset();
			stateIndex.reset();
			feedback.reset();
			environmentalSequence = 0;
			appearances.clear();
			hwanHairActors.clear();
			committedAuxiliary.clear();
			avatarOverrides.clear();
			auxiliaryActors.clear();
			posePresentation.reset();
			idleStates.clear();
			combatStanceEnds.clear();
			retiring.clear();
			disappearing.clear();
			damageTexts = [];
			rainEventActive = false;
			rainEventEntities.clear();
			rainEvents.length = 0;
			orbs.reset();
			previewReady = false;
			dockReady = false;
			previewDisplay = null;
			previewShape = null;
			previewWear.clear();
			dockStates.clear();
			lizardStarted = null;
			cameraTarget = null;
			cameraFade = null;
			effects.reset();
			referenceAppearances.reset();
			sounds.reset();
			resources.reset();
			states.clear();
			actionClocks.clear();
			deathFinalizes.clear();
			displayed.clear();
			displayedDependencies.clear();
			failure = null;
			renderer.setCharacterActors( [] );
		},
		/*
================
dispose
================
		*/
		dispose() {
			skillObjects.dispose();
			clearFootprints();
			warmSkills = undefined;
			warmBody = undefined;
			warmMotions = [];
			scenery.reset();
			entityLod.reset();
			modelEmission.reset();
			animationEmission.reset();
			stageAnimations.clear();
			groundClocks.clear();
			selection.reset();
			stateIndex.reset();
			feedback.reset();
			environmentalSequence = 0;
			appearances.clear();
			hwanHairActors.clear();
			committedAuxiliary.clear();
			avatarOverrides.clear();
			auxiliaryActors.clear();
			posePresentation.reset();
			idleStates.clear();
			combatStanceEnds.clear();
			retiring.clear();
			disappearing.clear();
			damageTexts = [];
			rainEventActive = false;
			rainEventEntities.clear();
			rainEvents.length = 0;
			orbs.reset();
			previewReady = false;
			dockReady = false;
			previewDisplay = null;
			previewShape = null;
			previewWear.clear();
			dockStates.clear();
			lizardStarted = null;
			cameraTarget = null;
			cameraFade = null;
			effects.dispose();
			referenceAppearances.reset();
			sounds.reset();
			resources.dispose();
			states.clear();
			actionClocks.clear();
			deathFinalizes.clear();
			displayed.clear();
			displayedDependencies.clear();
			catalog.clear();
			nativeMotionUrls.clear();
			animationStates.clear();
			soundProfiles.clear();
		}
	};
}
