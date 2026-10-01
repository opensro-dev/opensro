/*
===========================================================================

equiproster.go - item references and the equipment roster

===========================================================================
*/

package enterworld

import (
	"strconv"

	"opensro.online/server/internal/game/world/monster"

	log "github.com/sirupsen/logrus"
)

// ItemRef is the typed server projection of one v1.150 itemdata row.
/*
================
ItemRef
================
*/
type ItemRef struct {
	DescriptionSymbol string
	Icon              string
	RefObjID          uint32
	Codename          string
	TypeIDs           [4]int64
	NameStrID         string
	Name              string
	// NativeFields is the numeric RefItemData field projection consumed by
	// the browser's native item mirror. Keys are the offset-stable names in
	// itemdataRecordColumns; malformed or absent source cells are omitted.
	NativeFields NativeFields
	// ParamDescriptions retains the twenty authored (Param, Desc) string fields.
	// Native item families interpret these at their own admission boundary.
	ParamDescriptions [20]string
	// AssociatedCharacterCodename is itemdata column 119. For the
	// ITEM_COS_* summoner family it names the characterdata row the server
	// must instantiate. It is a natural-key join, never a client-supplied
	// RefObj id.
	AssociatedCharacterCodename string
	// Skill consumables resolve the same authored Param1 reference as a skill.
	AssociatedSkillCodename string
	// Return-scroll Param3 text (column 123), consumed by v1.188 4A0380.
	ReturnDestination string
	// Combat is the typed v1.150 RefItemData stat source consumed by the
	// server damage plane. It is separate from NativeFields because Combat has
	// a strict all-or-none
	// loader contract and may not silently coerce a missing combat column.
	Combat *ItemCombatRef
	// VarianceIntMin1c0 is record.varianceIntMin1c0, the fresh-item
	// durability derivation input (sub_78bd00 case 0 collapses to it at
	// variance 0, so current = max and the tooltip reads N/N like retail).
	// Pointer so an absent column takes the Node coercion fallback (100)
	// while a present zero clamps to 1.
	VarianceIntMin1c0 *int64

	// Equip requirement columns, typed for the server-side equip gates (the
	// native client's full-mask sub_789c60 bits compare the same
	// RefItemData bytes). Absent columns default to their pass values so a
	// degraded textdata load never invents a refusal.

	// Country is media column 14 (native ref[0x27]): 0 China, 1 Europe,
	// 3 = every character (the sub_789c60 bit 0x200 wildcard). Default 3.
	Country int64
	// ReqQuadTypes/ReqQuadValues are the four typed requirement pairs
	// (media columns 32/34/36/38 types + 33/35/37/39 values; native
	// +0xc8..+0xd4 types and +0xd8..+0xe4 values). Semantics per the
	// sub_789c60 legs:
	//   type 1     = character level (bit 0x020 vs player +0x820);
	//   type > 0xa = a masterydata ID (bit 0x100 vs the trained level;
	//                EU armor carries 513..518);
	//   types 2/3/4 (job grades) and 0xa (guild level) are tooltip-only -
	//                neither native bit tests them;
	//   -1         = empty slot.
	// Both native legs are ANY-of with an early exit: the first selected
	// slot that passes clears the whole bit. Defaults: types -1, values 0.
	// All four pairs also ride NativeFields as
	// reqLevelType1/requiredLevel + reqLevelType2..4/requiredLevel2..4
	// (itemdataRecordColumns).
	ReqQuadTypes  [4]int64
	ReqQuadValues [4]int64
	// RequiredSex is media column 58 (native ref[0x6b] / +0x1ac, bit
	// 0x040): 0 female, 1 male, 2 = unisex (verified against the shipped
	// itemdata: _W_ avatar rows carry 0, _M_ rows 1, weapons 2). Default 2.
	RequiredSex int64
	// RequiredStr / RequiredInt are media columns 59/60 (native
	// +0x1b0/+0x1b4, bits 0x008/0x010 vs the player STR/INT words
	// +0x834/+0x836). Zero across the entire shipped itemdata; default 0
	// (no requirement).
	RequiredStr int64
	RequiredInt int64
	// MaxDurability is media column 64 (Dur_U / varianceIntMax1c4): the
	// class-level "this item HAS a durability attribute" marker the razed
	// gate (sub_789c60 bit 0x004) is conditioned on - native tests the
	// instance attribute blob (+0xc0)[5], which only durability-bearing
	// classes carry. Shipped data: weapons/armor N > 0, accessories 0.
	// Default 0 (no durability concept, gate never fires).
	MaxDurability int64
	// Potion recovery inputs from v1.150 itemdata columns 118/120/122/124.
	// Kept typed and server-only because the browser RefItemData mirror does
	// not consume potion recovery semantics.
	RecoveryHP        float64
	RecoveryHPPercent float64
	RecoveryMP        float64
	RecoveryMPPercent float64
	// Cure fields are the same param stride as itemParamN (column 118+i*2
	// is RefItem+0x29C+i*4). 49B710's universal pill (TID4 1) passes
	// +0x2A0, +0x2E0, +0x2C0 as the mask, the chance numerator and the
	// grade base. A level cure reads six words at +0x2A0/+0x2C0/+0x2E0/
	// +0x300/+0x320/+0x340 (49AC50). The loaded param window ends at
	// +0x2E8, so the last three words stay 0.
	CureMask     int64
	CureChance   int64
	CureGradeSub int64
	CureLevels   [6]int64
	// FortressRoleMask is media column 124 (native +0x2ac / ref[0xab],
	// LOW BYTE compared): the fortress-guild role bits allowed to wield a
	// fortress siege weapon (sub_789c60 bit 0x400). Only the 14
	// TID-(3,1,6,16) hammer/axe rows carry it, all -1 (0xff = any role).
	// Default 0 - non-fortress items never reach the compare (the TID
	// gate refuses the leg first).
	FortressRoleMask int64
}

