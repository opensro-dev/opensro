/*
===========================================================================

petai_detour_test.go - the pet tick routes a blocked pet

A pet whose straight follow is blocked asks the runtime's companion route
planner (the monster AI's detour in production) for a way around.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestBlockedPetAsksTheCompanionRoutePlanner
================
*/
func TestBlockedPetAsksTheCompanionRoutePlanner(t *testing.T) {
	c := testCharacter()
	refs := testCosSource(testItems())
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 80}
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true}
	rt.CompanionSurfaceHeight = func(_ uint16, _ float64, y float64, _ float64) (float64, bool) { return y, true }
	// A wall right in front of the pet: every segment stops where it starts.
	rt.ConstrainMovement = func(_ string, from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return from, nil }
	plans := 0
	rt.PlanCompanionRoute = func(from, goal simulation.Spawn) []simulation.Spawn {
		plans++
		return nil
	}
	rt.BindPetSession(testDivision, c, 101)
	rt.TickHook()(1000)
	key := simulation.WorldKey(testDivision, c.Name)
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn.X += 200 })
	rt.TickHook()(1100)
	if plans == 0 {
		t.Fatal("a pet blocked from its owner never asked the companion route planner")
	}
}
