package inventory

// The retail clothes-vs-hard body-armor exclusivity - requirement bit 0x80 of
// sub_789c60 / CSOItem_PassesEquipRequirementMask (wave-9 bug C), mirrored
// server-side because that is where the equip authority lives (the client
// pane-drag composer passes mask 0x40 only, exactly like native sub_594980;
// retail surfaces this refusal on the full-mask use/equip paths and through
// its own server authority).
//
// Native semantics (asm 0x00789e60..0x00789f7b): equipping CLOTHES (TID3 1|9)
// refuses while ANY worn record 0..5 holds body armor that is NOT clothes
// (@0x00789f6f); equipping HARD body armor (TID3 2|3|10|11) refuses while ANY
// worn record 0..5 holds clothes (@0x00789ecd); an item outside the body-armor
// family never scans. Light+heavy mixes are LEGAL - the exclusivity is
// clothes vs non-clothes, not set identity. Native walks all six sockets
// unconditionally, so the scan deliberately includes the socket being
// replaced (and, on a swap-back, the piece that is about to vacate):
// switching families requires unequipping the old family first, exactly like
// retail.

// IsFortressWeaponTypeFlags is the native sub_5504c0 predicate: the TID
// word (3,1,6,16) - the fortress siege hammer/axe family, the ONLY items
// the fortress-guild equip gate (sub_789c60 bit 0x400) ever tests.
func IsFortressWeaponTypeFlags(typeFlags uint16) bool {
	return isEquipmentClassWord(typeFlags) && typeFlagsTid3(typeFlags) == 6 && typeFlagsTid4(typeFlags) == 16
}

// IsBodyArmorFamilyTypeFlags reports whether the TypeID word is in the
// six-value body-armor family: equipment-class word AND TID3 in
// {1,2,3,9,10,11} (CH garment/light/heavy + the EU triple).
// Native sub_593270 (folder ItemTid_IsWeaponTid3Set - a known misnomer).
func IsBodyArmorFamilyTypeFlags(typeFlags uint16) bool {
	return isEquipmentClassWord(typeFlags) && isBodyArmorTid3(typeFlagsTid3(typeFlags))
}

// IsClothesGarmentTypeFlags reports whether the TypeID word is the CH/EU
// CLOTHES garment class: the same equipment-class masks with TID3 in {1, 9}.
// Native sub_7895b0 / ItemTid_IsClothesGarmentTid3.
func IsClothesGarmentTypeFlags(typeFlags uint16) bool {
	if !isEquipmentClassWord(typeFlags) {
		return false
	}
	tid3 := typeFlagsTid3(typeFlags)
	return tid3 == 1 || tid3 == 9
}

// ClothesHardExclusivityConflict scans the CURRENT rows in the body sockets
// (wire slots 0..5) for a worn piece of the opposite family. The first
// conflicting row is returned; the second result is false when the equip is
// legal on this axis, including when the incoming item is outside the
// body-armor family and never scans.
func ClothesHardExclusivityConflict(rows []Item, incomingTypeFlags uint16) (Item, bool) {
	if !IsBodyArmorFamilyTypeFlags(incomingTypeFlags) {
		return Item{}, false
	}
	incomingIsClothes := IsClothesGarmentTypeFlags(incomingTypeFlags)
	for _, row := range rows {
		if row.Slot > SocketFoot {
			continue
		}
		if !IsBodyArmorFamilyTypeFlags(row.TypeFlags) {
			continue
		}
		if IsClothesGarmentTypeFlags(row.TypeFlags) != incomingIsClothes {
			return row, true
		}
	}
	return Item{}, false
}
