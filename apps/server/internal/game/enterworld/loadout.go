/*
===========================================================================

loadout.go - creation choices, starter items and the visual loadout

The native client dresses every character from items: the character list
carries each worn and avatar item as (RefItemID, plus)
(SCharacterInfo_ReadFromPacket), and the client attaches them through
CCObjCharacter_SetEquipSlotVisual. The loadout therefore names the model,
the worn items, the avatar items and the scale; it never invents
presentation keys. Creation owns which starter items a choice grants, with
the native Europe weapon/protector rule (CPSCharacterCreateEurope_OnProtectorChanged
0x730690) and the darkstaff as Europe weapon choice 6.

===========================================================================
*/

package enterworld

import "sort"

// VisualItem is one worn or avatar item: native (RefItemID, plus).
type VisualItem struct {
	RefObjID uint32 `json:"refObjId"`
	Plus     int64  `json:"plus"`
}

// VisualLoadout is the character-list render contract.
type VisualLoadout struct {
	ModelCodename    string       `json:"modelCodename"`
	Items            []VisualItem `json:"items"`
	Avatars          []VisualItem `json:"avatars"`
	AnimationSetName string       `json:"animationSetName"`
	HeightScale      float64      `json:"heightScale"`
	VolumeScale      float64      `json:"volumeScale"`
}

// Worn sockets 0..8 (SCharacterInfo_BuildDisplayActor walks nine).
const wornSlotCount = 9

// The native worn sockets of the starter items.
const (
	slotChest  = 1
	slotLegs   = 4
	slotFeet   = 5
	slotWeapon = 6
	slotShield = 7
)

type europeWeaponRule struct {
	Kind           string
	ProtectorKinds []string
}

// europeWeaponRules is the Europe creation weapon list in choice order ("" =
// no weapon) with the protectors each weapon class allows (0x730690): sword,
// two-hand sword and axe take heavy or light armor; darkstaff, two-hand staff
// and harp the robe only; crossbow and dagger light armor only; the one-hand
// staff light armor or the robe.
var europeWeaponRules = []europeWeaponRule{
	{Kind: "", ProtectorKinds: nil},
	{Kind: "DAGGER", ProtectorKinds: []string{"LIGHT"}},
	{Kind: "SWORD", ProtectorKinds: []string{"HEAVY", "LIGHT"}},
	{Kind: "TSWORD", ProtectorKinds: []string{"HEAVY", "LIGHT"}},
	{Kind: "AXE", ProtectorKinds: []string{"HEAVY", "LIGHT"}},
	{Kind: "CROSSBOW", ProtectorKinds: []string{"LIGHT"}},
	{Kind: "DARKSTAFF", ProtectorKinds: []string{"CLOTHES"}},
	{Kind: "TSTAFF", ProtectorKinds: []string{"CLOTHES"}},
	{Kind: "HARP", ProtectorKinds: []string{"CLOTHES"}},
	{Kind: "STAFF", ProtectorKinds: []string{"LIGHT", "CLOTHES"}},
}

// chinaWeaponKinds is the China creation weapon list ("" = no weapon).
var chinaWeaponKinds = []string{"", "SWORD", "BLADE", "SPEAR", "TBLADE", "BOW"}

// chinaProtectorKinds is the China protector list (choices 1..3).
var chinaProtectorKinds = []string{"HEAVY", "LIGHT", "CLOTHES"}

// shieldWeaponKinds are the one-hand creation weapons whose starter set
// includes a shield.
var shieldWeaponKinds = map[string]bool{
	"CH_SWORD": true,
	"CH_BLADE": true,
	"EU_SWORD": true,
	"EU_STAFF": true,
}

// protectorParts are the garments a protector choice grants, in socket order.
var protectorParts = []struct {
	Part string
	Slot int64
}{
	{Part: "BA", Slot: slotChest},
	{Part: "LA", Slot: slotLegs},
	{Part: "FA", Slot: slotFeet},
}

const nativeDefaultAnimationSet = "default"

