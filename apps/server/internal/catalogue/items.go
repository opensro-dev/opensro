/*
===========================================================================

items.go - C1 items.json: every item reference the server loads

The rows come from the server's item references (enterworld textdata), the
type path from the item's type ids as the client's tooltip names them
(56CDA0 family, client item-tooltip.ts), and the status from the server's
item-use registry (action.ItemUseSupported). Tooltip lines and the web
icon are filled by the Node stage; this file leaves Tooltip empty.

===========================================================================
*/
package catalogue

import (
	"fmt"
	"sort"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// itemTypeEquipment and itemTypeExpendable are type id 2 of an item (type id 1 = 3).
const (
	itemTypeEquipment  = 1
	itemTypeSummoner   = 2
	itemTypeExpendable = 3
)

// itemTypeGold is an expendable's type id 3 for a gold heap, which is not an
// item a player holds.
const itemTypeGold = 5

// itemRequirementLevel is ReqQuadTypes' character-level requirement.
const itemRequirementLevel = 1

// itemIssueUse is where an item family with no use handler is tracked.
const itemIssueUse = "https://github.com/opensro-dev/opensro/issues?q=is%3Aissue+label%3Aitems"

/*
================
Item
================
*/
type Item struct {
	ID       uint32 `json:"id"`
	Codename string `json:"codename"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Degree   int    `json:"degree,omitempty"`
	// Rarity is an equipment's seal: normal, or star/moon/sun.
	Rarity   string       `json:"rarity,omitempty" enum:"rarity"`
	Level    int          `json:"level,omitempty"`
	Race     string       `json:"race" enum:"race"`
	Gender   string       `json:"gender,omitempty" enum:"gender"`
	Icon     string       `json:"icon,omitempty"`
	Tooltip  []TooltipRow `json:"tooltip"`
	Sell     int64        `json:"sellPrice"`
	MaxStack int64        `json:"maxStack"`
	Standing
}

/*
================
TooltipRow

One line of the item's in-game tooltip, as client-next's itemTooltip draws
it: the text and the game's own colour (hex RGB, "#efdaa4"). A blank line
is {"text": ""} with no colour.
================
*/
type TooltipRow struct {
	Text  string `json:"text"`
	Color string `json:"color,omitempty"`
	// Strong is the client's bold line (55B540 style 3: magic options).
	Strong bool `json:"strong,omitempty"`
}

var raceEnum = []string{"chinese", "european", "any"}
var genderEnum = []string{"female", "male"}
var rarityEnum = []string{"normal", "star", "moon", "sun"}

// itemRaritySeal is RefObjCommon rarity 2, a sealed item (client
// item-tooltip.ts: rarity === 2 names the seal).
const itemRaritySeal = 2

// sealNames are the seal of each item class within its degree, as the
// tooltip's PARAM_RARE_FIRST/SECOND/THIRD rows name them.
var sealNames = [3]string{"star", "moon", "sun"}

/*
================
ItemsFile
================
*/
type ItemsFile struct {
	Header
	Items []Item `json:"items"`
}

/*
================
ItemSource

What the item export reads: the server's item references and its
item-use registry.
================
*/
type ItemSource struct {
	Refs   []*enterworld.ItemRef
	Usable func(*enterworld.ItemRef) bool
}

/*
================
BuildItems

One row per item reference, in id order. Gold and other references with no
name or no id are left out.
================
*/
func BuildItems(source ItemSource) []Item {
	items := make([]Item, 0, len(source.Refs))
	for _, ref := range source.Refs {
		if ref == nil || ref.RefObjID == 0 || strings.TrimSpace(ref.Name) == "" {
			continue
		}
		if ref.TypeIDs[1] == itemTypeExpendable && ref.TypeIDs[2] == itemTypeGold {
			continue
		}
		item := Item{ID: ref.RefObjID, Codename: ref.Codename, Name: ref.Name, Type: itemTypePath(ref.TypeIDs),
			Degree: itemDegree(ref), Rarity: itemRarity(ref), Level: itemLevel(ref), Race: itemRace(ref.Country), Gender: itemGender(ref.RequiredSex),
			Icon: webIcon(ref.Icon), Tooltip: []TooltipRow{}, Sell: nativeInt(ref, "sellPrice"), MaxStack: nativeInt(ref, "maxStack")}
		item.Standing = applyGaps("item", ref.RefObjID, itemStanding(ref, source.Usable))
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

/*
================
itemStanding

Equipment is worn through the generic equip path, so it is implemented. A
usable item is implemented when the server's use registry admits its
family and missing when it does not. Anything else (materials, goods, quest
items) has no registry yet and is unverified.
================
*/
func itemStanding(ref *enterworld.ItemRef, usable func(*enterworld.ItemRef) bool) Standing {
	if ref.TypeIDs[1] == itemTypeEquipment {
		return Standing{Status: StatusImplemented}
	}
	if nativeInt(ref, "canUse") != 0 && usable != nil {
		if usable(ref) {
			return Standing{Status: StatusImplemented}
		}
		return Standing{Status: StatusMissing, Gaps: []string{"Using this item does nothing yet."}, Issue: itemIssueUse}
	}
	return Standing{Status: StatusUnverified}
}

/*
================
itemTypePath

The type path the client's tooltip names (56CDA0 family): armor class and
position, weapon kind, shield, accessory kind, job suit, avatar part; any
other item is grouped by its type ids.
================
*/
func itemTypePath(t [4]int64) string {
	if t[1] == itemTypeSummoner {
		return "summon"
	}
	if t[1] != itemTypeEquipment {
		return etcTypePath(t)
	}
	armor := map[int64]string{1: "clothes", 2: "light", 3: "heavy", 9: "eu-robe", 10: "eu-light", 11: "eu-heavy"}
	positions := []string{"head", "shoulders", "chest", "legs", "hands", "feet"}
	if class, ok := armor[t[2]]; ok {
		if t[3] >= 1 && int(t[3]) <= len(positions) {
			return "armor/" + class + "/" + positions[t[3]-1]
		}
		return "armor/" + class
	}
	switch t[2] {
	case 4:
		return "shield"
	case 5, 12:
		kinds := []string{"earring", "necklace", "ring"}
		if t[3] >= 1 && int(t[3]) <= len(kinds) {
			return "accessory/" + kinds[t[3]-1]
		}
		return "accessory"
	case 6:
		weapons := map[int64]string{2: "sword", 3: "blade", 4: "spear", 5: "glaive", 6: "bow", 7: "one-handed-sword",
			8: "two-handed-sword", 9: "dual-axe", 10: "dark-staff", 11: "two-handed-staff", 12: "crossbow",
			13: "dagger", 14: "harp", 15: "light-staff", 16: "siege"}
		if kind, ok := weapons[t[3]]; ok {
			return "weapon/" + kind
		}
		return "weapon"
	case 7:
		return "job-suit"
	case 13:
		parts := []string{"hat", "dress", "attachment", "etc"}
		if t[3] >= 1 && int(t[3]) <= len(parts) {
			return "avatar/" + parts[t[3]-1]
		}
		return "avatar"
	}
	return fmt.Sprintf("equipment/%d-%d", t[2], t[3])
}

/*
================
itemDegree

Equipment: itemClass 1..3 is degree 1, 4..6 degree 2, and so on (client
item-tooltip.ts, UIIT_TOOLTIP_EQUIPMENT_CLASS). An alchemy material's
class is its degree itself (alchemy Reference.Degree).
================
*/
func itemDegree(ref *enterworld.ItemRef) int {
	class := nativeInt(ref, "itemClass")
	if class <= 0 {
		return 0
	}
	if ref.TypeIDs[1] == itemTypeEquipment {
		return int((class-1)/3 + 1)
	}
	if ref.TypeIDs[1] == itemTypeExpendable && (ref.TypeIDs[2] == etcReinforce || ref.TypeIDs[2] == etcAlchemy) {
		return int(class)
	}
	return 0
}

/*
================
itemRarity

An equipment's seal from its rarity and class (the client's tooltip rule);
other items have none.
================
*/
func itemRarity(ref *enterworld.ItemRef) string {
	if ref.TypeIDs[1] != itemTypeEquipment {
		return ""
	}
	class := nativeInt(ref, "itemClass")
	if nativeInt(ref, "rarity") != itemRaritySeal || class <= 0 {
		return "normal"
	}
	return sealNames[(class-1)%3]
}

/*
================
itemLevel
================
*/
func itemLevel(ref *enterworld.ItemRef) int {
	for i, kind := range ref.ReqQuadTypes {
		if kind == itemRequirementLevel {
			return int(ref.ReqQuadValues[i])
		}
	}
	return 0
}

/*
================
itemRace

Country 0 China, 1 Europe, 3 every character.
================
*/
func itemRace(country int64) string {
	switch country {
	case 0:
		return "chinese"
	case 1:
		return "european"
	}
	return "any"
}

/*
================
itemGender

RequiredSex 0 female, 1 male, 2 either (no field).
================
*/
func itemGender(sex int64) string {
	switch sex {
	case 0:
		return "female"
	case 1:
		return "male"
	}
	return ""
}

/*
================
webIcon

The icon's path inside the catalogue's icons/ folder: the media path with
forward slashes and a .webp extension ("item/china/weapon/sword_01.webp").
"xxx" is the media's empty icon.
================
*/
func webIcon(media string) string {
	path := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(media), "\\", "/"))
	if path == "" || path == "xxx" {
		return ""
	}
	return strings.TrimSuffix(path, ".ddj") + ".webp"
}

/*
================
nativeInt
================
*/
func nativeInt(ref *enterworld.ItemRef, field string) int64 {
	value, ok := ref.NativeFields.Lookup(field)
	if !ok {
		return 0
	}
	return int64(value)
}

// Expendable TID3 values the export names on their own.
const (
	etcReinforce = 10
	etcAlchemy   = 11
)

// etcGroups names each expendable TID3/TID4 pair. Native data has no names
// for these (the client's tooltip names only equipment), so the names
// follow the server owner that handles each pair: the item-use families
// (itemuse_admission.go), alchemy (reinforce.go, stones.go), commerce
// (tradepickup.go) and the fortress and guild lanes. Inference, recorded:
// a pair no owner handles is named by what its rows are.
var etcGroups = map[[2]int64]string{
	{1, 1}: "potion-hp", {1, 2}: "potion-mp", {1, 3}: "potion-vigor", {1, 4}: "pet-potion-hp",
	{1, 6}: "pet-revive", {1, 8}: "berserk-potion", {1, 9}: "pet-potion-hgp", {1, 10}: "fortress-repair-kit",
	{2, 1}: "cure", {2, 6}: "cure-all", {2, 7}: "pet-cure",
	{3, 1}: "scroll-return", {3, 2}: "transport", {3, 3}: "scroll-reverse-return", {3, 4}: "stall-decoration",
	{3, 5}: "global-chat", {3, 6}: "fortress-tablet", {3, 7}: "fortress-guard", {3, 8}: "fortress-manual",
	{3, 9}: "fortress-flag", {3, 10}: "exp-scroll",
	{4, 1}: "arrow", {4, 2}: "bolt",
	{6, 1}: "firework", {6, 2}: "fortress-bomb",
	{7, 1}: "campfire",
	{8, 0}: "event", {8, 1}: "trade-goods", {8, 2}: "trade-goods",
	{9, 0}:  "quest",
	{10, 1}: "alchemy/elixir", {10, 2}: "alchemy/lucky-powder",
	{11, 1}: "alchemy/magic-stone", {11, 2}: "alchemy/attribute-stone", {11, 3}: "alchemy/tablet",
	{11, 4}: "alchemy/material", {11, 5}: "alchemy/element", {11, 6}: "alchemy/rondo",
	{11, 7}: "alchemy/special-stone",
	{12, 1}: "guild-soldier", {12, 2}: "guild-crest", {12, 3}: "guild-recall",
	{13, 0}: "skill-restoration", {13, 1}: "mall-scroll", {13, 4}: "premium-ticket", {13, 5}: "skill-time-ticket",
	{13, 6}: "resurrection-scroll", {13, 7}: "repair-hammer", {13, 8}: "gender-change", {13, 9}: "skin-change",
	{13, 10}: "warehouse-ticket", {13, 11}: "reinforce-scroll", {13, 12}: "pet-extension", {13, 13}: "stat-recall",
	{13, 14}: "premium-time", {13, 15}: "pet-item",
	{14, 1}: "gacha-card", {14, 2}: "gacha-result",
	{15, 1}: "monster-box", {15, 2}: "party-summon",
}

/*
================
etcTypePath

The expendable's named group ("etc/potion-hp", "etc/alchemy/elixir"); a
pair etcGroups does not name keeps its type ids ("etc/5-3").
================
*/
func etcTypePath(t [4]int64) string {
	if group, ok := etcGroups[[2]int64{t[2], t[3]}]; ok {
		return "etc/" + group
	}
	return fmt.Sprintf("etc/%d-%d", t[2], t[3])
}
