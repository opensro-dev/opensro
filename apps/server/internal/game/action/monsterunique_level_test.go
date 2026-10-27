/*
===========================================================================

monsterunique_level_test.go - a top-band unique rolls at the top level

Port-only, not native (owner 2026-10-25, narrowed 2026-10-26):
SRO_UNIQUE_MAX_LEVEL_LOOT rolls a unique's equipment tables at the highest
installed level only while the unique's own level is in the catalog's top
loot band (within ten of the top), so a top-band unique leaves degree-9
gear at the native cap and degree-11 gear once the cap-110 catalog moves
the top. Tiger Girl (level 20) keeps her own degree-3 table, and off is
the native unique-level table for every unique.

===========================================================================
*/
package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestUniqueDropTableLevel

The rule's arithmetic: a unique in the top loot band (own level within ten
of the installed top, 90 here) rolls at the top level, a unique below the
band keeps its own, an ordinary monster keeps its own, a unique above the
top keeps its own, and off is native.
================
*/
func TestUniqueDropTableLevel(t *testing.T) {
	unique := monster.Instance{Ref: monster.MonsterRef{Level: 20, MonsterType: 3}}
	band := monster.Instance{Ref: monster.MonsterRef{Level: 85, MonsterType: 3}}
	floor := monster.Instance{Ref: monster.MonsterRef{Level: 80, MonsterType: 3}}
	below := monster.Instance{Ref: monster.MonsterRef{Level: 79, MonsterType: 3}}
	ordinary := monster.Instance{Ref: monster.MonsterRef{Level: 85, MonsterType: 0}}
	above := monster.Instance{Ref: monster.MonsterRef{Level: 94, MonsterType: 3}}
	for _, tc := range []struct {
		name  string
		rules progression.UniqueRules
		mob   monster.Instance
		want  uint8
	}{
		{"top-band unique at the cap", progression.UniqueRules{MaxLevelLoot: true}, band, 90},
		{"band floor at the cap", progression.UniqueRules{MaxLevelLoot: true}, floor, 90},
		{"below the band keeps its own", progression.UniqueRules{MaxLevelLoot: true}, below, 79},
		{"low unique keeps its own", progression.UniqueRules{MaxLevelLoot: true}, unique, 20},
		{"ordinary in the band keeps its own", progression.UniqueRules{MaxLevelLoot: true}, ordinary, 85},
		{"unique above the cap", progression.UniqueRules{MaxLevelLoot: true}, above, 94},
		{"top-band unique native table", progression.UniqueRules{}, band, 85},
	} {
		rt := Runtime{UniqueRules: tc.rules}
		if got := rt.uniqueDropTableLevel(tc.mob); got != tc.want {
			t.Fatalf("%s = %d, want %d", tc.name, got, tc.want)
		}
	}
}

/*
================
TestUniquePrepassRollsCapLevelEquipment

A top-band unique's prepass leaves degree-9 equipment while the rule is
on; a level-20 unique below the band leaves its native degree-3 equipment
with the rule on or off. The item is still the special constructor's:
+1..5, magic options, non-repairable.
================
*/
func TestUniquePrepassRollsCapLevelEquipment(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level uint8
		rules progression.UniqueRules
		want  string
	}{
		{"cap level", 85, progression.UniqueRules{MaxLevelLoot: true}, "_09_"},
		{"below the band", 20, progression.UniqueRules{MaxLevelLoot: true}, "_03_"},
		{"native", 20, progression.UniqueRules{}, "_03_"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, _, c, target := newCombatTestRuntimeAtLevel(t, 1, tc.level)
			installLootReferences(t, rt)
			target.Ref.Codename, target.Ref.MonsterType = "MOB_CH_TIGERWOMAN", 3
			target.Nest.HasRarityOverride, target.Nest.RarityOverride = true, 3
			rt.UniqueRules = tc.rules
			rt.DropRoll = constantDropRoll(0)
			drops := rt.prepareUniqueDrops(uniqueDropContext{mob: target, owner: c.Name, now: rt.Now()})
			equipment := 0
			for _, drop := range drops {
				if drop.TypeFlags&0x7c != 0x2c {
					continue
				}
				equipment++
				if !strings.Contains(drop.Codename, tc.want) {
					t.Fatalf("prepass equipment %s, want a %s item", drop.Codename, tc.want)
				}
				if drop.Plus < 1 || drop.Plus > 5 || len(drop.MagicOptions) == 0 {
					t.Fatalf("special constructor properties lost: %+v", drop)
				}
			}
			if equipment == 0 {
				t.Fatal("unique prepass dropped no equipment")
			}
		})
	}
}

