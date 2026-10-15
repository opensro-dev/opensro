/*
===========================================================================

mallride_test.go - the faster mall vehicles and pets, on the shipped rows

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestMallRidesMoveThirtyPercentFaster

Port-only, not native: applied, every row of the mall's vehicle and pet
families walks and runs 1.3 times its native speed, in the codename map
and the summonable list; every family is in the data; stable horses,
NPC pets and the mall transports keep their speeds. Unapplied (the flag
off) every row is native.
================
*/
func TestMallRidesMoveThirtyPercentFaster(t *testing.T) {
	licensed.RequireGameData(t)
	native := NewTextdataItems(gamedatatest.TextdataDir(t))
	native.once.Do(native.load)
	fast := NewTextdataItems(gamedatatest.TextdataDir(t))
	scaled, err := fast.ApplyMallRideSpeed()
	if err != nil {
		t.Fatal(err)
	}
	families := map[string]int{}
	for codename, ref := range fast.charactersByCodename {
		base := native.charactersByCodename[codename]
		wantWalk, wantRun := base.WalkSpeed, base.RunSpeed
		if isMallRide(ref) {
			wantWalk, wantRun = base.WalkSpeed*mallRideSpeedup, base.RunSpeed*mallRideSpeedup
			for _, family := range mallRideFamilies {
				if mallRideFamilyRow(codename, family) {
					families[family]++
				}
			}
		}
		if ref.WalkSpeed != wantWalk || ref.RunSpeed != wantRun {
			t.Fatalf("%s: %v/%v, want %v/%v", codename, ref.WalkSpeed, ref.RunSpeed, wantWalk, wantRun)
		}
	}
	if len(families) != len(mallRideFamilies) || scaled == 0 {
		t.Fatalf("mall ride families in data %v (want all %d), scaled %d", families, len(mallRideFamilies), scaled)
	}
	for _, codename := range []string{"COS_C_HORSE1", "COS_P_WOLF", "COS_T_BUFFALO", "COS_P_WOLF_WHITE_SMALL_001"} {
		ref := fast.charactersByCodename[codename]
		if ref == nil {
			continue
		}
		if isMallRide(ref) || ref.RunSpeed != native.charactersByCodename[codename].RunSpeed {
			t.Fatalf("%s is not a mall vehicle or pet but changed speed", codename)
		}
	}
	for _, ref := range fast.SummonableCharacterRefs() {
		base := native.charactersByCodename[ref.Codename]
		if isMallRide(&ref) && ref.RunSpeed != base.RunSpeed*mallRideSpeedup {
			t.Fatalf("summonable %s kept its native speed", ref.Codename)
		}
	}
}

/*
================
TestMallRideSpeedFromEnv
================
*/
func TestMallRideSpeedFromEnv(t *testing.T) {
	for env, want := range map[string]bool{"": false, "off": false, "on": true, "1": true} {
		t.Setenv(EnvMallRideSpeed, env)
		if got := MallRideSpeedFromEnv(); got != want {
			t.Errorf("%q: %v", env, got)
		}
	}
}
