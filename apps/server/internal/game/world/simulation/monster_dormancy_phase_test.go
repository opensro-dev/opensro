/*
===========================================================================

monster_dormancy_phase_test.go - phase-continuous dormancy wake (#1820 A)

The dormancy wake used to make a whole sector ring due on the wake tick.
A woken actor now resumes on its own native activity phase: never on the
wake tick, always within one cadence period, and a woken cohort spreads
across the period instead of landing together.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

// dormancyPhaseWorld seeds `actors` dormant pending actors in one region
// of a fresh dormancy-enabled division, with cadence phases spread over
// the 1900 ms interval the way spawn-time clocks spread them.
func dormancyPhaseWorld(t *testing.T, actors int) (*MonsterState, *divisionMonsterState) {
	t.Helper()
	s := NewMonsterState(monster.Template{})
	s.EnableRegionDormancy()
	if err := s.EnableDormantStorage(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.StartDivision("sleep")
	d := s.divs["sleep"]
	d.movers = newMoverStorage(nil)
	s.prepareDormancy(100000, "sleep", nil)
	for i := 0; i < actors; i++ {
		gid := uint32(1000 + i)
		actor := monster.Instance{Gid: gid, Ref: monster.MonsterRef{MaxHP: 100}, Nest: monster.NestRow{Respawn: true}, CurrentHP: 100}
		mover := monster.PendingMover{
			Pose:     monster.Pose{RegionID: 0x6060, X: float64(i), Y: 45, Z: 678},
			Activity: monster.ActivityCadence{Interval: 1900, LastCheck: 90000 - uint32(i%19)*100},
		}.Expand()
		d.instances.set(gid, actor)
		d.movers.set(gid, mover)
		s.scheduleBehavior(d, gid, 100000)
	}
	if len(d.dormant) != actors {
		t.Fatalf("seeded %d dormant actors, want %d", len(d.dormant), actors)
	}
	return s, d
}

// wakeDormancyPhaseWorld approaches the seeded region with one session
// and returns each actor's first dispatch tick over the next 21 ticks.
func wakeDormancyPhaseWorld(t *testing.T, s *MonsterState, d *divisionMonsterState, actors int) []int64 {
	t.Helper()
	session := SessionSnapshot{DivisionID: "sleep", Population: d.lease}
	session.World.Spawn.RegionID = 0x6061 // next sector, outside visible range
	s.prepareDormancy(200000, "sleep", []SessionSnapshot{session})
	if len(d.dormant) != 0 || d.behavior.Len() != actors {
		t.Fatalf("wake left dormant=%d scheduled=%d, want 0/%d", len(d.dormant), d.behavior.Len(), actors)
	}
	if at, _ := d.behavior.earliestDue(); at <= 200000 {
		t.Fatalf("earliest woken actor due at %d, on or before the wake tick", at)
	}
	first := make(map[uint32]int64)
	for tick := int64(200000); tick <= 202100; tick += 100 {
		for _, batch := range s.behaviorBatchesForDivision(tick, "sleep") {
			for _, ref := range batch.actors {
				if _, seen := first[ref.gid]; !seen {
					first[ref.gid] = tick
				}
			}
		}
	}
	if len(first) != actors {
		t.Fatalf("resumed %d of %d woken actors within one cadence period", len(first), actors)
	}
	out := make([]int64, 0, actors)
	for gid := uint32(1000); gid < uint32(1000+actors); gid++ {
		out = append(out, first[gid])
	}
	return out
}

func TestDormancyWakeSpreadsResumeAcrossCadencePhase(t *testing.T) {
	const actors = 40
	s, d := dormancyPhaseWorld(t, actors)
	first := wakeDormancyPhaseWorld(t, s, d, actors)

	perTick := make(map[int64]int)
	for _, tick := range first {
		if tick <= 200000 {
			t.Fatalf("actor resumed on the wake tick %d", tick)
		}
		if tick > 200000+1901 {
			t.Fatalf("actor resumed at %d, past one cadence period", tick)
		}
		perTick[tick]++
	}
	// 40 actors over a 1901 ms period at a 100 ms cadence: no tick may
	// carry the cohort; about a tenth per tick is the spread.
	for tick, n := range perTick {
		if n > actors/10+2 {
			t.Fatalf("tick %d resumed %d actors, want the cohort spread (max ~%d)", tick, n, actors/10)
		}
	}

	// The wake is deterministic: a fresh world resumes identically.
	s2, d2 := dormancyPhaseWorld(t, actors)
	second := wakeDormancyPhaseWorld(t, s2, d2, actors)
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("resume ticks diverged between runs at actor %d: %d vs %d", i, first[i], second[i])
		}
	}
}

func TestDormancyWakeMatchesNeverSleptTwin(t *testing.T) {
	// Slept twin: cadence {1500, 90000}, woken at 200000.
	s, d := dormancyPhaseWorld(t, 1)
	actor := monster.Instance{Gid: 42, Ref: monster.MonsterRef{MaxHP: 100}, Nest: monster.NestRow{Respawn: true}, CurrentHP: 100}
	mover := monster.PendingMover{
		Pose:     monster.Pose{RegionID: 0x6060, X: 1, Y: 45, Z: 678},
		Activity: monster.ActivityCadence{Interval: 1500, LastCheck: 90000},
	}.Expand()
	d.instances.set(actor.Gid, actor)
	d.movers.set(actor.Gid, mover)
	s.scheduleBehavior(d, actor.Gid, 100000)
	session := SessionSnapshot{DivisionID: "sleep", Population: d.lease}
	session.World.Spawn.RegionID = 0x6061
	s.prepareDormancy(200000, "sleep", []SessionSnapshot{session})
	sleptAt := int64(-1)
	if slot, ok := d.behavior.byGID[actor.Gid]; ok {
		sleptAt = d.behavior.slots[slot].at
	}

	// Never-slept twin: the same cadence stepped through the sleep window
	// (its LastCheck advanced by its own fires to 199573), scheduled at
	// the same tick by the same scheduler.
	s2, d2 := dormancyPhaseWorld(t, 0)
	twin := monster.PendingMover{
		Pose:     monster.Pose{RegionID: 0x6060, X: 1, Y: 45, Z: 678},
		Activity: monster.ActivityCadence{Interval: 1500, LastCheck: 199573},
	}.Expand()
	d2.instances.set(actor.Gid, actor)
	d2.movers.set(actor.Gid, twin)
	// The twin stands in a watched region (the sleeper's wake grace), so
	// the scheduler admits it instead of suspending it.
	d2.keepAwakeUntil[0x6060] = 200000 + regionSleepGraceMs
	s2.scheduleBehavior(d2, actor.Gid, 200000)
	twinAt := int64(-1)
	if slot, ok := d2.behavior.byGID[actor.Gid]; ok {
		twinAt = d2.behavior.slots[slot].at
	}
	if sleptAt != 201074 || twinAt != sleptAt {
		t.Fatalf("slept due %d, never-slept twin due %d, want both 201074", sleptAt, twinAt)
	}

	// The cadence the leg dispatches on fires exactly at that due tick:
	// the first dispatch after resume runs the activity check (the
	// acquisition gate), never before it.
	cadence := monster.ActivityCadence{Interval: 1500, LastCheck: 199573}
	if cadence.Due(201073) {
		t.Fatal("cadence due before the resumed dispatch tick")
	}
	if !cadence.Due(201074) {
		t.Fatal("cadence not due at the resumed dispatch tick")
	}
}