// TypeFlags packs TypeID1..4 exactly as the native RefItemData word the
// parser gates on: TID1<<2 | TID2<<5 | TID3<<7 | TID4<<11.
/*
================
TypeFlags
================
*/
func (r *ItemRef) TypeFlags() uint16 {
	return uint16(((r.TypeIDs[0] << 2) | (r.TypeIDs[1] << 5) | (r.TypeIDs[2] << 7) | (r.TypeIDs[3] << 11)) & 0xffff)
}

/*
==================
ItemRefSource

ItemRefSource resolves itemdata rows by codename. The itemdata extraction
is shared cross-lane state; the bootstrap only reads it. A nil source
behaves like the Node server with MISSION_EQUIP_ITEMS=0 (no wire items).
==================
*/
type ItemRefSource interface {
	ItemRefByCodename(codename string) (*ItemRef, bool)
}

/*
==================
CharacterRef

CharacterRef is the exact characterdata projection required to create a
COS and seed its RefObj mirror before a 0x30D7 spawn arrives. In
particular, MountedAttackCapability210 is media column 88: Rizin proves
sub_808670 stores that parsed dword at CCharacterData+0x210, and
sub_692cb0 treats a nonzero value as the mounted-attack gate.
==================
*/
type CharacterRef struct {
	// Parameters shares the RefObjChar tail with monsters, not the rider's
	// player keeper. The enclosing reference owns identity and movement.
	Parameters                 monster.MonsterRef
	RefObjID                   uint32
	TidWord                    uint16
	Codename                   string
	NameStrID                  string
	Name                       string
	WalkSpeed                  float32
	RunSpeed                   float32
	Scale                      float32
	Level                      uint8
	MaxHP                      uint32
	MaxMP                      uint32
	MountedAttackCapability210 uint32
	// RefObjChar parameter 4: minutes per percentage point of attack-pet HGP.
	SatietyMinutes uint32
	// Client column 72, server RefObjCommon+8C bit 0x400: vehicle use permission.
	CanRide bool
}

/*
==================
CharacterRefSource

CharacterRefSource resolves the itemdata -> characterdata COS join and
exposes the complete set of rows summonable by enabled ITEM_COS_* items.
It is intentionally a separate facet from ItemRefSource so existing
item-only fixtures do not have to fabricate character media.
==================
*/
type CharacterRefSource interface {
	CharacterRefByCodename(codename string) (*CharacterRef, bool)
	SummonableCharacterRefs() []CharacterRef
}

