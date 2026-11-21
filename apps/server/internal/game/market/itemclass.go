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

// The categories, by item TypeID (TID1 3 = item; TID2 1 = equipment,
// 2 = pet, 3 = consumable and the rest). The equipment TID3 values are
// v1.150's itemdata: 1-3 Chinese garment/protector/armor, 9-11 European
// robe/light/heavy armor, 4 shield, 5 and 12 accessories, 6 weapons, 13
// and 14 avatars. "other" takes every item no row names.
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

/*
================
ArmorType

The armour type filter's value for an item's TypeIDs: the body armour
rows of the categories table (TypeID3 1-3 Chinese, 9-11 European), ""
for anything else.
================
*/
func ArmorType(types [4]int64) string {
	if types[0] != 3 || types[1] != 1 {
		return ""
	}
	switch types[2] {
	case 1:
		return publicstats.MarketArmorGarment
	case 2:
		return publicstats.MarketArmorProtector
	case 3:
		return publicstats.MarketArmorArmor
	case 9:
		return publicstats.MarketArmorRobe
	case 10:
		return publicstats.MarketArmorLight
	case 11:
		return publicstats.MarketArmorHeavy
	}
	return ""
}

/*
================
Category

The picker slug for an item's TypeIDs (the categories table above).
================
*/
func Category(types [4]int64) string {
	if types[0] != 3 {
		return "other"
	}
	switch types[1] {
	case 2:
		return "pet"
	case 3:
		return "consumable"
	case 1:
		switch types[2] {
		case 6:
			return "weapon"
		case 4:
			return "shield"
		case 1, 2, 3, 9, 10, 11:
			return "armor"
		case 5, 12:
			return "accessory"
		case 13, 14:
			return "avatar"
		}
	}
	return "other"
}
