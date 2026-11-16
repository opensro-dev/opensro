/*
===========================================================================
sookarena_rewards.go - port-only stage bundles drawn from authored item families.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
)

// The v1.150 recovery grains (itemdata_5000.txt lists only the _01 rows;
// the _02 names have no item) and the stack one arena slot pays.
var arenaGrains = [...]string{"ITEM_ETC_HP_SPOTION_01", "ITEM_ETC_MP_SPOTION_01", "ITEM_ETC_ALL_SPOTION_01"}

const arenaGrainStack = 50

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
arenaReward - pet gear, useful supplies, and normal/Star/Moon/Sun gear.
================
*/
func (rt *Runtime) arenaReward(run *sookArenaRun, c *enterworld.Character, stage, index int) (inventory.Item, bool) {
	items := rt.deps.ItemReferences()
	if items == nil {
		return inventory.Item{}, false
	}
	// No reward pays silk (owner, 2026-11-12): silk is the paid currency,
	// and a free daily activity that mints it would be an economy mint.
	level := uint8(min(int64(90), max(int64(1), rewardLevel(c))))
	country := uint8(rt.arenaDraw(2))
	tier := rt.arenaRewardTier(stage)
	parent, code, ok := rt.arenaGear(items, country, level, tier)
	if !ok {
		return inventory.Item{}, false
	}
	if index%12 == 1 && rt.AttackPetExpansion {
		degree := min(9, max(1, (int(level)+7)/8))
		if parent, found := items.ItemRefByCodename(parent); found && parent != nil {
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
	// Native recovery grains only; regular potions and pills are not arena
	// rewards. Drawn straight from the grain rows: no monster drop table
	// lists a grain, so routing through loot.SelectConsumable (#3189) could
	// never return one and both slots fell through to gear (#3368).
	if (index%12 == 2 || index%12 == 3) && stage != 5 {
		code := arenaGrains[rt.arenaDraw(len(arenaGrains))]
		if ref, valid := items.ItemRefByCodename(code); valid && ref != nil {
			return inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: arenaGrainStack}, true
		}
	}
	drop, ok := rt.prepareSelectedDrop(loot.DropItem{Codename: code, Count: 1, Plus: uint8(rt.arenaDraw(stage + 2)), FromUnique: true}, run.center, c.Name, rt.Now())
	if !ok {
		return inventory.Item{}, false
	}
	return groundInventoryItem(drop, 1), true
}

// The drop stop's re-draw (#3669 follow-up): how far down one re-draw
// steps the selection level (one native degree band) and how many it
// tries before the slot pays nothing.
const (
	arenaDegreeStepLevels = 8
	arenaDegreeRedraws    = 3
)

/*
================
arenaGear

One gear pick for the arena: the drawn equipment's codename (the parent
the pet gear reads) and the reward's codename with the stage's seal.
Port-only, not native: while SRO_ABOVE_DEGREE9_DROPS_OFF is on no item
above degree 9 drops (prepareSelectedDrop), and a level-90 draw on a
cap-110 item shard can land on degree 10, which left the slot empty.
Such a pick is drawn again from one degree band lower, a few times,
so the chest still pays the best gear the rule allows. Off, the first
pick stands, as before.
================
*/
func (rt *Runtime) arenaGear(items enterworld.ItemRefSource, country, level uint8, tier int) (string, string, bool) {
	pick := level
	for redraw := 0; ; redraw++ {
		group, ok := loot.SpecialEquipmentGroup(pick)
		if !ok {
			return "", "", false
		}
		chosen, ok := loot.SelectEquipment(country, group, false, pick, rt.DropRoll)
		if !ok {
			return "", "", false
		}
		code := chosen.Codename
		if len(code) > 2 && tier > 0 {
			candidate := code[:len(code)-1] + []string{"A_RARE", "B_RARE", "C_RARE"}[tier-1]
			if _, exists := items.ItemRefByCodename(candidate); exists {
				code = candidate
			}
		}
		ref, found := items.ItemRefByCodename(code)
		allowed := !found || ref == nil || !rt.AboveDegree9Drops.Excludes(ref.TypeIDs, enterworld.ItemDegree(ref))
		if allowed || redraw == arenaDegreeRedraws || pick <= arenaDegreeStepLevels {
			return chosen.Codename, code, true
		}
		pick -= arenaDegreeStepLevels
	}
}

/*
================
arenaRewardTier - port-only seal odds, quantized to the native 15-bit roll.
================
*/
func (rt *Runtime) arenaRewardTier(stage int) int {
	// Basis points: Sun, Moon, Star. Stage I cannot roll Moon or Sun.
	odds := [...][3]int{{0, 0, 0}, {0, 0, 200}, {0, 25, 300}, {5, 50, 400}, {10, 75, 500}, {25, 100, 600}}[min(5, max(0, stage))]
	const samples = 1 << 15
	const basisPoints = 10000
	roll := rt.arenaDraw(samples)
	if roll < odds[0]*samples/basisPoints {
		return 3
	}
	if roll < (odds[0]+odds[1])*samples/basisPoints {
		return 2
	}
	if roll < (odds[0]+odds[1]+odds[2])*samples/basisPoints {
		return 1
	}
	return 0
}
