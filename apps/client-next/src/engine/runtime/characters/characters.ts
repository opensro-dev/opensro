/*
===========================================================================

characters.ts - character assemblies and their presentation lifetime

Owns resource admission, actor assembly, animation and effects for roster
previews and world entities. Readiness belongs to each required assembly;
scenery and auxiliary actors cannot satisfy a player readiness barrier.

===========================================================================
*/

import { fortressAppearance } from "@/engine/foundation/animation/fortress-appearance";
import { movementEntryRate, transitionActionStates } from "@/engine/foundation/animation/action-refresh";
import { createStatusOwner } from "@/engine/foundation/animation/status-presentation";
import { defaultWearFrozen, refreshDefaultWear } from "@/engine/foundation/animation/default-wear-policy";
import { selectAvatarOverride, type AvatarOverrideSelection } from "@/engine/foundation/animation/avatar-override";
import { assembleEquipmentAppearance, wornItemsFromList } from "@/engine/foundation/animation/equipment-appearance";
import { createAnimationEmission, type AnimationParticleSet } from "@/engine/foundation/animation/animation-emission";
import { COMBAT_STANCE_SECONDS, combatStanceOnHit } from "@/engine/foundation/animation/combat-stance";
import { createModelAnimation } from "@/engine/foundation/animation/model-animation";
import { createEntityLod } from "@/engine/foundation/animation/entity-lod";
import { createAnimationDispatch } from "@/engine/foundation/animation/animation-dispatch";
import { createModifierDelta } from "@/engine/foundation/rendering/modifier-delta";
import { animationActivation, type AnimationActivation } from "@/engine/foundation/animation/animation-activation";
import { createPresentationIds } from "@/engine/foundation/animation/presentation-ids";
import { createModelEmission } from "@/engine/foundation/animation/model-emission";
import { createStructureVisuals } from "./structure-visuals";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import {
	groundVisualClock,
	advanceGroundVisual,
	type GroundVisualClock
} from "@/engine/foundation/animation/ground-visual";
import { createSceneryEmission } from "@/engine/foundation/animation/scenery-emission";
import { damageAnchor } from "@/engine/foundation/animation/damage-anchor";
import {
	createReferenceAppearances,
	referenceAppearanceItems
} from "@/engine/foundation/animation/reference-appearance";
import { hawkResult } from "@/engine/foundation/gameplay/attached-effects";
import { createDamageFeedback } from "./damage-feedback";
import { blindableCharacter } from "@/engine/foundation/ui/name-visibility";
import { skillLookup, type SkillLookup } from "@/engine/foundation/ui/buff-viewer";
import { createPosePresentation } from "./pose-presentation";
import { createPresentationSamples } from "./presentation-samples";
import { createPresentationState } from "./presentation-state";
import { createPresentationActions } from "./presentation-actions";
import { createCharacterStateIndex } from "./state-index";
import { createSkillObjects, SKILL_OBJECT_MANIFESTS } from "./skill-objects";
import { monsterScale, monsterMaterialSlot } from "@/engine/foundation/rendering/monster-scale";
import { postureLayers } from "@/engine/foundation/animation/posture";
import { appendDamageText, damageText } from "@/engine/foundation/ui/damage-text";
import { oneShotLayers } from "@/engine/foundation/animation/one-shot-layers";
import { changeLocomotion, stopLocomotion, locomotionLayers } from "@/engine/foundation/animation/locomotion-blend";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import { createOrbs } from "./orbs/orbs";
import { spawnFadeAlpha, spawnFadeKind } from "@/engine/foundation/animation/spawn-fade";
import { skillMotionResolveAnimation } from "@/engine/foundation/animation/skill-motion-resolve";
import { CHARACTER_ACTORS } from "@/engine/foundation/animation/character-budget";
import { createCharacterSelection } from "@/engine/foundation/animation/character-selection";
import { weaponAnimationSet } from "@/engine/foundation/animation/animation-metadata";
import { createCharacterEffects } from "./effects/effects";
import { createCharacterSounds } from "./sounds/sounds";
import { createCharacterResources } from "./resources/resources";
import { createMallPreview } from "./mall-preview";
import { characterHeadingYaw } from "@/engine/foundation/math/angles";
import type { CharacterRecord } from "@/engine/contracts/session";
import { movementGait } from "@/engine/foundation/gameplay/native-movement";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Renderer } from "@/engine/contracts/runtime";
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { CharacterActor } from "@/engine/contracts/character";
import { createPresentationCatalog } from "./presentation-catalog";
import { createActorPresentation } from "./actor-presentation";
import { createDockPreview } from "./dock-preview";
import { createPresentationWeather, type WeatherLifecycleEvent } from "./presentation-weather";
import { createAuxiliaryPresentation } from "./presentation-auxiliary";
import {
	selectCameraTarget,
	applyCharacterVisibility,
	presentDisappearing,
	presentEmission,
	publishCharacters
} from "./presentation-finalize";
import type {
	Auxiliary,
	CharacterPresentationState,
	PresentationAppearance,
	PresentationDisappear,
	PresentationOutput,
	Resource
} from "./internal/presentation-contract";
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

