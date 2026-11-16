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
import type { AuxiliaryEntry, DressCatalog, SetEntry } from "@/engine/foundation/animation/equipment-appearance";
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
import type { Degradation } from "@/engine/contracts/network";

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
	// The thrown value behind failure, when a phase caught one: the
	// incident report needs its stack, the HUD line only its text (#2559).
	failureCause: unknown;
	cameraTarget: FollowCameraTarget | null;
	cameraFade: ({ gid: number; time: number; } & CharacterFade) | null;
	previewReady: boolean;
	dockReady: boolean;
	commonReady: boolean;
	// Skipped cosmetic steps the session survives (a model without a
	// timeline for this action): reported once each as degraded, never a
	// failure. recordDegradation (foundation/session/degradation-record.ts) is the one writer; takePresentationDegradations
	// (characters.ts) drains them.
	degradations: Degradation[];
	degradedKeys: Set<string>;
}

/*
================
ClipFacts

What presentation reads of a body's clip list for every actor every
frame, derived once when the catalogue admits the row (presentation-
catalog.ts clipFacts): the names as a set, and the clip each role takes
among the names a body may hold. A row's clip list is never edited after
admission; a manifest admitted later makes new rows with new facts.
================
*/
export interface ClipFacts {
	readonly names: ReadonlySet<string>;
	// The base clip when the body lacks its role: stand, else its first clip.
	readonly fallback: string;
	// 8E64F0's death from the down state 4: deathquick, else downdie, else death.
	readonly quickDeath: string;
	// The death base loop: deathLoop, else deathloop, else death.
	readonly deathLoop: string;
	// Whether the body holds either death loop (presentation-state enterDeath).
	readonly loopsDeath: boolean;
	readonly sitting: string;
	readonly sitDown: string;
	readonly standUp: string;
	// The hit reaction: hit1, else hit, else none ("").
	readonly hit: string;
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
	// Derived from clips at admission; a row built from another keeps both.
	clipFacts: ClipFacts;
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
	// A death model's facts replace the body's with its clips (resourceFor).
	readonly clipFacts: ClipFacts;
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
	// The packed type word (TypeID1..4, buildMissionPresentationAsset.mjs).
	typeFlags?: number;
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
	// The drawn yaw (heading words) a finished cast facing left behind:
	// 8DC440 wrote it (CICharactor_SetYaw), so the body turns back from it
	// to the drawn pose's own yaw at 86CBA0's rate until they meet. `at`
	// is the frame clock the turn last advanced at.
	castYawRelease?: { angle: number; at: number; };
	navigationHold?: {
		revision: number;
		pose: import("@/engine/contracts/gameplay").Pose;
		logical: import("@/engine/contracts/gameplay").Pose;
		mode: number | undefined;
	};
	// The pose actually drawn on the previous frame. A server burst that
	// stops the walk and starts the cast together (0xB2F5 with 0xB245) has
	// already spent its correction inside posePresentation by the time
	// actor-motion sees the cast, so the WAIT hold takes this pose, not
	// the corrected one the same frame's pose() returned.
	lastDrawnPose?: import("@/engine/contracts/gameplay").Pose;
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
	// The last steady decision and the inputs it was taken from
	// (MotionRetained); undefined after any frame that was not steady.
	motion?: MotionRetained;
	// The last steady resolve (actor-appearance.ts) and its inputs
	// (AppearanceRetained); undefined after any resolve that was not steady.
	appearance?: AppearanceRetained;
	// The actor the last steady decision published and the inputs of its
	// fields (KeptRecord); undefined after any frame that was not steady.
	record?: KeptRecord;
}

/*
================
StampScratch

A stamp an owner fills in place each frame, one object for every entity:
what a stage retains is a copy of it.
================
*/
export type StampScratch<T> = { -readonly [K in keyof T]: T[K]; };

