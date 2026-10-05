/*
===========================================================================

gameplay.ts - commands and immutable world gameplay publications

Simulation owns these values. UI and presentation consume snapshots and
submit intents; they never mutate inventory, learned ranks or server state.
Optional publication fields retain their previous value until a full reset.

===========================================================================
*/

/*
================
WorldClockSeed

The receipt time anchors the native clock between server publications.
================
*/
export interface WorldClockSeed {
	readonly day: number;
	readonly hour: number;
	readonly minute: number;
	readonly receivedAtMs: number;
}
// Native 3498 / 30EA and enter-world section 3 (75C0F0 / 8673D0).
// This is the NPC-marker registry, distinct from journal minimap tracking.
/*
================
QuestMarker

Preserves the native NPC marker payload independently of journal tracking.
================
*/
export interface QuestMarker {
	readonly refId: number;
	readonly flags: number;
	readonly valueA: number;
	readonly word: number;
	readonly tail6: readonly number[];
	readonly optional?: number;
}
/*
================
QuestRecord

Authored contents and target identities are server-owned journal state.
================
*/
export interface QuestRecord {
	readonly refId: number;
	readonly u08: number;
	readonly u09: number;
	readonly flags: number;
	readonly progress?: number;
	readonly u10?: number;
	readonly contents: readonly {
		readonly tag: number;
		readonly kind: number;
		readonly description: string;
		readonly objectiveSentinel: boolean;
		readonly objectiveValues: readonly number[];
	}[];
	readonly targetIds: readonly number[];
}
/*
================
QuestProgressEvent

One sequenced before/after pair drives journal progress presentation.
================
*/
export interface QuestProgressEvent {
	readonly sequence: number;
	readonly refId: number;
	readonly before: QuestRecord["contents"][number];
	readonly after: QuestRecord["contents"][number];
}
/*
================
QuestGathering

36BD carries a quest identity and seconds. The worker receipt clock anchors
the short native collection gauge independently of journal minute timers.
================
*/
export interface QuestGathering {
	readonly refId: number;
	readonly startedAtMs: number;
	readonly durationMs: number;
}
/*
================
Pose

Coordinates remain local to the native region, with height on the Y axis.
================
*/
export interface Pose {
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
	readonly angle: number;
}
/*
================
ChatLine

Outgoing intent and received chat share display data, not delivery status.
================
*/
export interface ChatLine {
	readonly sequence?: number;
	readonly channel: number;
	readonly name: string;
	readonly gid?: number;
	readonly text: string;
	readonly outgoing: boolean;
	// A line from the server's replayed transcript (OpChatHistory): already
	// said before this session, so never speech over a head.
	readonly history?: boolean;
}
/*
================
GameplayCommand

Closed intent vocabulary accepted by the simulation command owner. Server
operations carry identities and choices, never client-calculated rewards.
================
*/
export type GameplayCommand =
	| { readonly kind: "berserk"; }
	| { readonly kind: "recall-appoint"; readonly gid: number; }
	| { readonly kind: "return-cancel"; }
	| { readonly kind: "gathering-cancel"; }
	| { readonly kind: "effect-cancel"; readonly skillId: number; readonly token: number; }
	| { readonly kind: "minimap-floors"; readonly poses: readonly Pose[]; }
	| { readonly kind: "avatar-move"; readonly equip: boolean; readonly source: number; readonly destination: number; }
	| import("@/engine/foundation/gameplay/party-matching").PartyMatchCommand
	| { readonly kind: "gm-command"; readonly line: string; }
	| { readonly kind: "whisper-block"; readonly name: string; readonly blocked: boolean; }
	| { readonly kind: "stat-increase"; readonly stat: "str" | "int"; }
	| { readonly kind: "beginner-mark"; readonly enabled: boolean; }
	| {
		readonly kind: "auto-potion-save";
		readonly settings: import("@/engine/foundation/gameplay/auto-potion").AutoPotionSettings;
	}
	| { readonly kind: "rebirth"; readonly choice: 1 | 2; }
	| {
		readonly kind: "ground-move";
		readonly query: import("./navigation").GroundPickQuery;
		// Set by the world session while a logout/restart countdown runs.
		readonly departing?: boolean;
	}
	| import("@/engine/foundation/gameplay/academy").AcademyCommand
	| import("@/engine/foundation/gameplay/travel").GateCommand
	| { readonly kind: "cos-pickup"; readonly gid: number; readonly target: number; }
	| { readonly kind: "cos-drop"; readonly gid: number; readonly slot: number; }
	| { readonly kind: "guide-event"; readonly event: number; }
	| { readonly kind: "storage-open"; readonly gid: number; }
	// The guild manager's warehouse row (storage-room.ts openGuild) and the
	// declined war compensation quote (guild-manager-hud.ts).
	| { readonly kind: "storage-open-guild"; readonly gid: number; }
	| { readonly kind: "compensation-dismiss"; }
	// The job guild confirmations (job-guild.ts): join, withdraw and the alias.
	| { readonly kind: "job-join"; readonly gid: number; readonly job: number; }
	| { readonly kind: "job-withdraw"; readonly gid: number; }
	| { readonly kind: "job-alias"; readonly gid: number; readonly mode: number; readonly alias: string; }
	| { readonly kind: "storage-close"; }
	| { readonly kind: "storage-move"; readonly move: import("@/engine/foundation/gameplay/storage-room").StorageMove; }
	| {
		readonly kind: "cos-transfer";
		readonly gid: number;
		readonly toCos: boolean;
		readonly source: number;
		readonly destination: number;
	}
	| {
		readonly kind: "cos-shop-buy";
		readonly gid: number;
		readonly tab: number;
		readonly slot: number;
		readonly quantity: number;
	}
	| { readonly kind: "cos-shop-sell"; readonly gid: number; readonly slot: number; readonly quantity: number; }
	| {
		readonly kind: "cos-inventory-move";
		readonly gid: number;
		readonly source: number;
		readonly destination: number;
		readonly quantity: number;
	}
	| { readonly kind: "cos-behavior"; readonly gid: number; readonly mode: number; }
	| { readonly kind: "cos-follow"; readonly gid: number; }
	| { readonly kind: "cos-cancel"; readonly gid: number; }
	| { readonly kind: "cos-clean"; readonly gid: number; }
	| { readonly kind: "cos-pet-attack"; readonly gid: number; readonly pet: number; }
	| { readonly kind: "cos-ride"; readonly gid: number; readonly mounted: boolean; }
	| { readonly kind: "shop-buyback"; readonly id: number; }
	// 0x746F at the open shop's smith: mode 1 repairs one slot, 2 everything.
	| { readonly kind: "shop-repair"; readonly mode: 1 | 2; readonly slot: number; }
	| import("./item-process").ItemProcessCommand
	| { readonly kind: "mall-open"; }
	| {
		readonly kind: "mall-buy";
		readonly request: import("@/engine/foundation/gameplay/item-mall-wire").MallPurchase;
	}
	| { readonly kind: "shop-open"; readonly gid: number; }
	| { readonly kind: "shop-buy"; readonly tab: number; readonly slot: number; readonly quantity: number; }
	| { readonly kind: "shop-sell"; readonly slot: number; readonly quantity: number; }
	| { readonly kind: "item-drop"; readonly slot: number; }
	| { readonly kind: "gold-drop"; readonly amount: number; }
	| { readonly kind: "quickslot-set"; readonly binding: import("@/engine/foundation/gameplay/quickslots").QuickSlot; }
	| { readonly kind: "action-command"; readonly id: number; }
	| { readonly kind: "cos-attack"; readonly gid: number; }
	| import("@/engine/foundation/gameplay/social").SocialCommand
	| { readonly kind: "skill-train"; readonly id: number; }
	| { readonly kind: "mastery-train"; readonly id: number; }
	| { readonly kind: "quickslot-bind"; readonly slot: number; readonly skillId: number; }
	| { readonly kind: "quest-abandon"; readonly refId: number; }
	| { readonly kind: "quest-reward"; readonly refId: number; }
	| { readonly kind: "chat"; readonly channel: number; readonly text: string; readonly target?: string; }
	// The premium chat commands (count-job.ts); a reverse return's second
	// command carries its point.
	| {
		readonly kind: "premium-command";
		readonly command: import("@/engine/foundation/gameplay/count-job").PremiumCommand;
		readonly choice?: number;
	}
	| { readonly kind: "premium-command-cancel"; }
	| { readonly kind: "mount"; readonly gid: number; }
	| { readonly kind: "pickup"; readonly gid: number; }
	// The pickup shortcut: the worker chooses the item (pickup-nearest.ts).
	| { readonly kind: "pickup-nearest"; }
	// A click on another player: the worker decides whether it attacks
	// (player-attack.ts). Sent before the click's select.
	| { readonly kind: "player-interact"; readonly gid: number; readonly alt: boolean; }
	| {
		readonly kind: "release-target";
	}
	| {
		readonly kind: "move";
		readonly destination: Pose;
	}
	| {
		readonly kind: "select";
		readonly gid: number;
	}
	| {
		readonly kind: "attack";
		readonly gid: number;
	}
	| {
		readonly kind: "skill";
		readonly query?: import("./navigation").GroundPickQuery;
		readonly skillId: number;
		readonly gid?: number;
	}
	| {
		readonly kind: "inventory-move";
		readonly source: number;
		readonly destination: number;
		readonly quantity: number;
	}
	| {
		readonly kind: "item-use";
		readonly slot: number;
		readonly companionGid?: number;
		readonly revivalSlot?: number;
		readonly summonerSlot?: number;
		// The skin change window's choice (CIFChangePlayerModel_OnConfirm).
		readonly skin?: import("@/engine/foundation/gameplay/skin-change").SkinChoice;
		readonly targetSlot?: number;
	}
	| {
		readonly kind: "navigation";
		readonly requestId?: number;
		readonly regionId: number;
		readonly bundle: unknown;
	}
	| { readonly kind: "npc-talk"; }
	| { readonly kind: "npc-choice"; readonly choice: number; }
	| { readonly kind: "npc-close"; }
	| import("@/engine/foundation/gameplay/withdrawal").WithdrawalCommand;
