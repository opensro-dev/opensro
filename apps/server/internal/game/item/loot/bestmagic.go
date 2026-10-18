/*
===========================================================================

bestmagic.go - the strongest catalog-valid equipment option set

Operator grants that ask for perfect equipment want blues too. The pool and
its client-validated identities come from the same catalog EquipmentMagic
rolls from, so every param id resolves in the client. No option is invented.

"Perfect" is defined per property and this file owns only the blues. The
plus is the caller's configured value, and the white stats are every field of
the item's type at 31 (tools such as the level-up gear rule build them from
equipmentVarianceFieldCount). Here "best blues" means the highest legal
magnitude for each pool option, in catalog order, up to MaxMagic. That is not
the strongest possible combination, because the pool order is the authored
one.

Alchemy aids are excluded. Lucky, Immortal, Steady and Astral are consumable
charges that protect or boost a reinforcement attempt; they are not combat
bonuses, so a "max blues" grant must not spend them.

===========================================================================
*/
package loot

import "opensro.online/server/internal/game/item/alchemy"

// alchemyAidTags are the magic options with a consumable alchemy use. They
// are never part of a perfect combat option set: Lucky adds reinforcement
// chance, Immortal saves the item from destruction, Steady from a curse and
// Astral keeps the plus at +4 (alchemy/reinforce.go).
var alchemyAidTags = map[uint32]bool{
	alchemy.MagicTagLuck:     true,
	alchemy.MagicTagImmortal: true,
	alchemy.MagicTagSteady:   true,
	alchemy.MagicTagAstral:   true,
}

/*
================
BestEquipmentMagic

The strongest option set the client's catalog allows for the item: distinct
pool options in catalog order, each at its maximum magnitude, up to the
item's MaxMagic slots. Nonrepair and the alchemy aids are skipped. A missing
entry or an empty pool returns no options.
================
*/
func BestEquipmentMagic(code string) []uint64 {
	item, exists := properties.Items[code]
	if !exists || item.MaxMagic <= 0 {
		return nil
	}
	flags := uint16(item.Type[0])<<2 | uint16(item.Type[1])<<5 | uint16(item.Type[2])<<7 | uint16(item.Type[3])<<11
	pool := properties.pools[code]
	options := make([]uint64, 0, len(pool))
	for _, option := range pool {
		if len(options) >= item.MaxMagic {
			break
		}
		if option.Tag == nonRepairTag || alchemyAidTags[option.Tag] || !option.Allows(flags) {
			continue
		}
		value := bestMagicValue(option.Magic)
		if value == 0 {
			continue
		}
		options = append(options, uint64(value)<<32|uint64(option.ID))
	}
	return options
}

/*
================
bestMagicValue

The highest value the option's generator can produce: RollMagicValue draws
from the non-zero halves of its three params, so the maximum draw is their
maximum. A zero means the option carries no value.
================
*/
func bestMagicValue(m alchemy.Magic) uint32 {
	var best uint32
	for _, param := range m.Params {
		if v := param >> 16; v > best {
			best = v
		}
		if v := param & 0xffff; v > best {
			best = v
		}
	}
	return best
}
