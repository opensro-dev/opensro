/*
===========================================================================

character.go - the persisted character record

Character authority owns this graph. Transport projects it into native wire
records; detached snapshots must copy every mutable child before use.

===========================================================================
*/

package domain

import (
	"encoding/json"
)

/*
==================
InventoryRow

InventoryRow is one persisted missionInventory row: the character's
AUTHORITATIVE inventory (item moves and drops mutate it; bootstrap only
reads it). varianceBits persists as a decimal string because JSON has no
64-bit integer type the Node side trusts (BigInt).
==================
*/
type InventoryRow struct {
	Summon *CharacterCOS `json:"summon,omitempty"`
	// Portable persistence for an already supplied native +20/+24 identity.
	// This is not a serial allocator or a native SQL column declaration.
	RecordID     uint64 `json:"recordId,omitempty,string"`
	Slot         int64  `json:"slot"`
	RefObjID     uint32 `json:"refObjId"`
	Codename     string `json:"codename"`
	TypeFlags    uint16 `json:"typeFlags"`
	Plus         int64  `json:"plus"`
	VarianceBits string `json:"varianceBits"`
	Durability   int64  `json:"durability"`
	StackCount   int64  `json:"stackCount"`

	// MagicOptions are the item's encoded magic-option params, EXACTLY the
	// u64 values the CSOItem wire carries after the count byte (client fold
	// sub_78c830 L186-195 -> sub_78b1b0: u8 count asserted <= 12, then
	// count x u64 LE). Encoding is the SRO-standard reinforce shape the
	// client's REINFORCE leg decodes for ordinary gear: low u16 = the
	// magicoption.txt param id (MUST resolve in the client's
	// magicOptionDefsByParamId3e4 table or the parse throws "missing
	// required magic-option definition"), high u32 = the signed modifier
	// magnitude. Param ids are v1.150-native magicoption.txt rows resolved
	// by MATTR_* codename (see internal/game/enterworld/magicoptions.go). Absent =
	// no options (the BlockedWhisperers omitempty posture, no schema
	// bump). Persisted as JSON numbers: real option magnitudes are u32, so
	// every encoded value stays far below the 2^53 float boundary.
	MagicOptions []uint64 `json:"magicOptions,omitempty"`
	// TransformRefObjID is a monster capsule's Data (item record +0x38,
	// 42E54B): the monster the mask turns its user into.
	TransformRefObjID uint32 `json:"transformRefObjId,omitempty"`
}

/*
================
WorldSpawn

Settled position. The movement owner retains the in-flight segment separately.
================
*/
type WorldSpawn struct {
	RegionID *int64   `json:"regionId"`
	X        *float64 `json:"x"`
	Y        *float64 `json:"y"`
	Z        *float64 `json:"z"`
	Angle    *int64   `json:"angle"`
}