/*
================
InventoryItem

An authoritative slot publication includes its reference and instance data.
Moving a UI icon does not change this record before a server receipt.
================
*/
export interface InventoryItem {
	readonly tooltip?: import("@/engine/foundation/gameplay/item-tooltip-reference").ItemTooltipReference;
	readonly magicReferences?:
		readonly import("@/engine/foundation/gameplay/item-tooltip-reference").ItemMagicReference[];
	readonly icon?: string;
	readonly slotState?: number;
	readonly durationMs?: string;
	readonly label?: string;
	readonly summon?: {
		readonly state: number;
		readonly refObjId?: number;
		readonly name?: string;
		readonly remainingSeconds?: number;
		readonly rentals: readonly {
			readonly kind: 0 | 5;
			readonly id: number;
			readonly seconds: number;
			readonly tag?: number;
			readonly flag?: number;
		}[];
	};
	readonly transformRefObjId?: number;
	readonly name?: string;
	readonly slot: number;
	readonly refObjId: number;
	readonly typeFlags: number;
	readonly quantity: number;
	readonly plus: number;
	readonly durability: number;
	readonly variance: string;
	readonly magic: readonly string[];
}
/*
================
CastImpact

Result stages retain native ordering so delayed presentation cannot repeat
damage or move a later impact ahead of an earlier stage.
================
*/
export interface CastImpact {
	/** Absolute result-stage index; absent on legacy single-batch fixtures. */
	readonly stage?: number;
	readonly type?: number;
	readonly displacement?: Omit<Pose, "angle">;
	readonly auxiliary?: readonly [number, number];
	readonly damage: number;
	readonly fatal: boolean;
	readonly flags: number;
	readonly secondaryAmount: number;
}
/*
================
CastTargetResult

Each target owns its ordered impacts within a cast result.
================
*/
export interface CastTargetResult {
	readonly target: number;
	readonly impacts: readonly CastImpact[];
}
/*
================
CastDisplacement

The cast token correlates a forced displacement with its visual action.
================
*/
export interface CastDisplacement {
	readonly gid: number;
	readonly token: number;
	readonly kind: 2 | 4 | 5 | 8;
	readonly destination: Omit<Pose, "angle">;
}
/*
================
CastState

Keeps cast identity, timing and results together across incremental packets.
================
*/
export interface CastState {
	/** 7756D0/8D9940 capture this before the trap is despawned. */
	readonly effectPosition?: Pose;
	readonly resultStageCount?: number;
	/** A parsed temporary instance: results only, never a cast animation. */
	readonly resultOnly?: boolean;
	readonly discardPendingResults?: boolean;
	readonly cancellationDeferred?: boolean;
	readonly cancellationRequestedAtMs?: number;
	readonly cancelledAtMs?: number;
	// The client prediction this server cast took over (cast-prediction.ts):
	// the presentation keeps that prediction's running action.
	readonly predictedToken?: number;
	readonly results?: readonly CastTargetResult[];
	readonly shotAtMs?: number;
	readonly receivedAtMs?: number;
	readonly impacts?: readonly CastImpact[];
	readonly token: number;
	readonly caster: number;
	readonly skill: number;
	readonly target: number;
	readonly damage: number;
	readonly fatal: boolean;
}
/*
================
CosRecord

The server owns companion life, equipment and inventory state.
================
*/
export interface CosRecord {
	readonly inventory?: readonly InventoryItem[];
	readonly gid: number;
	readonly refObjId: number;
	readonly band: number;
	readonly hp: number;
	readonly mp: number;
	readonly status: number;
	readonly dead: boolean;
	readonly experience?: readonly [number, number];
	readonly level?: number;
	readonly satiety?: number;
	readonly commandMode?: number;
	readonly name?: string;
	readonly inventorySlot?: number;
}
/*
================
SelectionDecal

Ground intent and entity selection have separate presentation lifetimes.
================
*/
export type SelectionDecal = { readonly kind: "ground"; readonly pose: Pose; } | {
	readonly kind: "target";
	readonly gid: number;
	readonly slot: 1 | 2 | 3;
};
/*
================
GameplayState

The simulation's immutable publication boundary. Revision changes expose
committed state to consumers without sharing mutable owner collections.
================
*/
/*
================
BetaMapPlayer

One row of the beta world map roster (port-only 0x3FB0; beta-map.ts).
================
*/
export interface BetaMapPlayer {
	readonly gid: number;
	readonly regionId: number;
	readonly x: number;
	readonly z: number;
	readonly name: string;
}
export interface GameplayState {
	readonly itemMall?: import("./item-mall").MallState;
	readonly betaPlayers?: readonly BetaMapPlayer[];
	/** RefObjIDs the server never spends (the beta starter kit), drawn as unlimited. */
	readonly unlimitedItems?: readonly number[];
	readonly returnScroll?: import("@/engine/foundation/gameplay/return-scroll").ReturnScrollCast;
	readonly huntingPoints?: readonly import("@/engine/foundation/gameplay/hunting").HuntingPoint[];
	readonly npcConversation?: import("@/engine/foundation/gameplay/npc-dialogue").NpcConversation;
	readonly restorationRevision?: number;
	readonly buffSlots?: readonly import("@/engine/foundation/gameplay/buff-slots").BuffSlot[];
	readonly attachedEffects?: readonly import("@/engine/foundation/gameplay/attached-effects").AttachedEffect[];
	readonly environmentalDamage?: readonly import("./combat-feedback").EnvironmentalDamage[];
	readonly gmReplies?:
		readonly (import("@/engine/foundation/gameplay/gm-command").GmReply & { readonly sequence: number; })[];
	readonly eventGroups?: Readonly<Record<number, number>>;
	readonly eligibility?: { readonly gm: boolean; readonly pcRoomEvent: boolean; };
	readonly autoPotion?: import("@/engine/foundation/gameplay/auto-potion").AutoPotionSettings;
	readonly storage?: import("@/engine/foundation/gameplay/storage-room").StorageRoom | null;
	readonly playerModels?: readonly import("@/engine/foundation/gameplay/skin-change").PlayerModel[];
	readonly job?: import("@/engine/foundation/gameplay/job-guild").LocalJob;
	readonly cosWindows?: readonly (import("@/engine/foundation/gameplay/cos-timer").CosItemWindow & {
		readonly reference: import("@/engine/foundation/gameplay/cos-timer").CosItemWindowReference;
	})[];
	readonly paramJobs?: readonly (import("@/engine/foundation/gameplay/param-job").ParamJobRow & {
		readonly reference: import("@/engine/foundation/gameplay/param-job").ParamJobReference;
	})[];
	// A premium package's limited uses (count-job.ts), and a reverse return
	// waiting for its point.
	readonly countJobs?: readonly (import("@/engine/foundation/gameplay/count-job").CountJobRow & {
		readonly reference: import("@/engine/foundation/gameplay/count-job").CountJobReference;
		readonly itemName?: string;
	})[];
	readonly reverseReturnChoice?: boolean;
	readonly abnormalRecords?: readonly import("@/engine/foundation/gameplay/abnormal-snapshot").AbnormalRecord[];
	readonly selectionDecal?: SelectionDecal | null;
	readonly notices?: readonly import("@/engine/foundation/gameplay/system-notices").SystemNotice[];
	readonly partyMatching?: import("@/engine/foundation/gameplay/party-matching").PartyMatching;
	readonly academy?: import("@/engine/foundation/gameplay/academy").AcademyState;
	readonly guide?: import("@/engine/foundation/gameplay/guide").GuideState;
	readonly weather?: import("@/engine/foundation/gameplay/weather").WeatherOptions;
	readonly alchemy?: import("./item-process").AlchemyState;
	readonly gacha?: import("./item-process").GachaState;
	readonly magicOption?: import("./item-process").MagicOptionGrantState;
	readonly targetCapabilities?: number;
	readonly targetTaxRate?: number;
	readonly shopCompletionRevision?: number;
	readonly shop?: import("@/engine/foundation/gameplay/commerce").ShopState;
	readonly masteryTotalOverride?: number;
	readonly trainingPending?: boolean;
	readonly trainingError?: string | null;
	// Journal omission retains the previous projection; reset sends a complete replacement.
	readonly fortress?: import("@/engine/foundation/gameplay/fortress").FortressState;
	readonly musicMode?: number;
	readonly social?: import("@/engine/foundation/gameplay/social").SocialState;
	readonly skillCatalog?: readonly import("@/engine/foundation/gameplay/skill-catalog").SkillMetadata[];
	// skillCatalog by id. Main thread only: presentation builds it once per
	// catalog, because per-frame UI looked rows up by scanning ~3800 skills.
	readonly skillIndex?: ReadonlyMap<number, import("@/engine/foundation/gameplay/skill-catalog").SkillMetadata>;
	readonly progression?: import("@/engine/foundation/gameplay/progression").Progression;
	readonly skills?: readonly number[];
	readonly quickSlots?: readonly import("@/engine/foundation/gameplay/quickslots").QuickSlot[];
	readonly cosRecords?: readonly CosRecord[];
	readonly worldClock?: WorldClockSeed;
	readonly questMarkers?: readonly QuestMarker[];
	readonly completedQuests?: readonly number[];
	readonly quests?: readonly QuestRecord[];
	readonly questProgress?: readonly QuestProgressEvent[];
	readonly questPending?: number;
	readonly questGathering?: QuestGathering;
	readonly inventorySlotCount?: number;
	readonly equipmentSlotCount?: number;
	readonly chat?: {
		readonly blocked?: readonly string[];
		readonly blockPending?: boolean;
		readonly blockError?: string | null;
		readonly feedback?: readonly import("@/engine/foundation/gameplay/chat-feedback").ChatFeedback[];
		readonly lines: readonly ChatLine[];
		readonly pending: boolean;
		readonly error: string | null;
	};
	readonly navigationFloor?: number;
	readonly minimapFloors?: Readonly<Record<string, number | undefined>>;
	readonly navigationRequestId?: number;
	readonly navigationFailure?: { readonly region: number; readonly requestId?: number; readonly error: string; };
	readonly navigationRegion?: number;
	readonly navigationBlock?: number;
	readonly activeCos?: {
		readonly gid: number;
		readonly refObjId: number;
		readonly hp: number;
		readonly mp: number;
		readonly status: number;
		readonly dead: boolean;
	};
	readonly cosResult?: {
		readonly subtype: number;
		readonly selector: number;
		readonly result?: number;
		readonly gid: number;
		readonly itemGid?: number;
	};
	readonly revision: number;
	readonly localGid: number;
	readonly pose: Pose | null;
	readonly movementPath?: { readonly from: import("./gameplay").Pose; readonly to: import("./gameplay").Pose; };
	readonly movementRevision?: number;
	readonly moving?: boolean;
	/** Simulation time (ms) at which pose was sampled; ClockSample.originMs maps it to wall time. */
	readonly poseAtMs?: number;
	readonly authoritativePose: Pose | null;
	readonly pendingMoves: number;
	readonly acknowledgedMove: number;
	readonly target: number;
	readonly targetPending: number;
	readonly avatarInventory?: readonly InventoryItem[];
	readonly inventory: readonly InventoryItem[];
	readonly inventoryPending: boolean;
	// Slot flashes raised by the 0x3645 item-state update (item-slot-effects.ts).
	readonly itemFlashes?: readonly {
		readonly slot: number;
		readonly kind: "changed" | "life";
		readonly atMs: number;
	}[];
	readonly vitals: readonly VitalState[];
	readonly itemCooldowns?: readonly import("@/engine/foundation/gameplay/item-cooldowns").ItemCooldown[];
	readonly skillCooldowns?: readonly import("@/engine/foundation/gameplay/skill-cooldowns").SkillCooldown[];
	// The skill that casts next, held by the client for its cooldown or by
	// the server behind its open command, and the newest denied press
	// (skill-queue.ts); the HUD draws both.
	readonly skillQueue?: import("@/engine/foundation/gameplay/skill-queue").SkillQueueState;
	readonly skillDenied?: import("@/engine/foundation/gameplay/skill-queue").DeniedPress;
	// The local press's predicted cast, animated until the server's cast
	// adopts it or it blends out (cast-prediction.ts).
	readonly castPrediction?: CastState;
	readonly casts: readonly CastState[];
	readonly error: string | null;
}

// deathState is presentation-owned (native action state 1): the fatal impact has
// landed, or HP reached zero with no fatal pending. LIFE remains the durable authority.
/*
================
VitalState

Current gauges and projected maxima have distinct packet producers. Missing
fields retain prior knowledge rather than silently becoming zero.
================
*/
export interface VitalState {
	readonly gid: number;
	readonly hp?: number;
	readonly mp?: number;
	readonly maxHp?: number;
	readonly maxMp?: number;
	readonly satiety?: number;
	readonly abnormal?: number;
	readonly abnormalLevels?: readonly { readonly bit: number; readonly level: number; }[];
	readonly deathState?: boolean;
}
