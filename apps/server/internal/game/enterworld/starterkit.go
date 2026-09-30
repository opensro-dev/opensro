/*
===========================================================================

starterkit.go - the operator's beta starter kit

Not a native rule. For the public beta the operator gives every character,
new and existing, a return scroll and the fastest movement-speed scroll,
and the action lane never spends them (action.Runtime.UnlimitedItems). A
character that lacks a kit item receives it in its first free bag slot on
world entry; a full bag receives nothing and the next entry tries again.

The native creation grant (creation equipment and the @DefaultArrow stack,
equiproster.go) is unchanged; the kit is added on top of it.

SRO_BETA_STARTER_KIT=on enables it (the Nomad job's beta_starter_kit
variable); anything else leaves characters exactly as the native rules make
them.

===========================================================================
*/

package enterworld

import (
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/item/inventory"
)

// EnvBetaStarterKit enables the beta starter kit.
const EnvBetaStarterKit = "SRO_BETA_STARTER_KIT"

/*
================
betaStarterKitCodenames

The ordinary return scroll (30 s recall, itemdata 61) and the +100% movement
speed scroll (SKILL_MALL_MOVE_SPEED_UP_100_01, the largest speed value any
v1.150 speed item carries).
================
*/
var betaStarterKitCodenames = []string{
	"ITEM_ETC_SCROLL_RETURN_01",
	"ITEM_MALL_MOVE_SPEED_UP_100",
}

/*
================
BetaStarterKitEnabled
================
*/
func BetaStarterKitEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBetaStarterKit))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
ResolveStarterKit

The kit as item rows, one of each. A codename missing from itemdata is a
data defect: it is logged and left out, never replaced by a guess.
================
*/
func ResolveStarterKit(items ItemRefSource) []WireItem {
	kit := make([]WireItem, 0, len(betaStarterKitCodenames))
	for _, codename := range betaStarterKitCodenames {
		row, ok := items.ItemRefByCodename(codename)
		if !ok || row == nil {
			log.Warnf("bootstrap: starter kit item %s missing from itemdata", codename)
			continue
		}
		kit = append(kit, WireItem{
			RefObjID:     row.RefObjID,
			TypeFlags:    row.TypeFlags(),
			Codename:     row.Codename,
			Name:         row.Name,
			NativeFields: row.NativeFields,
			Icon:         row.Icon,
			Kind:         "item",
			StackCount:   1,
		})
	}
	return kit
}

/*
================
StarterKitCodenames

The codenames of a resolved kit, for the action lane's unlimited set.
================
*/
func StarterKitCodenames(kit []WireItem) map[string]bool {
	out := make(map[string]bool, len(kit))
	for _, item := range kit {
		out[item.Codename] = true
	}
	return out
}

/*
================
StarterKitMissing

True when the character holds none of some kit item, anywhere in its
inventory.
================
*/
func StarterKitMissing(character *Character, kit []WireItem) bool {
	for _, item := range kit {
		if !holdsItem(character, item.RefObjID) {
			return true
		}
	}
	return false
}

/*
================
GrantStarterKit

Puts each missing kit item into the first free bag slot. Returns how many
were granted. The caller holds the character's mutation door.
================
*/
func GrantStarterKit(character *Character, kit []WireItem) int {
	granted := 0
	for _, item := range kit {
		if holdsItem(character, item.RefObjID) {
			continue
		}
		slot, ok := firstFreeBagSlot(character)
		if !ok {
			log.Warnf("bootstrap: %s has no free bag slot for starter kit item %s", character.Name, item.Codename)
			return granted
		}
		rows := append([]InventoryRow(nil), character.MissionInventory...)
		character.MissionInventory = append(rows, InventoryRow{
			Slot:         slot,
			RefObjID:     item.RefObjID,
			Codename:     item.Codename,
			TypeFlags:    item.TypeFlags,
			VarianceBits: "0",
			StackCount:   1,
		})
		granted++
	}
	return granted
}

/*
================
holdsItem
================
*/
func holdsItem(character *Character, refObjID uint32) bool {
	for _, row := range character.MissionInventory {
		if row.RefObjID == refObjID && row.StackCount > 0 {
			return true
		}
	}
	return false
}

/*
================
firstFreeBagSlot
================
*/
func firstFreeBagSlot(character *Character) (int64, bool) {
	used := make(map[int64]bool, len(character.MissionInventory))
	for _, row := range character.MissionInventory {
		used[row.Slot] = true
	}
	for slot := int64(inventory.EquipmentSlotEnd); slot < int64(inventory.BagSlotEnd); slot++ {
		if !used[slot] {
			return slot, true
		}
	}
	return 0, false
}
