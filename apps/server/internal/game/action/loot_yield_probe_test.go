/*
===========================================================================

loot_yield_probe_test.go - what a synthetic level-80 kill drops, per kill

A measurement, not a regression: issue #461 reports the yield before and
after the ISRO-R loot merge. Runs only with SRO_LOOT_YIELD_PROBE=1 and the
shipped item references, and logs gold heaps, equipment and other stacks per
kill at the native rate and at the beta rate. The fixture is a Mangnyang with
its level changed to 80, not a sample of all production level-80 monsters.

===========================================================================
*/

package action

import (
	"math/rand/v2"
	"os"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

// The probe's sample: kills per rate and the level of killer and monster.
const (
	yieldProbeKills     = 10000
	yieldProbeLevel     = 80
	yieldProbeSeed      = 461
	yieldProbeRollRange = 1 << 15
)

/*
================
TestLootYieldProbe

Kills one level-80 monster yieldProbeKills times through planMonsterKillLoot
with a seeded roll and the shipped item references, at rate 1 (native) and
20 (the beta default), and logs raw stack counts and per-kill yield.
Six decimal places keep a nonzero low-frequency result from rounding to zero.
================
*/
func TestLootYieldProbe(t *testing.T) {
	if os.Getenv("SRO_LOOT_YIELD_PROBE") != "1" {
		t.Skip("set SRO_LOOT_YIELD_PROBE=1 to measure loot yield")
	}
	items := shippedItems(t)
	for _, rate := range []int{1, 20} {
		rt, _, c, target := newCombatTestRuntimeAtLevel(t, 1, yieldProbeLevel)
		rt.deps.(*enterworld.Deps).Items = items
		level := int64(yieldProbeLevel)
		c.Level = &level
		source := rand.New(rand.NewPCG(yieldProbeSeed, uint64(rate)))
		// DropRoll has the same inclusive 0..32767 domain as combat.Roll32767.
		rt.DropRoll = func() (uint32, error) { return source.Uint32N(yieldProbeRollRange), nil }
		rt.DropPassRate = rate
		pose := monster.Pose{RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z}
		gold, equipment, other := 0, 0, 0
		for kill := 0; kill < yieldProbeKills; kill++ {
			for _, drop := range rt.planMonsterKillLoot(c, target, pose, rt.Now().UnixMilli()) {
				if drop.IsGold() {
					gold++
					continue
				}
				ref, found := items.ItemRefByCodename(drop.Codename)
				if !found || ref == nil {
					t.Fatalf("drop references unknown item %q", drop.Codename)
				}
				if ref.TypeIDs[1] == 1 {
					equipment++
				} else {
					other++
				}
			}
		}
		perKill := func(n int) float64 { return float64(n) / yieldProbeKills }
		t.Logf("rate %2d: %.6f gold heaps, %.6f equipment, %.6f other stacks per kill; totals=%d/%d/%d (%d kills, fixture=%s, level=%d, seed=%d/%d)",
			rate, perKill(gold), perKill(equipment), perKill(other), gold, equipment, other,
			yieldProbeKills, target.Ref.Codename, yieldProbeLevel, yieldProbeSeed, rate)
	}
}
