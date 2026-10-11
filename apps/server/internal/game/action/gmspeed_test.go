/*
===========================================================================

gmspeed_test.go - a GM's /SETSPEED

Port-only (gmspeed.go). The flag off refuses it; on, a GM's own walk and
run speeds scale by the percent, the change is published, 100 resets it,
a non-GM is refused, and leaving the world forgets it.

===========================================================================
*/
package action

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestGMSetSpeedScalesTheGMsOwnSpeedUntilTheyLeave
================
*/
func TestGMSetSpeedScalesTheGMsOwnSpeedUntilTheyLeave(t *testing.T) {
	c := rebirthTestCharacter(1, 0)
	c.GMPrivilege = true
	rt, _ := newTestRuntime(c, testItems())
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }
	near := func(got float32, want float64) bool { return math.Abs(float64(got)-want) < 1e-3 }

	if rt.SetGMSpeed(testDivision, c.Name, 300) {
		t.Fatal("/SETSPEED ran with SRO_GM_SETSPEED off")
	}
	rt.GMSetSpeed = true
	for _, percent := range []uint16{0, GMSpeedMinPercent - 1, GMSpeedMaxPercent + 1} {
		if rt.SetGMSpeed(testDivision, c.Name, percent) {
			t.Fatalf("/SETSPEED accepted %d%%", percent)
		}
	}
	if !rt.SetGMSpeed(testDivision, c.Name, 300) {
		t.Fatal("/SETSPEED 300 refused")
	}
	walk, run := rt.EntryMovementSpeeds(testDivision, c.Name)
	if !near(walk, simulation.WalkSpeed*3) || !near(run, simulation.RunSpeed*3) {
		t.Fatalf("speeds %v/%v, want three times %v/%v", walk, run, simulation.WalkSpeed, simulation.RunSpeed)
	}
	published := false
	for _, frame := range pushed {
		published = published || frame.Opcode == movementSpeedOpcode
	}
	if !published {
		t.Fatal("the new speed was not published to the GM")
	}
	if !rt.SetGMSpeed(testDivision, c.Name, 100) {
		t.Fatal("/SETSPEED 100 refused")
	}
	if _, run = rt.EntryMovementSpeeds(testDivision, c.Name); !near(run, simulation.RunSpeed) {
		t.Fatalf("100%% left run speed %v, want %v", run, simulation.RunSpeed)
	}
	if !rt.SetGMSpeed(testDivision, c.Name, 200) {
		t.Fatal("/SETSPEED 200 refused")
	}
	rt.ForgetCharacter(testDivision, c.Name)
	if factor := rt.gmSpeedFactor(testDivision, c.Name); factor != 1 {
		t.Fatalf("leaving the world kept factor %v", factor)
	}
	c.GMPrivilege = false
	if rt.SetGMSpeed(testDivision, c.Name, 300) {
		t.Fatal("a non-GM changed their speed")
	}
}

/*
================
TestGMSetSpeedScalesATransformedGM

A transform replaces walk and run with the monster's speeds; /SETSPEED
applies after it, so a masked GM moves at the percent of the mask's speed.
================
*/
func TestGMSetSpeedScalesATransformedGM(t *testing.T) {
	rt, _, c, _ := maskFixture(t, 1, 0)
	c.GMPrivilege = true
	c.TransformMode, c.TransformRefObjID = 1, maskMonsterRef
	rt.GMSetSpeed = true
	if !rt.SetGMSpeed(testDivision, c.Name, 200) {
		t.Fatal("/SETSPEED 200 refused")
	}
	if walk, run := worldSpeeds(rt, c); walk != 2*maskWalk || run != 2*maskRun {
		t.Fatalf("masked GM speeds %v/%v, want %v/%v", walk, run, 2*maskWalk, 2*maskRun)
	}
}
