/*
===========================================================================

mallride_test.go - the faster mall vehicles and pets, on the shipped rows

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// mallRideTestFactor is the registry's default factor, the value "on" means.
const mallRideTestFactor = 1.3

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
	clientdata.RequireGameData(t)
	native := NewTextdataItems(gamedatatest.TextdataDir(t))
	native.once.Do(native.load)
	fast := NewTextdataItems(gamedatatest.TextdataDir(t))
	scaled, err := fast.ApplyMallRideSpeed(mallRideTestFactor)
	if err != nil {
		t.Fatal(err)
	}
	families := map[string]int{}
	for codename, ref := range fast.charactersByCodename {
		base := native.charactersByCodename[codename]
		wantWalk, wantRun := base.WalkSpeed, base.RunSpeed
		if isMallRide(ref) {
			wantWalk, wantRun = base.WalkSpeed*mallRideTestFactor, base.RunSpeed*mallRideTestFactor
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
		if isMallRide(&ref) && ref.RunSpeed != base.RunSpeed*mallRideTestFactor {
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
	for env, want := range map[string]struct {
		factor float64
		on     bool
	}{"": {1, false}, "off": {1, false}, "on": {1.3, true}, "1": {1.3, true}, "1.5": {1.5, true}} {
		t.Setenv(EnvMallRideSpeed, env)
		if factor, on, err := MallRideSpeedFromEnv(); err != nil || factor != want.factor || on != want.on {
			t.Errorf("%q: %v %v %v, want %v %v", env, factor, on, err, want.factor, want.on)
		}
	}
	t.Setenv(EnvMallRideSpeed, "9")
	if _, _, err := MallRideSpeedFromEnv(); err == nil {
		t.Fatal("a factor past 3 was accepted")
	}
}
