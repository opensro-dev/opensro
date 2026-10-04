/*
===========================================================================

world.ts - immutable entity state and ordered world-event contracts

Simulation publishes authoritative identity and pose. Presentation owns
rendering lifetimes and must not infer gameplay state from loaded models.

===========================================================================
*/
import type { Pose, GameplayState } from "./gameplay";
import type { CombatPresentationEvent } from "./effective-hp";
import type { VisualFeedback } from "./orb";
import type { ItemSoundRequest } from "./audio";
import type { MerchantBranch } from "@/engine/foundation/gameplay/merchant-branches";
import type { SpawnSkill } from "@/engine/foundation/gameplay/spawn-skills";
import type { UiSoundHandle } from "@/engine/foundation/ui/sound-catalog";

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
	readonly monsterSkin?: number;
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
	readonly guildName?: string;
	readonly guildId?: number;
	readonly guildGrantName?: string;
	readonly guildCrests?: readonly [number, number, number];
	readonly guildWarTeam?: number;
	readonly arenaTeam?: number;
	readonly appearanceState?: readonly number[];
	readonly groundItem?: {
		readonly typeFlags: number;
		readonly goldAmount: number;
		readonly ownerJid?: number;
		readonly tint: number;
		readonly appear?: number;
		readonly claimantGid?: number;
	};
	readonly ownerGid?: number;
	readonly ownerName?: string;
	readonly pvpState?: number;
	readonly pickupRevision?: number;
	readonly equipment?: readonly EntityEquipment[];
	readonly avatars?: readonly EntityEquipment[];
	readonly bodyShape?: number;
	readonly visualFlags?: number;
	readonly mountedOn?: number;
	readonly movementPath?: { readonly from: Pose; readonly to: Pose; };
	readonly movementRevision?: number;
	readonly moving?: boolean;
	/** Simulation time (ms) of the last stepped path sample; ClockSample.originMs maps it to wall time. */
	readonly poseAtMs?: number;
	readonly movementMode?: number;
	readonly walkSpeed?: number;
	readonly runSpeed?: number;
}

/*
================
WorldEvent

Events retain their wire order across worker batches. Native envelopes stay
available for packet families that do not yet own a semantic projection.
================
*/
export type WorldEvent =
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
