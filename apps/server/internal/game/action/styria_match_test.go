/*
===========================================================================

styria_match_test.go - Styria Clash: formation, entry, death, the end

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/styria"
)

/*
================
styriaStartApplied

Applies every character inside the participation window, then
ticks the engine across the start instant.
================
*/
func styriaStartApplied(t *testing.T, rt *Runtime, clock *fakeClock, characters ...*enterworld.Character) *styriaMatch {
	t.Helper()
	for _, c := range characters {
		rt.styriaApply(testDivision, c)
		if !rt.styriaRegistered(testDivision, c.Name) {
			t.Fatalf("%s holds no application", c.Name)
		}
	}
	rt.advanceStyriaClash(clock.NowMs()) // first tick: arms the edge watcher
	clock.now = time.Date(2026, 1, 5, 20, 0, 0, 0, time.UTC)
	rt.advanceStyriaClash(clock.NowMs())
	match := rt.styria.matches[testDivision]
	if match == nil {
		t.Fatal("no live match after the start")
	}
	return match
}

/*
================
styriaWorldOf
================
*/
func styriaWorldOf(c *enterworld.Character) instance.ID {
	return instance.ID(domain.CharacterWorldInstance(c))
}

/*
================
TestStyriaMatchFormsAndEntersAtTheGates

A Hunter and two Thieves apply: the smaller side fields one, so the
second Thief is turned away. The entered pair stands in world 107
at their own gates, on a return anchor that names where they stood.
================
*/
func TestStyriaMatchFormsAndEntersAtTheGates(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thiefA := styriaTestCharacter(2, "ThiefA", domain.JobThief)
	thiefB := styriaTestCharacter(3, "ThiefB", domain.JobThief)
	rt, clock := newStyriaRuntime(t, hunter, thiefA, thiefB)

	match := styriaStartApplied(t, rt, clock, hunter, thiefA, thiefB)

	if len(match.order) != 2 {
		t.Fatalf("entered %d participants, want the balanced 2", len(match.order))
	}
	if got := styriaWorldOf(hunter).Definition(); got != instance.DefinitionID(styria.WorldID) {
		t.Fatalf("hunter world definition = %d, want %d", got, styria.WorldID)
	}
	if got := styriaWorldOf(thiefA).Definition(); got != instance.DefinitionID(styria.WorldID) {
		t.Fatalf("thief world definition = %d, want %d", got, styria.WorldID)
	}
	if styriaWorldOf(thiefB) == match.lease.ID {
		t.Fatal("the turned-away thief entered the arena")
	}
	pose := rt.liveSpawn(simulation.WorldKey(testDivision, hunter.Name), hunter, clock.NowMs())
	if pose.RegionID != styria.HunterGate.RegionID {
		t.Fatalf("hunter landed in region %d, want the hunter gate's %d", pose.RegionID, styria.HunterGate.RegionID)
	}
	pose = rt.liveSpawn(simulation.WorldKey(testDivision, thiefA.Name), thiefA, clock.NowMs())
	if pose.RegionID != styria.ThiefGate.RegionID {
		t.Fatalf("thief landed in region %d, want the thief gate's %d", pose.RegionID, styria.ThiefGate.RegionID)
	}
	if hunter.World == nil || hunter.World.InstanceReturn == nil {
		t.Fatal("the hunter holds no return anchor")
	}
	if anchor := hunter.World.InstanceReturn; anchor.RegionID != instanceWorldTestStart.RegionID {
		t.Fatalf("anchor region = %d, want the entry pose's %d", anchor.RegionID, instanceWorldTestStart.RegionID)
	}
	// The window consumed every application, the turned-away included.
	if rt.styriaRegistered(testDivision, thiefB.Name) {
		t.Fatal("the turned-away thief still holds an application")
	}
}

/*
================
TestStyriaMatchNeedsBothSides
================
*/
func TestStyriaMatchNeedsBothSides(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	rt, clock := newStyriaRuntime(t, hunter)

	rt.styriaApply(testDivision, hunter)
	rt.advanceStyriaClash(clock.NowMs())
	clock.now = time.Date(2026, 1, 5, 20, 0, 0, 0, time.UTC)
	rt.advanceStyriaClash(clock.NowMs())

	if match := rt.styria.matches[testDivision]; match != nil {
		t.Fatal("a one-sided house started a match")
	}
	if rt.styriaRegistered(testDivision, hunter.Name) {
		t.Fatal("the application outlived its window")
	}
}

/*
================
TestStyriaDeathReturnsAndDropsTheSeat

Death inside returns the fallen to their entry anchor at once and
ends their seat; the survivor fights on until the clock.
================
*/
func TestStyriaDeathReturnsAndDropsTheSeat(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(2, "Thief", domain.JobThief)
	rt, clock := newStyriaRuntime(t, hunter, thief)
	match := styriaStartApplied(t, rt, clock, hunter, thief)

	*hunter.CurrentHP = 0
	rt.advanceStyriaMatches(clock.NowMs())

	if got := styriaWorldOf(hunter); got.Definition() == instance.DefinitionID(styria.WorldID) {
		t.Fatalf("the dead hunter is still in the arena world (%d)", got)
	}
	if hunter.World != nil && hunter.World.InstanceReturn != nil {
		t.Fatal("the returned hunter still holds an anchor")
	}
	match.mu.Lock()
	hunterSeat := match.participants[hunter.ID].entered
	thiefSeat := match.participants[thief.ID].entered
	match.mu.Unlock()
	if hunterSeat {
		t.Fatal("the dead hunter still holds a seat")
	}
	if !thiefSeat {
		t.Fatal("the survivor lost their seat to the other's death")
	}
}

/*
================
TestStyriaMatchEndReturnsEveryone

The clock ends the match: after the close grace the world closes,
both entrants stand back where they entered from, and the layer is
released.
================
*/
func TestStyriaMatchEndReturnsEveryone(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(2, "Thief", domain.JobThief)
	rt, clock := newStyriaRuntime(t, hunter, thief)
	match := styriaStartApplied(t, rt, clock, hunter, thief)
	lease := match.lease

	clock.now = time.UnixMilli(match.endsMs)
	rt.advanceStyriaMatches(clock.NowMs())
	if got := rt.styria.matches[testDivision]; got != match {
		t.Fatal("the match book dropped before the close")
	}
	clock.now = time.UnixMilli(match.endsMs + styriaCloseAfterEndMs)
	rt.advanceStyriaMatches(clock.NowMs())

	if got := rt.styria.matches[testDivision]; got != nil {
		t.Fatal("the match outlived its close")
	}
	for _, c := range []*enterworld.Character{hunter, thief} {
		if got := styriaWorldOf(c); got.Definition() == instance.DefinitionID(styria.WorldID) {
			t.Fatalf("%s is still in the arena world after the close", c.Name)
		}
		if c.World != nil && c.World.InstanceReturn != nil {
			t.Fatalf("%s still holds an anchor after the close", c.Name)
		}
		pose := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
		if pose.RegionID != instanceWorldTestStart.RegionID {
			t.Fatalf("%s returned to region %d, want the entry region %d", c.Name, pose.RegionID, instanceWorldTestStart.RegionID)
		}
	}
	if _, open := rt.Monsters.PopulationLease(testDivision, lease.ID); open {
		t.Fatal("the arena layer was never released")
	}
}