/*
==================
WireItem

WireItem is one wire-armed inventory row (the Node missionInventoryWireItems
output): the persisted row with varianceBits re-armed as a real u64 for the
binary writer, plus the semantic item identity and stat record.
==================
*/
type WireItem struct {
	Icon         string
	Slot         int64
	RefObjID     uint32
	TypeFlags    uint16
	Codename     string
	Name         string
	NativeFields NativeFields
	// ParamDescriptions retains the twenty authored (Param, Desc) string fields.
	// Native item families interpret these at their own admission boundary.
	ParamDescriptions [20]string
	Kind              string
	Plus              int64
	VarianceBits      uint64
	Durability        int64
	StackCount        int64
	// MagicOptions are the encoded magic-option u64 params the CSOItem body
	// emits after its count byte (see InventoryRow.MagicOptions for the
	// wire grammar and content authority).
	MagicOptions      []uint64
	TransformRefObjID uint32 // monster capsule Data
}

/*
==================
ResolveEquipRoster

The wire rows of the starter items a character's creation choice grants
(CreationStarterItems). Rows missing from itemdata are skipped with a
warning, never fabricated.
==================
*/
func ResolveEquipRoster(character *Character, modelCodename string, items ItemRefSource, equipItemsEnabled bool) []WireItem {
	if !equipItemsEnabled || items == nil {
		return []WireItem{}
	}
	type want struct {
		codename     string
		slot         int64
		varianceBits uint64
	}
	wanted := []want{}
	for _, item := range CreationStarterItems(character, modelCodename) {
		wanted = append(wanted, want{codename: item.Codename, slot: item.Slot})
	}
	// Diagnostic inventory witnesses belong in test fixtures, never starter grants.

	roster := []WireItem{}
	for _, entry := range wanted {
		row, ok := items.ItemRefByCodename(entry.codename)
		if !ok || row == nil {
			log.Warnf("bootstrap: starter item %s missing from itemdata", entry.codename)
			continue
		}
		name := row.Name
		durability := coerceInt(row.VarianceIntMin1c0, 1, 0xffffffff, 100)
		roster = append(roster, WireItem{
			Slot:         entry.slot,
			RefObjID:     row.RefObjID,
			TypeFlags:    row.TypeFlags(),
			Codename:     row.Codename,
			Name:         name,
			NativeFields: row.NativeFields,
			Icon:         row.Icon,
			Kind:         "equipment",
			Plus:         0,
			VarianceBits: entry.varianceBits,
			Durability:   durability,
			StackCount:   1,
		})
	}
	// The native creation procedure has an independent @DefaultArrow branch:
	// 250 starter arrows/bolts in the secondary socket. They are not garments
	// and cannot be recovered by the visual-loadout overlay after admission.
	for _, item := range roster {
		if item.Slot != 6 {
			continue
		}
		weapon, ok := items.ItemRefByCodename(item.Codename)
		if !ok || weapon == nil {
			continue
		}
		code := ""
		switch weapon.TypeIDs[3] {
		case 6:
			code = "ITEM_ETC_AMMO_ARROW_01_DEF"
		case 12:
			code = "ITEM_ETC_AMMO_BOLT_01_DEF"
		}
		if code == "" {
			continue
		}
		ammo, ok := items.ItemRefByCodename(code)
		if !ok || ammo == nil {
			log.Warnf("bootstrap: starter ammunition %s missing from itemdata", code)
			continue
		}
		roster = append(roster, WireItem{Slot: 7, RefObjID: ammo.RefObjID, Codename: ammo.Codename, TypeFlags: ammo.TypeFlags(), NativeFields: ammo.NativeFields, Name: ammo.Name, Icon: ammo.Icon, Kind: "item", StackCount: 250})
		break
	}
	return roster
}

