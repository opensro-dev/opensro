/*
===========================================================================

malltransport_test.go - the faster mall transports, on the shipped rows

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestMallTransportsMoveHalfAgainAsFast

Port-only, not native: applied, every mall transport row walks and runs
1.5 times its native characterdata speed, in the codename map and the
summonable list alike; a stable transport keeps its speed. Unapplied (the
flag off) every row is native.
================
*/
func TestMallTransportsMoveHalfAgainAsFast(t *testing.T) {
	clientdata.RequireGameData(t)
	native := NewTextdataItems(gamedatatest.TextdataDir(t))
	native.once.Do(native.load)
	fast := NewTextdataItems(gamedatatest.TextdataDir(t))
	scaled, err := fast.ApplyMallTransportSpeed()
	if err != nil {
		t.Fatal(err)
	}
	families := map[string]int{}
	for codename, ref := range fast.charactersByCodename {
		base := native.charactersByCodename[codename]
		mall := isMallTransport(ref)
		wantWalk, wantRun := base.WalkSpeed, base.RunSpeed
		if mall {
			wantWalk, wantRun = base.WalkSpeed*mallTransportSpeedup, base.RunSpeed*mallTransportSpeedup
			for _, family := range mallTransportFamilies {
				if codename == family || len(codename) > len(family) && codename[:len(family)+1] == family+"_" {
					families[family]++
				}
			}
		}
		if ref.WalkSpeed != wantWalk || ref.RunSpeed != wantRun {
			t.Fatalf("%s: %v/%v, want %v/%v", codename, ref.WalkSpeed, ref.RunSpeed, wantWalk, wantRun)
		}
	}
	if len(families) != len(mallTransportFamilies) || scaled == 0 {
		t.Fatalf("mall transport families in data %v, scaled %d", families, scaled)
	}
	if ref := fast.charactersByCodename["COS_T_DHORSE3"]; ref == nil || isMallTransport(ref) ||
		ref.RunSpeed != native.charactersByCodename["COS_T_DHORSE3"].RunSpeed {
		t.Fatal("a stable transport changed speed")
	}
	for _, ref := range fast.SummonableCharacterRefs() {
		base := native.charactersByCodename[ref.Codename]
		if isMallTransport(&ref) && ref.RunSpeed != base.RunSpeed*mallTransportSpeedup {
			t.Fatalf("summonable %s kept its native speed", ref.Codename)
		}
	}
}

/*
================
TestMallTransportSpeedFromEnv
================
*/
func TestMallTransportSpeedFromEnv(t *testing.T) {
	for env, want := range map[string]bool{"": false, "off": false, "on": true, "1": true} {
		t.Setenv(EnvMallTransportSpeed, env)
		if got := MallTransportSpeedFromEnv(); got != want {
			t.Errorf("%q: %v", env, got)
		}
	}
}
