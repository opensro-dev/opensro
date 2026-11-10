/*
===========================================================================
sookarena_rewards.go - port-only stage bundles drawn from authored item families.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"strings"
)

/*
================
arenaDraw
================
*/
func (rt *Runtime) arenaDraw(n int) int {
	if rt.DropRoll == nil || n <= 0 {
		return 0
	}
	v, e := rt.DropRoll()
	if e != nil {
		return 0
	}
	return int(v % uint32(n))
}

/*
================
arenaReward - vouchers, pet gear, useful supplies, and normal/Star/Moon/Sun gear.
================
*/
func (rt *Runtime) arenaReward(run *sookArenaRun, c *enterworld.Character, stage, index int) (inventory.Item, bool) {
	items := rt.deps.ItemReferences()
	if items == nil {
		return inventory.Item{}, false
	}
	if index%12 == 0 {
		ref, ok := items.ItemRefByID(domain.ArenaSilkScrollID)
		if ok && ref != nil {
			limits := [...]int{3, 5, 10, 15, domain.ArenaSilkScrollMaxStack}
			count := uint16(1 + rt.arenaDraw(limits[min(4, max(0, stage-1))]))
			return inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: count}, true
		}
	}
	level := uint8(min(int64(90), max(int64(1), rewardLevel(c))))
	group, ok := loot.SpecialEquipmentGroup(level)
	if !ok {
		return inventory.Item{}, false
	}
	country := uint8(rt.arenaDraw(2))
	chosen, ok := loot.SelectEquipment(country, group, false, level, rt.DropRoll)
	if !ok {
		return inventory.Item{}, false
	}
	code := chosen.Codename
	tier := rt.arenaRewardTier(stage)
	if len(code) > 2 && tier > 0 {
		base := code[:len(code)-1]
		candidate := base + []string{"A_RARE", "B_RARE", "C_RARE"}[tier-1]
		if _, exists := items.ItemRefByCodename(candidate); exists {
			code = candidate
		}
	}
	if index%12 == 1 && rt.AttackPetExpansion {
		degree := min(9, max(1, (int(level)+7)/8))
		if parent, found := items.ItemRefByCodename(chosen.Codename); found && parent != nil {
			degree = min(9, max(1, (int(parent.NativeFields.Get("itemClass"))-1)/3+1))
		}
		g := companion.D.Gear[degree-1]
		slot := rt.arenaDraw(len(g.Items))
		if len(g.Items) > 0 {
			if tier > 0 {
				if sealed, at, found := companion.GearByID(g.Items[slot].ID + uint32(tier*10)); found {
					g, slot = sealed, at
				}
			}
			if ref, exists := items.ItemRefByCodename(g.Items[slot].Codename); exists && ref != nil {
				affixes := rt.PetGearAffixes
				if affixes == nil {
					affixes = companion.D.Affixes
				}
				options := companion.RollGearWithAffixes(g, slot, 0, dropRoller{rt}, affixes)
				companion.ScaleSealOptions(options, tier)
				return inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: 1, MagicOptions: options}, true
			}
		}
	}
	// Native recovery grains only; regular potions and pills are not arena rewards.
	if (index%12 == 2 || index%12 == 3) && stage != 5 {
		if supply, exists := loot.SelectConsumable(rt.arenaDraw(4), level, uint32(rt.arenaDraw(1000000)), rt.DropRoll); exists {
			if ref, valid := items.ItemRefByCodename(supply.Codename); valid && ref != nil && strings.Contains(supply.Codename, "_SPOTION_") {
				return inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: 50}, true
			}
		}
	}
	drop, ok := rt.prepareSelectedDrop(loot.DropItem{Codename: code, Count: 1, Plus: uint8(rt.arenaDraw(stage + 2)), FromUnique: true}, run.center, c.Name, rt.Now())
	if !ok {
		return inventory.Item{}, false
	}
	return groundInventoryItem(drop, 1), true
}

/*
================
arenaRewardTier - per-gear Sun chances: none / none / 0.1% / 0.5% / 1%.
================
*/
func (rt *Runtime) arenaRewardTier(stage int) int {
	sun := [...]int{0, 0, 0, 10, 50, 100}[min(5, max(0, stage))]
	roll := rt.arenaDraw(10000)
	if roll < sun {
		return 3
	}
	if roll < sun+(10+stage*8)*100 {
		return 1 + rt.arenaDraw(2)
	}
	return 0
}
