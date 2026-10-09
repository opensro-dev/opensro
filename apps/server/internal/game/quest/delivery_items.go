/*
===========================================================================

delivery_items.go - items a delivery quest grants, holds and takes

Acceptance grants the delivery items in the same transaction that opens the
journal record; the hand-over takes them unless the mission keeps them, and
abandonment removes every held stack.

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
loadDelivery

Native 86ab00 -> 92a64c stores quantity/codename pairs, independently of
dialogue text. 9208d0 preflights and grants them before 92040e accepts.
This owner keeps inventory and journal admission in the same transaction.
================
*/
func loadDelivery(def *Definition, items enterworld.ItemRefSource) error {
	if def.Objective != ObjectiveDelivery {
		if len(def.DeliveryItems) != 0 {
			return fmt.Errorf("quest %s delivery items on another objective", def.Codename)
		}
		return nil
	}
	// A receive-only hand-over (no items, an exchange) is a delivery too. It
	// grants nothing at acceptance, so only a delivery that does needs the
	// acceptance bag-full line; the hand-over answers with +0xC8.
	if len(def.DeliveryItems) == 0 && (len(def.ExchangeItems) == 0 || def.HandOverNpcCodename == "") ||
		len(def.DeliveryItems) > 10 || items == nil || len(def.Stages) != 0 || len(def.Objectives) != 0 || def.MonsterDrop != nil || def.DeliveryNpcCodename != "" || len(def.DeliveryItems) > 0 && def.InventoryFullSymbol == "" || def.CollectItemCodename != "" || def.CollectCount != 0 || def.KillCount != 0 || len(def.KillMonsterCodenames) != 0 {
		return fmt.Errorf("quest %s incomplete acceptance delivery contract", def.Codename)
	}
	seen := map[string]bool{}
	def.deliveryRefs = nil
	for _, item := range def.DeliveryItems {
		ref, ok := items.ItemRefByCodename(item.ItemCodename)
		if !ok || ref == nil || item.Count == 0 || item.Count > 65535 || seen[item.ItemCodename] {
			return fmt.Errorf("quest %s invalid delivery item %s", def.Codename, item.ItemCodename)
		}
		seen[item.ItemCodename] = true
		def.deliveryRefs = append(def.deliveryRefs, ref.RefObjID)
	}
	return nil
}

/*
================
deliveryHeld
================
*/
func deliveryHeld(c *enterworld.Character, ref uint32) uint32 {
	var total uint64
	for _, row := range c.MissionInventory {
		if row.RefObjID == ref && inventory.InBag(c, row.Slot) {
			count := row.StackCount
			if count < 1 {
				count = 1
			}
			total += uint64(count)
		}
	}
	if total > uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(total)
}

/*
================
deliveryMet
================
*/
func deliveryMet(c *enterworld.Character, def *Definition) bool {
	if len(def.deliveryRefs) != len(def.DeliveryItems) {
		return false
	}
	// A receive-only hand-over has nothing to hold.
	if len(def.DeliveryItems) == 0 {
		return def.HandOverNpcCodename != "" && len(def.ExchangeItems) > 0
	}
	for i, item := range def.DeliveryItems {
		if deliveryHeld(c, def.deliveryRefs[i]) < item.Count {
			return false
		}
	}
	return true
}

/*
================
deliveryCleanup

923c31..923cdf removes all held stacks for each delivery codename on
abandonment, not merely the originally granted quantity.
================
*/
func deliveryCleanup(c *enterworld.Character, def *Definition) []inventory.ItemAmount {
	var amounts []inventory.ItemAmount
	for _, m := range deliveryMissions(def) {
		for i, ref := range m.deliveryRefs {
			if count := deliveryHeld(c, ref); count > 0 {
				amounts = append(amounts, inventory.ItemAmount{Codename: m.DeliveryItems[i].ItemCodename, Count: count})
			}
		}
	}
	return append(amounts, exchangeReturns(c, def)...)
}

/*
================
deliveryMissions

The quest's delivery missions as definitions: the quest itself for a single
delivery, or each delivery mission of a parallel quest.
================
*/
func deliveryMissions(def *Definition) []*Definition {
	var out []*Definition
	for i := 0; i < missionCount(def); i++ {
		if m := missionDefinition(def, i); m.Objective == ObjectiveDelivery {
			out = append(out, m)
		}
	}
	return out
}

