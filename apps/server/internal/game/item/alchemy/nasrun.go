/*
===========================================================================

nasrun.go - Nasrun alchemy: Sabakun's jewel on a Devil's Spirit

Port-only, not native (#3456 step 3c), on with SRO_NASRUN; off, a Spirit
is no reinforce target and the jewel is refused as before (0x10, then 0x09
for any other item). v1.150 has no Spirit and no jewel rule. The owner's
rule (2026-11-16, #3456): the jewel raises a Spirit one plus at a time up
to +6, at 70/60/50/40/30/20 %; a failure spends the jewel and keeps the
plus (no break, no curse, no reset, since the Spirit is bound).

The client already sends the pair: its alchemy window admits a 3/1/14
target as equipment (CItemData_IsEquipment, alchemy-selection.ts) and the
jewel by its elixir type (3/3/10/1). The jewel's class parameters admit
only class 14 (scripts/nasrun_import.py refuses any other), and no other
elixir names class 14, so an elixir on a Spirit is refused (0x09) as an
elixir on a foreign class is natively.

Inferred, recorded here: the chance is the owner's table alone. A lucky
powder has no Spirit degree, so staging one is refused with the powder
code (0x0a); the Lucky option, avatar luck and the shard's rate table do
not touch it; the native ceiling code (0x0f) answers at +6. The window
takes bag items only, as natively, so a worn Spirit is taken off first.

===========================================================================
*/

package alchemy

import (
	"slices"

	"opensro.online/server/internal/game/item/inventory"
)

// SpiritMaxPlus is the highest plus Sabakun's jewel reaches.
const SpiritMaxPlus = 6

// spiritClass is the Spirit's TID3, the only class the jewel admits.
const spiritClass = 14

// spiritChances is the owner's success chance (percent) of the attempt
// from each plus: +0 -> +1 at 70 down to +5 -> +6 at 20.
var spiritChances = [SpiritMaxPlus]int{70, 60, 50, 40, 30, 20}

/*
================
SpiritChance

The chance (percent) of a jewel attempt from plus, and false at or past
SpiritMaxPlus.
================
*/
func SpiritChance(plus uint8) (int, bool) {
	if int(plus) >= SpiritMaxPlus {
		return 0, false
	}
	return spiritChances[plus], true
}

/*
================
Catalog.reinforceSpirit

One jewel attempt on the Spirit at out[target] (see the file header).
out is Reinforce's detached copy; the jewel is spent either way.
================
*/
func (c *Catalog) reinforceSpirit(out []inventory.Item, target, elixir, powder int, roll Roll) (Outcome, error) {
	var zero Outcome
	item := &out[target]
	recipe := c.Items[out[elixir].Codename]
	if !slices.Contains(elixirClasses(recipe), spiritClass) {
		return zero, Refusal(9)
	}
	if powder >= 0 {
		return zero, Refusal(0x0a)
	}
	chance, ok := SpiritChance(item.Plus)
	if !ok {
		return zero, Refusal(0x0f)
	}
	n, err := draw(roll, 100)
	if err != nil {
		return zero, err
	}
	result := Outcome{Target: item.Slot, Success: int(n) < chance, From: item.Plus}
	if result.Success {
		item.Plus++
	}
	out[elixir].Quantity--
	for _, row := range out {
		if row.Quantity > 0 {
			result.Items = append(result.Items, row)
		}
	}
	return result, nil
}
