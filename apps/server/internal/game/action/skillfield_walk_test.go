/*
===========================================================================

skillfield_walk_test.go - a field's scan reads walks at the tick's time

A buff field scans every character in its division on each pass and
reads each one's live position. The tick's prepare phase already
stepped every walk to the tick's start, so the scan must read that pose
and never step a walk again: one step runs the full ground clip and
water guard whatever its length, and on the live shard on 2026-11-24
the re-steps were 4.5 s of a 30 s profile at 152 players.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// fieldWalkers is how many walking players stand inside the field.
const fieldWalkers = 6

// fieldHookLagMs is how far into the tick the skill-object hook runs
// after prepare stepped the walks: later than prepare, same tick.
const fieldHookLagMs = 7

/*
================
TestFieldScanStepsNoWalkPrepareStepped

Walking players inside a live field: prepare steps each walk once at
the tick's start, then the field's pass, a few milliseconds later in
the same tick, admits them without a single further walk step.
================
*/
func TestFieldScanStepsNoWalkPrepareStepped(t *testing.T) {
	rt, clock, c, row := harmonyFixture(t)
	radius := float64(row.TimedEffect.Field.Radius)
	steps := 0
	rt.Worlds.ConfigureGroundWalk(simulation.GroundWalkConfig{
		Now: clock.NowMs,
		Step: func(from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavOwner, bool) {
			steps++
			return to, owner, false
		},
	})
	members := []uint32{enterworld.ObjectIDForCharacter(c)}
	var walkers []*enterworld.Character
	for i := 0; i < fieldWalkers; i++ {
		mate := nearbyCharacter(rt, c, int64(20+i), fmt.Sprintf("walker-%d", i), radius/4)
		members = append(members, enterworld.ObjectIDForCharacter(mate))
		walkers = append(walkers, mate)
	}
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: members}} }
	releaseHarmony(t, rt, clock, c, row)

	// Each walker crosses the field, +1/4 to -3/4 of the radius: the walk
	// stays inside it and lasts past every pass below.
	for _, w := range walkers {
		key := simulation.WorldKey(testDivision, w.Name)
		rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(w) }, func(world *simulation.WorldState) {
			from := world.Spawn
			goal := from
			goal.X -= radius
			world.Spawn = goal
			world.SpawnSet = true
			world.MovementMode = simulation.WalkMode
			world.MoveSegment = &simulation.MoveSegment{
				Ground: true, From: from, StartedAtMs: clock.NowMs(), ArrivesAtMs: clock.NowMs() + 60_000,
			}
		})
	}

	// The first pass admits every walker. Installing the buff settles
	// each walk before its speed changes (refreshMovementEffects), a
	// write paid once per admission; the scans after it are what every
	// tick pays.
	fieldTick(rt, clock, walkers, &steps)
	for _, w := range walkers {
		if !hasSkillEffect(rt, w.Name, row.ID) {
			t.Fatalf("%s inside the field was not admitted", w.Name)
		}
	}
	for pass := 1; pass <= 3; pass++ {
		before := steps
		prepared := fieldTick(rt, clock, walkers, &steps)
		if extra := steps - before - prepared; extra != 0 {
			t.Fatalf("pass %d: the field's scan stepped %d walk(s) prepare had already stepped", pass, extra)
		}
		if prepared == 0 {
			t.Fatalf("pass %d: prepare stepped no walk; the fixture's walkers stopped", pass)
		}
		for _, w := range walkers {
			if !hasSkillEffect(rt, w.Name, row.ID) {
				t.Fatalf("pass %d: %s left the field it stands in", pass, w.Name)
			}
		}
	}
}

/*
================
fieldTick

One tick of the field: the clock reaches the next scan, the prepare
phase steps each walker's walk at the tick's start (WorldStore.Snapshot
at the wall clock, as the ticker's prepare does), and the skill-object
hook runs a few milliseconds later at the tick's own time. Returns how
many walk steps prepare took, read from the step counter.
================
*/
func fieldTick(rt *Runtime, clock *fakeClock, walkers []*enterworld.Character, steps *int) int {
	clock.Advance(enterworld.SkillFieldScanMs * time.Millisecond)
	tickMs := clock.NowMs()
	before := *steps
	for _, w := range walkers {
		rt.Worlds.Snapshot(simulation.WorldKey(testDivision, w.Name), func() simulation.WorldState { return simulation.SeedWorldState(w) })
	}
	prepared := *steps - before
	clock.Advance(fieldHookLagMs * time.Millisecond)
	rt.AdvanceSkillObjects(tickMs, nil)
	return prepared
}
