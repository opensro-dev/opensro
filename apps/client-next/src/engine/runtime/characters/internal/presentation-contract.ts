/*
===========================================================================

presentation-contract.ts - the shapes character presentation's modules share

A type-only contract owned by characters.ts. Every module under
runtime/characters may import it; the modules never import each other. The
catalogue row types are what presentation-catalog.ts publishes and every
assembly, animation and sound phase reads.

===========================================================================
*/

import type { RandomIdle } from "@/engine/foundation/animation/random-idle";
import type { Posture } from "@/engine/foundation/animation/posture";
import type { Disappear } from "@/engine/foundation/animation/disappear";
import type { AnimationParticleSet } from "@/engine/foundation/animation/animation-emission";
import type {
	createModelAnimation,
	ModifierSelector,
	ModelAnimationBinding
} from "@/engine/foundation/animation/model-animation";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import type { StructureVisuals } from "@/engine/foundation/rendering/structure-stage";
import type { AnimationMetadata } from "@/engine/foundation/animation/animation-metadata";
import type { CharacterFade } from "@/engine/foundation/animation/character-fade";
import type { CharacterActor } from "@/engine/contracts/character";
import type { FollowCameraTarget } from "@/engine/contracts/scene";
import type { AnimationActivation } from "@/engine/foundation/animation/animation-activation";
import type { createAnimationDispatch } from "@/engine/foundation/animation/animation-dispatch";
import type { skillMotionResolveAnimation } from "@/engine/foundation/animation/skill-motion-resolve";
import type { LocomotionBlend } from "@/engine/foundation/animation/locomotion-blend";
import type { DressCatalog, SetEntry } from "@/engine/foundation/animation/equipment-appearance";
import type { AvatarOverrideSelection } from "@/engine/foundation/animation/avatar-override";
import type { GroundVisualClock } from "@/engine/foundation/animation/ground-visual";
import type { AppearanceChoice } from "@/engine/foundation/animation/reference-appearance";
import type { StatusView } from "@/engine/foundation/animation/status-presentation";
import type { CharacterSoundContext } from "@/engine/foundation/animation/sound-selectors";
import type { SkillLookup } from "@/engine/foundation/ui/buff-viewer";
import type { SkillMetadata } from "@/engine/foundation/gameplay/skill-catalog";
import type { AttachedEffect } from "@/engine/foundation/gameplay/attached-effects";
import type { CharacterAttachment, CharacterLayer, CharacterPointLight } from "@/engine/contracts/character";
import type { CastState, GameplayState, Pose, VitalState } from "@/engine/contracts/gameplay";
import type { EntityState, TransformSkin } from "@/engine/contracts/world";

/*
================
PresentationOutput

What one presentation frame publishes and several phases write: the drawn
actors, the dock, preview and catalogue readiness, the follow-camera target
and its fade, and the frame's failure. characters.ts owns the one instance
and passes it to each phase that writes it.
================
*/
export interface PresentationOutput {
	displayed: Map<number, CharacterActor>;
	failure: string | null;
	cameraTarget: FollowCameraTarget | null;
	cameraFade: ({ gid: number; time: number; } & CharacterFade) | null;
	previewReady: boolean;
	dockReady: boolean;
	commonReady: boolean;
}

