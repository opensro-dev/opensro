/*
===========================================================================

itemgrade.go - an item's degree and an equipment item's seal tier

One owner for the itemClass rule every reader shares: the catalogue's item
pages, the beta seal rates and the website's gear points. Equipment
itemClass counts three classes per degree (a degree-8 sword is class 22,
23 or 24), so degree = (class - 1) / 3 + 1; on a sealed piece (rarity 2)
the class within its degree is the seal, Star, Moon, Sun - the client's
CSOItem_CalculateTierGroup (ItemClass - 1) % 3, named by
ItemTooltip_AppendRareGrade (558FF0).

===========================================================================
*/
package enterworld

// Seal tiers: (ItemClass - 1) % SealTiers on a sealed piece.
const (
	SealStar = 0
	SealMoon = 1
	SealSun  = 2
)

// SealTiers is the itemClass span of one degree.
const SealTiers = 3

// Item type ids of equipment (TypeIDs 3, 1, ...) and the sealed rarity.
const (
	equipmentTypeID1 = 3
	equipmentTypeID2 = 1
	sealedRarity     = 2
)

/*
================
itemClass
================
*/
func itemClass(ref *ItemRef) int {
	if ref == nil {
		return 0
	}
	value, ok := ref.NativeFields.Lookup("itemClass")
	if !ok {
		return 0
	}
	return int(value)
}

/*
================
IsEquipment
================
*/
func IsEquipment(ref *ItemRef) bool {
	return ref != nil && ref.TypeIDs[0] == equipmentTypeID1 && ref.TypeIDs[1] == equipmentTypeID2
}

/*
================
EquipmentDegree

An equipment piece's degree, or 0 for anything else.
================
*/
func EquipmentDegree(ref *ItemRef) int {
	class := itemClass(ref)
	if !IsEquipment(ref) || class <= 0 {
		return 0
	}
	return (class-1)/SealTiers + 1
}

/*
================
ItemDegree

Any item's degree, the server's degree rule: 0
without a class, the equipment rule for equipment, and the class itself
for anything else (an elixir or alchemy stone's class is its degree).
================
*/
func ItemDegree(ref *ItemRef) int {
	class := itemClass(ref)
	if class <= 0 {
		return 0
	}
	if IsEquipment(ref) {
		return EquipmentDegree(ref)
	}
	return class
}

/*
================
EquipmentSeal

A sealed equipment piece's tier (SealStar, SealMoon, SealSun) and true;
false for an unsealed piece or anything else.
================
*/
func EquipmentSeal(ref *ItemRef) (tier int, sealed bool) {
	class := itemClass(ref)
	if !IsEquipment(ref) || class <= 0 {
		return 0, false
	}
	if rarity, ok := ref.NativeFields.Lookup("rarity"); !ok || int(rarity) != sealedRarity {
		return 0, false
	}
	return (class - 1) % SealTiers, true
}
