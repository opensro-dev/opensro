/*
===========================================================================

petai_test.go - independent COS motion and session retirement

===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPetSessionDrivesIndependentMotionAndCleansUp
================
*/
func TestPetSessionDrivesIndependentMotionAndCleansUp(t *testing.T) {
	c := testCharacter()
	refs := testCosSource(testItems())
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 80}
	rt, _ := newTestRuntime(c, refs)
	// The spawn sampler places the pet around its owner. About one secure
	// sample in sixteen (24056..26675) spawns it where the 1100 ms follow
	// leaves it short, so it legitimately moves again at 1200 ms; pin the
	// sample so the duplicate bind below is the only thing under test.
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true}
	rt.CompanionSurfaceHeight = func(_ uint16, _ float64, y float64, _ float64) (float64, bool) { return y, true }
	rt.ConstrainMovement = func(_ string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	if f := rt.TickHook()(1000); len(f) != 0 {
		t.Fatal("offline pet ticked")
	}
	rt.BindPetSession(testDivision, c, 101)
	if f := rt.TickHook()(1000); len(f) != 0 {
		t.Fatal("new pet moved")
	}
	key := simulation.WorldKey(testDivision, c.Name)
	world := rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn.X += 200 })
	before := c.Snapshot()
	if f := rt.TickHook()(1100); len(f) != 1 || len(f[0].Frames) != 3 || f[0].OnlyCharacterID != c.ID {
		t.Fatal("live pet did not follow", f)
	}
	before.ActiveCOS.FollowRunFactor, before.ActiveCOS.FollowRunSet = c.ActiveCOS.FollowRunFactor, c.ActiveCOS.FollowRunSet
	if after := rt.Worlds.Snapshot(key, func() simulation.WorldState { return world }); !reflect.DeepEqual(world, after) || !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("pet moved player or mutated durable state")
	}
	presentation := rt.PetPresentation(testDivision, c.Name)
	if presentation == nil || presentation.Row.Gid != gid || presentation.World.MoveSegment == nil {
		t.Fatal("missing live public pet snapshot")
	}
	presentation.World.MoveSegment.From.X += 999
	if reflect.DeepEqual(presentation.World, rt.PetPresentation(testDivision, c.Name).World) {
		t.Fatal("public snapshot aliases pet motion")
	}
	rt.BindPetSession(testDivision, c, 101)
	if f := rt.TickHook()(1200); len(f) != 0 {
		t.Fatal("duplicate session reset motion", f)
	}
	c.ActiveCOS.CurrentHP = 0
	if f := rt.TickHook()(1300); len(f) != 1 || len(f[0].Frames) != 1 {
		t.Fatal("death failed to stop pet", f)
	}
	if f := rt.TickHook()(1400); len(f) != 0 {
		t.Fatal("dead pet kept updating")
	}
	rt.ForgetCharacter(testDivision, c.Name)
	if len(rt.petSessions) != 0 {
		t.Fatal("session retained AI owner")
	}
}
