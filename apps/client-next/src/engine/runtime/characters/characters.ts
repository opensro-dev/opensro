/*
===========================================================================

characters.ts - character assemblies and their presentation lifetime

Owns resource admission, actor assembly, animation and effects for roster
previews and world entities. Readiness belongs to each required assembly;
scenery and auxiliary actors cannot satisfy a player readiness barrier.

===========================================================================
*/

import { createStatusOwner } from "@/engine/foundation/animation/status-presentation";
import type { AvatarOverrideSelection } from "@/engine/foundation/animation/avatar-override";
import { createAnimationEmission } from "@/engine/foundation/animation/animation-emission";
import { createEntityLod } from "@/engine/foundation/animation/entity-lod";
import { createModifierDelta } from "@/engine/foundation/rendering/modifier-delta";
import { createPresentationIds } from "@/engine/foundation/animation/presentation-ids";
import { createModelEmission } from "@/engine/foundation/animation/model-emission";
import { createStructureVisuals } from "./structure-visuals";
import type { GroundVisualClock } from "@/engine/foundation/animation/ground-visual";
import { createSceneryEmission } from "@/engine/foundation/animation/scenery-emission";
import { createReferenceAppearances } from "@/engine/foundation/animation/reference-appearance";
import { createDamageFeedback } from "./damage-feedback";
import { skillLookup, type SkillLookup } from "@/engine/foundation/ui/buff-viewer";
import { createPosePresentation } from "./pose-presentation";
import { createPresentationSamples } from "./presentation-samples";
import { createPresentationState } from "./presentation-state";
import { createPresentationActions } from "./presentation-actions";
import { createPresentationEvents } from "./presentation-events";
import { createFootprints } from "./footprints";
import { createCharacterStateIndex } from "./state-index";
import { createSkillObjects, SKILL_OBJECT_MANIFESTS } from "./skill-objects";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import { createOrbs } from "./orbs/orbs";
import { CHARACTER_ACTORS } from "@/engine/foundation/animation/character-budget";
import { createCharacterSelection } from "@/engine/foundation/animation/character-selection";
import { createCharacterEffects } from "./effects/effects";
import { createCharacterSounds } from "./sounds/sounds";
import { createCharacterResources } from "./resources/resources";
import { createMallPreview } from "./mall-preview";
import type { CharacterRecord } from "@/engine/contracts/session";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Renderer } from "@/engine/contracts/runtime";
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { CharacterActor } from "@/engine/contracts/character";
import { createPresentationCatalog } from "./presentation-catalog";
import { createActorPresentation } from "./actor-presentation";
import { createDockPreview } from "./dock-preview";
import { createPresentationWeather, type WeatherLifecycleEvent } from "./presentation-weather";
import { createSpawnFades } from "./spawn-fades";
import { createAppearanceLookup } from "./appearance-lookup";
import { uncensoredShard } from "@/engine/foundation/animation/default-wear-policy";
import { initialCameraPitch } from "@/engine/foundation/rendering/camera-options";
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
	PresentationOutput
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

/*
================
TeardownMode

reset clears the presenter for the next world and keeps it usable; dispose
retires it for good. Both run the one ordered teardown.
================
*/
type TeardownMode = "reset" | "dispose";