/*
==================
EnsureMissionInventory

EnsureMissionInventory ports ensureMissionInventory: the character's
AUTHORITATIVE inventory, initialized once from the equip roster (first-ever
bootstrap) and persisted with the record so item moves survive re-login.
Rows persisted before the stack column existed default to a single unit.
==================
*/
func EnsureMissionInventory(character *Character, equipRoster []WireItem) []InventoryRow {
	if character.MissionInventory == nil {
		rows := make([]InventoryRow, 0, len(equipRoster))
		for _, item := range equipRoster {
			slot := item.Slot
			if slot < 0 || slot > 0xff {
				slot = 6
			}
			stackCount := item.StackCount
			if stackCount < 0 {
				stackCount = 0
			}
			if stackCount > 0xffff {
				stackCount = 0xffff
			}
			rows = append(rows, InventoryRow{
				Slot:              slot,
				RefObjID:          item.RefObjID,
				Codename:          item.Codename,
				TypeFlags:         item.TypeFlags,
				Plus:              coercePlainInt(item.Plus, 0, 0xff),
				VarianceBits:      strconv.FormatUint(item.VarianceBits, 10),
				Durability:        coercePlainInt(item.Durability, 0, 0xffffffff),
				StackCount:        stackCount,
				MagicOptions:      copyMagicOptions(item.MagicOptions),
				TransformRefObjID: item.TransformRefObjID,
			})
		}
		character.MissionInventory = rows
	}
	for index := range character.MissionInventory {
		if character.MissionInventory[index].StackCount == 0 {
			character.MissionInventory[index].StackCount = 1
		}
	}
	return character.MissionInventory
}

/*
==================
StarterEquipRoster

The starter item rows of a character's creation choice, for its resolved
model. Reads only the roster and itemdata, so callers resolve it outside
the authority write lock.
==================
*/
func StarterEquipRoster(character *Character, roster *Roster, items ItemRefSource, equipItemsEnabled bool) []WireItem {
	entry := ResolveLocalPlayerEntry(character, roster)
	return ResolveEquipRoster(character, entry.VisualLoadout.ModelCodename, items, equipItemsEnabled)
}

/*
==================
GrantStarterInventory

The creation grant: the starter items as the authoritative inventory and
the starting gold when the record holds none. It applies once; a character
that already has an inventory is left unchanged.
==================
*/
func GrantStarterInventory(character *Character, starter []WireItem) {
	if character.MissionInventory != nil {
		return
	}
	EnsureMissionInventory(character, starter)
	gold := int64(0)
	if character.Gold != nil {
		gold = coerceInt(character.Gold, 0, 1<<53-1, 0)
	}
	if gold == 0 {
		gold = defaultStartingGold
	}
	character.Gold = &gold
}

/*
==================
StarterInventorySeeder

The store's creation seed (store.Options.DefaultInventory), so a new
character is dressed in the character list before its first entry.
==================
*/
func StarterInventorySeeder(roster *Roster, items ItemRefSource, equipItemsEnabled bool) func(*Character) {
	return func(character *Character) {
		GrantStarterInventory(character, StarterEquipRoster(character, roster, items, equipItemsEnabled))
	}
}

/*
==================
copyMagicOptions

copyMagicOptions returns an owned copy, nil staying nil (empty and absent
emit the same zero count byte; nil keeps the persisted record's omitempty
shape round-trippable).
==================
*/
func copyMagicOptions(options []uint64) []uint64 {
	if options == nil {
		return nil
	}
	out := make([]uint64, len(options))
	copy(out, options)
	return out
}

/*
================
coercePlainInt
================
*/
func coercePlainInt(value, min, max int64) int64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// InventoryWireItems ports missionInventoryWireItems: the persisted rows
// re-armed for the binary writer (decimal-string variance -> u64).
/*
================
InventoryWireItems
================
*/
func InventoryWireItems(rows []InventoryRow) []WireItem {
	out := make([]WireItem, 0, len(rows))
	for _, row := range rows {
		variance, err := strconv.ParseUint(row.VarianceBits, 10, 64)
		if err != nil {
			variance = 0
		}
		out = append(out, WireItem{
			Slot:              row.Slot,
			RefObjID:          row.RefObjID,
			TypeFlags:         row.TypeFlags,
			Codename:          row.Codename,
			Plus:              row.Plus,
			VarianceBits:      variance,
			Durability:        row.Durability,
			StackCount:        row.StackCount,
			MagicOptions:      copyMagicOptions(row.MagicOptions),
			TransformRefObjID: row.TransformRefObjID,
		})
	}
	return out
}
