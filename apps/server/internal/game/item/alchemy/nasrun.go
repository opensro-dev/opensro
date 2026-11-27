/*
===========================================================================

nasrun.go - Nasrun alchemy: Sabakun's jewel on a Devil's Spirit

Port-only, not native to v1.150 (#3456 step 3c), on with SRO_NASRUN; off,
a Spirit is no reinforce target and the jewel is refused as before (0x10,
then 0x09 for any other item). v1.150 has no Spirit and no jewel rule.

The later official sources do have one, and this file departs from it on
the owner's pick. What ISRO-R records (textdata_object, textuisystem):
the jewel "can be used on Devil's Spirit to upgrade"; a Spirit never
breaks (UIIT_STT_NASRUN_NOT_BREAK, overruled below); and an event Spirit cannot
be upgraded (UIIT_STT_NASRUN_EVENTNASRUN_NOT_UPGRADE). The v1.696
client's ability rows author every Spirit from +0 to +10. The UPGRADE_
SUCCESS/FAILED/TIME messages (a grade change "for the next %d
minute(s)") sit under the awakening-enhancement scroll's section: they
belong to that separate later item, not to the jewel, and that item is
not ported. Sabakun's scroll (ITEM_ETC_NASRUN_ARCHEMY_RECIPE) is the
jewel's recipe, also not ported. No table read gives the jewel's chances,
whether it can fail, or its ceiling. The owner's rule stands in for all
three. It was first +6 with no loss (2026-11-16, #3456). On 2026-11-28 the
owner made it harder, after a player found it too easy:

- the jewel raises a Spirit one plus at a time, permanently, up to +10;
- the chances fall from 70 % (+0 -> +1) to 5 % (+9 -> +10), spiritChances;
- a failure at +5 or below keeps the plus;
- a failure above +5 drops the Spirit back to +5 (SpiritFloorPlus);
- a failure from +8 or above also destroys it SpiritDestroyPercent of the
  time. This breaks the later sources' NOT_BREAK, on the owner's word.

The jewel is spent either way.

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
not touch it; the native ceiling code (0x0f) answers at +10.
"Above +8" in the player's words is read as "at +8 or above", so the
attempts for +9 and +10 carry the risk. The destruction draw is a second
draw after the failed one, as the native reinforce's is (reinforce.go). The window
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

// SpiritMaxPlus is the highest plus Sabakun's jewel reaches.
const SpiritMaxPlus = 10

// SpiritFloorPlus is the plus a failure above it drops the Spirit back to.
const SpiritFloorPlus = 5

// SpiritRiskPlus is the plus from which a failure can destroy the Spirit.
const SpiritRiskPlus = 8

// SpiritDestroyPercent is the chance a failure from SpiritRiskPlus up
// destroys the Spirit.
const SpiritDestroyPercent = 30

// spiritClass is the Spirit's TID3, the only class the jewel admits.
const spiritClass = 14

// spiritChances is the owner's success chance (percent) of the attempt
// from each plus: +0 -> +1 at 70 down to +9 -> +10 at 5.
var spiritChances = [SpiritMaxPlus]int{70, 60, 50, 40, 30, 20, 15, 10, 7, 5}

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
	} else if err = failSpirit(item, &result, roll); err != nil {
		return zero, err
	}
	out[elixir].Quantity--
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

/*
================
failSpirit

A failed attempt's loss (see the file header): from SpiritRiskPlus up, a
second draw destroys the Spirit; otherwise a Spirit above SpiritFloorPlus
drops back to it, and one at or below it keeps its plus.
================
*/
func failSpirit(item *inventory.Item, result *Outcome, roll Roll) error {
	if item.Plus >= SpiritRiskPlus {
		n, err := draw(roll, 100)
		if err != nil {
			return err
		}
		if int(n) < SpiritDestroyPercent {
			result.Destroyed = true
			return nil
		}
	}
	if item.Plus > SpiritFloorPlus {
		item.Plus = SpiritFloorPlus
	}
	return nil
}