/*
================
MotionStamp

What a steady decide (actor-motion.ts) read, as values and identities: a
frame whose stamp equals the retained one's would decide the same again.
The entity row stands for every row field decide reads: the world
projection replaces a row on any change and freezes it (presentation.ts).
The published catalogue's epoch stands for its animation states, motion
URLs and dress. Each other term is one read of an owner decide makes.
================
*/
export interface MotionStamp {
	readonly entity: EntityState;
	readonly glb: string;
	readonly codename: string;
	readonly clips: readonly string[];
	readonly clipFacts: ClipFacts;
	readonly animationStates: Record<string, AnimationMetadata> | undefined;
	// The appearance's avatar commit; every empty commit is one value.
	readonly commit: readonly number[] | undefined;
	readonly manifest: number;
	readonly hpZero: boolean;
	readonly abnormal: number;
	readonly healthDead: boolean;
	readonly pendingDeath: boolean;
	// The token of a hit with damage, the only hit that changes the state.
	readonly hitToken: string | undefined;
	readonly cast: CastState | undefined;
	readonly waiting: boolean;
	readonly actionLayers: number;
	readonly hostMotions: number;
	readonly stance: boolean;
	readonly posture: Posture | undefined;
	// An idle fidget is set on a clip the body holds.
	readonly idle: boolean;
	readonly poseMoving: boolean;
	readonly weaponSet: string | undefined;
	readonly armed: boolean;
	readonly local: boolean;
}

/*
================
MotionRetained

A steady decision, its stamp, and the presentation frame it was last
presented at: it stands for decide only on the very next frame, so a
frame the entity was not presented (unselected, not resolved, a throw)
ends it.
================
*/
export interface MotionRetained {
	readonly decision: MotionDecision;
	readonly stamp: MotionStamp;
	frame: number;
}

/*
================
MotionDue

Whether decide must run for an entity this frame. retained and stamp are
both given only when a steady decision from the previous frame exists;
the production rule is presentation-due.ts motionDue, and a test passes
an always-due rule to present every entity in full.
================
*/
export type MotionDue = ( retained: MotionRetained | undefined, stamp: MotionStamp | undefined ) => boolean;

/*
================
AppearanceStamp

What a steady resolve (actor-appearance.ts) read besides the residency it
asks again every frame: the entity row (every row field it reads), the
published catalogue's epoch (catalog, dress, items and item ids), the
resource resourceFor answers (its skin, disguise, death model, stage and
variant), and the reads of the equipment dress: the active skin, the
hidden weapon, the emptied hands, the fortress clothes' inputs and the
server's dress rules.
================
*/
export interface AppearanceStamp {
	readonly entity: EntityState;
	readonly manifest: number;
	readonly resource: Resource | undefined;
	readonly disguise: AppearanceChoice | undefined;
	readonly skin: TransformSkin | undefined;
	readonly local: boolean;
	readonly weaponHidden: boolean;
	readonly emoteHidden: boolean;
	readonly dead: boolean;
	// The fortress clothes last shown (fortressAppearance's own input).
	readonly fortressIndex: number | undefined;
	// The local player's arena team, -1 without a local player.
	readonly localTeam: number;
	readonly normalFortressClothes: boolean;
	readonly fortress: GameplayState["fortress"];
	readonly social: GameplayState["social"];
	readonly serverName: string | undefined;
	readonly enchantSparks: boolean;
	readonly jobSuitHidesArmor: boolean;
	readonly moonlitThiefSuit: boolean;
}

/*
================
AppearanceRetained

A steady resolve's result and what it asked the resource owner, in its
order: the body, its particle programs, then the worn parts (parts) and
the assembly it set (assembly). A skipped resolve asks the same again, so
the cache order, the wanted and planned sets and every retry stay as a
resolve leaves them; it stands only on the next frame, like
MotionRetained.
================
*/
export interface AppearanceRetained {
	readonly result: ActorAppearance;
	readonly stamp: AppearanceStamp;
	readonly parts: readonly string[] | undefined;
	readonly assembly: readonly CharacterAttachment[] | undefined;
	frame: number;
}

/*
================
AppearanceDue

Whether resolve must run for an entity this frame, given as MotionDue is.
================
*/
export type AppearanceDue = (
	retained: AppearanceRetained | undefined,
	stamp: AppearanceStamp | undefined
) => boolean;

/*
================
PresentationDue

The rules that decide when an entity's appearance is resolved and its
motion decided again (presentation-due.ts); a test passes always-due ones.
================
*/
export interface PresentationDue {
	readonly appearance: AppearanceDue;
	readonly motion: MotionDue;
	readonly record: RecordDue;
}

