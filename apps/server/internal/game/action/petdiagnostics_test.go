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
================
*/
func TestRefusedPetOrderIsLoggedOncePerInterval(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	// A target that does not exist (despawned, or never in this world).
	rt, _, c, m := newPetCombatRuntime(t, 100, attackPetBand)
	order := petAttackOrder(c.ActiveCOS.GID, m.Gid+999)
	rt.HandleCosCommand(testDivision, c, order)
	rt.HandleCosCommand(testDivision, c, order)
	reasons := petFightReasons(hook)
	if len(reasons) != 1 || reasons[0] != "order refused: target not found" {
		t.Fatalf("logged %v, want one 'target not found' within the quiet interval", reasons)
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