/*
================
Resource
================
*/
export interface Resource {
	particleModifiers?: unknown;
	animationParticles?: readonly AnimationParticleSet[];
	animationParticlePaths?: readonly string[];
	animationBindings?: unknown;
	modifierSets?: unknown;
	modifierSelectors?: readonly ModifierSelector[];
	modifierBindings?: readonly ModelAnimationBinding[];
	ambientParticles?: readonly ModelParticle[];
	// The manifest's atstructeffect fields, read into structureVisuals.
	structureStages?: unknown;
	structureSounds?: unknown;
	structureDamageEffects?: unknown;
	structureVisuals?: StructureVisuals;
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
LinkedRide

A characterInfo "ride" BSR (skilleffect.txt section characterInfo, columns
Ride Type and ride), published as an npc manifest row of kind "ride".
================
*/
export interface LinkedRide {
	readonly glb: string;
	readonly clips: readonly string[];
}

/*
================
SecondaryModel

A separately loaded BSR with its own animation and modifier planes. Death
appearance overlays and child booth actors share this resource shape.
================
*/
export interface SecondaryModel {
	readonly glb: string;
	readonly clips: readonly string[];
	readonly animationStates?: Record<string, AnimationMetadata>;
	readonly ambientParticles?: readonly ModelParticle[];
	readonly animationParticles?: readonly AnimationParticleSet[];
	readonly animationParticlePaths?: readonly string[];
	readonly modifierBindings?: readonly ModelAnimationBinding[];
	readonly modifierSelectors?: readonly ModifierSelector[];
}

/*
================
DeathModel

CharacterInfo column 5 replaces the body's mesh on death (8E64F0) while
retaining its identity, sound profile and scale.
================
*/
export type DeathModel = SecondaryModel;

/*
================
ItemPresentation

One item's presentation row, keyed by its RefObjID (itemsByRefObjId).
================
*/
export interface ItemPresentation {
	codename: string;
	dropModelPath?: string;
	wornModelPath?: string | null;
}

/*
================
SoundRule

One effectsound row: the object/handle/skill/event key and the sound it plays.
================
*/
export interface SoundRule {
	readonly object: string;
	readonly handle: string;
	readonly event1: string;
	readonly skillId?: string;
	readonly event2?: string;
	readonly event3?: string;
	readonly publicPath?: string;
	readonly volume?: number;
	// effectsound column 7: triggers swallowed between two plays (8F9280).
	readonly skip?: number;
}

/*
================
DropModel

One ground-drop model from the item-drop manifest.
================
*/
export interface DropModel {
	glb: string;
	clips: readonly string[];
	clipLoop: boolean;
	particleModifiers?: unknown;
	ambientParticles?: readonly ModelParticle[];
}

/*
================
ActionInput

Only changes to these values request an action transition. Retain the values
instead of allocating and serializing the same key for every rendered actor.
================
*/
export interface ActionInput {
	readonly dead: boolean;
	readonly sitting: boolean;
	readonly stall: boolean;
	readonly mountedOn: number;
	readonly movementMode: number | undefined;
	readonly requestedMoving: boolean;
	readonly movementRevision: number;
	readonly posture: string;
	readonly waiting: boolean;
	readonly casting: boolean;
}

/*
================
CharacterPresentationState

One presented character's retained animation, action and posture state,
keyed by gid in characters.ts's states map.
================
*/
export interface CharacterPresentationState {
	modifierId: number;
	feedbackSettled?: boolean;
	modifierResource?: Resource;
	modifierLayers?: readonly import("@/engine/contracts/character").CharacterLayer[];
	activations?: Map<string, AnimationActivation>;
	dispatch?: ReturnType<typeof createAnimationDispatch>;
	modelAnimation?: ReturnType<typeof createModelAnimation>;
	castFacing?: import("@/engine/foundation/gameplay/cast-facing").CastFacing;
	navigationHold?: {
		revision: number;
		pose: import("@/engine/contracts/gameplay").Pose;
		mode: number | undefined;
	};
	actionRevision?: number;
	actionMode?: number;
	actionMask?: number;
	actionInput?: ActionInput;
	actionHeight?: { from: number; to: number; at: number; };
	dead?: boolean;
	sitting?: boolean;
	postureClip?: string;
	postureStarted?: number;
	stallMotion?: CharacterPresentationState["combatIdle"];
	combatIdle?: {
		body: Resource;
		metadata: Record<string, AnimationMetadata> | undefined;
		set: string;
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
}

/*
================
Auxiliary
================
*/
export type Auxiliary = { id: number; entry: SetEntry & { bone: string; clips: readonly string[]; }; };

/*
================
PresentationAppearance

A character's last assembled appearance: its resolved model, worn parts,
particles and auxiliary attachments, and the signature that decides reuse.
================
*/
export interface PresentationAppearance {
	resource: Resource;
	dress: DressCatalog;
	items: Record<string, ItemPresentation>;
	signature: string;
	defaultWear: readonly string[];
	particles: readonly ModelParticle[];
	parts: import("@/engine/contracts/character").CharacterAttachment[];
	auxiliary: readonly Auxiliary[];
	avatarIds: readonly number[];
	model: string;
	dependencies: readonly string[];
}

/*
================
PresentationIdleState

Persistent posture and idle timing shared by the state and actor phases.
================
*/
export interface PresentationIdleState {
	x: number;
	z: number;
	region: number;
	idle: RandomIdle;
	posture?: Posture;
	emoteRevision?: number;
	downDeath?: boolean;
	// 8E64F0, decided once on entering death: the body shows its death model,
	// and the death one-shot (motion 4) plays over deathLoop (0x24).
	deathEntered?: boolean;
	deathModel?: boolean;
	deathAction?: boolean;
	attachmentsHidden?: boolean;
}

/*
================
PresentationDisappear

Retired body and attachment presentation retained until its fade completes.
================
*/
export type PresentationDisappear = Disappear & {
	children?: readonly CharacterActor[];
	particles: readonly ModelParticle[];
	animation?: {
		resource: Resource;
		dispatch: ReturnType<typeof createAnimationDispatch>;
		selection: ReturnType<typeof createModelAnimation>;
	};
};

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
			boneRotation?: CharacterActor["boneRotation"];
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
	readonly entitiesByGid: ReadonlyMap<number, EntityState>;
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
ActorPass

What one presentation pass shares across its actors: the holders the frame
finishes with, and the local player found once per frame.
================
*/
export interface ActorPass {
	readonly animationHolders: { actor: CharacterActor; sets: readonly AnimationParticleSet[]; }[];
	readonly particleHolders: { actor: CharacterActor; particles: readonly ModelParticle[]; }[];
	readonly localEntity: EntityState | undefined;
}

/*
================
ActorAppearance

The appearance phase's result for one character: its resource and the
dependencies, auxiliary, particle, override and default-wear commits the
motion phase publishes with its actor.
================
*/
export interface ActorAppearance {
	readonly resource: Resource;
	readonly model: string;
	readonly dependencies: readonly string[];
	readonly auxiliaryCommit: readonly Auxiliary[] | undefined;
	readonly particleCommit: readonly ModelParticle[] | undefined;
	readonly overrideCommit: readonly number[] | undefined;
	readonly defaultWearCommit: readonly string[] | undefined;
}
