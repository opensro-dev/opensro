/*
===========================================================================

jobtransport_test.go - the faster job transports, on the shipped rows

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
TestJobTransportsMoveThirtyPercentFaster

Port-only, not native (#2474): applied, every job (trade) transport row
walks and runs 1.3 times its native characterdata speed, in the codename
map and the summonable list alike; the mall transports keep their own
speed (their 1.5 rule is separate) and every other row is native.
Unapplied (the flag off) every row is native - the speed the live job
transports run at today.
================
*/
func TestJobTransportsMoveThirtyPercentFaster(t *testing.T) {
	clientdata.RequireGameData(t)
	native := NewTextdataItems(gamedatatest.TextdataDir(t))
	native.once.Do(native.load)
	fast := NewTextdataItems(gamedatatest.TextdataDir(t))
	scaled, err := fast.ApplyJobTransportSpeed()
	if err != nil {
		t.Fatal(err)
	}
	if scaled == 0 {
		t.Fatal("no job transport rows scaled")
	}
	for codename, ref := range fast.charactersByCodename {
		base := native.charactersByCodename[codename]
		wantWalk, wantRun := base.WalkSpeed, base.RunSpeed
		if isJobTransportRef(ref) {
			wantWalk, wantRun = base.WalkSpeed*jobTransportSpeedup, base.RunSpeed*jobTransportSpeedup
		}
		if ref.WalkSpeed != wantWalk || ref.RunSpeed != wantRun {
			t.Fatalf("%s: %v/%v, want %v/%v", codename, ref.WalkSpeed, ref.RunSpeed, wantWalk, wantRun)
		}
	}
	if ref := fast.charactersByCodename["COS_T_DHORSE3"]; ref == nil || !isJobTransportRef(ref) ||
		ref.RunSpeed != native.charactersByCodename["COS_T_DHORSE3"].RunSpeed*jobTransportSpeedup {
		t.Fatal("the stable trade transport did not gain the job speed")
	}
	for _, ref := range fast.SummonableCharacterRefs() {
		base := native.charactersByCodename[ref.Codename]
		if isJobTransportRef(&ref) && ref.RunSpeed != base.RunSpeed*jobTransportSpeedup {
			t.Fatalf("summonable %s kept its native speed", ref.Codename)
		}
	}
}

/*
================
TestJobTransportSpeedFromEnv
================
*/
func TestJobTransportSpeedFromEnv(t *testing.T) {
	for env, want := range map[string]bool{"": false, "off": false, "on": true, "1": true} {
		t.Setenv(EnvJobTransportSpeed, env)
		if got := JobTransportSpeedFromEnv(); got != want {
			t.Errorf("%q: %v", env, got)
		}
	}
}
