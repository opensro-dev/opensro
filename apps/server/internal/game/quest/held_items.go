/*
===========================================================================

held_items.go - acceptance grants, end-of-quest clearing and per-item gold

Port-only helpers shared by the Blue Berserk chains (hwan_chain.go): items a
quest hands out when it is accepted, items its reset handler takes when it ends,
and the gold paid for items still held at the turn-in.

===========================================================================
*/
package quest

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

/*
================
validateHeldItemSpec

Every named item resolves, once, with a count the u16 stack allows.
================
*/
func validateHeldItemSpec(spec QuestSpec, items enterworld.ItemRefSource) error {
	if len(spec.AcceptanceGrants) == 0 && len(spec.ClearOnEnd) == 0 && len(spec.HeldItemGold) == 0 {
		return nil
	}
	if items == nil {
		return fmt.Errorf("quest %s needs the item catalog for its held items", spec.Codename)
	}
	resolve := func(code string) error {
		if ref, ok := items.ItemRefByCodename(code); !ok || ref == nil {
			return fmt.Errorf("quest %s unresolved item %s", spec.Codename, code)
		}
		return nil
	}
	seen := map[string]bool{}
	for _, grant := range spec.AcceptanceGrants {
		if grant.Count == 0 || grant.Count > 65535 || seen[grant.ItemCodename] {
			return fmt.Errorf("quest %s invalid acceptance grant %s", spec.Codename, grant.ItemCodename)
		}
		seen[grant.ItemCodename] = true
		if err := resolve(grant.ItemCodename); err != nil {
			return err
		}
	}
	for _, code := range spec.ClearOnEnd {
		if err := resolve(code); err != nil {
			return err
		}
	}
	for _, pay := range spec.HeldItemGold {
		if pay.GoldEach <= 0 || pay.GoldCap < pay.GoldEach {
			return fmt.Errorf("quest %s invalid per-item gold for %s", spec.Codename, pay.ItemCodename)
		}
		if err := resolve(pay.ItemCodename); err != nil {
			return err
		}
	}
	return nil
}

/*
================
specAcceptanceGrants
================
*/
func specAcceptanceGrants(def *Definition) []inventory.ItemAmount {
	out := make([]inventory.ItemAmount, 0, len(def.AcceptanceGrants))
	for _, grant := range def.AcceptanceGrants {
		out = append(out, inventory.ItemAmount{Codename: grant.ItemCodename, Count: grant.Count})
	}
	return out
}

/*
================
clearOnEndAmounts

Every stack held of the quest's ClearOnEnd items.
================
*/
func clearOnEndAmounts(c *enterworld.Character, def *Definition) []inventory.ItemAmount {
	var out []inventory.ItemAmount
	for _, code := range def.ClearOnEnd {
		if held := captureItemCount(c, code); held > 0 {
			out = append(out, inventory.ItemAmount{Codename: code, Count: held})
		}
	}
	return out
}

/*
================
heldItemGold

The gold the quest pays for the items still held, each item's total capped.
================
*/
func heldItemGold(c *enterworld.Character, def *Definition) int64 {
	var total int64
	for _, pay := range def.HeldItemGold {
		total += min(int64(captureItemCount(c, pay.ItemCodename))*pay.GoldEach, pay.GoldCap)
	}
	return total
}
