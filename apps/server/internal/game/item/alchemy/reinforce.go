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
	"slices"
	"sort"

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

// The reinforcement rules around the authored chances (the server's
// material-consumption step): a Lucky
// option adds ReinforceLuckBonus, the result is clamped to
// ReinforceMinChance..ReinforceMaxChance, and a failure from
// ReinforceRiskPlus up destroys the item ReinforceDestroyPercent of the time
// (else curses it); a failure resets the plus to 0, or to
// ReinforceAstralFloor under an Astral charge.
const (
	ReinforceLuckBonus      = 5
	ReinforceMinChance      = 10
	ReinforceMaxChance      = 100
	ReinforceRiskPlus       = 5
	ReinforceDestroyPercent = 50
	ReinforceAstralFloor    = 4
)

// The magic option tags reinforcement reads (magicoption.txt, the option's
// four-character tag as a big-endian word): MATTR_LUCK adds to the chance,
// MATTR_ATHANASIA saves an item from destruction, MATTR_SOLID from a curse,
// MATTR_ASTRAL keeps +4; each spends one charge. MagicTagAlchemyBonus
// ("luca") is the avatar option the server's magic-option application
// adds to every attempt.
const (
	MagicTagLuck         uint32 = 0x6c75636b
	MagicTagImmortal     uint32 = 0x61746861
	MagicTagSteady       uint32 = 0x736f6c69
	MagicTagAstral       uint32 = 0x61737472
	MagicTagAlchemyBonus uint32 = 0x6c756361
)

// equipmentFlagsBase is TID1 3 / TID2 1 packed, the equipment band that
// category reads; an elixir parameter byte supplies the TID3.
const equipmentFlagsBase = 0x2c

/*
================
elixirClasses

The item classes (TID3) an elixir admits: the non-zero bytes of its first
and last parameters. Reinforce refuses any other item (0x09); the
catalogue names the families from the same list.
================
*/
func elixirClasses(recipe Reference) []uint16 {
	var classes []uint16
	for _, p := range []uint32{recipe.Params[0], recipe.Params[4]} {
		for shift := uint(0); shift < 32; shift += 8 {
			if class := uint16(p >> shift & 255); class != 0 {
				classes = append(classes, class)
			}
		}
	}
	return classes
}

/*
================
elixirKinds

The equipment families of an elixir's classes (elixirClasses), sorted.
================
*/
func elixirKinds(recipe Reference) []string {
	var kinds []string
	seen := map[string]bool{}
	for _, class := range elixirClasses(recipe) {
		kind := category(equipmentFlagsBase | class<<7)
		if kind == "" || seen[kind] {
			continue
		}
		seen[kind] = true
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// reinforceMaxDegree bounds a powder's authored degree (item classes run
// to degree 16 in the widest catalog).
const reinforceMaxDegree = 16

// reinforceChanceLevels is the plus levels probability distinguishes:
// +0..+10 each, and +11 for every level beyond.
const reinforceChanceLevels = 12

/*
================
ReinforceTable

One elixir's or powder's authored chance at each plus (+0..+11; +11 holds
for every higher plus). Degree is a powder's equipment degree.
================
*/
type ReinforceTable struct {
	Codename string
	Degree   int
	Chances  [reinforceChanceLevels]int
	// Kinds are the equipment families an elixir accepts (weapon, shield,
	// armor, accessory), from the classes its first and last parameters
	// list; Reinforce refuses any other item with 0x09.
	Kinds []string
}

/*
================
Catalog.ReinforceTables

The elixirs (3/3/10/1) and lucky powders (3/3/10/2) Reinforce reads, by
codename. The website's catalogue publishes them.
================
*/
func (c *Catalog) ReinforceTables() (elixirs, powders []ReinforceTable) {
	for codename, ref := range c.Items {
		var table ReinforceTable
		table.Codename = codename
		for plus := range table.Chances {
			table.Chances[plus] = probability(ref, uint8(plus))
		}
		switch ref.Flags {
		case wire.PackTypeFlags(3, 3, 10, 1):
			table.Kinds = elixirKinds(ref)
			elixirs = append(elixirs, table)
		case wire.PackTypeFlags(3, 3, 10, 2):
			// A powder's first parameter is its equipment degree; a value
			// past any degree is authoring damage and publishes none.
			if degree := ref.Params[0]; degree <= reinforceMaxDegree {
				table.Degree = int(degree)
			}
			powders = append(powders, table)
		}
	}
	sort.Slice(elixirs, func(i, j int) bool { return elixirs[i].Codename < elixirs[j].Codename })
	sort.Slice(powders, func(i, j int) bool { return powders[i].Codename < powders[j].Codename })
	return elixirs, powders
}

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
		// items holds the character's bag only: a slot past its capacity
		// has no row and is refused below.
		if slot < inventory.EquipmentSlotEnd || slot >= inventory.MaxBagEnd || seen[slot] {
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
	if item.Plus >= nativeReinforceCeiling {
		return zero, Refusal(0x0f)
	}
	// Port-only, not native: the shard's cap (maxplus.go) refuses with the
	// native ceiling's own code.
	if c.MaxPlus > 0 && item.Plus >= c.MaxPlus {
		return zero, Refusal(0x0f)
	}
	if !slices.Contains(elixirClasses(recipe), item.TypeFlags>>7&15) {
		return zero, Refusal(9)
	}
	chance, floor := probability(recipe, item.Plus), ReinforceMinChance
	if c.Rates != nil {
		// Port-only, not native: the shard's table (rates.go) replaces the
		// elixir's authored chance and is its own floor.
		chance, floor = c.Rates.chance(item.Plus), 0
	}
	if powder >= 0 {
		p := c.Items[out[powder].Codename]
		if int(p.Params[0]) != ref.Degree() {
			return zero, Refusal(0x0a)
		}
		if chance < ReinforceMaxChance {
			chance += probability(p, item.Plus)
		}
	}
	if chance < ReinforceMaxChance && c.charge(item, MagicTagLuck) {
		chance += ReinforceLuckBonus
	}
	chance += bonus
	if chance < floor {
		chance = floor
	}
	if chance > ReinforceMaxChance {
		chance = ReinforceMaxChance
	}
	n, err := draw(roll, 100)
	if err != nil {
		return zero, err
	}
	result := Outcome{Target: item.Slot, Success: int(n) < chance}
	if result.Success {
		item.Plus++
	} else {
		if item.Plus >= ReinforceRiskPlus {
			n, err = draw(roll, 100)
			if err != nil {
				return zero, err
			}
			if n < ReinforceDestroyPercent {
				// The server's failure application destroys only when its
				// cash-item test is false: !(w&2), TID1 3 and bit 0, CashItem.
				// A reinforce target is always equipment, so only the cash
				// flag decides; the port's word leaves bit 0 clear, so the
				// reference's Cash is read instead.
				result.Destroyed = !ref.Cash
				if result.Destroyed && c.charge(item, MagicTagImmortal) {
					result.Destroyed = false
				}
			} else if !c.charge(item, MagicTagSteady) {
				if err = c.curse(item, roll); err != nil {
					return zero, err
				}
			}
		}
		if !result.Destroyed {
			if item.Plus >= ReinforceAstralFloor && c.charge(item, MagicTagAstral) {
				item.Plus = ReinforceAstralFloor
			} else {
				item.Plus = 0
			}
		}
	}
	// The server's consumption step: consume one of each staged material,
	// and the target only when the destruction bit survives the
	// protection branch.
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
