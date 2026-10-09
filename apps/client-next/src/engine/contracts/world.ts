/*
===========================================================================

world.ts - immutable entity state and ordered world-event contracts

Simulation publishes authoritative identity and pose. Presentation owns
rendering lifetimes and must not infer gameplay state from loaded models.

===========================================================================
*/
import type { Pose, GameplayState, MovementPath } from "./gameplay";
import type { CombatPresentationEvent } from "./effective-hp";
import type { VisualFeedback } from "./orb";
import type { ItemSoundRequest } from "./audio";
import type { MerchantBranch } from "@/engine/foundation/gameplay/merchant-branches";
import type { SpawnSkill } from "@/engine/foundation/gameplay/spawn-skills";
import type { UiSoundHandle } from "@/engine/foundation/ui/sound-catalog";

// EntityState.movementMode of a seated character: the 0x7017 posture request
// carries 4 to sit (gameplay.ts, native 6933A6 ground click and 692D19
// entity click). Presentation treats a seated character as neither idling
// nor walking.
export const MOVEMENT_MODE_SEATED = 4;

/*
================
WorldTravel

Revision separates successive travel operations with the same destination.
================
*/
export interface WorldTravel {
	readonly revision?: number;
	readonly mode: 0 | 1 | 2 | 3 | 4 | 5 | 6;
	readonly region: number;
}

/*
================
EntityEquipment
================
*/
export interface EntityEquipment {
	readonly slot: number;
	readonly refObjId: number;
	readonly typeFlags: number;
	readonly plus: number;
}

/*
================
TransformSkin

The msch-1 skin at CICharactor+294 (85C060). Revision distinguishes a new
application from a previously restored transformation.
================
*/
export interface TransformSkin {
	readonly refObjId: number;
	readonly player: boolean;
	readonly equipment: readonly EntityEquipment[];
	readonly revision: number;
}

/*
================
EntityState

Optional family records carry native spawn data without borrowing another
family's catalog identity. Skill objects resolve appearance by skill ID.
================
*/
export interface EntityState {
	readonly gid: number;
	readonly refObjId: number;
	readonly kind: string;
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
	readonly heading: number;
	readonly name: string;
	readonly skillObject?: { readonly skillId: number; readonly appear?: number; };
	readonly spawnAppearance?: number;
	readonly teleport?: { readonly radius: number; readonly height: number; readonly fortressId?: number; };
	readonly nameColor?: number;
	readonly holdType?: number;
	readonly merchantBranches?: readonly MerchantBranch[];
	readonly emote?: { readonly action: number; readonly revision: number; readonly atMs: number; };
	readonly attackFlags?: number;
	readonly tidWord?: number;
	readonly level?: number;
	readonly maxHp?: number;
	readonly countryByte9c?: number;
	readonly rarity?: number;
	readonly rarityAuxIcon?: number;
	// 861B00: a thief or hunter trade NPC's equipment variant (861720 indexes the trade equipment table).
	readonly tradeVariant?: number;
	// 4FA0B0: a fortress structure's hit points, event zone (RefEventStructID) and state word.
	readonly structureHp?: number;
	readonly eventStructId?: number;
	readonly structureState?: number;
	readonly spawnSkills?: readonly SpawnSkill[];
	readonly titleText?: string;
	readonly titleId?: number;
	readonly transformSkin?: TransformSkin;
	readonly cosAppearanceRefObjId?: number;
	readonly spawnDestination?: Pose;
	readonly jobType?: number;
	readonly jobGrade?: number;
	// The local player's job block (entry only): joined job, grade, exp, alias.
	readonly localJob?: import("@/engine/foundation/gameplay/job-guild").LocalJob;
	// The overhead dress bar (CICUser +0x780/+0x77C, 0x3434): its seconds and
	// the simulation time it started.
	readonly actionProgress?: { readonly seconds: number; readonly startedAtMs: number; };
	readonly guildName?: string;
	readonly guildId?: number;
	readonly guildGrantName?: string;
	readonly guildCrests?: readonly [number, number, number];
	readonly guildWarTeam?: number;
	readonly arenaTeam?: number;
	// The hover cursor's attack verdict for a player or a pet (6875F0), kept
	// by the worker: HOVER_ATTACK_PLAIN without Alt, HOVER_ATTACK_ALT with it
	// (foundation/gameplay/player-attack.ts).
	readonly hoverAttack?: number;
	readonly appearanceState?: readonly number[];
	readonly groundItem?: {
		readonly typeFlags: number;
		readonly goldAmount: number;
		readonly ownerJid?: number;
		readonly tint: number;
		readonly appear?: number;
		readonly claimantGid?: number;
		// The monster whose death published this drop: the server sends a
		// victim's drops right after its LIFE dead frame (the native death
		// credit, CGObjMob_CreditKillerOnDeath 4C42F0). The drop waits while
		// that death still waits for its killing hit.
		readonly dropperGid?: number;
	};
	readonly ownerGid?: number;
	readonly ownerName?: string;
	readonly pvpState?: number;
	readonly pickupRevision?: number;
	readonly equipment?: readonly EntityEquipment[];
	readonly avatars?: readonly EntityEquipment[];
	readonly bodyShape?: number;
	readonly visualFlags?: number;
	// CPSMission_OnTargetActionState0x314D (7786E0): a caught result put
	// SYSTEM_CAPTURE_MARK in this character's state-decoration slot.
	readonly captureMark?: boolean;
	readonly mountedOn?: number;
	readonly movementPath?: MovementPath;
	readonly movementRevision?: number;
	readonly movementTransition?: import("./gameplay").MovementTransition;
	readonly moving?: boolean;
	/** Simulation time (ms) of the last stepped path sample; ClockSample.originMs maps it to wall time. */
	readonly poseAtMs?: number;
	readonly movementMode?: number;
	readonly walkSpeed?: number;
	readonly runSpeed?: number;
	readonly animationRate?: number;
}

/*
================
WorldEvent

Lifecycle and feedback events retain their wire order across worker batches.
Queued gameplay snapshots may merge at the first snapshot's position until
a reset or batch handoff; consumers read gameplay after applying the whole
batch. Native envelopes stay available for packet families that do not yet
own a semantic projection.
================
*/
export type WorldEvent =
	| import("./movement-diagnostic").MovementDiagnostic
	| CombatPresentationEvent
	| VisualFeedback
	| { readonly kind: "travel"; readonly travel: WorldTravel; }
	| { readonly kind: "ui-sound"; readonly handle: UiSoundHandle; readonly at: number; }
	| { readonly kind: "gameplay"; readonly state: GameplayState; }
	| { readonly kind: "synchronized"; readonly epoch: number; }
	| { readonly kind: "reset"; readonly epoch: number; }
	| { readonly kind: "item-sound"; readonly cue: ItemSoundRequest; readonly at: number; }
	| { readonly kind: "spawn"; readonly entity: EntityState; }
	| { readonly kind: "state"; readonly entity: EntityState; }
	| { readonly kind: "despawn"; readonly gid: number; }
	| { readonly kind: "native"; readonly opcode: number; readonly payload: Uint8Array; }
	| { readonly kind: "bootstrap"; readonly value: unknown; };

/*
================
WorldBatch

Sequence establishes publication order independently of renderer frames.
================
*/
export interface WorldBatch {
	readonly sequence: number;
	readonly events: readonly WorldEvent[];
}
