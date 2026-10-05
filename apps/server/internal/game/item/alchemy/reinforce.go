/*
===========================================================================

reinforce.go - elixir reinforcement and the shared alchemy plan types

Reinforce plans one +N attempt over a detached copy of the bag; the action
lane commits the plan. Roll, Outcome and Refusal are shared by every
alchemy owner in this package.

===========================================================================
*/

package alchemy

import (
	"fmt"

	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
Roll

Roll returns the native CRT rand domain [0,32767]. Keeping draws explicit
allows failure/protection paths to be replayed without a mutable global RNG.
================
*/
type Roll func() (uint32, error)

/*
================
Outcome
================
*/
type Outcome struct {
	Items     []inventory.Item
	Target    uint8
	Success   bool
	Destroyed bool
}

/*
================
Refusal

An alchemy answer code; the low byte of the client's 0x54xx notice.
================
*/
type Refusal uint8

/*
================
Refusal.Error
================
*/
func (e Refusal) Error() string { return fmt.Sprintf("alchemy refusal 0x54%02x", uint8(e)) }

/*
================
draw
================
*/
func draw(roll Roll, modulus uint32) (uint32, error) {
	if roll == nil || modulus == 0 {
		return 0, fmt.Errorf("alchemy: missing random source or domain")
	}
	n, err := roll()
	if err != nil {
		return 0, err
	}
	if n > 32767 {
		return 0, fmt.Errorf("alchemy: random draw outside CRT domain")
	}
	return n % modulus, nil
}

/*
================
clone
================
*/
func clone(items []inventory.Item) []inventory.Item {
	out := append([]inventory.Item(nil), items...)
	for i := range out {
		out[i].MagicOptions = append([]uint64(nil), out[i].MagicOptions...)
	}
	return out
}

/*
================
probability

505F00: three packed groups, high byte first. Beyond +11 the final byte
remains authoritative (it is not a guessed exponential).
================
*/
func probability(r Reference, plus uint8) int {
	i := int(plus)
	if i > 11 {
		i = 11
	}
	return int(r.Params[1+i/4] >> uint((3-i%4)*8) & 255)
}

/*
================
charge

Spends one charge of the item's tag option, dropping it at zero.
================
*/
func (c *Catalog) charge(item *inventory.Item, tag uint32) bool {
	for i, v := range item.MagicOptions {
		m, ok := c.Magic[uint16(v)]
		if !ok || m.Tag != tag {
			continue
		}
		value := uint32(v >> 32)
		if value <= 1 {
			item.MagicOptions = append(item.MagicOptions[:i], item.MagicOptions[i+1:]...)
		} else {
			item.MagicOptions[i] = uint64(value-1)<<32 | uint64(uint16(v))
		}
		return true
	}
	return false
}

/*
================
curse

A failed +5 attempt adds the level-3 durability curse, stacking onto an
existing one and clamped to 1..99.
================
*/
func (c *Catalog) curse(item *inventory.Item, roll Roll) error {
	m, ok := c.Option("MATTR_DEC_MAXDUR", 3)
	if !ok {
		return fmt.Errorf("alchemy: missing durability curse")
	}
	if !m.Allows(item.TypeFlags) {
		return nil
	}
	value, err := RollSingleRange(m, roll)
	if err != nil {
		return err
	}
	index := -1
	for i, v := range item.MagicOptions {
		if old, ok := c.Magic[uint16(v)]; ok && old.Tag == 0x64757261 {
			index = i
			value += uint32(v >> 32)
			m = old
			break
		}
	}
	if value < 1 {
		value = 1
	}
	if value > 99 {
		value = 99
	}
	v := uint64(value)<<32 | uint64(m.ID)
	if index >= 0 {
		item.MagicOptions[index] = v
	} else {
		if len(item.MagicOptions) >= wire.MaxMagicOptionsPerItem {
			return fmt.Errorf("alchemy: no room for durability option")
		}
		item.MagicOptions = append(item.MagicOptions, v)
	}
	return nil
}

/*
================
Reinforce

Plans one attempt over detached inventory. Validation and random source
errors return no plan; failures are committed outcomes that consume the
elixir/powder and, when applicable, protection charges.
================
*/
func (c *Catalog) Reinforce(items []inventory.Item, slots []uint8, bonus int, roll Roll) (Outcome, error) {
	var zero Outcome
	if len(slots) < 2 || len(slots) > 3 {
		return zero, Refusal(0x10)
	}
	out := clone(items)
	indices := map[uint8]int{}
	for i, item := range out {
		if _, exists := indices[item.Slot]; exists {
			return zero, fmt.Errorf("alchemy: duplicate inventory slot")
		}
		indices[item.Slot] = i
	}
	target, elixir, powder := -1, -1, -1
	seen := map[uint8]bool{}
	for _, slot := range slots {
		if slot < inventory.EquipmentSlotEnd || slot >= inventory.BagSlotEnd || seen[slot] {
			return zero, Refusal(0x12)
		}
		seen[slot] = true
		i, ok := indices[slot]
		if !ok {
			return zero, Refusal(6)
		}
		item := out[i]
		ref, ok := c.Items[item.Codename]
		if !ok || ref.ID != item.RefObjID || ref.Flags != item.TypeFlags || item.Quantity == 0 {
			return zero, Refusal(0x10)
		}
		for _, v := range item.MagicOptions {
			if _, ok := c.Magic[uint16(v)]; !ok {
				return zero, fmt.Errorf("alchemy: unknown magic option %d", uint16(v))
			}
		}
		switch {
		case category(ref.Flags) != "":
			if target >= 0 {
				return zero, Refusal(0x12)
			}
			target = i
		case ref.Flags == wire.PackTypeFlags(3, 3, 10, 1):
			if elixir >= 0 {
				return zero, Refusal(0x12)
			}
			elixir = i
		case ref.Flags == wire.PackTypeFlags(3, 3, 10, 2):
			if powder >= 0 {
				return zero, Refusal(0x12)
			}
			powder = i
		default:
			return zero, Refusal(0x10)
		}
	}
	if target < 0 {
		return zero, Refusal(6)
	}
	if elixir < 0 {
		return zero, Refusal(7)
	}
	item := &out[target]
	ref := c.Items[item.Codename]
	recipe := c.Items[out[elixir].Codename]
	if item.Plus >= 250 {
		return zero, Refusal(0x0f)
	}
	compatible := false
	for _, p := range []uint32{recipe.Params[0], recipe.Params[4]} {
		for shift := uint(0); shift < 32; shift += 8 {
			if uint16(p>>shift&255) == item.TypeFlags>>7&15 {
				compatible = true
			}
		}
	}
	if !compatible {
		return zero, Refusal(9)
	}
	chance := probability(recipe, item.Plus)
	if powder >= 0 {
		p := c.Items[out[powder].Codename]
		if int(p.Params[0]) != ref.Degree() {
			return zero, Refusal(0x0a)
		}
		if chance < 100 {
			chance += probability(p, item.Plus)
		}
	}
	if chance < 100 && c.charge(item, 0x6c75636b) {
		chance += 5
	}
	chance += bonus
	if chance < 10 {
		chance = 10
	}
	if chance > 100 {
		chance = 100
	}
	n, err := draw(roll, 100)
	if err != nil {
		return zero, err
	}
	result := Outcome{Target: item.Slot, Success: int(n) < chance}
	if result.Success {
		item.Plus++
	} else {
		if item.Plus >= 5 {
			n, err = draw(roll, 100)
			if err != nil {
				return zero, err
			}
			if n < 50 {
				result.Destroyed = item.TypeFlags&1 == 0
				if result.Destroyed && c.charge(item, 0x61746861) {
					result.Destroyed = false
				}
			} else if !c.charge(item, 0x736f6c69) {
				if err = c.curse(item, roll); err != nil {
					return zero, err
				}
			}
		}
		if !result.Destroyed {
			if item.Plus >= 4 && c.charge(item, 0x61737472) {
				item.Plus = 4
			} else {
				item.Plus = 0
			}
		}
	}
	// 501C00: consume one of each staged material, and the target only
	// when the destruction bit survives the protection branch.
	out[elixir].Quantity--
	if powder >= 0 {
		out[powder].Quantity--
	}
	if result.Destroyed {
		item.Quantity = 0
	}
	for _, row := range out {
		if row.Quantity > 0 {
			result.Items = append(result.Items, row)
		}
	}
	return result, nil
}
