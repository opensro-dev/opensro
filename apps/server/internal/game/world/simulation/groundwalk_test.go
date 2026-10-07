/*
===========================================================================

groundwalk_test.go - accepted pose, actual elapsed stepping and stop fences

===========================================================================
*/
package simulation

import (
	"fmt"
	"testing"
)

/*
================
groundFixture
================
*/
func groundFixture(t *testing.T, step func(Spawn, NavOwner, Spawn) (Spawn, NavOwner, bool)) (*WorldStore, *int64, func() WorldState, string) {
	t.Helper()
	now := int64(1000)
	st := NewWorldStore()
	st.ConfigureGroundWalk(GroundWalkConfig{Now: func() int64 { return now }, Step: step})
	start := Spawn{RegionID: 0x679A, X: 10, Y: 0, Z: 50}
	seed := func() WorldState { return DefaultWorldState(start) }
	key := WorldKey("test", "Walker")
	st.Update(key, seed, func(w *WorldState) {
		w.Run = 50
		goal := start
		goal.X = 500
		w.Spawn = goal
		w.MoveSegment = w.TravelSegment(start, goal, RunMode, now)
	})
	return st, &now, seed, key
}

/*
================
TestGroundWalkUsesElapsedStepsAndNeverExtrapolatesUnchecked
================
*/
func TestGroundWalkUsesElapsedStepsAndNeverExtrapolatesUnchecked(t *testing.T) {
	var sources, goals []Spawn
	st, now, seed, key := groundFixture(t, func(from Spawn, owner NavOwner, to Spawn) (Spawn, NavOwner, bool) {
		sources, goals = append(sources, from), append(goals, to)
		return to, owner, false
	})
	initial := st.Snapshot(key, seed)
	if initial.Spawn.X != 500 || initial.PersistedSpawn().X != 10 {
		t.Fatal("intent was published as accepted position")
	}
	*now += 300
	first := st.Snapshot(key, seed)
	if len(goals) != 1 || sources[0].X != 10 || goals[0].X != 25 {
		t.Fatalf("actual 300ms step = %v -> %v", sources, goals)
	}
	if first.LiveSpawnAt(*now+60000).X != 25 {
		t.Fatal("snapshot extrapolated through unchecked geometry")
	}
	*now += 10000
	second := st.Snapshot(key, seed)
	if second.LiveSpawnAt(*now).X != 185 || len(goals) != 2 {
		t.Fatalf("stall was not one capped native step: %+v calls=%d", second.LiveSpawnAt(*now), len(goals))
	}
	if first.LiveSpawnAt(*now).X != 25 {
		t.Fatal("an older snapshot changed under its reader")
	}
	if !st.AdmissionCurrent(key, initial) {
		t.Fatal("progression invalidated accepted command acknowledgement")
	}
}

/*
================
TestGroundCollisionKeepsActualLastStepSourceAndOneStop
================
*/
func TestGroundCollisionKeepsActualLastStepSourceAndOneStop(t *testing.T) {
	st, now, seed, key := groundFixture(t, func(from Spawn, owner NavOwner, to Spawn) (Spawn, NavOwner, bool) {
		if to.X >= 40 {
			return from, owner, true
		}
		return to, owner, false
	})
	admitted := st.Snapshot(key, seed)
	*now += 500
	if got := st.Snapshot(key, seed).PersistedSpawn(); got.X != 35 {
		t.Fatalf("last accepted = %+v", got)
	}
	*now += 200
	stopped := st.Snapshot(key, seed)
	if stopped.Spawn.X != 35 || stopped.MoveSegment != nil {
		t.Fatalf("collision fabricated pre-contact source: %+v", stopped)
	}
	updates := st.DrainGroundUpdates()
	if len(updates) != 1 || !updates[0].Stopped {
		t.Fatalf("stop delivery = %+v", updates)
	}
	if len(st.DrainGroundUpdates()) != 0 {
		t.Fatal("stop delivered twice")
	}
	if !st.AdmissionCurrent(key, admitted) {
		t.Fatal("collision suppressed its earlier accepted acknowledgement")
	}
	st.Update(key, seed, func(w *WorldState) { w.Spawn.X = 80; w.LifeRevision++ })
	if st.GroundRevisionCurrent(key, updates[0].Revision) || st.AdmissionCurrent(key, admitted) {
		t.Fatal("teleport retained stale stop or acknowledgement")
	}
}