/*
================
CharacterCreationValid

Validates the authored creation controls against the same roster and
choice tables that grant the starter items. The HTTP boundary owns
decoding; this package owns which model, weapon and protector combinations
exist.
================
*/
func CharacterCreationValid(character *Character, roster *Roster) bool {
	if character == nil || roster == nil || roster.ModelByCodename(character.ModelCodename) == nil {
		return false
	}
	if !creationIndexInRange(character.HeightIndex, 0, 4) ||
		!creationIndexInRange(character.VolumeIndex, 0, 4) {
		return false
	}

	weaponIndex, weaponValid := creationIndex(character.WeaponIndex, 1)
	protectorIndex, protectorValid := creationIndex(character.ProtectorIndex, 0)
	if !character.WeaponSelected || !weaponValid || !protectorValid ||
		character.ArmorSelected != (protectorIndex > 0) {
		return false
	}

	if ResolveCharacterRaceKey(character) == RaceKeyChina {
		return weaponIndex < int64(len(chinaWeaponKinds)) && protectorIndex <= int64(len(chinaProtectorKinds))
	}
	if weaponIndex >= int64(len(europeWeaponRules)) || !character.ArmorSelected {
		return false
	}
	return protectorIndex <= int64(len(europeWeaponRules[int(weaponIndex)].ProtectorKinds))
}

/*
================
creationIndexInRange
================
*/
func creationIndexInRange(value *int64, minimum, maximum int64) bool {
	if value == nil {
		return false
	}
	return *value >= minimum && *value <= maximum
}

/*
================
creationIndex
================
*/
func creationIndex(value *int64, minimum int64) (int64, bool) {
	if value == nil || *value < minimum {
		return 0, false
	}
	return *value, true
}

// nativeWeaponAnimationSetByKind is the animation set of each creation
// weapon kind (the authored "harf" spelling included). The darkstaff shares
// the one-hand staff set.
var nativeWeaponAnimationSetByKind = map[string]string{
	"CH_BLADE":     "sword",
	"CH_SWORD":     "sword",
	"CH_TBLADE":    "spear",
	"CH_SPEAR":     "spear",
	"CH_BOW":       "bow",
	"EU_SWORD":     "onehand_sword",
	"EU_TSWORD":    "twohand_sword",
	"EU_AXE":       "dual_axe",
	"EU_DARKSTAFF": "onehand_staff",
	"EU_STAFF":     "onehand_staff",
	"EU_TSTAFF":    "twohand_staff",
	"EU_CROSSBOW":  "bow",
	"EU_DAGGER":    "dagger",
	"EU_HARP":      "harf",
}

// nativeWeaponAnimationSetByTid4 is the string-name projection of v1.150
// ItemTypeWord_ToAnimationSetName: weapon class -> animation set. Class 0x0a
// (darkstaff) returns the second std::string object at 0xccce10, whose text
// is "onehand_staff" (InitGlobalAnimationSetNameStrings 0xbbdd61..0xbbdd84).
var nativeWeaponAnimationSetByTid4 = map[uint16]string{
	2: "sword", 3: "sword",
	4: "spear", 5: "spear",
	6: "bow", 7: "onehand_sword", 8: "twohand_sword",
	9: "dual_axe", 10: "onehand_staff", 11: "twohand_staff", 12: "bow",
	13: "dagger", 14: "harf", 15: "onehand_staff",
}

/*
================
NativeWeaponAnimationSetNameForTypeFlags

The named archive set of the packed type word CInterfaceModel reads at
equipment slot 6. Unknown families return empty so callers keep their
evidenced fallback instead of inventing an animation set.
================
*/
func NativeWeaponAnimationSetNameForTypeFlags(typeFlags uint16) string {
	return nativeWeaponAnimationSetByTid4[(typeFlags>>11)&0x1f]
}

/*
================
ResolveWeaponKind

The creation weapon kind, or "" when no weapon was selected.
================
*/
func ResolveWeaponKind(c *Character) string {
	if c == nil || !c.WeaponSelected {
		return ""
	}
	if ResolveCharacterRaceKey(c) == RaceKeyEurope {
		weaponIndex := coerceInt(c.WeaponIndex, 0, int64(len(europeWeaponRules)-1), 0)
		return europeWeaponRules[weaponIndex].Kind
	}
	weaponIndex := coerceInt(c.WeaponIndex, 0, int64(len(chinaWeaponKinds)-1), 0)
	return chinaWeaponKinds[weaponIndex]
}

/*
================
ResolveProtectorKind

The creation armor class ("HEAVY"/"LIGHT"/"CLOTHES"), or "" for none. A
protector outside the weapon's allowed list is no protector: the native
window never offers it, and validation refuses it at creation.
================
*/
func ResolveProtectorKind(c *Character) string {
	if c == nil || !c.ArmorSelected {
		return ""
	}
	protectorIndex := coerceInt(c.ProtectorIndex, 0, 255, 0)
	if protectorIndex <= 0 {
		return ""
	}
	kinds := chinaProtectorKinds
	if ResolveCharacterRaceKey(c) == RaceKeyEurope {
		weaponIndex := coerceInt(c.WeaponIndex, 0, int64(len(europeWeaponRules)-1), 0)
		kinds = europeWeaponRules[weaponIndex].ProtectorKinds
	}
	if int(protectorIndex) > len(kinds) {
		return ""
	}
	return kinds[protectorIndex-1]
}