/*
================
acceptanceGrants

Every delivery mission's items, granted together when the quest is
accepted (9208D0 walks all kind-3 missions).
================
*/
func acceptanceGrants(def *Definition) []inventory.ItemAmount {
	var amounts []inventory.ItemAmount
	for _, m := range deliveryMissions(def) {
		amounts = append(amounts, deliveryAmounts(m)...)
	}
	return amounts
}

/*
================
deliveryAmounts
================
*/
func deliveryAmounts(def *Definition) []inventory.ItemAmount {
	amounts := make([]inventory.ItemAmount, 0, len(def.DeliveryItems))
	for _, item := range def.DeliveryItems {
		amounts = append(amounts, inventory.ItemAmount{Codename: item.ItemCodename, Count: item.Count})
	}
	return amounts
}

/*
================
validateDeliveryExtras

Kept deliveries, hand-overs and their exchange are delivery contracts. An
acceptance or exchange item must resolve, appear once and fit the native
u16 count.
================
*/
func validateDeliveryExtras(spec QuestSpec, items enterworld.ItemRefSource) error {
	delivery := spec.Objective == ObjectiveDelivery
	if !delivery && (spec.DeliveryKeepsItems || spec.HandOverNpcCodename != "") {
		return fmt.Errorf("quest %s delivery behaviour without a delivery", spec.Codename)
	}
	if spec.HandOverNpcCodename != "" && spec.HandOverSymbol == "" {
		return fmt.Errorf("quest %s hand-over without its line", spec.Codename)
	}
	if (len(spec.ExchangeItems) > 0 || spec.ExchangeFullSymbol != "") && spec.HandOverNpcCodename == "" {
		return fmt.Errorf("quest %s exchange without a hand-over", spec.Codename)
	}
	for _, code := range append(append([]string(nil), spec.RequiredHeldItems...), spec.RequiredAnyHeldItems...) {
		if items == nil {
			return fmt.Errorf("quest %s needs the item catalog for its held items", spec.Codename)
		}
		if ref, ok := items.ItemRefByCodename(code); !ok || ref == nil {
			return fmt.Errorf("quest %s unresolved held item %s", spec.Codename, code)
		}
	}
	for _, list := range [][]RewardItemLead{spec.AcceptanceConsumes, spec.ExchangeItems} {
		seen := map[string]bool{}
		for _, item := range list {
			if items == nil || item.Count == 0 || item.Count > 65535 || seen[item.ItemCodename] {
				return fmt.Errorf("quest %s invalid delivery item %s", spec.Codename, item.ItemCodename)
			}
			if ref, ok := items.ItemRefByCodename(item.ItemCodename); !ok || ref == nil {
				return fmt.Errorf("quest %s unresolved delivery item %s", spec.Codename, item.ItemCodename)
			}
			seen[item.ItemCodename] = true
		}
	}
	return nil
}

/*
================
handedOver

A two-leg delivery's mission latch, set by the hand-over (91CA00 writes
quest-user +3) and persisted as the node's CompletionReached.
================
*/
func handedOver(record enterworld.ActiveQuestRecord) bool {
	return len(record.Contents) == 1 && missionCompletionReached(record.Contents[0])
}

/*
================
heldAmounts

Each item up to its count, as many as are held.
================
*/
func heldAmounts(c *enterworld.Character, list []RewardItemLead) []inventory.ItemAmount {
	var amounts []inventory.ItemAmount
	for _, item := range list {
		count := min(captureItemCount(c, item.ItemCodename), item.Count)
		if count > 0 {
			amounts = append(amounts, inventory.ItemAmount{Codename: item.ItemCodename, Count: count})
		}
	}
	return amounts
}

/*
================
exchangeReturns

What the reward takes back of a hand-over's exchange: CBasicQuest_vfF8
removes a delivery mission's items as the quest completes (inference: its
kind-3 case names one item group, and every achieved-now line sends the
exchange to the reporting NPC). Only held items leave, as removal there
never refuses.
================
*/
func exchangeReturns(c *enterworld.Character, def *Definition) []inventory.ItemAmount {
	var amounts []inventory.ItemAmount
	for _, m := range deliveryMissions(def) {
		if m.HandOverNpcCodename != "" {
			amounts = append(amounts, heldAmounts(c, m.ExchangeItems)...)
		}
	}
	return amounts
}

/*
================
acceptanceConsumption

What 897680 takes at acceptance: each item up to its count, as many as are
held. A missing item only raised a minidump natively; acceptance goes on.
================
*/
func acceptanceConsumption(c *enterworld.Character, def *Definition) []inventory.ItemAmount {
	return heldAmounts(c, def.AcceptanceConsumes)
}
