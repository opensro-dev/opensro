/*
===========================================================================

nasrun.go - Nasrun alchemy: Sabakun's jewel on a Devil's Spirit

Port-only, not native to v1.150 (#3456 step 3c), on with SRO_NASRUN; off,
a Spirit is no reinforce target and the jewel is refused as before (0x10,
then 0x09 for any other item). v1.150 has no Spirit and no jewel rule.

The later official sources do have one, and this file departs from it on
the owner's pick. What ISRO-R records (textdata_object, textuisystem):
the jewel "can be used on Devil's Spirit to upgrade"; a Spirit never
breaks (UIIT_STT_NASRUN_NOT_BREAK, as here); and an event Spirit cannot
be upgraded (UIIT_STT_NASRUN_EVENTNASRUN_NOT_UPGRADE). The v1.696
client's ability rows author every Spirit from +0 to +10. The UPGRADE_
SUCCESS/FAILED/TIME messages (a grade change "for the next %d
minute(s)") sit under the awakening-enhancement scroll's section: they
belong to that separate later item, not to the jewel, and that item is
not ported. Sabakun's scroll (ITEM_ETC_NASRUN_ARCHEMY_RECIPE) is the
jewel's recipe, also not ported. No table read gives the jewel's chances,
whether it can fail, or its ceiling. The owner's rule (2026-11-16, #3456)
stands in for all three: the jewel raises a Spirit one plus at a time,
permanently, at 70/60/50/40/30/20 % up to +6, and a failure spends the
jewel. The owner's later rule (2026-11-28, #4732) raises the cap to +10
(the last plus the ability rows author, nasrun.MaxAbilityPlus) at
15/10/8/5 % beyond +6, and makes it harder: a failure above +5 drops the
Spirit back to +5 (spiritFailFloor), and a failure at or below +5 keeps
the plus. #4732's third rule, a failure from +8 that can also destroy the
Spirit, waits there for its chance.

Event Spirits: nasrun.IsSpirit matches any 3/1/14, and ISRO-R has ten
ITEM_EVENT_AVATAR_*_NASRUN* rows of that type. They are out of reach only
because scripts/nasrun_import.py does not import them; an import that adds
them must also refuse them here, as the native rule does.

The client already sends the pair: its alchemy window admits a 3/1/14
target as equipment (CItemData_IsEquipment, alchemy-selection.ts) and the
jewel by its elixir type (3/3/10/1). The jewel's class parameters admit
only class 14 (scripts/nasrun_import.py refuses any other), and no other
elixir names class 14, so an elixir on a Spirit is refused (0x09) as an
elixir on a foreign class is natively.

Inferred, recorded here: the chance is the owner's table alone. A lucky
powder has no Spirit degree, so staging one is refused with the powder
code (0x0a); the Lucky option, avatar luck and the shard's rate table do
not touch it; the native ceiling code (0x0f) answers at +10. The window
takes bag items only, as natively: the v1.150 client sends bag slots
alone, and the v1.188 server's reinforce handler refuses any target slot
outside the bag (0x5414). So a worn Spirit is taken off first.

===========================================================================
*/

package alchemy

import (
	"slices"

	"opensro.online/server/internal/game/item/inventory"
)

// SpiritMaxPlus is the highest plus Sabakun's jewel reaches: the last
// plus the Spirit's ability rows author (owner decision 2026-11-28, #4732).
const SpiritMaxPlus = 10

// spiritFailFloor is the plus a failed attempt above it drops the Spirit
// back to (#4732).
const spiritFailFloor = 5

// spiritClass is the Spirit's TID3, the only class the jewel admits.
const spiritClass = 14

// spiritChances is the owner's success chance (percent) of the attempt
// from each plus: +0 -> +1 at 70 down to +5 -> +6 at 20 (2026-11-16, #3456),
// then +6 -> +7 at 15 down to +9 -> +10 at 5 (#4732).
var spiritChances = [SpiritMaxPlus]int{70, 60, 50, 40, 30, 20, 15, 10, 8, 5}

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
	switch {
	case result.Success:
		item.Plus++
	case item.Plus > spiritFailFloor:
		item.Plus = spiritFailFloor
	}
	out[elixir].Quantity--
	for _, row := range out {
		if row.Quantity > 0 {
			result.Items = append(result.Items, row)
		}
	}
	return result, nil
}
