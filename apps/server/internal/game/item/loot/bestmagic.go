/*
===========================================================================

bestmagic.go - the strongest catalog-valid equipment option set

Operator grants that ask for perfect equipment want blues too. The pool and
its client-validated identities come from the same catalog EquipmentMagic
rolls from, so every param id resolves in the client. No option is invented.

===========================================================================
*/
package loot

import "opensro.online/server/internal/game/item/alchemy"

/*
================
BestEquipmentMagic

The strongest option set the client's catalog allows for the item: distinct
pool options in catalog order, each at its maximum magnitude, up to the
item's MaxMagic slots. A missing entry or an empty pool returns no options.
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
		if option.Tag == nonRepairTag || !option.Allows(flags) {
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