/*
==================
CharacterWorld

CharacterWorld is the persisted character.world record: the fields the
bootstrap start profile reads, plus opaque passthrough for the
movement-lane bookkeeping so the character snapshot does not drop keys the
Node record carries (moveSegment stays raw - the live plane belongs to the
movement lane, the snapshot only echoes it).
==================
*/
type CharacterWorld struct {
	// SavedReturn is the native CSO return world/region/position. Region
	// admission updates it only in type-0 worlds on layer 1 (4E6FE0).
	// It is independent of the movement goal and appointed rebirth town.
	SavedReturn *SavedReturnLocation `json:"savedReturn,omitempty"`
	// PackedInstance is the native world identity: definition in the low
	// word, instance in the high word. An absent legacy record belongs to
	// INS_DEFAULT instance 1; explicit values are never inferred from region.
	PackedInstance *uint32     `json:"packedInstance,omitempty"`
	Spawn          *WorldSpawn `json:"spawn"`
	// AuthoredAreaReturn is the settled ordinary-world spawn captured before
	// entering a server-authored area. Authored areas are real world regions,
	// so movement continues to update Spawn normally; this independent return
	// anchor lets an explicit leave operation restore the previous world
	// location without client-owned coordinates or area-specific server hacks.
	AuthoredAreaReturn *WorldSpawn `json:"authoredAreaReturn,omitempty"`
	// RebirthPoint is the resolved town return position appointed through an eligible
	// NPC or teleport gate (retail AppointedTeleport). It is independent of Spawn:
	// ordinary movement must never overwrite the designated return city.
	RebirthPoint *WorldSpawn `json:"rebirthPoint,omitempty"`
	// Native char-data +0xa4 stores the appointed RefObj identity, not a runtime GID.
	// RebirthPoint remains the durable fallback for legacy records and removed gates.
	RebirthGateRefID uint32 `json:"rebirthGateRefId,omitempty"`
	// LastRecallPoint is where the player last used a return scroll (native
	// char-data +0xCC.., CGObjPC_SaveLatestRecallPosition 4E0250, called from
	// the return scroll's location check); LastDeathPoint is where the player
	// last died (+0xDC.., 4E0330 from ProcessNormalDeath). The reverse return
	// scroll takes the player back to either.
	LastRecallPoint *WorldSpawn `json:"lastRecallPoint,omitempty"`
	LastDeathPoint  *WorldSpawn `json:"lastDeathPoint,omitempty"`
	MovementMode    *int64      `json:"movementMode"`
	SpawnSet        bool        `json:"spawnSet"`
	// DungeonFloorIndex is semantic game state. The browser combines it with
	// its packed minimap catalogue; presentation prefixes, labels, tile paths
	// and bounds never belong in the authority record.
	DungeonFloorIndex    *int64          `json:"dungeonFloorIndex,omitempty"`
	MovementSourceSeeded bool            `json:"movementSourceSeeded,omitempty"`
	UpdatedAt            string          `json:"updatedAt,omitempty"`
	MoveSegment          json.RawMessage `json:"moveSegment,omitempty"`
}

/*
================
SavedReturnLocation

World identity and position saved for native return-location skills.
================
*/
type SavedReturnLocation struct {
	Definition uint16  `json:"definition"`
	RegionID   uint16  `json:"regionId"`
	X          float32 `json:"x"`
	Y          float32 `json:"y"`
	Z          float32 `json:"z"`
}

/*
==================
BuybackEntry

Character mirrors the persisted character record fields the bootstrap /
appearance / visual-loadout path reads. Pointer fields distinguish "absent"
from zero the way the Node coercers do (Number(undefined) -> NaN -> the
fallback), which is load-bearing for e.g. bodyShapeByte vs heightIndex.

MissionInventory nil vs empty matters and maps 1:1 onto the Node
Array.isArray gate: nil means the inventory was never seeded (first-ever
bootstrap, overlay early-returns), an empty non-nil slice means "seeded and
empty" (an unworn weapon must NOT render).
BuybackEntry retains the exact sold item in the character transaction.
Entries belong to a live player lifetime and are deliberately not persisted.
Native pool reset clears PC+2008; no wall-clock TTL is inferred from that.
==================
*/
type BuybackEntry struct {
	ID          uint32       `json:"id"`
	MerchantRef uint32       `json:"merchantRef"`
	Price       uint64       `json:"price"`
	Item        InventoryRow `json:"item"`
}

