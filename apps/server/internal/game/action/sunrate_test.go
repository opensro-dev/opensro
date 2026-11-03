/*
===========================================================================

sunrate_test.go - the Sun rate on the rare roll and the drop gate

Port-only, not native (progression/sunrate.go): while the rate is on, a
combined roll inside the widened degree-9 Sun class picks the Sun group
on a level in scope; with the rate off the same roll is the native one
and picks nothing there. The event-only exclusion still wins over the
rate: a Sun pick then drops nothing.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
)

// At level 76 the shipped rare row ends ...3654 (group 25), 3657
// (group 26, the degree-9 Sun class): rolls 3654-3656 pick Sun
// natively, and the m=60 row runs the Sun class to 3834. Roll 3700
// lands inside the widened class only.
const sunRateWitnessRoll = 3700

/*
================
sunRateRuntime
================
*/
func sunRateRuntime(t *testing.T) *Runtime {
	t.Helper()
	rt, _, _, _ := newCombatTestRuntime(t, 1)
	rt.deps.(*enterworld.Deps).Items = shippedItems(t)
	rt.SunRate = progression.SunRateFromEnv()
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	return rt
}

/*
================
TestSunRateWidensTheRareRoll
================
*/
func TestSunRateWidensTheRareRoll(t *testing.T) {
	clientdata.RequireGameData(t)
	for _, tc := range []struct {
		name    string
		sunRate string
		want    int
		wantOK  bool
	}{
		{"rate 2 (m=60)", "2", 26, true},
		{"off", "", 0, false},
	} {
		t.Setenv(progression.EnvSunDropRate, tc.sunRate)
		rt := sunRateRuntime(t)
		calls := 0
		rt.DropRoll = func() (uint32, error) {
			calls++
			if calls == 1 {
				return sunRateWitnessRoll, nil
			}
			return 0, nil
		}
		group, ok := rt.selectEquipmentGroup(76, true)
		if ok != tc.wantOK || (ok && group != tc.want) {
			t.Fatalf("%s: group %d ok %v, want %d/%v", tc.name, group, ok, tc.want, tc.wantOK)
		}
	}
}

/*
================
TestSunRateStillLosesToTheEventOnlyExclusion
================
*/
func TestSunRateStillLosesToTheEventOnlyExclusion(t *testing.T) {
	clientdata.RequireGameData(t)
	t.Setenv(progression.EnvSunDropRate, "2")
	t.Setenv(progression.EnvSunEventOnly, "on")
	rt := sunRateRuntime(t)
	rt.SunEventOnly = progression.SunEventOnlyFromEnv()
	_, dropped := rt.prepareSelectedDrop(loot.DropItem{Codename: "ITEM_CH_BLADE_09_C_RARE", Count: 1}, simulation.Spawn{RegionID: 0x5F76}, "owner", time.UnixMilli(0))
	if dropped {
		t.Fatal("event-only on must still drop nothing for a Sun pick while the rate is on")
	}
	rt.SunEventOnly = progression.SunEventOnly{}
	_, dropped = rt.prepareSelectedDrop(loot.DropItem{Codename: "ITEM_CH_BLADE_09_C_RARE", Count: 1}, simulation.Spawn{RegionID: 0x5F76}, "owner", time.UnixMilli(0))
	if !dropped {
		t.Fatal("event-only off must admit the Sun pick while the rate is on")
	}
}