Own character catalog admission and actor assembly across the dock, creation
preview and world. An active empty dock still needs its catalog admitted.
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
	let frameWork: import("@/engine/contracts/runtime").FrameWork | undefined;
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
	const mallPreview = createMallPreview();
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
	const presentationState = createPresentationState();
	let damageTexts: import("@/engine/contracts/damage-text").DamageText[] = [];
	let environmentalSequence = 0;
	const feedback = createDamageFeedback( ( key, at ) => health?.release( key, at ) );
	const retiring = new Set<number>(),
		disappearing = new Map<number, PresentationDisappear>();
	const allocateActor = createPresentationIds(), lizardGid = allocateActor();
	// The frame's published results; several phases write them (presentation-contract.ts).
	const output: PresentationOutput = {
		displayed: new Map<number, CharacterActor>(),
		failure: null,
		cameraTarget: null,
		cameraFade: null,
		previewReady: false,
		dockReady: false,
		commonReady: false
	};
	const weather = createPresentationWeather();
	const states = new Map<number, CharacterPresentationState>();
	const animationEmission = createAnimationEmission( allocateActor );
	const auxiliary = createAuxiliaryPresentation( allocateActor );
	const modelEmission = createModelEmission( allocateActor );
	const structureVisuals = createStructureVisuals();
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
				const item = published.items[String( id )];
				return item ? { ...item, model: published.dropModels[item.dropModelPath ?? ""] } : undefined;
			},
			allocateActor,
			statusOwner
		);
	const published = createPresentationCatalog( { resources, sounds, referenceAppearances } );
	const posePresentation = createPosePresentation();
	const selection = createCharacterSelection( CHARACTER_ACTORS );
	const stateIndex = createCharacterStateIndex();
	const presentationActions = createPresentationActions();
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
	applySpawnFades

	CIDecoAppear for every spawned player, monster and COS (spawn-fade.ts),
	scaling whatever opacity the other owners already chose. The ramp starts
	on the actor's first drawable frame: natively the model exists at spawn,
	here it may still be loading, and a ramp spent on an unloaded model would
	pop in. A monster's linked ride carries its own equal ramp (861EE2).
	================
	*/
	function applySpawnFades( entities: readonly EntityState[], next: Map<number, CharacterActor>, seconds: number ) {
		fadePresent.clear();
		for ( const entity of entities ) {
			fadePresent.add( entity.gid );
			if ( !spawnFadeKind( entity.kind ) ) continue;
			if ( !fadeSeen.has( entity.gid ) ) {
				fadeSeen.add( entity.gid );
				spawnFades.set( entity.gid, null );
				rideFades.set( entity.gid, null );
			}
			fadeActor( spawnFades, entity.gid, entity.gid, next, seconds );
			// 861EE2 gives the linked ride its own CIDecoAppear: its ramp starts
			// when the ride itself can draw, which may be after the rider's ends.
			const rideGid = linkedRides.get( entity.gid );
			if ( rideGid !== undefined ) fadeActor( rideFades, entity.gid, rideGid, next, seconds );
		}
		for ( const gid of fadeSeen ) {
			if ( fadePresent.has( gid ) ) continue;
			fadeSeen.delete( gid );
			spawnFades.delete( gid );
			rideFades.delete( gid );
		}
	}
	/*
	================
	fadeActor

	Advances one armed ramp, keyed by its spawned entity, onto one drawn actor:
	the clock starts on the actor's first drawable frame and retires at 1.
	================
	*/
	function fadeActor(
		ramps: Map<number, number | null>,
		key: number,
		gid: number,
		next: Map<number, CharacterActor>,
		seconds: number
	) {
		const start = ramps.get( key ), actor = next.get( gid );
		if ( start === undefined || !actor ) return;
		if ( start === null ) ramps.set( key, seconds );
		const alpha = spawnFadeAlpha( seconds - (start ?? seconds) );
		if ( alpha >= 1 ) {
			ramps.delete( key );
			return;
		}
		next.set( gid, { ...actor, opacity: (actor.opacity ?? 1) * alpha } );
	}
	/*
	================
	resourceFor
	================
	*/
	function resourceFor( entity: EntityState ): Resource | undefined {
		const resource = published.catalog.get( appearanceRef( entity ) );
		const staged = resource?.structureVisuals &&
			structureVisuals.appearance( entity.gid, resource.glb, resource.ambientParticles ?? [] );
		if ( resource && staged ) return { ...resource, glb: staged.glb, ambientParticles: staged.particles };
		const variant = resource && entity.kind === "monster" ?
			resource.materialVariants
				?.[String( monsterMaterialSlot( entity.rarity ?? 0, entity.tidWord ?? 0, resource.materialKind ) )] :
			undefined;
		return resource && variant ? { ...resource, glb: variant } : resource;
	}
	const dockPreview = createDockPreview( { renderer, resources, scenery, lizardGid } );
	const displayedDependencies = new Map<number, readonly string[]>();
	// Appearance topology is independent of pose time. Revalidate resource
	// readiness each frame, but derive garment/cosmetic parts only on change.
	// characterInfo rides: each live rider's presentation-owned ride actor
	// (CICMonster_DeserializeSpawnPacket); the ride models are published.ridesByRider.
	const linkedRides = auxiliary.linkedRides;
	// CIDecoAppear (spawn-fade.ts): each spawned character's ramp start, null
	// while armed and waiting for its first drawable frame. fadeSeen holds the
	// gids already armed, so one present the whole time fades only once.
	const spawnFades = new Map<number, number | null>(), fadeSeen = new Set<number>(), fadePresent = new Set<number>();
	// The linked ride's own ramp, keyed by its rider's entity gid.
	const rideFades = new Map<number, number | null>();
	const avatarOverrides = new Map<number, AvatarOverrideSelection>();
	const committedAuxiliary = new Map<number, readonly Auxiliary[]>();
	const auxiliaryActors = auxiliary.auxiliaryActors;
	const appearances = new Map<number, PresentationAppearance>();
	let hideSilkCos = false;
	// The per-actor phase borrows these owners for the presenter's lifetime.
	const actorPresentation = createActorPresentation( {
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
	} );
	return {
		/*
		================
		frameWork
		================
		*/
		frameWork( work: import("@/engine/contracts/runtime").FrameWork ) {
			frameWork = work;
		},
		/*
		================
		mallOutfit / mallPreviewState
		================
		*/
		mallOutfit( items: readonly number[] | null, skin: import("./mall-preview").MallSkin | null = null ) {
			mallPreview.request( items, skin );
		},
		/*
		================
		mallPreviewState
		================
		*/
		mallPreviewState() {
			return mallPreview.state();
		},
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
			const next: WeatherLifecycleEvent[] = [];
			for ( const event of events ) {
				if ( event.kind === "reset" ) {
					presentationState.combatStanceEnds.clear();
					modelEmission.reset();
					structureVisuals.reset();
					animationEmission.reset();
					auxiliary.resetStages();
					groundClocks.clear();
					retiring.clear();
					disappearing.clear();
					spawnFades.clear();
					rideFades.clear();
					fadeSeen.clear();
					next.length = 0;
					next.push( { kind: "reset" } );
				} else if ( event.kind === "spawn" || event.kind === "state" ) {
					if ( event.kind === "spawn" ) {
						presentationState.combatStanceEnds.delete( event.entity.gid );
						// A respawn under a live gid is a new CICharactor: fade it again.
						fadeSeen.delete( event.entity.gid );
					}
					next.push( { kind: event.kind, gid: event.entity.gid, refObjId: event.entity.refObjId } );
				} else if ( event.kind === "despawn" ) {
					presentationState.combatStanceEnds.delete( event.gid );
					retiring.add( event.gid );
					next.push( { kind: "despawn", gid: event.gid } );
				}
			}
			weather.receive( next );
		},
		/*
		================
		eventRain
		================
		*/
		eventRain() {
			// A replayed world reset first resets the owners a reset retires.
			return weather.eventRain( published, () => {
				presentationState.combatStanceEnds.clear();
				modelEmission.reset();
				structureVisuals.reset();
				animationEmission.reset();
				auxiliary.resetStages();
				groundClocks.clear();
				retiring.clear();
				disappearing.clear();
				damageTexts = [];
			} );
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
				} else if ( event.kind === "system-effect" ) {
					if ( entities.some( e => e.gid === event.gid ) ) effects.system( event.gid, event.effect | 0 );
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
			// The local player (and the mount it rides) is where its movement owner
			// put it; its entity row can still hold the spawn point, and a LOD
			// measured from that drifts while running (equipment glow, effects).
			entityLod.step(
				entities,
				gameplay?.localGid,
				renderer.presentationCamera?.() ?? null,
				Math.trunc( seconds * 1000 ),
				gameplay?.pose ?? undefined,
				entities.find( e => e.gid === gameplay?.localGid )?.mountedOn
			);
			const animationDeltaMs = animationDelta( seconds );
			skillObjects.retain( entities );
			resources.begin( seconds );
			output.failure = null;
			const { localMover, logicalPose, samples } = createPresentationSamples( entities, gameplay );
			posePresentation.samples( samples );
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
			if ( result && !skillObjectResult ) published.admit( result );
			// An empty roster is an active dock: it must admit the catalog before
			// dockReady can reveal the button that opens character creation.
			if (
				(entities.length || dock || preview) &&
				published.manifest < (dock || preview ? 1 : published.manifests.length)
			) {
				resources.manifest( published.manifests[published.manifest]! );
			}
			if ( published.manifest === published.manifests.length ) {
				const path = skillObjects.nextManifest( entities );
				if ( path ) resources.manifest( path );
			}
			if ( dockPreview.step( { seconds, dock, preview, lizard, nativeServerName }, output, published ) ) return;
			for ( const gid of retiring ) {
				const actor = output.displayed.get( gid );
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
							const actor = output.displayed.get( child.gid );
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
					const resource = published.catalog.get( appearanceRef( entity ) );
					if ( resource ) resources.plan( [ resource.glb ] );
				}
			}
			// Baseline drops own residency before one-shot combat effects compete.
			output.commonReady = published.manifest === published.manifests.length && !!local &&
				output.displayed.has( local.gid );
			if ( gameplay?.localGid && output.commonReady ) {
				for ( const key of GOLD_DROP_MODELS ) {
					const model = published.dropModels[key];
					if ( model && !resources.ready( model.glb ) ) output.commonReady = false;
				}
			}
			probe?.detailEnd( "presentation-selection" );
			probe?.detailBegin( "presentation-events" );
			const { castByActor, castTokens, vitalsByGid, entitiesByGid } = stateIndex.update( entities, gameplay );
			const actionFrame = presentationActions.step(
				{
					entities,
					gameplay,
					seconds,
					simulationMs,
					local,
					entitiesByGid,
					castTokens,
					vitalsByGid,
					groundClocks,
					combatStanceEnds: presentationState.combatStanceEnds,
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
				},
				output,
				published
			);
			gameplay = actionFrame.gameplay;
			const { actionLayersByActor, waitingActors, triggers, soundContext } = actionFrame;
			if ( damageTexts.length ) damageTexts = damageTexts.filter( row => seconds - row.started <= 3 );
			for ( const event of gameplay?.environmentalDamage ?? [] ) {
				if ( event.sequence <= environmentalSequence ) continue;
				environmentalSequence = event.sequence;
				const target = entitiesByGid.get( event.gid ),
					at = simulationMs === undefined ? seconds : seconds + (event.atMs - simulationMs) / 1000;
				if ( !target || seconds - at >= 1 ) continue;
				if ( damageTexts.length >= 2048 ) throw Error( "Damage text capacity exceeded" );
				const native = logicalPose( target );
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
					const phase = presentationActions.actionClocks.get( trigger.cast.token )?.phases[phaseIndex];
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
					presentationState.combatStanceEnds.set(
						target.gid,
						Math.max(
							presentationState.combatStanceEnds.get( target.gid ) ?? -Infinity,
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
					if ( damageTexts.length >= 2048 ) throw Error( "Damage text capacity exceeded" );
					const anchor = posePresentation.pose( target.gid, native, seconds );
					damageTexts = appendDamageText(
						damageTexts,
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
			const active = new Set( entities.map( entity => entity.gid ) ), next = new Map<number, CharacterActor>();
			// Admission order owns idle RNG, before distance/camera presentation selection.
			const deadGids = new Set( gameplay?.vitals.filter( v => v.hp === 0 ).map( v => v.gid ) ?? [] );
			probe?.detailEnd( "presentation-events" );
			probe?.detailBegin( "presentation-state" );
			presentationState.step(
				{
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
				},
				output,
				published
			);
			probe?.detailEnd( "presentation-state" );
			probe?.detailBegin( "presentation-actors" );
			const { animationHolders, particleHolders } = actorPresentation.present( {
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
			} );
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
				const state = states.get( entity.gid ), resource = published.catalog.get( appearanceRef( entity ) );
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
			auxiliary.presentStages(
				{ orbActors, effectActors, next, seconds, animationHolders },
				{ renderer, resources, effects }
			);
			auxiliary.step(
				{ entities, seconds, next, gameplay, localMover, castByActor },
				{
					resourceFor,
					dress: published.dress,
					resources,
					ridesByRider: published.ridesByRider,
					riderModes: published.riderModes,
					committedAuxiliary
				}
			);
			for ( const gid of avatarOverrides.keys() ) if ( !active.has( gid ) ) avatarOverrides.delete( gid );
			sounds.retain( active );
			posePresentation.retain( active );
			selectCameraTarget( { local, gameplay, next, entitiesByGid }, output, published );
			applyCharacterVisibility(
				{ entities, next, local, gameplay, seconds, cameraPitch, concealmentSkills, entityLod },
				output
			);
			presentDisappearing(
				{ disappearing, seconds, next, animationDeltaMs, resources, animationHolders, particleHolders }
			);
			presentEmission(
				{ entities, next, seconds, hideSilkCos, particleHolders, animationHolders, frameWork },
				{ resources, renderer, modelEmission, animationEmission, scenery, entityLod, applySpawnFades }
			);
			publishCharacters(
				{ local, gameplay, next, seconds, blindHeld },
				output,
				published,
				{ renderer, resources, mallPreview }
			);
			probe?.detailEnd( "presentation-finalize" );
		},
		ready: ( gid: number ) => output.displayed.has( gid ),
		/*
		================
		entryReady

		Keep first-use baseline work behind world entry without spawning fake drops.
		================
		*/
		entryReady: () => output.commonReady && presentationActions.warm.warmMotions.length === 0 && effects.loaded(),
		previewReady: () => output.previewReady,
		dockReady: () => output.dockReady,
		/*
		================
		profile
		================
		*/
		profile( value: CharacterFrameProbe | undefined ) {
			probe = value;
		},
		cameraTarget: () => output.cameraTarget,
		takeCameraScripts: () => effects.takeCameraScripts(),
		orbGauge: () => orbs.gauge(),
		damageText: () => damageTexts as readonly import("@/engine/contracts/damage-text").DamageText[],
		error: () => output.failure ?? resources.error() ?? effects.error(),
		/*
		================
		simulationOrigin

		Frame-clock milliseconds of simulation time zero; local poses carry their
		simulation time (GameplayState.poseAtMs).
		================
		*/
		simulationOrigin( ms: number ) {
			posePresentation.origin( ms );
		},
		/*
		================
		reset
		================
		*/
		reset() {
			mallPreview.reset();
			skillObjects.reset();
			clearFootprints();
			animationDelta = createModifierDelta();
			presentationActions.warm.warmSkills = undefined;
			presentationActions.warm.warmBody = undefined;
			presentationActions.warm.warmMotions = [];
			output.commonReady = false;
			scenery.reset();
			entityLod.reset();
			modelEmission.reset();
			structureVisuals.reset();
			animationEmission.reset();
			auxiliary.resetStages();
			groundClocks.clear();
			selection.reset();
			stateIndex.reset();
			feedback.reset();
			environmentalSequence = 0;
			appearances.clear();
			auxiliary.resetHair();
			committedAuxiliary.clear();
			avatarOverrides.clear();
			auxiliaryActors.clear();
			posePresentation.reset();
			presentationState.idleStates.clear();
			presentationState.combatStanceEnds.clear();
			retiring.clear();
			disappearing.clear();
			spawnFades.clear();
			rideFades.clear();
			fadeSeen.clear();
			damageTexts = [];
			weather.reset();
			orbs.reset();
			output.previewReady = false;
			output.dockReady = false;
			dockPreview.reset();
			output.cameraTarget = null;
			output.cameraFade = null;
			effects.reset();
			referenceAppearances.reset();
			sounds.reset();
			resources.reset();
			states.clear();
			presentationActions.actionClocks.clear();
			presentationActions.predictedEvents.clear();
			presentationActions.deathFinalizes.clear();
			output.displayed.clear();
			displayedDependencies.clear();
			output.failure = null;
			renderer.setCharacterActors( [] );
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			mallPreview.reset();
			skillObjects.dispose();
			clearFootprints();
			presentationActions.warm.warmSkills = undefined;
			presentationActions.warm.warmBody = undefined;
			presentationActions.warm.warmMotions = [];
			scenery.reset();
			entityLod.reset();
			modelEmission.reset();
			structureVisuals.reset();
			animationEmission.reset();
			auxiliary.resetStages();
			groundClocks.clear();
			selection.reset();
			stateIndex.reset();
			feedback.reset();
			environmentalSequence = 0;
			appearances.clear();
			auxiliary.resetHair();
			committedAuxiliary.clear();
			avatarOverrides.clear();
			auxiliaryActors.clear();
			posePresentation.reset();
			presentationState.idleStates.clear();
			presentationState.combatStanceEnds.clear();
			retiring.clear();
			disappearing.clear();
			spawnFades.clear();
			rideFades.clear();
			fadeSeen.clear();
			damageTexts = [];
			weather.reset();
			orbs.reset();
			output.previewReady = false;
			output.dockReady = false;
			dockPreview.reset();
			output.cameraTarget = null;
			output.cameraFade = null;
			effects.dispose();
			referenceAppearances.reset();
			sounds.reset();
			resources.dispose();
			states.clear();
			presentationActions.actionClocks.clear();
			presentationActions.predictedEvents.clear();
			presentationActions.deathFinalizes.clear();
			output.displayed.clear();
			displayedDependencies.clear();
			published.dispose();
		}
	};
}
