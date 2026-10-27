/*
===========================================================================

sealpity_test.go - seal pity: debt, forced drops, caps, exclusions (#1690)

Port-only, not native (owner decision 2026-10-27). The tests pin the
three modes against the real planner: off and shadow produce the
native loot list and the native random sequence; on pays the owed
degree-9 Moon or Sun itself once the counted misses reach the
threshold, once per tier per UTC day; a natural drop of the tier, and
only that, resets its counter; trash kills, GM-spawned monsters and
the GM kill path never move a counter. The calibration test measures
the planner's degree-9 Star frequency against the per-kill chance the
counters accrue by (SealKillChance times admission), the phase-A
check the live shadow counters must pass.

===========================================================================
*/
package action

import (
	"math/rand/v2"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

var sealPityNoon = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

/*
================
sealPityRuntime

A combat runtime with seal pity at the named mode and the shipped item
references. The monster is a level-mobLevel fixture; the killer's level
is set by the caller.
================
*/
func sealPityRuntime(t *testing.T, mode string, mobLevel uint8) (*Runtime, *enterworld.Character, monster.Instance) {
	t.Helper()
	t.Setenv(progression.EnvSealPity, mode)
	pity, err := progression.SealPityFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	rt, _, killer, mob := newCombatTestRuntimeAtLevel(t, 100, mobLevel)
	rt.deps.(*enterworld.Deps).Items = shippedItems(t)
	rt.SealPity = pity
	return rt, killer, mob
}

/*
================
sealPityPose
================
*/
func sealPityPose(mob monster.Instance) monster.Pose {
	return monster.Pose{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X, Y: mob.Spawn.Y, Z: mob.Spawn.Z}
}

/*
================
setSealPityDebt

Preloads one counter the way a long dry spell leaves it.
================
*/
func setSealPityDebt(c *enterworld.Character, degree, tier int, debt float64) {
	class := sealPityItemClass(degree, tier)
	for i := range c.SealPity {
		if c.SealPity[i].ItemClass == class {
			c.SealPity[i].Debt = debt
			return
		}
	}
	c.SealPity = append(c.SealPity, domain.SealPityDebt{ItemClass: class, Debt: debt})
}

/*
================
sealPityDebtOf
================
*/
func sealPityDebtOf(c *enterworld.Character, degree, tier int) float64 {
	row, _ := sealPityRow(c, sealPityItemClass(degree, tier))
	return row.Debt
}

/*
================
dropSummary

The loot list as the off/shadow identity claim reads it.
================
*/
func dropSummary(drops []grounditem.Item) []string {
	out := make([]string, len(drops))
	for i, drop := range drops {
		out[i] = drop.Codename
	}
	return out
}

/*
================
TestSealPityShadowMatchesOffLootAndRolls

With a counter standing past its threshold, shadow logs and counts
what it would force but must not change one item or one random draw:
the off and shadow hunts are byte-identical.
================
*/
func TestSealPityShadowMatchesOffLootAndRolls(t *testing.T) {
	run := func(mode string) ([]string, int) {
		rt, killer, mob := sealPityRuntime(t, mode, 85)
		*killer.Level = 85
		setSealPityDebt(killer, 9, enterworld.SealSun, 99)
		setSealPityDebt(killer, 9, enterworld.SealMoon, 99)
		source := rand.New(rand.NewPCG(1690, 9))
		draws := 0
		rt.DropRoll = func() (uint32, error) {
			draws++
			return source.Uint32N(dropRollDomain), nil
		}
		pose := sealPityPose(mob)
		var got []string
		for kill := 0; kill < 300; kill++ {
			drops, delta := rt.planMonsterKillLoot(killer, mob, pose, sealPityNoon.UnixMilli())
			rt.commitSealPity(killer, delta, sealPityNoon.UnixMilli())
			got = append(got, dropSummary(drops)...)
		}
		return got, draws
	}
	offLoot, offDraws := run("off")
	shadowLoot, shadowDraws := run("shadow")
	if offDraws != shadowDraws {
		t.Fatalf("draws off %d, shadow %d: shadow touched the random sequence", offDraws, shadowDraws)
	}
	if len(offLoot) != len(shadowLoot) {
		t.Fatalf("loot off %d items, shadow %d", len(offLoot), len(shadowLoot))
	}
	for i := range offLoot {
		if offLoot[i] != shadowLoot[i] {
			t.Fatalf("loot[%d] off %s, shadow %s", i, offLoot[i], shadowLoot[i])
		}
	}
}

/*
================
TestSealPityOnForcesOwedSun

On a never-rare stream the degree-9 Sun arrives exactly when the
counted misses reach the threshold: one forced drop, owned and marked,
and the counter starts again.
================
*/
func TestSealPityOnForcesOwedSun(t *testing.T) {
	rt, killer, mob := sealPityRuntime(t, "on", 85)
	*killer.Level = 90
	rt.RareRate = 500 // shortens the count; the roll stream never drops a seal
	rt.DropRoll = constantDropRoll(0)

	settings := rt.sealChanceSettings(mob)
	increment := loot.SealKillChance(mob.Ref.Level, mob.Rarity(), mob.Ref.Codename, mob.Ref.Country, settings)[8][enterworld.SealSun]
	increment *= rt.admissionChance(uint8(*killer.Level), mob)
	if increment <= 0 {
		t.Fatal("the fixture kill carries no degree-9 Sun chance")
	}
	// The kill the running sum reaches the threshold on, summed the
	// way the commit sums it.
	wantKill := 0
	for debt := 0.0; debt < 1.5; wantKill++ {
		debt += increment
	}
	pose := sealPityPose(mob)
	items := shippedItems(t)
	forcedAt, forcedCount := 0, 0
	for kill := 1; kill <= wantKill; kill++ {
		drops, delta := rt.planMonsterKillLoot(killer, mob, pose, sealPityNoon.UnixMilli())
		rt.commitSealPity(killer, delta, sealPityNoon.UnixMilli())
		for _, drop := range drops {
			if !drop.SealPityForced {
				continue
			}
			ref, found := items.ItemRefByCodename(drop.Codename)
			if !found {
				t.Fatalf("forced drop %s is not a shipped item", drop.Codename)
			}
			if tier, sealed := enterworld.EquipmentSeal(ref); !sealed || tier != enterworld.SealSun || enterworld.EquipmentDegree(ref) != 9 {
				continue // a forced Moon is the other counter paying out
			}
			forcedAt, forcedCount = kill, forcedCount+1
			if drop.OwnerJID != enterworld.ObjectIDForCharacter(killer) {
				t.Fatalf("forced Sun owned by %d, want the killer %d", drop.OwnerJID, enterworld.ObjectIDForCharacter(killer))
			}
		}
	}
	if forcedCount != 1 || forcedAt != wantKill {
		t.Fatalf("forced Sun x%d at kill %d, want exactly one at kill %d (increment %g)", forcedCount, forcedAt, wantKill, increment)
	}
	if debt := sealPityDebtOf(killer, 9, enterworld.SealSun); debt != 0 {
		t.Fatalf("Sun debt after the forced drop is %g, want 0", debt)
	}
}

/*
================
TestSealPityForcedDropSurvivesCapAndKeepSeal

The guarantee is not a natural drop: with a debt standing past the
threshold, one kill pays the Sun even under a one-item drop cap and
the beta seal rates' keep-seal thinning, both of which the force
skips by construction.
================
*/
func TestSealPityForcedDropSurvivesCapAndKeepSeal(t *testing.T) {
	rt, killer, mob := sealPityRuntime(t, "on", 85)
	*killer.Level = 85
	rt.RareRate = 50
	rt.DropCap = 1
	t.Setenv(progression.EnvBetaSealRates, "on")
	rt.SealRates = progression.BetaSealRatesFromEnv()
	rt.DropRoll = constantDropRoll(0)
	setSealPityDebt(killer, 9, enterworld.SealSun, 99)
	items := shippedItems(t)
	drops, delta := rt.planMonsterKillLoot(killer, mob, sealPityPose(mob), sealPityNoon.UnixMilli())
	rt.commitSealPity(killer, delta, sealPityNoon.UnixMilli())
	forced := 0
	for _, drop := range drops {
		if !drop.SealPityForced {
			continue
		}
		if ref, found := items.ItemRefByCodename(drop.Codename); found {
			if tier, sealed := enterworld.EquipmentSeal(ref); sealed && tier == enterworld.SealSun && enterworld.EquipmentDegree(ref) == 9 {
				forced++
			}
		}
	}
	if forced != 1 {
		t.Fatalf("forced degree-9 Suns under cap 1 and seal rates: %d, want 1 (drops %v)", forced, dropSummary(drops))
	}
}

/*
================
TestSealPityTrashMonsterAccruesNothing

A level-90 killer on a level-1 monster is outside the native level-gap
admission, so every tracked tier's chance is worth zero: ten thousand
kills accrue nothing and force nothing.
================
*/
func TestSealPityTrashMonsterAccruesNothing(t *testing.T) {
	rt, killer, mob := sealPityRuntime(t, "on", 1)
	*killer.Level = 90
	if got := rt.admissionChance(uint8(*killer.Level), mob); got != 0 {
		t.Fatalf("admission chance %g, want 0 past the native gap", got)
	}
	kills := 10_000
	if testing.Short() {
		kills = 100
	}
	rt.DropRoll = constantDropRoll(0)
	pose := sealPityPose(mob)
	for kill := 0; kill < kills; kill++ {
		_, delta := rt.planMonsterKillLoot(killer, mob, pose, sealPityNoon.UnixMilli())
		rt.commitSealPity(killer, delta, sealPityNoon.UnixMilli())
	}
	if len(killer.SealPity) != 0 {
		t.Fatalf("trash kills left counters %+v", killer.SealPity)
	}
}

/*
================
TestSealPityNaturalDropResetsDebt

Only the character's own admitted drop of the tier resets its counter:
a natural degree-9 Sun clears the Sun debt and reads as a natural drop
in the rollup, while a kill without one keeps accruing.
================
*/
func TestSealPityNaturalDropResetsDebt(t *testing.T) {
	rt, killer, mob := sealPityRuntime(t, "on", 85)
	*killer.Level = 85
	rt.RareRate = 50
	items := shippedItems(t)
	ref, found := items.ItemRefByCodename("ITEM_CH_BLADE_09_C_RARE")
	if !found {
		t.Fatal("the degree-9 Sun fixture item is missing")
	}
	if tier, sealed := enterworld.EquipmentSeal(ref); !sealed || tier != enterworld.SealSun || enterworld.EquipmentDegree(ref) != 9 {
		t.Fatal("ITEM_CH_BLADE_09_C_RARE is not the degree-9 Sun fixture expected")
	}
	setSealPityDebt(killer, 9, enterworld.SealSun, 1.2)
	admitted := []grounditem.Item{{Codename: "ITEM_CH_BLADE_09_C_RARE"}}
	_, delta := rt.planSealPity(killer, mob, simulation.Spawn{}, admitted, sealPityNoon)
	rt.commitSealPity(killer, delta, sealPityNoon.UnixMilli())
	if debt := sealPityDebtOf(killer, 9, enterworld.SealSun); debt != 0 {
		t.Fatalf("Sun debt after its own natural drop is %g, want 0", debt)
	}
	rows := rt.drainSealPityStats()
	foundNatural := false
	for _, row := range rows {
		if row.Degree == 9 && row.Tier == enterworld.SealSun && row.NaturalDrops == 1 {
			foundNatural = true
		}
	}
	if !foundNatural {
		t.Fatalf("rollup %+v holds no natural degree-9 Sun", rows)
	}
	_, delta = rt.planSealPity(killer, mob, simulation.Spawn{}, nil, sealPityNoon)
	rt.commitSealPity(killer, delta, sealPityNoon.UnixMilli())
	if debt := sealPityDebtOf(killer, 9, enterworld.SealSun); debt <= 0 {
		t.Fatalf("Sun debt after a dry kill is %g, want accrual", debt)
	}
}

/*
================
TestSealPityDailyCap

One forced drop of a tier per character per UTC day: a debt that
reaches the threshold again the same day is refused (and counted as a
cap hit) with the debt standing; the next day pays.
================
*/
func TestSealPityDailyCap(t *testing.T) {
	rt, killer, mob := sealPityRuntime(t, "on", 85)
	*killer.Level = 85
	rt.RareRate = 50
	rt.DropRoll = constantDropRoll(0)
	pose := sealPityPose(mob)
	sunClass := sealPityItemClass(9, enterworld.SealSun)
	plan := func(at time.Time) []grounditem.Item {
		drops, delta := rt.planMonsterKillLoot(killer, mob, pose, at.UnixMilli())
		rt.commitSealPity(killer, delta, at.UnixMilli())
		return drops
	}
	forced := func(drops []grounditem.Item) int {
		n := 0
		for _, drop := range drops {
			if drop.SealPityForced {
				if ref, ok := shippedItems(t).ItemRefByCodename(drop.Codename); ok {
					if tier, sealed := enterworld.EquipmentSeal(ref); sealed && tier == enterworld.SealSun {
						n++
					}
				}
			}
		}
		return n
	}
	setSealPityDebt(killer, 9, enterworld.SealSun, 99)
	if n := forced(plan(sealPityNoon)); n != 1 {
		t.Fatalf("first reached threshold forced %d Suns, want 1", n)
	}
	setSealPityDebt(killer, 9, enterworld.SealSun, 99)
	if n := forced(plan(sealPityNoon.Add(2 * time.Hour))); n != 0 {
		t.Fatalf("same-day second threshold forced %d Suns, want the cap to refuse", n)
	}
	if debt := sealPityDebtOf(killer, 9, enterworld.SealSun); debt < 1.5 {
		t.Fatalf("capped debt is %g, want it standing at the threshold", debt)
	}
	rows := rt.drainSealPityStats()
	capHits := int64(0)
	for _, row := range rows {
		if row.Degree == 9 && row.Tier == enterworld.SealSun {
			capHits += row.DailyCapHits
		}
	}
	if capHits != 1 {
		t.Fatalf("rollup cap hits %d, want 1", capHits)
	}
	if n := forced(plan(sealPityNoon.AddDate(0, 0, 1))); n != 1 {
		t.Fatalf("next day forced %d Suns, want 1", n)
	}
	row, _ := sealPityRow(killer, sunClass)
	if row.ForcedDay != killStatDayOf(sealPityNoon.AddDate(0, 0, 1).UnixMilli()) || row.ForcedToday != 1 {
		t.Fatalf("forced-day counters %+v after the next day's drop", row)
	}
}

/*
================
TestSealPityGMExclusions

A GM-spawned monster's kill plans no counter movement at all, and the
GM kill path (settles with counting off) leaves a standing debt
untouched: conjured loot is not hunted.
================
*/
func TestSealPityGMExclusions(t *testing.T) {
	rt, killer, mob := sealPityRuntime(t, "on", 85)
	*killer.Level = 85
	rt.DropRoll = constantDropRoll(0)
	setSealPityDebt(killer, 9, enterworld.SealSun, 99)
	mob.GMSpawned = true
	drops, delta := rt.planMonsterKillLoot(killer, mob, sealPityPose(mob), sealPityNoon.UnixMilli())
	if len(delta.counters) != 0 || len(delta.notes) != 0 {
		t.Fatalf("GM-spawned kill produced a pity delta: %+v", delta)
	}
	for _, drop := range drops {
		if drop.SealPityForced {
			t.Fatalf("GM-spawned kill forced %s", drop.Codename)
		}
	}
	rt.commitSealPity(killer, delta, sealPityNoon.UnixMilli())
	if debt := sealPityDebtOf(killer, 9, enterworld.SealSun); debt != 99 {
		t.Fatalf("GM-spawned kill moved the Sun debt to %g, want 99 standing", debt)
	}
}

/*
================
TestSealPityCalibrationStar

Phase-A calibration: the planner's measured degree-9 Star frequency
must sit within 5% of the chance the counters accrue by (SealKillChance
times admission, here 1). The drop cap is lifted out of the way and
the rare rate is raised until the sample is affordable; the
composition reads the same settings, so the comparison stays honest.
Each cell targets about 3,600 expected Stars (5% is three standard
deviations), the cells run in parallel, and -short skips the sample.
================
*/
func TestSealPityCalibrationStar(t *testing.T) {
	if testing.Short() {
		t.Skip("calibration sample is too slow for -short")
	}
	items := shippedItems(t)
	for _, tc := range []struct {
		name   string
		level  uint8
		rarity uint8
	}{
		{"level 85 normal", 85, 0},
		{"level 85 champion", 85, 1},
		{"level 85 giant", 85, 4},
		{"level 85 party", 85, 0x10},
		{"level 88 normal", 88, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Mode off (the zero value): the native planner, measured.
			rt, _, killer, mob := newCombatTestRuntimeAtLevel(t, 100, tc.level)
			rt.deps.(*enterworld.Deps).Items = items
			level := int64(tc.level)
			killer.Level = &level
			rt.DropPassRate, rt.DropCap, rt.RareRate = 20, 100000, 200
			if tc.rarity != 0 {
				mob.Nest.HasRarityOverride = true
				mob.Nest.RarityOverride = tc.rarity
			}
			settings := rt.sealChanceSettings(mob)
			want := loot.SealKillChance(mob.Ref.Level, mob.Rarity(), mob.Ref.Codename, mob.Ref.Country, settings)[8][enterworld.SealStar]
			if want <= 0 {
				t.Fatalf("no degree-9 Star chance")
			}
			kills := int(3600 / want)
			source := rand.New(rand.NewPCG(1691, uint64(tc.level)*256+uint64(tc.rarity)))
			rt.DropRoll = func() (uint32, error) { return source.Uint32N(dropRollDomain), nil }
			pose := sealPityPose(mob)
			stars := 0
			for kill := 0; kill < kills; kill++ {
				drops, _ := rt.planMonsterKillLoot(killer, mob, pose, sealPityNoon.UnixMilli())
				for _, drop := range drops {
					if ref, found := items.ItemRefByCodename(drop.Codename); found {
						if tier, sealed := enterworld.EquipmentSeal(ref); sealed && tier == enterworld.SealStar && enterworld.EquipmentDegree(ref) == 9 {
							stars++
						}
					}
				}
			}
			ratio := float64(stars) / float64(kills) / want
			t.Logf("%d kills, %d Stars, ratio %.4f", kills, stars, ratio)
			if ratio < 0.95 || ratio > 1.05 {
				t.Fatalf("measured/want %.4f outside 5%%", ratio)
			}
		})
	}
}

