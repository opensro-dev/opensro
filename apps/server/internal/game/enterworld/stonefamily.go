/*
===========================================================================

stonefamily.go - alchemy stone equipment-family masks for the client binder

Publishes the numeric stoneFamilyMask native field on magic and attribute
stone references so the browser client's reinforcement binder can refuse a
stone placement locally, the way SRO_Client.exe 0x0062B6D0
(CIFAlchemyReinforce_BindItemSlot) does, instead of binding the slot and
letting the fuse fail later. The server fuse remains the authority; this
field only feeds the client's local refusal. The same pass publishes the
stoneTooltipMask field the client's stone tooltip (557C50) prints its
"Object item" row from; the tooltip searches the same haystacks with its
own word order and a different repair rule, so the two stay separate
fields.

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

// Tooltip target word bits, in the order client 557C50 tests them. Mirrored by
// STONE_TARGET_SYMBOLS in the client's item-tooltip-stone.ts; change both
// together.
const (
	stoneTargetWeapon uint32 = 1 << iota
	stoneTargetArmor
	stoneTargetShield
	stoneTargetAccessory
	stoneTargetHelm
	stoneTargetShoulderguard
	stoneTargetMail
	stoneTargetPants
	stoneTargetGauntlet
	stoneTargetBoots
	stoneTargetEarring
	stoneTargetNecklace
	stoneTargetRing
	stoneTargetHonor
)

var stoneTooltipWords = []struct {
	word string
	bit  uint32
}{
	{"weapon", stoneTargetWeapon},
	{"armor", stoneTargetArmor},
	{"shield", stoneTargetShield},
	{"accessory", stoneTargetAccessory},
	{"helm", stoneTargetHelm},
	{"shoulderguard", stoneTargetShoulderguard},
	{"mail", stoneTargetMail},
	{"pants", stoneTargetPants},
	{"gauntlet", stoneTargetGauntlet},
	{"boots", stoneTargetBoots},
	{"earring", stoneTargetEarring},
	{"necklace", stoneTargetNecklace},
	{"ring", stoneTargetRing},
	{"honor", stoneTargetHonor},
}

/*
================
stoneFamilyMaskFor

The native binder joins the stone's family words and substring-searches the
target word in the joined text. The client's compare is case-sensitive
(client 0x0062B6D0 reaches the ordinal wide-string find with no case fold; the
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
stoneTooltipMaskFor

Client 557C50 substring-searches the stone's text once per word in
stoneTooltipWords order and prints the matching UIIT_STT_* type symbols
after the "UIIT_STT_TARGET_ITEM: " label. The find is the ordinal
wide-string one: case-sensitive, and 'ring' matches inside 'earring',
exactly as the native text holds it.
================
*/
func stoneTooltipMaskFor(text string) uint32 {
	var mask uint32
	for _, entry := range stoneTooltipWords {
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

The tooltip mask follows client 557C50 instead of the binder: it searches the
same haystacks in the tooltip's own word order, and a stone whose first
parameter description is MATTR_REPAIR searches its fourth description
(client 0x557F86 compares the description against the whole 12-char literal),
never the option's words. Client 557C50 prints the row even when the option
does not resolve - its text stays empty - so an unresolved magic stone
publishes a zero tooltip mask where the binder mask stays unpublished.
The option's ten authored words ride in Descriptors; the shipped table
holds no flag-0 word, so the joined text searches exactly like the
native record's ten strings.
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
		text := ref.ParamDescriptions[3]
		return fields.
			With("stoneFamilyMask", float64(stoneFamilyMaskFor(text))).
			With("stoneTooltipMask", float64(stoneTooltipMaskFor(text)))
	}
	source, ok := magic.(interface {
		MagicOptionByCodenameDegree(string, int64) (*MagicOptionRow, bool)
	})
	if !ok {
		return fields
	}
	option, found := source.MagicOptionByCodenameDegree(ref.ParamDescriptions[0], int64(degree))
	if !found {
		// Client 557C50 prints the row with an empty text: label only.
		return fields.With("stoneTooltipMask", 0)
	}
	joined := strings.Join(option.Descriptors, " ")
	mask := stoneFamilyMaskFor(joined)
	if option.OptionName == "MATTR_REPAIR" {
		mask |= stoneFamilyHonor
	}
	tooltip := joined
	if ref.ParamDescriptions[0] == "MATTR_REPAIR" {
		tooltip = ref.ParamDescriptions[3]
	}
	return fields.
		With("stoneFamilyMask", float64(mask)).
		With("stoneTooltipMask", float64(stoneTooltipMaskFor(tooltip)))
}