/*
================
TestUniqueSealRewardsAreGuaranteed

A kill whose draws admit the Moon leaves exactly one Star and one Moon, of
the top degree, no matter how small the ordinary drop cap is: the seals are
appended after the cap like the ticket. No Sun is ever guaranteed, and a
draw that admits no Moon leaves the Star only.
================
*/
func TestUniqueSealRewardsAreGuaranteed(t *testing.T) {
	for _, tc := range []struct {
		name string
		roll uint32
		want map[string]int
	}{
		{"star and moon", 0, map[string]int{"_09_A_RARE": 1, "_09_B_RARE": 1, "_09_C_RARE": 0}},
		{"star only", 7, map[string]int{"_09_A_RARE": 1, "_09_B_RARE": 0, "_09_C_RARE": 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, _, c, target := newCombatTestRuntimeAtLevel(t, 1, 85)
			installLootReferences(t, rt)
			target.Ref.Codename, target.Ref.MonsterType = "MOB_CH_TIGERWOMAN", 3
			target.Nest.HasRarityOverride, target.Nest.RarityOverride = true, 3
			rt.UniqueRules = progression.UniqueRules{MaxLevelLoot: true, SealDrops: true}
			rt.DropCap = 1
			rt.DropRoll = constantDropRoll(tc.roll)
			drops, _ := rt.planMonsterKillLoot(c, target, monster.Pose{
				RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z,
			}, rt.Now().UnixMilli())
			got := map[string]int{}
			for _, drop := range drops {
				for suffix := range tc.want {
					if strings.HasSuffix(drop.Codename, suffix) {
						got[suffix]++
					}
				}
			}
			for suffix, want := range tc.want {
				if got[suffix] != want {
					t.Fatalf("%s drops %d, want %d (all seals %v)", suffix, got[suffix], want, got)
				}
			}
		})
	}
}

/*
================
TestUniqueSealRewardsBelowTheBandKeepTheOwnDegree

Below the band the guarantee keeps the unique's own degree: a level-20
unique with both rules on leaves a degree-3 Star and Moon, never the top
degree's seals.
================
*/
func TestUniqueSealRewardsBelowTheBandKeepTheOwnDegree(t *testing.T) {
	rt, _, c, target := newCombatTestRuntimeAtLevel(t, 1, 20)
	installLootReferences(t, rt)
	target.Ref.Codename, target.Ref.MonsterType = "MOB_CH_TIGERWOMAN", 3
	target.Nest.HasRarityOverride, target.Nest.RarityOverride = true, 3
	rt.UniqueRules = progression.UniqueRules{MaxLevelLoot: true, SealDrops: true}
	rt.DropRoll = constantDropRoll(0)
	got := map[string]int{}
	drops, _ := rt.planMonsterKillLoot(c, target, monster.Pose{
		RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z,
	}, rt.Now().UnixMilli())
	for _, drop := range drops {
		for _, suffix := range []string{"_03_A_RARE", "_03_B_RARE", "_09_A_RARE", "_09_B_RARE"} {
			if strings.HasSuffix(drop.Codename, suffix) {
				got[suffix]++
			}
		}
	}
	want := map[string]int{"_03_A_RARE": 1, "_03_B_RARE": 1, "_09_A_RARE": 0, "_09_B_RARE": 0}
	for suffix, count := range want {
		if got[suffix] != count {
			t.Fatalf("%s drops %d, want %d (all seals %v)", suffix, got[suffix], count, got)
		}
	}
}

/*
================
TestUniqueSealSwitchOffIsNative

With the seal switch off the kill keeps the native rare roll only: a draw
that never rolls rare leaves no sealed equipment even with the top-level
table in force.
================
*/
func TestUniqueSealSwitchOffIsNative(t *testing.T) {
	rt, _, c, target := newCombatTestRuntimeAtLevel(t, 1, 20)
	installLootReferences(t, rt)
	target.Ref.Codename, target.Ref.MonsterType = "MOB_CH_TIGERWOMAN", 3
	target.Nest.HasRarityOverride, target.Nest.RarityOverride = true, 3
	rt.UniqueRules = progression.UniqueRules{MaxLevelLoot: true}
	rt.DropRoll = constantDropRoll(7)
	drops, _ := rt.planMonsterKillLoot(c, target, monster.Pose{
		RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z,
	}, rt.Now().UnixMilli())
	for _, drop := range drops {
		if strings.HasSuffix(drop.Codename, "_RARE") {
			t.Fatalf("seal switch off left %s", drop.Codename)
		}
	}
}

/*
================
TestUniqueOrdinaryEquipmentTakesTheTopStrengthTier

A top-band unique's ordinary (non-seal) pick under
SRO_UNIQUE_MAX_LEVEL_LOOT comes from the level's highest class: degree 9's
C tier at the native cap, whose required level and stats are the top of
the three.
================
*/
func TestUniqueOrdinaryEquipmentTakesTheTopStrengthTier(t *testing.T) {
	rt, _, _, target := newCombatTestRuntimeAtLevel(t, 1, 85)
	installLootReferences(t, rt)
	target.Ref.Codename, target.Ref.MonsterType = "MOB_CH_TIGERWOMAN", 3
	target.Nest.HasRarityOverride, target.Nest.RarityOverride = true, 3
	rt.UniqueRules = progression.UniqueRules{MaxLevelLoot: true}
	rt.DropRoll = constantDropRoll(7)
	item, ok := rt.prepareEquipmentDropKind(target, simulation.Spawn{}, target.Ref.Codename, rt.Now(), false)
	if !ok {
		t.Fatal("ordinary unique equipment pick failed")
	}
	if !strings.Contains(item.Codename, "_09_") || !strings.HasSuffix(item.Codename, "_C") {
		t.Fatalf("ordinary unique pick %s, want the degree-9 C tier", item.Codename)
	}
}
