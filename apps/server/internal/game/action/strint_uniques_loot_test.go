/*
===========================================================================

strint_uniques_loot_test.go - the unique reward switches reach the copies

Pins that a build-locked copy inherits every SRO_UNIQUE_* reward rule: the
seal guarantee and the top-loot band read the reference's grade, level and
country, never its codename, so a copy pays exactly what its base pays.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
sealCounts
================
*/
func sealCounts(drops []grounditem.Item) map[string]int {
	counts := map[string]int{}
	for _, drop := range drops {
		for _, suffix := range []string{"_09_A_RARE", "_09_B_RARE", "_09_C_RARE"} {
			if len(drop.Codename) >= len(suffix) && drop.Codename[len(drop.Codename)-len(suffix):] == suffix {
				counts[suffix]++
			}
		}
	}
	return counts
}

/*
================
TestUniqueSealRewardsApplyToACopy

The same kill, once as the shipped unique and once as its STR copy, under
SRO_UNIQUE_MAX_LEVEL_LOOT and SRO_UNIQUE_SEAL_DROPS: both leave one
degree-9 Seal of Star and one Seal of Moon, and no guaranteed Sun. The
copy's own codename keys nothing on this path.
================
*/
func TestUniqueSealRewardsApplyToACopy(t *testing.T) {
	rt, _, c, target := newCombatTestRuntimeAtLevel(t, 1, 85)
	installLootReferences(t, rt)
	rt.UniqueRules = progression.UniqueRules{MaxLevelLoot: true, SealDrops: true}
	rt.DropCap = 1
	rt.DropRoll = constantDropRoll(0)

	plan := func(instance monster.Instance) map[string]int {
		return sealCounts(rt.planMonsterKillLoot(c, instance, monster.Pose{
			RegionID: instance.Spawn.RegionID, X: instance.Spawn.X, Y: instance.Spawn.Y, Z: instance.Spawn.Z,
		}, rt.Now().UnixMilli()))
	}

	base := target
	base.Ref.Codename, base.Ref.MonsterType = "MOB_CH_TIGERWOMAN", 3
	base.Nest.HasRarityOverride, base.Nest.RarityOverride = true, 3

	copy := target
	copy.Ref.Codename, copy.Ref.MonsterType = "MOB_CH_TIGERWOMAN_STR", 3
	copy.Ref.VariantOf, copy.Ref.UniqueBuild = "MOB_CH_TIGERWOMAN", monster.UniqueBuildSTR
	copy.Nest.HasRarityOverride, copy.Nest.RarityOverride = true, 3

	baseCounts, copyCounts := plan(base), plan(copy)
	if copyCounts["_09_A_RARE"] != 1 || copyCounts["_09_B_RARE"] != 1 {
		t.Fatalf("copy seals = %v, want one Star and one Moon", copyCounts)
	}
	if copyCounts["_09_C_RARE"] != 0 {
		t.Fatalf("copy seals = %v, want no guaranteed Sun", copyCounts)
	}
	for suffix, want := range baseCounts {
		if copyCounts[suffix] != want {
			t.Fatalf("copy %s seals = %d, base = %d", suffix, copyCounts[suffix], want)
		}
	}
}
