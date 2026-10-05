/*
===========================================================================

stones.go - alchemy stone application and shared magic value selection

===========================================================================
*/

package alchemy

import (
	"fmt"
	"math"
	"strings"

	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
attributes
================
*/
func attributes(flags uint16) []string {
	switch category(flags) {
	case "weapon":
		return []string{"NATTR_DUR", "NATTR_PASTR", "NATTR_MAINT", "NATTR_HR", "NATTR_PA", "NATTR_MA", "NATTR_CRITICAL"}
	case "armor":
		return []string{"NATTR_DUR", "NATTR_PDSTR", "NATTR_MDINT", "NATTR_PD", "NATTR_MD", "NATTR_ER"}
	case "shield":
		return []string{"NATTR_DUR", "NATTR_PDSTR", "NATTR_MDINT", "NATTR_BR", "NATTR_PD", "NATTR_MD"}
	case "accessory":
		return []string{"NATTR_PAR", "NATTR_MAR"}
	}
	return nil
}

/*
================
magicLimit
================
*/
func (c *Catalog) magicLimit(item inventory.Item) int {
	n := c.Items[item.Codename].MaxMagic
	for _, v := range item.MagicOptions {
		m := c.Magic[uint16(v)]
		if m.Tag == 0x64757261 || m.Tag == 0x6e726570 {
			n++
		}
	}
	if n > 12 {
		n = 12
	}
	return n
}

// 501EA0 selects the minimum of four (rare equipment: five) six-way
// rolls, then converts the authored percentage table to a five-bit value.
/*
================
varianceValue
================
*/
func (c *Catalog) varianceValue(item inventory.Item, roll Roll) (uint64, error) {
	n := uint32(c.magicLimit(item))
	count := 4
	if c.Items[item.Codename].Rarity == 2 {
		count = 5
	}
	for i := 0; i < count; i++ {
		v, e := draw(roll, 6)
		if e != nil {
			return 0, e
		}
		if v < n {
			n = v
		}
	}
	if n > 5 {
		return 0, fmt.Errorf("alchemy: invalid variance selector")
	}
	percent := [6]uint64{0, 23, 43, 63, 83, 100}
	return percent[n] * 31 / 100, nil
}

/*
================
setVariance
================
*/
func setVariance(item *inventory.Item, index int, value uint64) {
	shift := uint(index * 5)
	item.VarianceBits = item.VarianceBits & ^(uint64(31)<<shift) | value<<shift
}

/*
================
RollMagicValue
================
*/
func RollMagicValue(m Magic, roll Roll) (uint32, error) {
	values := []uint32{}
	for _, p := range m.Params {
		for _, v := range []uint32{p >> 16, p & 65535} {
			if v > 0 {
				values = append(values, v)
			}
		}
	}
	if len(values) == 0 {
		return 0, fmt.Errorf("alchemy: empty magic value set %s", m.Name)
	}
	n, e := draw(roll, uint32(len(values)))
	if e != nil {
		return 0, e
	}
	return values[n], nil
}

/*
================
RollSingleRange

mfunc_single_range (v1.188 72FC60): an option whose min and max match
returns min without drawing; otherwise rand/32767 is rounded to float32 and
min + ratio*(max-min) truncates. Both endpoints are reachable.
================
*/
func RollSingleRange(m Magic, roll Roll) (uint32, error) {
	low, high := m.Params[1], m.Params[2]
	if low == high {
		return low, nil
	}
	n, err := draw(roll, 32768)
	if err != nil {
		return 0, err
	}
	return uint32(math.Trunc(float64(low) + float64(float32(float64(n)/32767))*float64(high-low))), nil
}

/*
================
findMagic
================
*/
func (c *Catalog) findMagic(item inventory.Item, tag uint32) (int, uint32) {
	for i, v := range item.MagicOptions {
		if c.Magic[uint16(v)].Tag == tag {
			return i, uint32(v >> 32)
		}
	}
	return -1, 0
}

/*
================
assimilate
================
*/
func (c *Catalog) assimilate(item *inventory.Item, stone inventory.Item, changedAttribute int, magic bool, oldCount int, roll Roll) error {
	n, e := draw(roll, 101)
	if e != nil {
		return e
	}
	if uint32(stone.Plus) <= n {
		return nil
	}
	if c.charge(item, 0x617065) {
		return nil
	}
	attrs := attributes(item.TypeFlags)
	n, e = draw(roll, uint32(len(attrs)+len(item.MagicOptions)))
	if e != nil {
		return e
	}
	if int(n) < len(attrs) {
		if !magic && int(n) == changedAttribute {
			return nil
		}
		v, e := c.varianceValue(*item, roll)
		if e != nil {
			return e
		}
		setVariance(item, int(n), v)
		return nil
	}
	index := int(n) - len(attrs)
	// 503CD2 selects magic slot zero in the magic-stone branch. Attribute
	// stones pass the randomly selected magic index (5035D9).
	if magic {
		if oldCount == 0 {
			return nil
		}
		index = 0
	}
	if len(item.MagicOptions) >= c.magicLimit(*item) {
		return nil
	}
	m := c.Magic[uint16(item.MagicOptions[index])]
	switch m.Tag {
	case 0x61746861, 0x6c75636b, 0x736f6c69, 0x61737472, 0x64757261, 0x726570:
		return nil
	}
	v, e := RollMagicValue(m, roll)
	if e != nil {
		return e
	}
	if v < 1 {
		v = 1
	}
	if v > 1700 {
		v = 1700
	}
	item.MagicOptions[index] = uint64(v)<<32 | uint64(m.ID)
	return nil
}

// Stone applies one attribute or magic stone. Invalid recipes are refused
// before consuming anything. Assimilation runs only after a successful
// application, and all its changes belong to the same detached plan.
/*
================
Stone
================
*/
func (c *Catalog) Stone(items []inventory.Item, slots []uint8, magic bool, bonus int, roll Roll) (Outcome, error) {
	var zero Outcome
	if len(slots) != 2 || slots[0] == slots[1] {
		return zero, Refusal(0x12)
	}
	out := clone(items)
	target, material := -1, -1
	for _, slot := range slots {
		if slot < 13 || slot >= inventory.BagSlotEnd {
			return zero, Refusal(0x10)
		}
		found := false
		for i, item := range out {
			if item.Slot != slot {
				continue
			}
			if found {
				return zero, Refusal(0x12)
			}
			found = true
			r, ok := c.Items[item.Codename]
			if !ok || r.ID != item.RefObjID || r.Flags != item.TypeFlags || item.Quantity == 0 {
				return zero, Refusal(0x10)
			}
			if category(r.Flags) != "" {
				if target >= 0 {
					return zero, Refusal(0x12)
				}
				target = i
			} else {
				allowed := r.Flags == wire.PackTypeFlags(3, 3, 11, 2)
				if magic {
					allowed = r.Flags == wire.PackTypeFlags(3, 3, 11, 1) || r.Flags == wire.PackTypeFlags(3, 3, 11, 7)
				}
				if !allowed || material >= 0 {
					return zero, Refusal(0x10)
				}
				material = i
			}
		}
		if !found {
			return zero, Refusal(6)
		}
	}
	if target < 0 {
		return zero, Refusal(6)
	}
	if material < 0 {
		return zero, Refusal(7)
	}
	item := &out[target]
	stone := out[material]
	r := c.Items[stone.Codename]
	if c.Items[item.Codename].Degree() != int(r.Params[0]) {
		return zero, Refusal(0x21)
	}
	if len(item.MagicOptions) > c.magicLimit(*item) {
		return zero, Refusal(0x22)
	}
	for _, v := range item.MagicOptions {
		if _, ok := c.Magic[uint16(v)]; !ok {
			return zero, fmt.Errorf("alchemy: unknown magic option %d", uint16(v))
		}
	}
	attrIndex := -1
	var option Magic
	oldIndex := -1
	var oldValue uint32
	if magic {
		var ok bool
		option, ok = c.Option(r.Descriptions[0], int(r.Params[0]))
		if !ok {
			return zero, Refusal(0x21)
		}
		if !option.Allows(item.TypeFlags) {
			return zero, Refusal(9)
		}
		oldIndex, oldValue = c.findMagic(*item, option.Tag)
		if oldIndex < 0 && len(item.MagicOptions) >= c.magicLimit(*item) {
			return zero, Refusal(0x22)
		}
		if option.Tag == 0x726570 && oldIndex < 0 {
			return zero, Refusal(0x25)
		}
		if option.Tag == 0x61737472 {
			_, immortal := c.findMagic(*item, 0x61746861)
			if immortal <= oldValue {
				return zero, Refusal(0x24)
			}
		}
		switch option.Tag {
		case 0x61746861, 0x6c75636b, 0x736f6c69, 0x61737472, 0x617065:
			if oldValue >= 6 {
				return zero, Refusal(0x22)
			}
		case 0x726570:
			if oldValue >= 7 {
				return zero, Refusal(0x22)
			}
		}
	} else {
		allowed := false
		for _, cat := range strings.Split(r.Descriptions[3], ",") {
			if cat == category(item.TypeFlags) {
				allowed = true
			}
		}
		if !allowed {
			return zero, Refusal(0x21)
		}
		for i, name := range attributes(item.TypeFlags) {
			if name == r.Descriptions[0] {
				attrIndex = i
			}
		}
		if attrIndex < 0 {
			return zero, Refusal(0x21)
		}
	}
	chance := int(int32(r.Params[3])) + bonus
	if chance < 5 {
		chance = 5
	}
	if chance > 100 {
		chance = 100
	}
	n, e := draw(roll, 100)
	if e != nil {
		return zero, e
	}
	result := Outcome{Target: item.Slot, Success: int(n) <= chance}
	if result.Success {
		oldCount := len(item.MagicOptions)
		if magic {
			value := oldValue + 1
			switch option.Tag {
			case 0x61746861, 0x6c75636b, 0x736f6c69, 0x61737472, 0x617065, 0x726570:
			default:
				value, e = RollMagicValue(option, roll)
				if e != nil {
					return zero, e
				}
			}
			if value < 1 {
				value = 1
			}
			if value > 1700 {
				value = 1700
			}
			v := uint64(value)<<32 | uint64(option.ID)
			if oldIndex >= 0 {
				item.MagicOptions[oldIndex] = v
			} else {
				item.MagicOptions = append(item.MagicOptions, v)
			}
		} else {
			v, e := c.varianceValue(*item, roll)
			if e != nil {
				return zero, e
			}
			setVariance(item, attrIndex, v)
		}
		if e = c.assimilate(item, stone, attrIndex, magic, oldCount, roll); e != nil {
			return zero, e
		}
	}
	out[material].Quantity--
	for _, row := range out {
		if row.Quantity > 0 {
			result.Items = append(result.Items, row)
		}
	}
	return result, nil
}