/*
================
Character

One authority transaction owns inventory, progression and quest state together.
Pointers distinguish absent legacy values from meaningful zero values.
================
*/
type Character struct {
	// Actor-only teleport state. Character-store snapshots carry it; reconnect
	// never resurrects a timer belonging to the previous native actor lifetime.
	NativeTeleportMode uint8     `json:"-"`
	PK                 *PKRecord `json:"pk,omitempty"`
	// LastSeenUnixMs is when the character last left the world (guild
	// votes measure a master's and a voter's absence from it).
	LastSeenUnixMs  int64             `json:"lastSeenUnixMs,omitempty"`
	Aggressions     map[uint32]uint32 `json:"-"`
	EventMembership *EventMembership  `json:"-"`
	// NativeBodyStatus is runtime-only state, copied under the character store
	// door. Writers use TransitionBodyStatus; presentation never owns it.
	BerserkPoints  uint8 `json:"berserkPoints"`
	BerserkUntilMs int64 `json:"-"`
	// BattleUntilMs ends the battle state (CGObjPC state+0xD, channel 8).
	// Striking or being struck restarts its countdown; zero is peace.
	BattleUntilMs int64 `json:"-"`
	// LastExpLoss is CGObjPC+0x1CE0, the size of the last negative EXP
	// change (CGObjPC_ApplyExpDelta 4E5710). Resurrection returns a percent
	// of it; every revival clears it.
	LastExpLoss      int64  `json:"-"`
	NativeBodyStatus uint8  `json:"-"`
	BodyStatusOwner  uint64 `json:"-"`
	// TransformRefObjID is the transform block (CGObjPC+0x21A0): the monster
	// a mask made the player (msch 1), or the player a Duplicate copies
	// (msch 2). Runtime-only; the effect instance that set it clears it
	// (4F0210).
	TransformRefObjID uint32 `json:"-"`
	// TransformMode is that instance's msch word (1 mask, 2 duplicate), the
	// value CSkillManager_GetTransformMschMode (49A1B0) answers; 0 when none.
	TransformMode uint8 `json:"-"`
	// TransformShape and TransformEquipment are a Duplicate's copy of the
	// other player's record byte and nine worn slots (4F0320).
	TransformShape     uint8          `json:"-"`
	TransformEquipment [9]uint32      `json:"-"`
	BuybackNext        uint32         `json:"buybackNext,omitempty"`
	Buyback            []BuybackEntry `json:"-"`
	BuybackSession     uint64         `json:"-"`
	ID                 int64          `json:"id"`
	// AccountID is the authoritative owner identity minted by title login.
	// It is mandatory for every live record: character-select reads and
	// writes are tenant-scoped by it, while names remain division-global.
	AccountID     string `json:"accountId,omitempty"`
	Name          string `json:"name"`
	DeletePending bool   `json:"deletePending"`
	// DeleteReservedAt is the ISO instant the deletion reservation was
	// placed. The character-select screen renders the countdown from it and
	// the deletion reaper archives matured reservations. Empty means no
	// reservation is pending.
	DeleteReservedAt string `json:"deleteReservedAt,omitempty"`

	RaceIndex *int64 `json:"raceIndex"`
	Gender    *int64 `json:"gender"`

	ModelCodename string `json:"modelCodename"`
	ModelRef      *int64 `json:"modelRef"`

	WeaponSelected bool   `json:"weaponSelected"`
	WeaponIndex    *int64 `json:"weaponIndex"`
	ArmorSelected  bool   `json:"armorSelected"`
	ProtectorIndex *int64 `json:"protectorIndex"`

	BodyShapeByte *int64   `json:"bodyShapeByte"`
	HeightIndex   *int64   `json:"heightIndex"`
	VolumeIndex   *int64   `json:"volumeIndex"`
	HeightScale   *float64 `json:"heightScale"`
	VolumeScale   *float64 `json:"volumeScale"`

	AnimationSetName string `json:"animationSetName"`

	MissionInventory []InventoryRow  `json:"missionInventory"`
	World            *CharacterWorld `json:"world,omitempty"`

	// ActiveCOS is the server-authoritative summoned companion/transport.
	// The item use that creates it consumes the ITEM_COS_* row and persists
	// this identity in the same character transaction; later 0x769E commands
	// must match this exact GID and never trust a client-proposed vehicle.
	ActiveCOS       *CharacterCOS    `json:"activeCos,omitempty"`
	PetSkillWindows []PetSkillWindow `json:"petSkillWindows,omitempty"`
	// ParamJobs are the live item parameter jobs (CTJ_CharParamKeeper):
	// EXP/skill-EXP scroll bonuses with an absolute deadline.
	ParamJobs []ParamJob `json:"paramJobs,omitempty"`
	// ItemGroupCooldowns maps an item COOLTIME group to its absolute end.
	ItemGroupCooldowns map[uint32]int64 `json:"itemGroupCooldowns,omitempty"`
	TimedSkillJobs     []TimedSkillJob  `json:"timedSkillJobs,omitempty"`

	// AvatarInventory is the persisted costume inventory. Rows reuse the
	// equipment item body and occupy native avatar slots 0..3. Every row's
	// reference object must be present in the enter-world item snapshot.
	AvatarInventory *AvatarInventory `json:"avatarInventory,omitempty"`

	// Current progression values. Maxima are derived from level and stats and
	// are deliberately not persisted.
	Level       *int64 `json:"level,omitempty"`
	MaxLevel    *int64 `json:"maxLevel,omitempty"`
	CurrentHP   *int64 `json:"currentHp,omitempty"`
	CurrentMP   *int64 `json:"currentMp,omitempty"`
	SkillPoints *int64 `json:"skillPoints,omitempty"`
	StatPoints  *int64 `json:"statPoints,omitempty"`
	Experience  *int64 `json:"experience,omitempty"`
	SkillExp    *int64 `json:"skillExp,omitempty"`
	Gold        *int64 `json:"gold,omitempty"`
	// VisualFlags is the complete CICUser+0x779 byte. Bit 0 is the
	// beginner/rudiment nameplate mark; bit 1 is the server-owned attached
	// state-effect flag. The client always submits and receives the complete
	// byte (0x7683 -> 0xB683), so this must be persisted atomically rather
	// than represented as an independent UI preference.
	VisualFlags *int64 `json:"visualFlags,omitempty"`
	// Strength/Intellect are the native player STR/INT words used by the
	// login-stat block and item requirement gates. Current records always
	// carry them; detached fixtures use the creation fallback.
	Strength  *int64 `json:"strength,omitempty"`
	Intellect *int64 `json:"intellect,omitempty"`

	// Masteries contain the complete racial mastery set. Creation installs
	// each row at MasterySeedLevel and training raises its level.
	Masteries []CharacterMastery `json:"masteries,omitempty"`

	// Skills contains one current skill id per group. Creation installs the
	// racial base attacks; learning an upgrade replaces its group's previous
	// id.
	Skills []uint32 `json:"skills,omitempty"`
	// Absolute authority deadlines survive reconnects and skill-level upgrades.
	OffensiveSkillCooldowns map[uint32]int64 `json:"offensiveSkillCooldowns,omitempty"`
	// Native nonzero CoolTimeGroup shares one deadline across skill families.
	SharedSkillCooldowns map[uint8]int64 `json:"sharedSkillCooldowns,omitempty"`
	// The native reuse manager also retains one action-recovery deadline.
	// Different skill groups cannot bypass it at the execution phase.
	SkillActionRecoveryUntilMs int64 `json:"skillActionRecoveryUntilMs,omitempty"`

	// ItemUseCooldowns holds absolute server deadlines for native recovery
	// categories 1/2/3 (HP/MP/universal), independent of bag slots and item IDs.
	// Value storage keeps snapshots detached and bounds the state without a
	// timer/map per item. Only the successful item-use transaction writes it;
	// reconnect, stack movement and rebirth must not reset these deadlines.
	ItemUseCooldowns [3]int64 `json:"itemUseCooldowns,omitempty"`
	// ItemCureCooldowns are the cure lanes of the same reuse mask (4EB410):
	// [0] the universal pill (bit 0x100), [1] every other cure (bit 0x40).
	ItemCureCooldowns [2]int64 `json:"itemCureCooldowns,omitempty"`
	// PetPotionCooldowns are 49D240's 1.1 s recovery lock on the owner, one
	// lane per pet-potion TID4 the v1.150 client sends (4, 5, 7).
	PetPotionCooldowns [3]int64 `json:"petPotionCooldowns,omitempty"`

	// Job is the job guild membership (v1.188 CJobInfo at CGObjPC+0x1FB4).
	Job CharacterJob `json:"job,omitzero"`

	// QuickSlots are the persisted CIFUnderBar bindings. The entered-player
	// HUD-state block replays these records before mission reveal; live changes
	// arrive through client opcode 0x7541. The list contains at most one
	// non-empty binding per native slot 0..50 and is stored in slot order.
	QuickSlots []QuickSlotBinding `json:"quickSlots,omitempty"`

	// Mission is the authoritative event-guide runtime record.
	Mission *MissionRuntime `json:"mission,omitempty"`

	// CompletedQuestIds are the finished-quest questdata ids the 0x32B3
	// quest block's FIRST section emits (sub_8673d0 section 1: u8 count +
	// count x u32 refId into the CICPlayer +0x18d0 container's completed
	// list). Content ids are v1.150-NATIVE: rows of the shipped
	// server_dep/silkroad/textdata/questdata.txt (also mirrored to the
	// client at assets/data/questData.json), resolved by CODENAME - e.g.
	// 2 = QTUTORIAL_CH, 3 = QNO_CH_SMITH_1 - never transplanted from the
	// v1.188 shard dump. Absent = none completed (the BlockedWhisperers
	// posture: optional omitempty list, no schema version bump - strict
	// decode knows the key from this struct).
	CompletedQuestIds []uint32 `json:"completedQuestIds,omitempty"`
	// Completion counts are server-side repeat limits; the native login wire
	// continues to carry only the unique completed quest IDs.
	QuestCompletionCounts map[uint32]uint32           `json:"questCompletionCounts,omitempty"`
	QuestSupplies         map[uint32]QuestSupplyState `json:"questSupplies,omitempty"`

	// ActiveQuests are the in-progress quest records the 0x32B3 quest
	// block's SECOND section emits (sub_8673d0 section 2: u8 count +
	// count x [u32 refId + SQuestInfo body], client parse sub_788210).
	// Quest ids follow the CompletedQuestIds content rule (v1.150-native
	// questdata rows resolved by codename). The client registry insert is
	// insert-if-absent (@0x789039: a duplicate refId keeps the FIRST
	// record), so persisted lists should not carry duplicates. Absent =
	// none active (the same no-bump omitempty posture as above).
	ActiveQuests []ActiveQuestRecord `json:"activeQuests,omitempty"`

	// TrackedQuests are the tracker records the 0x32B3 quest block's
	// THIRD section emits (sub_8673d0 section 3, applied client-side by
	// sub_866c60: quest-state apply + tracker UI refresh - the browser
	// client hosts it over its quest plane and drives the CIFQuest +0x398
	// tracked id the minimap quest pass gates on). Absent = none tracked.
	TrackedQuests []TrackedQuestRecord `json:"trackedQuests,omitempty"`

	// EnterEventGroupIds are the event-group ids the entered tail emits
	// (client fold sub_77b220 @0x0077b2f4..: u8 count + count x u32,
	// right after the fortress-war dword and before the discarded
	// mission-mode byte). Client behavior per id: EVERY id sets the
	// event-start slot (RefObjDataManager head record +0x10 := 1); id 1
	// ADDITIONALLY fires MapRenderer_SetRenderOption(3,3)/(4,0x14) and
	// the UIIT_MSG_EVENT_START guide message. POLICY CONTENT (declared
	// choice, the internal/game/world/monster/tactics.go posture): no v1.150 table
	// constrains these ids - the wire grammar and the id-1 special case
	// are the only client authority - so values are a server-side event
	// schedule decision, not data-resolved content. Absent = no events
	// running (the same no-bump omitempty posture as above).
	EnterEventGroupIds []uint32 `json:"enterEventGroupIds,omitempty"`

	// BlockedWhisperers is the persisted whisper-block name list (the
	// retail _BlockedWhisperer(OwnerID, TargetName) shape carried as a
	// JSON list on the character record; the Eternity dump is schema
	// reference only). Mutated exclusively by the community lane's
	// 0x766F handler through the Mutate door; read by the entered
	// chunk-C emission (BuildLocalPlayerEntryPayload), which is the
	// client's ONLY reflection channel for this list - sub_77ad10 reads
	// it mid-entered-stream, and no standalone S->C ack opcode is
	// pinned. Capacity is the client panel's literal 0x14 cap
	// (sub_61dc50 ": %d / %d").
	BlockedWhisperers []string `json:"blockedWhisperers,omitempty"`

	// Friends is the persisted friend-edge list (the retail
	// _Friend(CharID, FriendCharID, FriendCharName, RefObjID) shape
	// carried as a JSON list on the character record; DuckSoup/Eternity
	// are schema reference only). Edges are MUTUAL by construction: the
	// community friend-add handler appends to BOTH characters through
	// one multi-record Mutate door, and delete removes both sides through
	// the same door. The online
	// state byte is deliberately NOT persisted - it is derived from live
	// session presence at every encode (roster seed 0x3769, event 0x3F9A
	// case 4). Cross-session reads go through FriendsView and writers publish
	// replacements through SwapFriends inside the multi-character door.
	Friends []FriendRecord `json:"friends,omitempty"`

	// GuildID is the FK into the authority store's guilds table: the guild
	// this character is a member of, nil when
	// guildless. The guild entity itself - name, notice, member set -
	// lives in the guilds/guild_members tables behind the GuildStore
	// door, never on the character record. The enter-world 0x32C4 seed
	// emits ONLY when this FK resolves to a real stored guild row;
	// a dangling FK logs loud and emits nothing (absence over
	// invention - the internal/game/social/guild package doc).
	GuildID *int64 `json:"guildId,omitempty"`

	// GMPrivilege is the v1.150 GM privilege flag: bit 0 of the u8 the
	// entered 0x32B3 stream deposits at CICPlayer+0x1890 (deserialize
	// sub_8675f0 @0x867a90; tested by sub_862a70 "movzx +0x1890 & 1").
	// The client gates on it: Console_IsAllowed @0x004fa670 (console /
	// GM-command availability), the GM default chat channel mode 3 in
	// CIFChatViewer_HandleChatInputKey @0x006aed90, and the 30F2/33C4/
	// 3647 mission handlers. It is never writable from any
	// network path. Normal startup requires strict accounts plus
	// character-bound EnterWorld tickets; even the explicitly insecure
	// local-development posture has no privilege setter. The only setter
	// is the boot-time division-qualified operator allowlist reconcile
	// (store.ReconcileGMPrivilege over SRO_GM_CHARACTERS), which is
	// host-access-only by construction.
	// False means an ordinary player.
	GMPrivilege bool `json:"gmPrivilege,omitempty"`

	// PC-room event eligibility is server-owned: client 862A80 -> 547750(1).
	PCRoomEvent bool               `json:"pcRoomEvent,omitempty"`
	AutoPotion  AutoPotionSettings `json:"autoPotion"`

	// Additional persisted presentation fields carried by the full snapshot.
	CreatedAt string `json:"createdAt,omitempty"`
	// Legacy snapshot compatibility only. Roster presentation derives XP from
	// Experience and leveldata; progression does not maintain this old cache.
	ExperiencePercent *float64 `json:"experiencePercent,omitempty"`
	FigureIndex       *int64   `json:"figureIndex,omitempty"`
}

