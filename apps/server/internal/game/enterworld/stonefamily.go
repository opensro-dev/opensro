/*
===========================================================================

stonefamily.go - alchemy stone equipment-family masks for the client binder

Publishes the numeric stoneFamilyMask native field on magic and attribute
stone references so the browser client's reinforcement binder can refuse a
stone placement locally, the way SRO_Client.exe 0x0062B6D0
(CIFAlchemyReinforce_BindItemSlot) does, instead of binding the slot and
letting the fuse fail later. The server fuse remains the authority; this
field only feeds the client's local refusal.

===========================================================================
*/
package enterworld

import "strings"

// Stone family word bits. Mirrored by STONE_FAMILY_BITS in the client's
// alchemy-selection.ts; change both together.
const (
	stoneFamilyWeapon uint32 = 1 << iota
	stoneFamilyShield
	stoneFamilyAccessory
	stoneFamilyEarring
	stoneFamilyNecklace
	stoneFamilyRing
	stoneFamilyArmor
	stoneFamilyHelm
	stoneFamilyShoulderguard
	stoneFamilyMail
	stoneFamilyPants
	stoneFamilyGauntlet
	stoneFamilyBoots
	stoneFamilyHonor
)

// The searched words, in bit order. 'honor' is not an authored descriptor:
// the binder searches it for a MATTR_REPAIR magic stone placed on an item
// that itself carries the repair attribute, so ItemStoneNativeFields sets
// the bit directly from the option identity.
var stoneFamilyWords = []struct {
	word string
	bit  uint32
}{
	{"weapon", stoneFamilyWeapon},
	{"shield", stoneFamilyShield},
	{"accessory", stoneFamilyAccessory},
	{"earring", stoneFamilyEarring},
	{"necklace", stoneFamilyNecklace},
	{"ring", stoneFamilyRing},
	{"armor", stoneFamilyArmor},
	{"helm", stoneFamilyHelm},
	{"shoulderguard", stoneFamilyShoulderguard},
	{"mail", stoneFamilyMail},
	{"pants", stoneFamilyPants},
	{"gauntlet", stoneFamilyGauntlet},
	{"boots", stoneFamilyBoots},
	{"honor", stoneFamilyHonor},
}

/*
================
stoneFamilyMaskFor

The native binder joins the stone's family words and substring-searches the
target word in the joined text. The client's compare is case-sensitive
(0x0062B6D0 reaches the ordinal wide-string find with no case fold; the
client's separate case-insensitive compare serves MATTR_REPAIR instead),
so this search runs on the text as authored. The shipped descriptors and
the searched literals are all lowercase, which keeps the two readings in
agreement on shipped data; an uppercase descriptor admits nothing.
================
*/
func stoneFamilyMaskFor(text string) uint32 {
	var mask uint32
	for _, entry := range stoneFamilyWords {
		if strings.Contains(text, entry.word) {
			mask |= entry.bit
		}
	}
	return mask
}

/*
================
ItemStoneNativeFields

Magic stones (3/3/11/1 and 3/3/11/7) take their family words from the
magicoption descriptor pairs of the option named by the stone's first
parameter description at the stone's degree; attribute stones (3/3/11/2)
carry the family list in their fourth parameter description. A magic
stone whose option does not resolve publishes no mask: the client then
binds without a local family refusal and the server fuse stays the only
gate, which is also the pre-field behaviour for older data. A resolved
stone publishes the mask even when it is zero - presence distinguishes
"checked, admits nothing" (the native kind gate folded into the mask)
from "not published".
================
*/
func ItemStoneNativeFields(fields NativeFields, ref *ItemRef, magic MagicOptionSource) NativeFields {
	if ref == nil || magic == nil {
		return fields
	}
	if ref.TypeIDs != [4]int64{3, 3, 11, 1} && ref.TypeIDs != [4]int64{3, 3, 11, 7} && ref.TypeIDs != [4]int64{3, 3, 11, 2} {
		return fields
	}
	degree, ok := fields.Lookup("itemParam1_29c")
	if !ok {
		return fields
	}
	if ref.TypeIDs[3] == 2 {
		return fields.With("stoneFamilyMask", float64(stoneFamilyMaskFor(ref.ParamDescriptions[3])))
	}
	source, ok := magic.(interface {
		MagicOptionByCodenameDegree(string, int64) (*MagicOptionRow, bool)
	})
	if !ok {
		return fields
	}
	option, found := source.MagicOptionByCodenameDegree(ref.ParamDescriptions[0], int64(degree))
	if !found {
		return fields
	}
	mask := stoneFamilyMaskFor(strings.Join(option.Descriptors, " "))
	if option.OptionName == "MATTR_REPAIR" {
		mask |= stoneFamilyHonor
	}
	return fields.With("stoneFamilyMask", float64(mask))
}