const GOLD_DROP_MODELS = [
	"item/etc/drop_ch_money_ing.bsr",
	"item/etc/drop_ch_money_small.bsr",
	"item/etc/drop_ch_money_normal.bsr",
	"item/etc/drop_ch_money_large.bsr"
] as const;
// orbs.step's full detail: every orb flies. At 1 or below orbs are consumed
// without visuals.
const ORB_DETAIL_FULL = 3;

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
	const footprints = createFootprints( renderer, soundSurface );
	const groundClocks = new Map<number, GroundVisualClock & { duration: number; modifierId: number; }>();
	const referenceAppearances = createReferenceAppearances( () => random.range( 0, 32768 ) );
	const presentationState = createPresentationState();
	const presentationEvents = createPresentationEvents();
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
		sounds = createCharacterSounds( play, random.range );
	const published = createPresentationCatalog( { resources, sounds, referenceAppearances } );
	const effects = createCharacterEffects(
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
	const posePresentation = createPosePresentation();
	const selection = createCharacterSelection( CHARACTER_ACTORS );
	const stateIndex = createCharacterStateIndex();
	const presentationActions = createPresentationActions();
	const { appearanceRef, activeSkin, wornEquipment, resourceFor } = createAppearanceLookup(
		{
			referenceAppearances,
			published,
			structureVisuals,
			deathShown: gid => presentationState.idleStates.get( gid )?.deathModel === true
		}
	);
	const dockPreview = createDockPreview( { renderer, resources, scenery, lizardGid } );
	const displayedDependencies = new Map<number, readonly string[]>();
	// Appearance topology is independent of pose time. Revalidate resource
	// readiness each frame, but derive garment/cosmetic parts only on change.
	// characterInfo rides: each live rider's presentation-owned ride actor
	// (CICMonster_DeserializeSpawnPacket); the ride models are published.ridesByRider.
	const linkedRides = auxiliary.linkedRides;
	const spawnFades = createSpawnFades( linkedRides );
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
		footContact: footprints.footContact,
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
	/*
	================
	teardown

	The one ordered teardown that reset and dispose share. A reset keeps the
	presenter usable for the next world; a dispose retires it. The order is
	the original one, statement for statement: the owners are independent,
	but nothing here is reordered without its own proof. Only skill objects,
	effects and resources dispose instead of resetting, and only a reset
	renews the modifier delta and drops common readiness.

	The partial resets on a world reset event (receiveLifecycle) and its
	replay (eventRain) are a different trigger and stay where they are.
	================
	*/
	function teardown( mode: TeardownMode ) {
		mallPreview.reset();
		if ( mode === "reset" ) skillObjects.reset();
		else skillObjects.dispose();
		footprints.clearFootprints();
		if ( mode === "reset" ) animationDelta = createModifierDelta();
		presentationActions.resetWarm();
		if ( mode === "reset" ) output.commonReady = false;
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
		presentationEvents.resetSequence();
		appearances.clear();
		auxiliary.resetHair();
		committedAuxiliary.clear();
		avatarOverrides.clear();
		auxiliary.resetChildren();
		posePresentation.reset();
		presentationState.reset();
		retiring.clear();
		disappearing.clear();
		spawnFades.reset();
		presentationEvents.resetDamageTexts();
		weather.reset();
		orbs.reset();
		output.previewReady = false;
		output.dockReady = false;
		dockPreview.reset();
		output.cameraTarget = null;
		output.cameraFade = null;
		if ( mode === "reset" ) effects.reset();
		else effects.dispose();
		referenceAppearances.reset();
		sounds.reset();
		if ( mode === "reset" ) resources.reset();
		else resources.dispose();
		states.clear();
		presentationActions.resetClocks();
		output.displayed.clear();
		displayedDependencies.clear();
	}
	// ============================================================================
	// The frame phases step runs, in order. Each body is the original step code.
	/*
	================
	beginFrame

	Opens the frame: entity LOD from where the local player is drawn, the
	animation delta, the resource budget and the pose samples, then one
	admitted catalogue result and the next catalogue request.
	================
	*/
	function beginFrame(
		entities: readonly EntityState[],
		gameplay: GameplayState | null,
		seconds: number,
		dock: readonly CharacterRecord[] | undefined,
		preview: import("@/engine/contracts/frontend").CreationSnapshot | null | undefined
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
		return { animationDeltaMs, localMover, logicalPose, samples };
	}
	/*
	================
	retireDespawned

	Turns each despawned character into a fading copy (with its particles,
	animation and children) under a fresh actor id, forgets the despawned
	gids, and drops fading copies older than 1.5 seconds.
	================
	*/
	function retireDespawned( seconds: number ) {
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
	}
	/*
	================
	selectPresented

	Chooses the frame's presented entities and plans their resident models
	before any transient effect asks for the budget, then decides whether
	the common baseline (catalogues, local player, gold drops) is ready.
	================
	*/
	function selectPresented( entities: readonly EntityState[], gameplay: GameplayState | null ) {
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
				if ( !model ) {
					output.commonReady = false;
					output.failure ??= `Missing world-entry drop model: ${key}`;
					continue;
				}
				if ( !resources.ready( model.glb ) ) output.commonReady = false;
			}
		}
		probe?.detailEnd( "presentation-selection" );
		return { sampleActorDetails, local, selected };
	}
	/*
	================
	prepareEvents

	Indexes the delivered state, advances the action clocks and adopts their
	gameplay, then turns the frame's casts, hits and deaths into effects.

	The action phase returns a replaced gameplay snapshot: the same state with
	cast cancellation times adopted ({ ...gameplay, casts }): the cast objects
	are replaced, but their length, order, tokens and casters are unchanged. The pose samples and indexes built from
	the delivered snapshot stay valid for it, so every later phase reads the
	returned one, as the original single step did.
	================
	*/
	function prepareEvents(
		input: {
			entities: readonly EntityState[];
			gameplay: GameplayState | null;
			seconds: number;
			simulationMs: number | undefined;
			effectDetail: number;
			bloodEnabled: boolean;
		},
		begun: ReturnType<typeof beginFrame>,
		presented: ReturnType<typeof selectPresented>
	) {
		const { entities, seconds, simulationMs, effectDetail, bloodEnabled } = input;
		let { gameplay } = input;
		const { localMover, logicalPose, samples } = begun;
		const { local } = presented;
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
		const { hitByActor, effectActors, pendingDeaths } = presentationEvents.step(
			{
				entities,
				gameplay,
				seconds,
				simulationMs,
				entitiesByGid,
				actionClocks: presentationActions.actionClocks,
				combatStanceEnds: presentationState.combatStanceEnds,
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
			},
			output,
			published
		);
		const active = new Set( entities.map( entity => entity.gid ) ), next = new Map<number, CharacterActor>();
		// Admission order owns idle RNG, before distance/camera presentation selection.
		const deadGids = new Set( gameplay?.vitals.filter( v => v.hp === 0 ).map( v => v.gid ) ?? [] );
		probe?.detailEnd( "presentation-events" );
		return {
			gameplay,
			castByActor,
			vitalsByGid,
			entitiesByGid,
			actionLayersByActor,
			waitingActors,
			soundContext,
			hitByActor,
			effectActors,
			pendingDeaths,
			active,
			next,
			deadGids
		};
	}
	/*
	================
	presentActors

	The posture and idle state of every entity, then every selected entity's
	actor: appearance, animation layers and sounds, written into next.
	================
	*/
	function presentActors(
		input: {
			entities: readonly EntityState[];
			seconds: number;
			simulationMs: number | undefined;
			nativeServerName: string | undefined;
			normalFortressClothes: boolean;
			uncensored: boolean;
		},
		begun: ReturnType<typeof beginFrame>,
		presented: ReturnType<typeof selectPresented>,
		events: ReturnType<typeof prepareEvents>
	) {
		const { entities, seconds, simulationMs, nativeServerName, normalFortressClothes, uncensored } = input;
		const { animationDeltaMs, localMover, logicalPose } = begun;
		const { sampleActorDetails, selected } = presented;
		const {
			gameplay,
			castByActor,
			vitalsByGid,
			actionLayersByActor,
			waitingActors,
			soundContext,
			hitByActor,
			pendingDeaths,
			active,
			next,
			deadGids,
			entitiesByGid
		} = events;
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
				active,
				pendingDeaths,
				uncensored
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
			entitiesByGid,
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
		return { animationHolders, particleHolders };
	}
	/*
	================
	finishFrame

	Retires the state of characters that left, settles finished deaths, adds
	orbs, stage effects and auxiliary actors, then the original finalization
	order: camera target, visibility, fading copies, emission, publication.
	================
	*/
	function finishFrame(
		input: { entities: readonly EntityState[]; seconds: number; cameraPitch: number; blindHeld: boolean; },
		begun: ReturnType<typeof beginFrame>,
		presented: ReturnType<typeof selectPresented>,
		events: ReturnType<typeof prepareEvents>,
		actors: ReturnType<typeof presentActors>
	) {
		const { entities, seconds, cameraPitch, blindHeld } = input;
		const { animationDeltaMs, localMover } = begun;
		const { local } = presented;
		const { gameplay, active, next, castByActor, entitiesByGid, effectActors } = events;
		const { animationHolders, particleHolders } = actors;
		probe?.detailBegin( "presentation-finalize" );
		for ( const gid of states.keys() ) {
			if ( !active.has( gid ) ) {
				states.delete( gid );
				displayedDependencies.delete( gid );
			}
		}
		footprints.step( seconds );
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
		// The socket callback runs only inside orbs.step, while next is fixed:
		// build its actor list once, on the first orb that needs a socket.
		let socketActors: CharacterActor[] | null = null;
		const orbActors = orbs.step(
			entities,
			seconds,
			settled,
			( gid, bone ) => renderer.characterSocket( socketActors ??= [ ...next.values() ], gid, bone, [ 0, 0, 0 ] ),
			resources.ready,
			resources.duration,
			ORB_DETAIL_FULL,
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
			{
				resources,
				renderer,
				modelEmission,
				animationEmission,
				scenery,
				entityLod,
				applySpawnFades: spawnFades.apply
			}
		);
		publishCharacters(
			{ local, gameplay, next, seconds, blindHeld },
			output,
			published,
			{ renderer, resources, mallPreview }
		);
		probe?.detailEnd( "presentation-finalize" );
	}
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
					presentationState.clearStances();
					modelEmission.reset();
					structureVisuals.reset();
					animationEmission.reset();
					auxiliary.resetStages();
					groundClocks.clear();
					retiring.clear();
					disappearing.clear();
					spawnFades.reset();
					next.length = 0;
					next.push( { kind: "reset" } );
				} else if ( event.kind === "spawn" || event.kind === "state" ) {
					if ( event.kind === "spawn" ) {
						presentationState.forgetStance( event.entity.gid );
						// A respawn under a live gid is a new CICharactor: fade it again.
						spawnFades.respawn( event.entity.gid );
					}
					next.push( { kind: event.kind, gid: event.entity.gid, refObjId: event.entity.refObjId } );
				} else if ( event.kind === "despawn" ) {
					presentationState.forgetStance( event.gid );
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
				presentationState.clearStances();
				modelEmission.reset();
				structureVisuals.reset();
				animationEmission.reset();
				auxiliary.resetStages();
				groundClocks.clear();
				retiring.clear();
				disappearing.clear();
				presentationEvents.resetDamageTexts();
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
			cameraPitch = initialCameraPitch(),
			dock?: readonly CharacterRecord[],
			preview?: import("@/engine/contracts/frontend").CreationSnapshot | null,
			lizard = false,
			effectDetail = 2,
			blindHeld = false,
			nativeServerName?: string,
			normalFortressClothes = false
		) {
			const begun = beginFrame( entities, gameplay, seconds, dock, preview );
			if ( dockPreview.step( { seconds, dock, preview, lizard, nativeServerName }, output, published ) ) return;
			retireDespawned( seconds );
			const presented = selectPresented( entities, gameplay );
			// GameConfig +0x12E (745D10) forces the death model (8E655D) on an
			// uncensored Korean shard. It also picks the authored blood over the
			// green one (8D5631); deliberate deviation (owner, 2026-10-08): every
			// shard shows the authored blood.
			const uncensored = uncensoredShard( published.dress.defaultWearLanguage ?? 4, nativeServerName );
			const events = prepareEvents(
				{ entities, gameplay, seconds, simulationMs, effectDetail, bloodEnabled: true },
				begun,
				presented
			);
			const actors = presentActors(
				{ entities, seconds, simulationMs, nativeServerName, normalFortressClothes, uncensored },
				begun,
				presented,
				events
			);
			finishFrame( { entities, seconds, cameraPitch, blindHeld }, begun, presented, events, actors );
		},
		/*
		================
		uncensored

		GameConfig +0x129/+0x12D/+0x12E for the login shard (745D10): the
		client language with the shard name's #$T marker. Before a shard is
		chosen there is none, and nothing reads the flags.
		================
		*/
		uncensored( nativeServerName: string | undefined ) {
			return nativeServerName !== undefined &&
				uncensoredShard( published.dress.defaultWearLanguage ?? 4, nativeServerName );
		},
		ready: ( gid: number ) => output.displayed.has( gid ),
		/*
		================
		entryReady

		Keep first-use baseline work behind world entry without spawning fake drops.
		================
		*/
		entryReady: () => output.commonReady && presentationActions.warmed() && effects.loaded(),
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
		damageText: () =>
			presentationEvents.state.damageTexts as readonly import("@/engine/contracts/damage-text").DamageText[],
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
			teardown( "reset" );
			output.failure = null;
			renderer.setCharacterActors( [] );
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			teardown( "dispose" );
			published.dispose();
		}
	};
}