/*
================
TestGroundUpdateSettlesBeforeGameplayAndSpeedChange
================
*/
func TestGroundUpdateSettlesBeforeGameplayAndSpeedChange(t *testing.T) {
	st, now, seed, key := groundFixture(t, func(from Spawn, owner NavOwner, to Spawn) (Spawn, NavOwner, bool) { return to, owner, false })
	revision := st.Snapshot(key, seed).GroundRevision()
	*now += 200
	st.Update(key, seed, func(w *WorldState) {
		if w.LiveSpawnAt(*now).X != 20 {
			t.Fatal("gameplay mutation observed unadvanced movement")
		}
		w.UpdateMovementSpeeds(20, 100, *now)
	})
	if got := st.Snapshot(key, seed).GroundRevision(); got != revision {
		t.Fatalf("speed retime replaced initiating approach revision: %d != %d", got, revision)
	}
	*now += 200
	if got := st.Snapshot(key, seed).PersistedSpawn(); got.X != 40 {
		t.Fatalf("speed change restarted or skipped movement: %+v", got)
	}
}

/*
================
TestGroundArrivalUsesNativePlanarFloatGoal
================
*/
func TestGroundArrivalUsesNativePlanarFloatGoal(t *testing.T) {
	for _, delta := range []Spawn{{X: 12.3456789}, {X: 12.3456789, Z: 8.7654321}, {X: 12.3456789, Y: 99, Z: 8.7654321}} {
		t.Run(fmt.Sprintf("%v", delta), func(t *testing.T) {
			st, now, seed, key := groundFixture(t, func(from Spawn, owner NavOwner, to Spawn) (Spawn, NavOwner, bool) { to.Y = 7; return to, owner, false })
			st.Update(key, seed, func(w *WorldState) {
				from := w.LiveSpawnAt(*now)
				goal := from
				goal.X += delta.X
				goal.Y += delta.Y
				goal.Z += delta.Z
				w.Spawn = goal
				w.MoveSegment = w.TravelSegment(from, goal, RunMode, *now)
			})
			goal := st.Snapshot(key, seed).Spawn
			if updates := st.DrainGroundUpdates(); len(updates) != 0 {
				t.Fatalf("admission persisted unchecked intent: %+v", updates)
			}
			for i := 0; i < 20 && st.Snapshot(key, seed).GroundActive(); i++ {
				*now += 100
			}
			got := st.Snapshot(key, seed)
			if got.GroundActive() || WorldDistance2D(got.Spawn, goal) > 0.5 || got.Spawn.Y != 7 {
				t.Fatalf("native planar goal failed to settle: goal=%+v got=%+v active=%v", goal, got.PersistedSpawn(), got.GroundActive())
			}
			updates := st.DrainGroundUpdates()
			if len(updates) != 1 || !updates[0].Arrived || updates[0].Stopped {
				t.Fatalf("terminal arrival update = %+v", updates)
			}
		})
	}
}

/*
================
TestGroundNativeArrivalBranches
================
*/
func TestGroundNativeArrivalBranches(t *testing.T) {
	for _, tc := range []struct {
		name           string
		dx, dz, ox, oz float32
		want           bool
	}{
		{"inside native radius", 0.3, 0.3, -10, -10, true},
		{"equal native radius", 0.5, 0, -10, 0, true},
		{"outside native radius", 0.5001, 0, -10, 0, false},
		{"passed destination", -1, 0, -11, 0, true},
		{"not passed destination", 1, 0, -9, 0, false},
		{"perpendicular is not passed", 1, 0, 0, -10, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NativeGroundArrived(tc.dx, tc.dz, tc.ox, tc.oz); got != tc.want {
				t.Fatalf("arrival=%v want%v", got, tc.want)
			}
		})
	}
}