/*
==================
PetSkillWindow

CharacterCOS is the durable half of one summoned COS lifecycle. Static
model/speed/TID data remains in characterdata and is re-resolved by
codename; only mutable entity identity/state belongs on the character.
PetSkillWindow is one live ITEM_MALL_PET_SKILL_* / growth-potion usage
window. sub_6E6150 keys its board row by kind and item id, so the ref id is
the identity. EndUnixMs is absolute rather than a countdown, the way retail
persists _CharCOS.RentEndTime, so a save keeps the real deadline.
==================
*/
type PetSkillWindow struct {
	ItemRefObjID uint32 `json:"itemRefObjId"`
	// Codename keeps the item's reference resolvable once the stack is spent:
	// the browser resolves a window's icon and limit from refItemSnapshot,
	// which is otherwise seeded only from what the character still carries.
	Codename  string `json:"codename"`
	EndUnixMs int64  `json:"endUnixMs"`
}

/*
================
ParamJob

One live CTJ_CharParamKeeper (SR_GameServer 654F30): an internal param item
(TID 3/3/3/10) writes Value to ParamKeeper parameter Param until EndUnixMs.
The internal item's reference id is the client board row's identity
(UpdateMagicStateSlot kind 4).
================
*/
type ParamJob struct {
	ItemRefObjID uint32 `json:"itemRefObjId"`
	Codename     string `json:"codename"`
	Param        uint16 `json:"param"`
	Value        int64  `json:"value"`
	EndUnixMs    int64  `json:"endUnixMs"`
}