/*
================
TestSealPitySettleCountsOnlyTheWinner

Through a real settlement the counted kill pays the owed drop into
the settlement and restarts the winner's counter; the GM settlement
shape (counting off) moves nobody's, forced drops included.
================
*/
func TestSealPitySettleCountsOnlyTheWinner(t *testing.T) {
	rt, clock, killer, mob := newCombatTestRuntimeAtLevel(t, 100, 85)
	*killer.Level = 85
	*killer.MaxLevel = 85
	rt.deps.(*enterworld.Deps).Items = shippedItems(t)
	t.Setenv(progression.EnvSealPity, "on")
	pity, err := progression.SealPityFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	rt.SealPity = pity
	rt.RareRate = 50
	rt.DropRoll = constantDropRoll(0)
	setSealPityDebt(killer, 9, enterworld.SealSun, 99)
	gid := enterworld.ObjectIDForCharacter(killer)
	impact := simulation.MonsterDamageResult{
		Fatal:    true,
		Instance: mob,
		Contributions: []simulation.MonsterContribution{
			{CreditGID: gid, Damage: mob.EffectiveMaxHP()},
		},
	}
	roster := rt.monsterRewardRoster(testDivision, killer, clock.NowMs())
	settlement := rt.settleMonsterInsideDoorCounted(testDivision, killer, roster, impact, monster.Pose{}, clock.NowMs(), true)
	forcedSeen := false
	for _, drop := range settlement.drops {
		if drop.SealPityForced {
			forcedSeen = true
		}
	}
	if !forcedSeen {
		t.Fatal("counted settlement paid no forced drop at a threshold debt")
	}
	if debt := sealPityDebtOf(killer, 9, enterworld.SealSun); debt != 0 {
		t.Fatalf("counted settlement left the winner's Sun debt at %g, want 0 after the forced drop", debt)
	}

	// Counting off (the GM kill path's shape): the debt is untouched and
	// no forced drop rides the settlement.
	setSealPityDebt(killer, 9, enterworld.SealSun, 99)
	settlement = rt.settleMonsterInsideDoorCounted(testDivision, killer, roster, impact, monster.Pose{}, clock.NowMs(), false)
	for _, drop := range settlement.drops {
		if drop.SealPityForced {
			t.Fatalf("uncounted settlement forced %s", drop.Codename)
		}
	}
	if debt := sealPityDebtOf(killer, 9, enterworld.SealSun); debt != 99 {
		t.Fatalf("uncounted settlement moved the Sun debt to %g, want 99 standing", debt)
	}
}
