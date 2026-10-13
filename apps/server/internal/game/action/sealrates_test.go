/*
===========================================================================

sealrates_test.go - tests for the beta seal rates in monsterloot.go

===========================================================================
*/

package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestSealRatesKeepEachSealAtItsMultiplier

Over every keep draw DropRoll can return below the rejection limit, a
chosen rare of each degree and seal stays rare exactly multiplier / 5 of the
time, read through the shipped item's ItemClass: degrees 1-8 three of five,
degree 9 Star three, Moon five (always), Sun one. With the rare window five
times as wide, each seal's chance is its multiplier times the old one.
================
*/
func TestSealRatesKeepEachSealAtItsMultiplier(t *testing.T) {
	licensed.RequireGameData(t)
	t.Setenv(progression.EnvBetaSealRates, "on")
	rt, _, _, _ := newCombatTestRuntime(t, 1)
	rt.deps.(*enterworld.Deps).Items = shippedItems(t)
	rt.SealRates = progression.BetaSealRatesFromEnv()
	if rt.SealRates.Widening() != 5 {
		t.Fatalf("widening %d, want 5", rt.SealRates.Widening())
	}
	want := func(degree int, seal string) int {
		if degree < 9 {
			return 3
		}
		return map[string]int{"A": 3, "B": 5, "C": 1}[seal]
	}
	limit := uint32(dropRollDomain / 5 * 5)
	for degree := 1; degree <= 9; degree++ {
		for _, seal := range []string{"A", "B", "C"} {
			codename := fmt.Sprintf("ITEM_CH_BLADE_%02d_%s_RARE", degree, seal)
			kept, draws := 0, 0
			for roll := uint32(0); roll < limit; roll++ {
				rt.DropRoll = func() (uint32, error) { draws++; return roll, nil }
				keep, ok := rt.keepSeal(codename)
				if !ok {
					t.Fatalf("%s: draw failed", codename)
				}
				if keep {
					kept++
				}
			}
			m := want(degree, seal)
			if kept*5 != int(limit)*m {
				t.Fatalf("%s kept %d of %d, want %d/5", codename, kept, limit, m)
			}
			if m == 5 && draws != 0 {
				t.Fatalf("%s: the largest multiplier keeps without a draw, drew %d", codename, draws)
			}
		}
	}
}

/*
================
TestSealRatesOffLeaveTheRareRollAsItWas

Off, the rare window is RareRate itself and a chosen rare is kept without
drawing, so the roll sequence is the one before the setting. On, the window
widens five times from at least the native one residue.
================
*/
func TestSealRatesOffLeaveTheRareRollAsItWas(t *testing.T) {
	rt, _, _, _ := newCombatTestRuntime(t, 1)
	for _, rate := range []int{0, 1, 5} {
		rt.RareRate = rate
		if got := rt.rareWindow(); got != rate {
			t.Fatalf("off: window %d at rare rate %d", got, rate)
		}
	}
	draws := 0
	rt.DropRoll = func() (uint32, error) { draws++; return 0, nil }
	if keep, ok := rt.keepSeal("ITEM_CH_BLADE_09_C_RARE"); !keep || !ok || draws != 0 {
		t.Fatalf("off: keep=%v ok=%v draws=%d", keep, ok, draws)
	}
	t.Setenv(progression.EnvBetaSealRates, "on")
	rt.SealRates = progression.BetaSealRatesFromEnv()
	for rate, want := range map[int]int{0: 5, 1: 5, 5: 25} {
		rt.RareRate = rate
		if got := rt.rareWindow(); got != want {
			t.Fatalf("on: window %d at rare rate %d, want %d", got, rate, want)
		}
	}
}