/*
================
CharacterCOS

Owned companion state; its inventory commits with the owning character.
================
*/
type CharacterCOS struct {
	SummonGeneration       uint64        `json:"summonGeneration,omitempty"`
	RentalExpiresAtUnix    int64         `json:"rentalExpiresAtUnix,omitempty"`
	RentalRemainingSeconds int32         `json:"rentalRemainingSeconds,omitempty"`
	Rentals                []COSRental   `json:"rentals,omitempty"`
	NativeBodyStatus       uint8         `json:"-"`
	Experience             uint64        `json:"experience,omitempty"`
	Level                  uint8         `json:"level,omitempty"`
	Satiety                uint16        `json:"satiety,omitempty"`
	InventorySlot          uint8         `json:"inventorySlot,omitempty"`
	Container              *COSContainer `json:"container,omitempty"`
	CommandMode            uint32        `json:"commandMode,omitempty"`
	GID                    uint32        `json:"gid"`
	RefObjID               uint32        `json:"refObjId"`
	Codename               string        `json:"codename"`
	Name                   string        `json:"name,omitempty"`
	CurrentHP              uint32        `json:"currentHp"`
	CurrentMP              uint32        `json:"currentMp"`
	// StateFlags bit 0 is CCOSData+0x38 (49D240): revival refuses while it
	// is set and sets it when the revive succeeds.
	StateFlags uint32 `json:"stateFlags,omitempty"`
	Summoned   bool   `json:"summoned"`
	Mounted    bool   `json:"mounted"`
}

