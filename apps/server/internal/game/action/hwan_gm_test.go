/*
===========================================================================

hwan_gm_test.go - /SETBERSERK and /SETGAUGE

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

func TestGMSetBerserkChangesTheLevelAndTellsTheTarget(t *testing.T) {
	gm, target := testCharacter(), testCharacter()
	gm.ID, gm.Name, gm.GMPrivilege = 1, "Gm", true
	target.ID, target.Name = 2, "Ayla"
	rt, _ := newTestRuntime(gm, nil)
	rt.deps.(*enterworld.Deps).Characters = enterworld.StaticCharacterSource{testDivision: {gm, target}}
	levels := map[int64]uint8{}
	rt.HwanLevel = func(c *enterworld.Character) uint8 { return levels[c.ID] }
	rt.SetHwanLevel = func(c *enterworld.Character, level uint8) bool { levels[c.ID] = level; return true }
	pushed := map[string][]wire.Frame{}
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) { pushed[name] = append(pushed[name], frames...) }

	if !rt.SetGMBerserkLevel(testDivision, "Gm", "Ayla", 5) || levels[2] != 5 {
		t.Fatalf("level %d after /SETBERSERK Ayla 5", levels[2])
	}
	got := pushed["Ayla"]
	if len(got) != 2 || got[0].Opcode != OpHwanLevel || got[1].Opcode != 0x36bf {
		t.Fatalf("Ayla was told %+v, want the level frame and its title", got)
	}
	// Level 0 clears it and sends the level frame alone.
	pushed = map[string][]wire.Frame{}
	if !rt.SetGMBerserkLevel(testDivision, "Gm", "Ayla", 0) || levels[2] != 0 || len(pushed["Ayla"]) != 1 {
		t.Fatalf("level 0: levels %v pushed %v", levels, pushed)
	}
	// Too high, an unknown name and a non-GM change nothing.
	if rt.SetGMBerserkLevel(testDivision, "Gm", "Ayla", 7) || rt.SetGMBerserkLevel(testDivision, "Gm", "Nobody", 2) {
		t.Fatal("an invalid level or an unknown character was accepted")
	}
	gm.GMPrivilege = false
	if rt.SetGMBerserkLevel(testDivision, "Gm", "Ayla", 2) || levels[2] != 0 {
		t.Fatal("a non-GM set a level")
	}
}

func TestGMSetGaugeSetsTheBallsAndTellsTheTarget(t *testing.T) {
	gm, target := testCharacter(), testCharacter()
	gm.ID, gm.Name, gm.GMPrivilege = 1, "Gm", true
	target.ID, target.Name = 2, "Ayla"
	rt, _ := newTestRuntime(gm, nil)
	rt.deps.(*enterworld.Deps).Characters = enterworld.StaticCharacterSource{testDivision: {gm, target}}
	pushed := map[string][]wire.Frame{}
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) { pushed[name] = append(pushed[name], frames...) }

	if !rt.SetGMBerserkGauge(testDivision, "Gm", "Ayla", 5) || target.BerserkPoints != 5 || len(pushed["Ayla"]) != 1 {
		t.Fatalf("balls %d pushed %v", target.BerserkPoints, pushed)
	}
	if !rt.SetGMBerserkGauge(testDivision, "Gm", "Ayla", 2) || target.BerserkPoints != 2 {
		t.Fatalf("lowering to 2 left %d", target.BerserkPoints)
	}
	if rt.SetGMBerserkGauge(testDivision, "Gm", "Ayla", 6) || rt.SetGMBerserkGauge(testDivision, "Gm", "Nobody", 1) {
		t.Fatal("an invalid count or an unknown character was accepted")
	}
	// A gauge cannot be raised while Berserk is active, as native.
	target.NativeBodyStatus = 1
	if rt.SetGMBerserkGauge(testDivision, "Gm", "Ayla", 5) || target.BerserkPoints != 2 {
		t.Fatal("the gauge was raised during Berserk")
	}
	target.NativeBodyStatus = 0
	gm.GMPrivilege = false
	if rt.SetGMBerserkGauge(testDivision, "Gm", "Ayla", 0) || target.BerserkPoints != 2 {
		t.Fatal("a non-GM set the gauge")
	}
}
