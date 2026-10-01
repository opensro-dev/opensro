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
	// CCharacterData+0x210, parsed from characterdata column 88. Despite
	// older host names calling the slot a skill id, sub_692cb0 consumes it
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
The two vectors are the exact sub_64b440 (+0x00) and sub_64b450 (+0x10)
projections consumed by the fortress NPC manager-hire menu.
================
*/
type SiegeItemForgeGroupRow struct {
	GroupID         uint32   `json:"groupId"`
	SmithItemRefs   []uint32 `json:"smithItemRefs"`
	TrainerItemRefs []uint32 `json:"trainerItemRefs"`
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
	NativeResult    int
	NativeErrorCode int
	NativeError     *NativeAgentError
	Reason          string

	DivisionID           string
	Character            *Character
	AcademyMember        bool
	EventGuideStateMask  uint32
	RefObjSnapshot       []RefObjRow
	RefSkillSnapshot     []SpawnSkillRow
	RefItemSnapshot      []RefItemRow
	MagicOptionSnapshot  []MagicOptionRow
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
	UnlimitedItems []uint32
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
	RefItemSnapshot           []RefItemRow             `json:"refItemSnapshot"`
	MagicOptionSnapshot       []MagicOptionRow         `json:"magicOptionSnapshot,omitempty"`
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
	return json.Marshal(bootstrapSuccessView{
		InventorySlotCount:        inventory.BagSlotEnd,
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
		RefItemSnapshot:           r.RefItemSnapshot,
		MagicOptionSnapshot:       r.MagicOptionSnapshot,
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