// StarterItem is one item a creation choice grants: its codename and socket.
type StarterItem struct {
	Codename string
	Slot     int64
}

/*
================
CreationStarterItems

The _DEF items a creation choice grants, in socket order: chest, legs,
feet, weapon, shield. No protector grants no garments; the native preview
(CharacterCreatePreview_SetChoice with index 0) shows none either.
================
*/
func CreationStarterItems(c *Character, modelCodename string) []StarterItem {
	key := RaceGenderKey(c, modelCodename)
	if len(key) < 4 {
		return nil
	}
	race := key[:2]
	items := []StarterItem{}
	if armor := ResolveProtectorKind(c); armor != "" {
		for _, piece := range protectorParts {
			items = append(items, StarterItem{Codename: "ITEM_" + key + "_" + armor + "_01_" + piece.Part + "_A_DEF", Slot: piece.Slot})
		}
	}
	if weapon := ResolveWeaponKind(c); weapon != "" {
		items = append(items, StarterItem{Codename: "ITEM_" + race + "_" + weapon + "_01_A_DEF", Slot: slotWeapon})
		if shieldWeaponKinds[race+"_"+weapon] {
			items = append(items, StarterItem{Codename: "ITEM_" + race + "_SHIELD_01_A_DEF", Slot: slotShield})
		}
	}
	return items
}

/*
================
creationAnimationSetName

The animation set of the creation weapon, used until slot 6 holds an item.
================
*/
func creationAnimationSetName(c *Character, modelCodename string) string {
	weapon := ResolveWeaponKind(c)
	key := RaceGenderKey(c, modelCodename)
	if weapon == "" || len(key) < 2 {
		return nativeDefaultAnimationSet
	}
	if name, ok := nativeWeaponAnimationSetByKind[key[:2]+"_"+weapon]; ok {
		return name
	}
	return nativeDefaultAnimationSet
}

/*
================
ResolveVisualLoadout

The character-list loadout: the model, the worn items (sockets 0..8 in
socket order) and the avatar items, straight from the persisted
inventories. The worn weapon's type word chooses the animation set, as
CInterfaceModel does; before the inventory is seeded the creation weapon
does.
================
*/
func ResolveVisualLoadout(character *Character, roster *Roster, modelRef uint32) VisualLoadout {
	modelCodename := charModelCodename(character)
	if model := roster.ModelByRefObjID(modelRef); model != nil && model.Codename != "" {
		modelCodename = model.Codename
	} else if model := roster.ModelByCodename(modelCodename); model != nil {
		modelCodename = model.Codename
	}
	loadout := VisualLoadout{
		ModelCodename:    modelCodename,
		Items:            []VisualItem{},
		Avatars:          []VisualItem{},
		AnimationSetName: creationAnimationSetName(character, modelCodename),
		HeightScale:      ResolveCharacterHeightScale(character),
		VolumeScale:      ResolveCharacterVolumeScale(character),
	}
	if character == nil {
		return loadout
	}
	worn := []InventoryRow{}
	for _, row := range character.MissionInventory {
		if row.Slot >= 0 && row.Slot < wornSlotCount && row.RefObjID != 0 {
			worn = append(worn, row)
		}
	}
	sort.SliceStable(worn, func(a, b int) bool { return worn[a].Slot < worn[b].Slot })
	for _, row := range worn {
		loadout.Items = append(loadout.Items, VisualItem{RefObjID: row.RefObjID, Plus: row.Plus})
		if row.Slot == slotWeapon {
			if name := NativeWeaponAnimationSetNameForTypeFlags(row.TypeFlags); name != "" {
				loadout.AnimationSetName = name
			}
		}
	}
	if character.AvatarInventory != nil {
		for _, row := range character.AvatarInventory.Rows {
			if row.RefObjID != 0 {
				loadout.Avatars = append(loadout.Avatars, VisualItem{RefObjID: row.RefObjID, Plus: row.Plus})
			}
		}
	}
	return loadout
}

/*
================
charModelCodename
================
*/
func charModelCodename(c *Character) string {
	if c == nil {
		return ""
	}
	return c.ModelCodename
}

/*
================
findRowBySlot
================
*/
func findRowBySlot(rows []InventoryRow, slot int64) *InventoryRow {
	for index := range rows {
		if rows[index].Slot == slot {
			return &rows[index]
		}
	}
	return nil
}
