/*
===========================================================================

response.go - Package enterworld.

===========================================================================
*/

package enterworld

import (
	"encoding/json"
	"opensro.online/server/internal/game/item/inventory"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
ChatPermissions

ChatPermissions describes the chat lanes available to the character.
================
*/
type ChatPermissions struct {
	CanPartyChat bool `json:"canPartyChat"`
	CanGuildChat bool `json:"canGuildChat"`
	CanUnionChat bool `json:"canUnionChat"`
}

/*
================
RefObjRow

RefObjRow is one refObjSnapshot row.
================
*/
type RefObjRow struct {
	Teleport  *simulation.TeleportGateBounds `json:"teleport,omitempty"`
	RefObjID  uint32                         `json:"refObjId"`
	TidWord   uint16                         `json:"tidWord"`
	Codename  string                         `json:"codename"`
	NameStrID string                         `json:"nameStrId,omitempty"`
	Name      string                         `json:"name,omitempty"`
	Level     uint8                          `json:"level,omitempty"`
	MaxHP     uint32                         `json:"maxHp,omitempty"`
	// BodyRadius is the row's RefObjChar BCRadius: the body combat spacing
	// adds around the action's reach. A monster's instance grades it (the
	// client applies the grade from the live spawn's rarity byte); a
	// character's body is this value ungraded. Absent where no spacing
	// reads the row (NPCs, teleports), which keeps the client's predicted
	// run-up on its unpinned aim there.
	BodyRadius float64 `json:"bodyRadius,omitempty"`
	// MonsterType is a monster row's static type byte (client record +0xA0):
	// /LOADMONSTER sends it when no type token is typed.
	MonsterType uint8 `json:"monsterType,omitempty"`
	// CCharacterData+0x210, parsed from characterdata column 88. Despite
	// older host names calling the slot a skill id, client 692CB0 consumes it
	// as the nonzero mounted-attack capability gate.
	MountedAttackCapability210 uint32 `json:"mountedAttackCapability210,omitempty"`
	Kind                       string `json:"kind"`
	// Player-character rows carry the two RefObjData selectors that the
	// CICUser construction/default-wear path reads. Pointers preserve a
	// meaningful zero while keeping the fields absent on NPC/monster rows.
	CountryByte9C  *uint8 `json:"countryByte9c,omitempty"`
	SexSelector1AC *uint8 `json:"sexSelector1ac,omitempty"`
	// Pointer-to-slice keeps RefObjRow comparable for the pinned monster
	// fixture while JSON still emits the exact array shape for NPC rows.
	NpcTalkStoreGroups *[]simulation.NpcTalkStoreGroup `json:"npcTalkStoreGroups,omitempty"`
}

/*
================
RefItemRow

RefItemRow is one refItemSnapshot row.
================
*/
type RefItemRow struct {
	DescriptionSymbol          string       `json:"descriptionSymbol,omitempty"`
	Icon                       string       `json:"icon,omitempty"`
	SummonedCharacterTypeFlags *uint16      `json:"summonedCharacterTypeFlags,omitempty"`
	RefObjID                   uint32       `json:"refObjId"`
	TypeFlags                  uint16       `json:"typeFlags"`
	Codename                   string       `json:"codename"`
	Kind                       string       `json:"kind"`
	Name                       string       `json:"name"`
	NativeFields               NativeFields `json:"nativeFields,omitempty"`
}

/*
================
EquipItemRow

EquipItemRow is one equipped item's browser view.
================
*/
type EquipItemRow struct {
	RefObjID uint32 `json:"refObjId"`
	Slot     int64  `json:"slot"`
	Body     []int  `json:"body"`
}

/*
================
SiegeItemForgeGroupRow

SiegeItemForgeGroupRow is one v1.150 siegefortressitemforge.txt group.
The two vectors are the exact client 64B440 (+0x00) and 64B450 (+0x10)
projections consumed by the fortress NPC manager-hire menu.
================
*/
type SiegeItemForgeGroupRow struct {
	GroupID         uint32               `json:"groupId"`
	SmithItemRefs   []uint32             `json:"smithItemRefs"`
	TrainerItemRefs []uint32             `json:"trainerItemRefs"`
	Items           []SiegeItemForgeItem `json:"items"`
}

/*
================
SiegeItemForgeItem

One row's price and time per item (the native record:
+0xC gold, +0x10 guild points, +0x14 minutes). The production
window (CIFFortressMakeItemWnd 65A280) counts count * minutes * 60 down.
================
*/
type SiegeItemForgeItem struct {
	RefObjID uint32 `json:"refObjId"`
	Gold     uint32 `json:"gold"`
	GP       uint32 `json:"gp"`
	Minutes  uint32 `json:"minutes"`
}

/*
================
SiegeFortressDataRow

SiegeFortressDataRow is the native case-0x36 siegefortress.txt projection
consumed by the fortress-id/name/NPC-code lookup family at manager+0x488.
================
*/
type SiegeFortressDataRow struct {
	Icon            string `json:"icon"`
	FortressID      uint32 `json:"fortressId"`
	CodeName        string `json:"codeName"`
	NameStrID       string `json:"nameStrId"`
	OfficialNpcCode string `json:"officialNpcCode"`
	// RequestFee is the row's +0x80 gold, which the official's application
	// question quotes (client 6649C0 box 0x64, 7E16A0).
	RequestFee uint64 `json:"requestFee"`
	// TaxTargets is column 10 (+0x96): bits 0..6 tick the tax management
	// window's smith, armor, grocery, potion, portal, specialty and warp
	// boxes (CIFTaxManagement_Refresh 665470).
	TaxTargets uint8 `json:"taxTargets"`
	// OfficialRefObjID resolves OfficialNpcCode for the browser, which
	// knows NPCs by RefObjID (client 662E80 matches the official in conversation).
	OfficialRefObjID uint32 `json:"officialRefObjId,omitempty"`
}

/*
================
GameWorldDataRow

GameWorldDataRow is the case-0x38 gameworlddata.txt projection consumed by
manager+0x4d0's id/code/first-war-name lookup family.
================
*/
type GameWorldDataRow struct {
	GameWorldID uint16 `json:"gameWorldId"`
	CodeName    string `json:"codeName"`
	WarName     string `json:"warName"`
}

/*
================
BootstrapRequest

BootstrapRequest is the /mission/bootstrap request body.
================
*/
type BootstrapRequest struct {
	DivisionID    string
	CharacterName string
}

/*
================
UnmarshalJSON

UnmarshalJSON accepts divisionId as either a string or a number.
================
*/
func (r *BootstrapRequest) UnmarshalJSON(data []byte) error {
	var raw struct {
		DivisionID    json.RawMessage `json:"divisionId"`
		CharacterName string          `json:"characterName"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.CharacterName = raw.CharacterName
	r.DivisionID = ""
	if len(raw.DivisionID) == 0 {
		return nil
	}

	var asString string
	if err := json.Unmarshal(raw.DivisionID, &asString); err == nil {
		r.DivisionID = asString
		return nil
	}

	var asNumber json.Number
	if err := json.Unmarshal(raw.DivisionID, &asNumber); err == nil {
		r.DivisionID = asNumber.String()
	}
	return nil
}

/*
================
BootstrapResult

BootstrapResult is the /mission/bootstrap response.
================
*/
type BootstrapResult struct {
	// inventoryOwner stays private to entry preparation: its live capacity
	// adopts Character's presented size only after browser encoding succeeds.
	// liveOwner holds the live character record so HandleEnterWorld stamps
	// LastLoginAtMs after browser encoding succeeds.
	inventoryOwner      *Character
	liveOwner           *Character
	DiagnosticSessionID string
	NativeResult        int
	NativeErrorCode     int
	NativeError         *NativeAgentError
	Reason              string

	DivisionID           string
	Character            *Character
	AcademyMember        bool
	EventGuideStateMask  uint32
	RefObjSnapshot       []RefObjRow
	RefSkillSnapshot     []SpawnSkillRow
	SkillCooldowns       []EntrySkillCooldown
	RefItemSnapshot      []RefItemRow
	MagicOptionSnapshot  []MagicOptionRow
	AvatarMagicOptions   []AvatarMagicOptionRow
	SiegeItemForgeGroups []SiegeItemForgeGroupRow
	SiegeFortressData    []SiegeFortressDataRow
	GameWorldData        []GameWorldDataRow
	AvatarItems          []EquipItemRow
	EquipItems           []EquipItemRow
	LocalPlayerEntry     *LocalPlayerEntry
	ChatPermissions      ChatPermissions
	ChatMessages         []string
	SystemMessages       interface{}
	Packets              []Packet
	// UnlimitedItems are the RefObjIDs the beta starter kit never spends, so
	// the client can show them as unlimited rather than a stack of one.
	UnlimitedItems       []uint32
	MasteryTotalOverride int64
	// LevelCap is the shard's configured character level cap, published
	// so the client's party-matching and quest-guide windows follow it
	// (port-only plumbing for the expansion; zero is the native cap and
	// is not published).
	LevelCap int64
	// StorageAutoStack tells the client's storage quick move to prefer a
	// matching stack (port-only, not native; false is native).
	StorageAutoStack bool
	// FastDenReturn tells the client's delay bar that the Bandit Den scroll
	// counts down half its Param1 (port-only, not native; false is native).
	FastDenReturn bool
	// TradeTiers tells the client's TRADESCALE combo to rate a purchase by
	// gold against the shard's tiers (port-only, not native; false is native).
	TradeTiers bool
	// JobSuitHidesArmor tells the client that trader and hunter suits hide
	// worn armour as thief suits do (port-only, not native; false is native).
	JobSuitHidesArmor bool
	// MoonlitThiefSuit tells the client to draw the level-20 thief suits as
	// Moonlit Ash (port-only, not native; false is native).
	MoonlitThiefSuit bool
	// UiTuning is the shard's client tuning (port-only, uituning.go); nil
	// while every value is its default, so the blob stays as before.
	UiTuning *UiTuning
}

/*
================
bootstrapFailureView
================
*/
type bootstrapFailureView struct {
	NativeResult    int               `json:"nativeResult"`
	NativeErrorCode int               `json:"nativeErrorCode"`
	NativeError     *NativeAgentError `json:"nativeError"`
	Packets         []Packet          `json:"packets"`
	Reason          string            `json:"reason,omitempty"`
}

/*
================
bootstrapSuccessView
================
*/
type bootstrapSuccessView struct {
	DiagnosticSessionID       string                   `json:"diagnosticSessionId,omitempty"`
	InventorySlotCount        uint8                    `json:"inventorySlotCount"`
	EquipmentSlotCount        uint8                    `json:"equipmentSlotCount"`
	SimulationProtocolVersion int                      `json:"simulationProtocolVersion"`
	NativeResult              int                      `json:"nativeResult"`
	ProtocolVersion           int                      `json:"protocolVersion"`
	DivisionID                string                   `json:"divisionId"`
	Character                 *characterSnapshotView   `json:"character"`
	BootstrapMode             string                   `json:"bootstrapMode"`
	AcademyMember             bool                     `json:"academyMember"`
	EventGuideStateMask       uint32                   `json:"eventGuideStateMask"`
	RefObjSnapshot            []RefObjRow              `json:"refObjSnapshot"`
	RefSkillSnapshot          []SpawnSkillRow          `json:"refSkillSnapshot"`
	SkillCooldowns            []EntrySkillCooldown     `json:"skillCooldowns"`
	RefItemSnapshot           []RefItemRow             `json:"refItemSnapshot"`
	MagicOptionSnapshot       []MagicOptionRow         `json:"magicOptionSnapshot,omitempty"`
	AvatarMagicOptions        []AvatarMagicOptionRow   `json:"avatarMagicOptions,omitempty"`
	SiegeItemForgeGroups      []SiegeItemForgeGroupRow `json:"siegeItemForgeGroups"`
	SiegeFortressData         []SiegeFortressDataRow   `json:"siegeFortressData"`
	GameWorldData             []GameWorldDataRow       `json:"gameWorldData"`
	AvatarItems               []EquipItemRow           `json:"avatarItems"`
	EquipItems                []EquipItemRow           `json:"equipItems"`
	LocalPlayerEntry          *LocalPlayerEntry        `json:"localPlayerEntry"`
	ChatPermissions           ChatPermissions          `json:"chatPermissions"`
	ChatMessages              []string                 `json:"chatMessages"`
	SystemMessages            interface{}              `json:"systemMessages"`
	Packets                   []Packet                 `json:"packets"`
	UnlimitedItems            []uint32                 `json:"unlimitedItems,omitempty"`
	MasteryTotalOverride      int64                    `json:"masteryTotalOverride,omitempty"`
	LevelCap                  int64                    `json:"levelCap,omitempty"`
	StorageAutoStack          bool                     `json:"storageAutoStack,omitempty"`
	FastDenReturn             bool                     `json:"fastDenReturn,omitempty"`
	TradeTiers                bool                     `json:"tradeTiers,omitempty"`
	JobSuitHidesArmor         bool                     `json:"jobSuitHidesArmor,omitempty"`
	MoonlitThiefSuit          bool                     `json:"moonlitThiefSuit,omitempty"`
	UiTuning                  *UiTuning                `json:"uiTuning,omitempty"`
}

/*
================
characterSnapshotView

characterSnapshotView adds derived values without polluting persistence.
================
*/
type characterSnapshotView struct {
	*Character
	MaxHP int64 `json:"maxHp"`
	MaxMP int64 `json:"maxMp"`
}

/*
================
browserCharacterView

browserCharacterView carries the clamped hp/mp and derived maxHp/maxMp fields
the browser expects on OpEnterWorldResult without a map round-trip.
================
*/
type browserCharacterView struct {
	*Character
	HP    int64 `json:"hp"`
	MP    int64 `json:"mp"`
	MaxHP int64 `json:"maxHp"`
	MaxMP int64 `json:"maxMp"`
}

/*
================
bootstrapBlobFailureView
================
*/
type bootstrapBlobFailureView struct {
	NativeResult    int               `json:"nativeResult"`
	NativeErrorCode int               `json:"nativeErrorCode"`
	NativeError     *NativeAgentError `json:"nativeError"`
	Reason          string            `json:"reason,omitempty"`
}

/*
================
bootstrapBlobSuccessView
================
*/
type bootstrapBlobSuccessView struct {
	DiagnosticSessionID       string                   `json:"diagnosticSessionId,omitempty"`
	InventorySlotCount        uint8                    `json:"inventorySlotCount"`
	EquipmentSlotCount        uint8                    `json:"equipmentSlotCount"`
	SimulationProtocolVersion int                      `json:"simulationProtocolVersion"`
	NativeResult              int                      `json:"nativeResult"`
	ProtocolVersion           int                      `json:"protocolVersion"`
	DivisionID                string                   `json:"divisionId"`
	Character                 *browserCharacterView    `json:"character"`
	BootstrapMode             string                   `json:"bootstrapMode"`
	AcademyMember             bool                     `json:"academyMember"`
	EventGuideStateMask       uint32                   `json:"eventGuideStateMask"`
	RefObjSnapshot            []RefObjRow              `json:"refObjSnapshot"`
	RefSkillSnapshot          []SpawnSkillRow          `json:"refSkillSnapshot"`
	SkillCooldowns            []EntrySkillCooldown     `json:"skillCooldowns"`
	RefItemSnapshot           []RefItemRow             `json:"refItemSnapshot"`
	MagicOptionSnapshot       []MagicOptionRow         `json:"magicOptionSnapshot,omitempty"`
	AvatarMagicOptions        []AvatarMagicOptionRow   `json:"avatarMagicOptions,omitempty"`
	SiegeItemForgeGroups      []SiegeItemForgeGroupRow `json:"siegeItemForgeGroups"`
	SiegeFortressData         []SiegeFortressDataRow   `json:"siegeFortressData"`
	GameWorldData             []GameWorldDataRow       `json:"gameWorldData"`
	AvatarItems               []EquipItemRow           `json:"avatarItems"`
	EquipItems                []EquipItemRow           `json:"equipItems"`
	LocalPlayerEntry          *LocalPlayerEntry        `json:"localPlayerEntry"`
	ChatPermissions           ChatPermissions          `json:"chatPermissions"`
	ChatMessages              []string                 `json:"chatMessages"`
	SystemMessages            interface{}              `json:"systemMessages"`
	UnlimitedItems            []uint32                 `json:"unlimitedItems,omitempty"`
	MasteryTotalOverride      int64                    `json:"masteryTotalOverride,omitempty"`
	LevelCap                  int64                    `json:"levelCap,omitempty"`
	StorageAutoStack          bool                     `json:"storageAutoStack,omitempty"`
	FastDenReturn             bool                     `json:"fastDenReturn,omitempty"`
	TradeTiers                bool                     `json:"tradeTiers,omitempty"`
	JobSuitHidesArmor         bool                     `json:"jobSuitHidesArmor,omitempty"`
	MoonlitThiefSuit          bool                     `json:"moonlitThiefSuit,omitempty"`
	UiTuning                  *UiTuning                `json:"uiTuning,omitempty"`
}

/*
================
bootstrapReferenceBlobSuccessView
================
*/
type bootstrapReferenceBlobSuccessView struct {
	DiagnosticSessionID       string                   `json:"diagnosticSessionId,omitempty"`
	InventorySlotCount        uint8                    `json:"inventorySlotCount"`
	EquipmentSlotCount        uint8                    `json:"equipmentSlotCount"`
	SimulationProtocolVersion int                      `json:"simulationProtocolVersion"`
	NativeResult              int                      `json:"nativeResult"`
	ProtocolVersion           int                      `json:"protocolVersion"`
	DivisionID                string                   `json:"divisionId"`
	Character                 *browserCharacterView    `json:"character"`
	BootstrapMode             string                   `json:"bootstrapMode"`
	AcademyMember             bool                     `json:"academyMember"`
	EventGuideStateMask       uint32                   `json:"eventGuideStateMask"`
	RefObjSnapshot            []RefObjRow              `json:"refObjSnapshot"`
	SkillCooldowns            []EntrySkillCooldown     `json:"skillCooldowns"`
	RefItemSnapshot           []RefItemRow             `json:"refItemSnapshot"`
	MagicOptionSnapshot       []MagicOptionRow         `json:"magicOptionSnapshot,omitempty"`
	AvatarMagicOptions        []AvatarMagicOptionRow   `json:"avatarMagicOptions,omitempty"`
	SiegeItemForgeGroups      []SiegeItemForgeGroupRow `json:"siegeItemForgeGroups"`
	SiegeFortressData         []SiegeFortressDataRow   `json:"siegeFortressData"`
	GameWorldData             []GameWorldDataRow       `json:"gameWorldData"`
	AvatarItems               []EquipItemRow           `json:"avatarItems"`
	EquipItems                []EquipItemRow           `json:"equipItems"`
	LocalPlayerEntry          *LocalPlayerEntry        `json:"localPlayerEntry"`
	ChatPermissions           ChatPermissions          `json:"chatPermissions"`
	ChatMessages              []string                 `json:"chatMessages"`
	SystemMessages            interface{}              `json:"systemMessages"`
	UnlimitedItems            []uint32                 `json:"unlimitedItems,omitempty"`
	MasteryTotalOverride      int64                    `json:"masteryTotalOverride,omitempty"`
	LevelCap                  int64                    `json:"levelCap,omitempty"`
	StorageAutoStack          bool                     `json:"storageAutoStack,omitempty"`
	FastDenReturn             bool                     `json:"fastDenReturn,omitempty"`
	TradeTiers                bool                     `json:"tradeTiers,omitempty"`
	JobSuitHidesArmor         bool                     `json:"jobSuitHidesArmor,omitempty"`
	MoonlitThiefSuit          bool                     `json:"moonlitThiefSuit,omitempty"`
	UiTuning                  *UiTuning                `json:"uiTuning,omitempty"`
}

/*
================
BootstrapResult.browserCharacter
================
*/
func (r *BootstrapResult) browserCharacter() *browserCharacterView {
	if r.Character == nil {
		return nil
	}
	return &browserCharacterView{
		Character: r.Character,
		HP:        CurrentHP(r.Character),
		MP:        CurrentMP(r.Character),
		MaxHP:     DerivedMaxHP(r.Character),
		MaxMP:     DerivedMaxMP(r.Character),
	}
}

/*
================
BootstrapResult.blobView
================
*/
func (r *BootstrapResult) blobView() any {
	if r.NativeResult == nativeResultFailure {
		return bootstrapBlobFailureView{
			NativeResult:    r.NativeResult,
			NativeErrorCode: r.NativeErrorCode,
			NativeError:     r.NativeError,
			Reason:          r.Reason,
		}
	}
	cooldowns := r.SkillCooldowns
	if cooldowns == nil {
		cooldowns = []EntrySkillCooldown{}
	}
	return bootstrapBlobSuccessView{
		DiagnosticSessionID:       r.DiagnosticSessionID,
		InventorySlotCount:        inventory.BagEnd(r.Character),
		EquipmentSlotCount:        inventory.EquipmentSlotEnd,
		SimulationProtocolVersion: 1,
		NativeResult:              r.NativeResult,
		ProtocolVersion:           BootstrapProtocolVersion,
		DivisionID:                r.DivisionID,
		Character:                 r.browserCharacter(),
		BootstrapMode:             BootstrapMode,
		AcademyMember:             r.AcademyMember,
		EventGuideStateMask:       r.EventGuideStateMask,
		RefObjSnapshot:            r.RefObjSnapshot,
		RefSkillSnapshot:          r.RefSkillSnapshot,
		SkillCooldowns:            cooldowns,
		RefItemSnapshot:           r.RefItemSnapshot,
		MagicOptionSnapshot:       r.MagicOptionSnapshot,
		AvatarMagicOptions:        r.AvatarMagicOptions,
		SiegeItemForgeGroups:      r.SiegeItemForgeGroups,
		SiegeFortressData:         r.SiegeFortressData,
		GameWorldData:             r.GameWorldData,
		AvatarItems:               r.AvatarItems,
		EquipItems:                r.EquipItems,
		LocalPlayerEntry:          r.LocalPlayerEntry,
		ChatPermissions:           r.ChatPermissions,
		ChatMessages:              r.ChatMessages,
		SystemMessages:            r.SystemMessages,
		UnlimitedItems:            r.UnlimitedItems,
		MasteryTotalOverride:      r.MasteryTotalOverride,
		LevelCap:                  r.LevelCap,
		StorageAutoStack:          r.StorageAutoStack,
		FastDenReturn:             r.FastDenReturn,
		TradeTiers:                r.TradeTiers,
		JobSuitHidesArmor:         r.JobSuitHidesArmor,
		MoonlitThiefSuit:          r.MoonlitThiefSuit,
		UiTuning:                  r.UiTuning,
	}
}

/*
================
BootstrapResult.referenceBlobView
================
*/
func (r *BootstrapResult) referenceBlobView() any {
	if r.NativeResult == nativeResultFailure {
		return bootstrapBlobFailureView{
			NativeResult:    r.NativeResult,
			NativeErrorCode: r.NativeErrorCode,
			NativeError:     r.NativeError,
			Reason:          r.Reason,
		}
	}
	cooldowns := r.SkillCooldowns
	if cooldowns == nil {
		cooldowns = []EntrySkillCooldown{}
	}
	return bootstrapReferenceBlobSuccessView{
		DiagnosticSessionID:       r.DiagnosticSessionID,
		InventorySlotCount:        inventory.BagEnd(r.Character),
		EquipmentSlotCount:        inventory.EquipmentSlotEnd,
		SimulationProtocolVersion: 1,
		NativeResult:              r.NativeResult,
		ProtocolVersion:           BootstrapProtocolVersion,
		DivisionID:                r.DivisionID,
		Character:                 r.browserCharacter(),
		BootstrapMode:             BootstrapMode,
		AcademyMember:             r.AcademyMember,
		EventGuideStateMask:       r.EventGuideStateMask,
		RefObjSnapshot:            r.RefObjSnapshot,
		SkillCooldowns:            cooldowns,
		RefItemSnapshot:           r.RefItemSnapshot,
		MagicOptionSnapshot:       r.MagicOptionSnapshot,
		AvatarMagicOptions:        r.AvatarMagicOptions,
		SiegeItemForgeGroups:      r.SiegeItemForgeGroups,
		SiegeFortressData:         r.SiegeFortressData,
		GameWorldData:             r.GameWorldData,
		AvatarItems:               r.AvatarItems,
		EquipItems:                r.EquipItems,
		LocalPlayerEntry:          r.LocalPlayerEntry,
		ChatPermissions:           r.ChatPermissions,
		ChatMessages:              r.ChatMessages,
		SystemMessages:            r.SystemMessages,
		UnlimitedItems:            r.UnlimitedItems,
		MasteryTotalOverride:      r.MasteryTotalOverride,
		LevelCap:                  r.LevelCap,
		StorageAutoStack:          r.StorageAutoStack,
		FastDenReturn:             r.FastDenReturn,
		TradeTiers:                r.TradeTiers,
		JobSuitHidesArmor:         r.JobSuitHidesArmor,
		MoonlitThiefSuit:          r.MoonlitThiefSuit,
		UiTuning:                  r.UiTuning,
	}
}

/*
================
MarshalJSON

MarshalJSON emits the native failure or success envelope.
================
*/
func (r *BootstrapResult) MarshalJSON() ([]byte, error) {
	if r.NativeResult == nativeResultFailure {
		return json.Marshal(bootstrapFailureView{
			NativeResult:    r.NativeResult,
			NativeErrorCode: r.NativeErrorCode,
			NativeError:     r.NativeError,
			Packets:         r.Packets,
			Reason:          r.Reason,
		})
	}

	var characterView *characterSnapshotView
	if r.Character != nil {
		characterView = &characterSnapshotView{
			Character: r.Character,
			MaxHP:     DerivedMaxHP(r.Character),
			MaxMP:     DerivedMaxMP(r.Character),
		}
	}
	cooldowns := r.SkillCooldowns
	if cooldowns == nil {
		cooldowns = []EntrySkillCooldown{}
	}
	return json.Marshal(bootstrapSuccessView{
		DiagnosticSessionID:       r.DiagnosticSessionID,
		InventorySlotCount:        inventory.BagEnd(r.Character),
		EquipmentSlotCount:        inventory.EquipmentSlotEnd,
		SimulationProtocolVersion: 1,
		NativeResult:              r.NativeResult,
		ProtocolVersion:           BootstrapProtocolVersion,
		DivisionID:                r.DivisionID,
		Character:                 characterView,
		BootstrapMode:             BootstrapMode,
		AcademyMember:             r.AcademyMember,
		EventGuideStateMask:       r.EventGuideStateMask,
		RefObjSnapshot:            r.RefObjSnapshot,
		RefSkillSnapshot:          r.RefSkillSnapshot,
		SkillCooldowns:            cooldowns,
		RefItemSnapshot:           r.RefItemSnapshot,
		MagicOptionSnapshot:       r.MagicOptionSnapshot,
		AvatarMagicOptions:        r.AvatarMagicOptions,
		SiegeItemForgeGroups:      r.SiegeItemForgeGroups,
		SiegeFortressData:         r.SiegeFortressData,
		GameWorldData:             r.GameWorldData,
		AvatarItems:               r.AvatarItems,
		EquipItems:                r.EquipItems,
		LocalPlayerEntry:          r.LocalPlayerEntry,
		ChatPermissions:           r.ChatPermissions,
		ChatMessages:              r.ChatMessages,
		SystemMessages:            r.SystemMessages,
		Packets:                   r.Packets,
		UnlimitedItems:            r.UnlimitedItems,
		MasteryTotalOverride:      r.MasteryTotalOverride,
		LevelCap:                  r.LevelCap,
		StorageAutoStack:          r.StorageAutoStack,
		FastDenReturn:             r.FastDenReturn,
		TradeTiers:                r.TradeTiers,
		JobSuitHidesArmor:         r.JobSuitHidesArmor,
		MoonlitThiefSuit:          r.MoonlitThiefSuit,
		UiTuning:                  r.UiTuning,
	})
}

/*
================
Failure

Failure builds a failed bootstrap result.
================
*/
func Failure(nativeErrorCode int, reason string) *BootstrapResult {
	errorCode := nativeErrorCode & 0xff
	described := DescribeNativeAgentError(errorCode)
	return &BootstrapResult{
		NativeResult:    nativeResultFailure,
		NativeErrorCode: errorCode,
		NativeError:     &described,
		Reason:          reason,
		Packets:         []Packet{},
	}
}
