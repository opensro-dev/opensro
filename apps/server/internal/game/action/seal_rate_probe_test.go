/*
===========================================================================

seal_rate_probe_test.go - rare seals per kill, before and after the seal rates

A measurement, not a regression: the beta seal rates' PR reports per-kill
odds of each degree and seal for a level 85-90 monster. Runs only with
SRO_SEAL_RATE_PROBE=1 and the shipped item references. Each run kills the
fixture (a Mangnyang with its level changed) sealProbeKills times through
planMonsterKillLoot at the beta drop settings, with and without the seal
rates, from the same seed.

===========================================================================
*/

package action

import (
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
)

// The probe's sample and the beta drop settings it runs at (growth.go).
const (
	sealProbeKills    = 2_000_000
	sealProbeSeed     = 1013
	sealProbeDropRate = 20
	sealProbeDropCap  = 16
	sealProbeRareRate = 5
)

/*
================
TestSealRateProbe
================
*/
func TestSealRateProbe(t *testing.T) {
	if os.Getenv("SRO_SEAL_RATE_PROBE") != "1" {
		t.Skip("set SRO_SEAL_RATE_PROBE=1 to measure seal odds")
	}
	items := shippedItems(t)
	for _, level := range []uint8{80, 85, 88, 90} {
		counts := map[string][2]int{}
		for run, setting := range []string{"off", "on"} {
			t.Setenv(progression.EnvBetaSealRates, setting)
			rt, _, c, target := newCombatTestRuntimeAtLevel(t, 1, level)
			rt.deps.(*enterworld.Deps).Items = items
			killer := int64(level)
			c.Level = &killer
			rt.DropPassRate, rt.DropCap, rt.RareRate = sealProbeDropRate, sealProbeDropCap, sealProbeRareRate
			rt.SealRates = progression.BetaSealRatesFromEnv()
			source := rand.New(rand.NewPCG(sealProbeSeed, uint64(level)))
			rt.DropRoll = func() (uint32, error) { return source.Uint32N(dropRollDomain), nil }
			pose := monster.Pose{RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z}
			for kill := 0; kill < sealProbeKills; kill++ {
				drops, _ := rt.planMonsterKillLoot(c, target, pose, rt.Now().UnixMilli())
				for _, drop := range drops {
					if !strings.HasSuffix(drop.Codename, "_RARE") {
						continue
					}
					ref, _ := items.ItemRefByCodename(drop.Codename)
					class, _ := ref.NativeFields.Lookup("itemClass")
					key := fmt.Sprintf("d%d %s", (int(class)+2)/3, []string{"Star", "Moon", "Sun"}[(int(class)-1)%3])
					row := counts[key]
					row[run]++
					counts[key] = row
				}
			}
		}
		keys := make([]string, 0, len(counts))
		for key := range counts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			row := counts[key]
			t.Logf("level %d %-9s off 1 in %8.0f (%7d)  on 1 in %8.0f (%7d)  ratio %.2f", level, key,
				oneIn(row[0]), row[0], oneIn(row[1]), row[1], float64(row[1])/float64(max(row[0], 1)))
		}
	}
}

/*
================
oneIn

Kills per drop over the probe, or 0 when none dropped.
================
*/
func oneIn(n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(sealProbeKills) / float64(n)
}
