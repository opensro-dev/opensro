/*
===========================================================================

petdiagnostics_test.go - an attack pet that does not fight says why

===========================================================================
*/

package action

import (
	"testing"
	"time"

	logtest "github.com/sirupsen/logrus/hooks/test"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
petFightReasons

The reasons logged since the hook was installed, in order.
================
*/
func petFightReasons(hook *logtest.Hook) []string {
	var reasons []string
	for _, entry := range hook.AllEntries() {
		if entry.Message == "action: attack pet did not fight" {
			reasons = append(reasons, entry.Data["reason"].(string))
		}
	}
	return reasons
}

/*
================
TestRefusedPetOrderIsLoggedOncePerInterval

An order on a live target the pet may not attack (here its own owner) is a
real failure: logged, once per quiet interval.
================
*/
func TestRefusedPetOrderIsLoggedOncePerInterval(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	rt, _, c, _ := newPetCombatRuntime(t, 100, attackPetBand)
	order := petAttackOrder(c.ActiveCOS.GID, enterworld.ObjectIDForCharacter(c))
	rt.HandleCosCommand(testDivision, c, order)
	rt.HandleCosCommand(testDivision, c, order)
	reasons := petFightReasons(hook)
	if len(reasons) != 1 || reasons[0] != "order refused: target not found" {
		t.Fatalf("logged %v, want one 'target not found' within the quiet interval", reasons)
	}
	if counters := rt.PetCombatCounters(); counters["order_refused.target_gone"] != 0 || counters["order_refused.target_died"] != 0 {
		t.Fatalf("a live target counted as ended: %v", counters)
	}
}

/*
================
TestPetOrderOnDepartedTargetIsCountedNotLogged

An order on a gid nothing holds any more (a despawned monster) is the normal
end of a fight: counted in the metrics, never logged.
================
*/
func TestPetOrderOnDepartedTargetIsCountedNotLogged(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	rt, _, c, m := newPetCombatRuntime(t, 100, attackPetBand)
	order := petAttackOrder(c.ActiveCOS.GID, m.Gid+999)
	rt.HandleCosCommand(testDivision, c, order)
	rt.HandleCosCommand(testDivision, c, order)
	if reasons := petFightReasons(hook); len(reasons) != 0 {
		t.Fatalf("logged %v for a departed target", reasons)
	}
	if got := rt.PetCombatCounters()["order_refused.target_gone"]; got != 2 {
		t.Fatalf("order_refused.target_gone = %d, want 2", got)
	}
}

/*
================
TestPetFightOnKilledTargetIsCountedNotLogged

A fight whose monster dies under it ends normally: counted, not logged.
================
*/
func TestPetFightOnKilledTargetIsCountedNotLogged(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	rt, clock, c, m := newPetCombatRuntime(t, 100000, attackPetBand)
	rt.HandleCosCommand(testDivision, c, petAttackOrder(c.ActiveCOS.GID, m.Gid))
	clock.now = clock.now.Add(100 * time.Millisecond)
	rt.TickHook()(clock.NowMs())
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	rt.Monsters.ApplyDamage(testDivision, m.Gid, live.CurrentHP)
	for range 5 {
		clock.now = clock.now.Add(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
	}
	if reasons := petFightReasons(hook); len(reasons) != 0 {
		t.Fatalf("logged %v for a killed target", reasons)
	}
	counters := rt.PetCombatCounters()
	if counters["fight_ended.target_died"]+counters["fight_ended.target_gone"] != 1 {
		t.Fatalf("fight end not counted once: %v", counters)
	}
}

/*
================
TestPetTargetEndedReadsLifeWithoutACopy

A player target's end is a life question, answered from the record's
life facts under the read door instead of a detached copy (#1943):
the same died verdict the copy gave, at a fraction of its cost.
================
*/
func TestPetTargetEndedReadsLifeWithoutACopy(t *testing.T) {
	rt, _, a, v := newPvpPair(t)
	target := enterworld.ObjectIDForCharacter(v)
	if got := rt.petTargetEnded(testDivision, a, target); got != "" {
		t.Fatalf("a living player target ended: %q", got)
	}
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, v) })
	endedAllocs := testing.AllocsPerRun(20, func() { rt.petTargetEnded(testDivision, a, target) })
	t.Logf("allocs: one character copy %.0f, target-end read %.0f", copyAllocs, endedAllocs)
	if endedAllocs >= copyAllocs {
		t.Fatalf("the target-end read still copies the target: %.0f allocs; one copy is %.0f",
			endedAllocs, copyAllocs)
	}
	v.CurrentHP = testInt64(0)
	if got := rt.petTargetEnded(testDivision, a, target); got != petTargetDied {
		t.Fatalf("a dead player target ended as %q, want %q", got, petTargetDied)
	}
}

/*
================
TestStalledPetApproachIsLogged

A pet that pursues for petApproachStallMs without reaching strike range is
reported once, with the distances that tell a blocked path from a bad goal.
================
*/
func TestStalledPetApproachIsLogged(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	rt, clock, c, m := newPetCombatRuntime(t, 100, attackPetBand)
	// Every segment stops where it starts: the pet can never close in.
	rt.ConstrainMovement = func(_ string, from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return from, nil }
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	state.follower = simulation.NewPetFollower(c.ActiveCOS.GID, simulation.Spawn{RegionID: live.Spawn.RegionID, X: live.Spawn.X - 60, Y: live.Spawn.Y, Z: live.Spawn.Z})
	rt.HandleCosCommand(testDivision, c, petAttackOrder(c.ActiveCOS.GID, m.Gid))
	for range 40 {
		clock.now = clock.now.Add(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
	}
	var stalled int
	for _, entry := range hook.AllEntries() {
		if entry.Data["reason"] == "approach not reaching strike range" {
			stalled++
			if entry.Data["distance"].(float64) <= entry.Data["admission"].(float64) {
				t.Fatalf("a stall reported inside strike range: %v", entry.Data)
			}
		}
	}
	if stalled != 1 {
		t.Fatalf("stall reported %d times in 4 s, want once (reasons: %v)", stalled, petFightReasons(hook))
	}
}