/*
================
COSContainer

Capacity comes from admitted companion state, never from a client request.
================
*/
type COSContainer struct {
	Capacity uint8          `json:"capacity"`
	Rows     []InventoryRow `json:"rows"`
}

// Job types (v1.150 CICPlayer+0x782; v1.188 CJobInfo_GetJobType).
const (
	JobNone   uint8 = 0
	JobTrader uint8 = 1
	JobThief  uint8 = 2
	JobHunter uint8 = 3
)

/*
==================
CharacterJob

A job guild membership: the job type joined at a guild NPC, its grade
(+0x783, 1 on joining) and experience (+0x18B4), the alias a dressed job
player shows, and the time before a thief or hunter who withdrew may join
again (v1.188 CGObjPC_HandleJobLeave70E2's seven-day timed job).
==================
*/
type CharacterJob struct {
	Type       uint8  `json:"type,omitempty"`
	Grade      uint8  `json:"grade,omitempty"`
	Exp        uint32 `json:"exp,omitempty"`
	Alias      string `json:"alias,omitempty"`
	RejoinAtMs int64  `json:"rejoinAtMs,omitempty"`
}

/*
==================
AvatarInventory

AvatarInventory is the persisted avatar (costume) inventory record: the
wire capacity byte plus the worn avatar rows (see the Character field doc
for the wire grammar and content constraints).
==================
*/
type AvatarInventory struct {
	// Capacity is the avatar block's capacity byte. Zero coerces to
	// AvatarCapacityDefault at emission so a rows-only record cannot
	// accidentally emit capacity 0.
	Capacity int64 `json:"capacity"`
	// Rows are the worn avatar items, one per avatar socket (slot 0..3).
	Rows []InventoryRow `json:"rows,omitempty"`
}

/*
================
TimedSkillJob

CInstanceTimedJob type 0 checkpoints remaining online time across login.
================
*/
type TimedSkillJob struct {
	SkillID     uint32 `json:"skillId"`
	Token       uint32 `json:"token"`
	RemainingMs uint32 `json:"remainingMs"`
}