/*
================
RecordStamp

What the published actor of a steady decision (actor-advance.ts) reads
beyond the decision and the resolved appearance, as values and
identities: the catalogue's epoch and the two lookups a rejected manifest
can leave written without one (shadow size, blood effects), the local
player, the effects owner's appearance (one object while its tint,
light, bone rotation and scale hold), the
level of detail and its optional test, the drawn pose, the action height,
the pet resolve's stack window and the modifier owner.
================
*/
export interface RecordStamp {
	readonly resolved: ActorAppearance;
	readonly manifest: number;
	readonly localGid: number | undefined;
	readonly shadowSize: number | undefined;
	readonly bloodEffects: readonly [string | null, string | null] | undefined;
	readonly appearance: ReturnType<ActorOwner["effects"]["appearance"]>;
	readonly fraction: number;
	readonly crowded: boolean;
	readonly optional: boolean;
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
	readonly yaw: number;
	readonly height: number | undefined;
	readonly petResolve: boolean;
	readonly modifierId: number;
}

/*
================
KeptRecord

The actor a steady decision last published, frozen, with its stamp and
the retained decision it was published from: a frame that reuses that
decision with the same stamp publishes it again with only its clocks
(time, layers, model animation) advanced.
================
*/
export interface KeptRecord {
	readonly actor: CharacterActor;
	readonly stamp: RecordStamp;
	readonly retained: MotionRetained | undefined;
}

/*
================
RecordDue

Whether advance must build the actor again, given as MotionDue is: the
kept record and this frame's stamp, both only when the decision was
reused from the frame that published it.
================
*/
export type RecordDue = ( kept: KeptRecord | undefined, stamp: RecordStamp | undefined ) => boolean;

/*
================
Auxiliary
================
*/
export type Auxiliary = { id: number; entry: AuxiliaryEntry; };

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
		// The same object while every field holds (effects.ts).
		appearance( gid: number ): Readonly<{
			weaponHidden: boolean;
			materialTint: readonly [number, number, number] | undefined;
			scale: number;
			pointLight?: CharacterPointLight;
			boneRotation?: CharacterActor["boneRotation"];
		}>;
		hostMotions(
			gid: number
		): readonly Readonly<
			{ key: string; started: number; stoppedAt: number | undefined; set: string; id: number; }
		>[];
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
		carry( gid: number, displayed: Pose ): void;
	};
	readonly published: {
		readonly catalog: ReadonlyMap<number, Resource>;
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
		// Port-only, not native: the Experimental enchant sparks; absent is off.
		enchantSparks?(): boolean;
		// Port-only, not native: the Experimental silent wing flight; absent is off.
		silentWingFlight?(): boolean;
	};
	readonly resourceFor: ( entity: EntityState ) => Resource | undefined;
	readonly resources: {
		animation( body: string, name: string, path: string ): boolean;
		duration( path: string, clip: string ): number;
		/** True while the source waits out a failure's retry. */
		failed( path: string ): boolean;
		plan( paths: readonly string[] ): boolean;
		prioritize( paths: readonly string[] ): void;
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
	readonly due: PresentationDue;
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
	// The frame's drawn poses, resolved once in presentPoses (#1442c).
	readonly drawnPoses: ReadonlyMap<number, Pose>;
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
MotionDecision

What actor-motion.ts decide hands advance (actor-advance.ts) for one
actor this frame: its retained state, the layers to play at this frame's
time, the overlay installations decide bound, and the choices the
published actor is built from.
================
*/
export interface MotionDecision {
	readonly state: CharacterPresentationState;
	readonly nativePose: Pose;
	readonly renderPose: Pose;
	// Advance writes each dispatched layer back in place.
	readonly layers: CharacterLayer[];
	// The overlay installations bound this frame; advance drops the others.
	readonly activationKeys: ReadonlySet<string>;
	readonly installations: Map<string, AnimationActivation>;
	readonly cast: CastState | undefined;
	readonly dead: boolean;
	readonly authorityDead: boolean;
	readonly seat: number | undefined;
	readonly clip: string;
	readonly looping: boolean;
	readonly posture: Posture | undefined;
	readonly armedIdle: string;
	// The action mask after this frame's transitions (0x200: state 9, feet).
	readonly actionMask: number;
	// decide changed no retained state and nothing it decided depends on
	// the clock but the locomotion layers: with the same stamp next frame
	// it would decide the same, so advance may run on this decision again.
	readonly steady: boolean;
	// This frame reused the retained decision instead of deciding.
	readonly reused: boolean;
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
