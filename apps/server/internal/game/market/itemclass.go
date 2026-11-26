/*
===========================================================================

itemclass.go - what kind of item a listing holds, for the search filters

The market's item classification: the picker's categories, the body
armour type and the rarity filter's seal test, all read from the item
reference's TypeIDs and seal (ItemInfo). service.go builds each listing's
view with them; nothing here reads the store.

===========================================================================
*/
package market

import "opensro.online/server/internal/agent/publicstats"

// The categories the picker offers, in its order. Equipment takes its
// slug from equipmentClasses; "other" takes every item no row names.
var categories = []publicstats.MarketCategory{
	{Slug: "weapon", Name: "Weapons"},
	{Slug: "shield", Name: "Shields"},
	{Slug: "armor", Name: "Armor"},
	{Slug: "accessory", Name: "Accessories"},
	{Slug: "avatar", Name: "Avatars"},
	{Slug: "pet", Name: "Pets"},
	{Slug: "consumable", Name: "Consumables"},
	{Slug: "other", Name: "Other"},
}

/*
================
rarityMatches

Whether an item passes the rarity filter: "" any, sealed any seal, normal
no seal, star, moon or sun that one seal tier.
================
*/
func rarityMatches(item publicstats.MarketItem, rarity string) bool {
	switch rarity {
	case "":
		return true
	case publicstats.MarketRaritySealed:
		return item.Sealed
	case publicstats.MarketRarityNormal:
		return !item.Sealed
	}
	return item.Sealed && item.Seal == rarity
}

// The item TypeIDs the classification reads (TID1 3 = item; TID2 1 =
// equipment, 2 = pet/cube, 3 = consumable; TID2 2 / TID3 3 = Magic Cube).
const (
	typeItem       = 3
	typeEquipment  = 1
	typePet        = 2
	typeConsumable = 3
	typeMagicCube3 = 3
)

// equipmentClass is one equipment TypeID3 row: its category and, for body
// armour, its armour type.
type equipmentClass struct {
	category string
	armor    string
}

// equipmentClasses is v1.150's itemdata equipment TypeID3: the one table
// both the category and the armour type read.
var equipmentClasses = map[int64]equipmentClass{
	1:  {"armor", publicstats.MarketArmorGarment},
	2:  {"armor", publicstats.MarketArmorProtector},
	3:  {"armor", publicstats.MarketArmorArmor},
	9:  {"armor", publicstats.MarketArmorRobe},
	10: {"armor", publicstats.MarketArmorLight},
	11: {"armor", publicstats.MarketArmorHeavy},
	4:  {category: "shield"},
	5:  {category: "accessory"},
	12: {category: "accessory"},
	6:  {category: "weapon"},
	13: {category: "avatar"},
	14: {category: "avatar"},
}

/*
================
classOf

An equipment item's row of equipmentClasses, false for anything else.
================
*/
func classOf(types [4]int64) (equipmentClass, bool) {
	if types[0] != typeItem || types[1] != typeEquipment {
		return equipmentClass{}, false
	}
	class, ok := equipmentClasses[types[2]]
	return class, ok
}

/*
================
ArmorType

The armour type filter's value for an item's TypeIDs, "" for anything
that is no body armour.
================
*/
func ArmorType(types [4]int64) string {
	class, _ := classOf(types)
	return class.armor
}

/*
================
Category

The picker slug for an item's TypeIDs (the categories table above).
================
*/
func Category(types [4]int64) string {
	if class, ok := classOf(types); ok {
		return class.category
	}
	if types[0] == typeItem {
		switch types[1] {
		case typePet:
			if types[2] == typeMagicCube3 {
				return "other"
			}
			return "pet"
		case typeConsumable:
			return "consumable"
		}
	}
	return "other"
}
