package quest

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

// Native 86ab00 -> 92a64c stores quantity/codename pairs, independently of
// dialogue text. 9208d0 preflights and grants them before 92040e accepts.
// This owner keeps inventory and journal admission in the same transaction.
func loadDelivery(def *Definition, items enterworld.ItemRefSource) error {
	if def.Objective != ObjectiveDelivery {
		if len(def.DeliveryItems) != 0 {
			return fmt.Errorf("quest %s delivery items on another objective", def.Codename)
		}
		return nil
	}
	if len(def.DeliveryItems) == 0 || len(def.DeliveryItems) > 10 || items == nil || len(def.Stages) != 0 || len(def.Objectives) != 0 || def.MonsterDrop != nil || def.DeliveryNpcCodename != "" || def.InventoryFullSymbol == "" || def.CollectItemCodename != "" || def.CollectCount != 0 || def.KillCount != 0 || len(def.KillMonsterCodenames) != 0 {
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

func deliveryMet(c *enterworld.Character, def *Definition) bool {
	if len(def.deliveryRefs) == 0 || len(def.deliveryRefs) != len(def.DeliveryItems) {
		return false
	}
	for i, item := range def.DeliveryItems {
		if deliveryHeld(c, def.deliveryRefs[i]) < item.Count {
			return false
		}
	}
	return true
}

// 923c31..923cdf removes all held stacks for each delivery codename on
// abandonment, not merely the originally granted quantity.
func deliveryCleanup(c *enterworld.Character, def *Definition) []inventory.ItemAmount {
	var amounts []inventory.ItemAmount
	for i, ref := range def.deliveryRefs {
		if count := deliveryHeld(c, ref); count > 0 {
			amounts = append(amounts, inventory.ItemAmount{Codename: def.DeliveryItems[i].ItemCodename, Count: count})
		}
	}
	return amounts
}

func deliveryAmounts(def *Definition) []inventory.ItemAmount {
	amounts := make([]inventory.ItemAmount, 0, len(def.DeliveryItems))
	for _, item := range def.DeliveryItems {
		amounts = append(amounts, inventory.ItemAmount{Codename: item.ItemCodename, Count: item.Count})
	}
	return amounts
}
