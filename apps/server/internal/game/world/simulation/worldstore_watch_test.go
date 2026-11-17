/*
===========================================================================

worldstore_watch_test.go - the movement revision agrees with the compare

A MovementWatch that says unchanged must never contradict
WorldStore.MovementCurrent: over a randomized run of updates (moves,
ground walks that step, arrive or are blocked, life changes, gait
changes, unrelated edits, values that change and change back), forgets
and re-seeds, every snapshot ever taken is checked after every step.

===========================================================================
*/
package simulation

import (
	"math/rand"
	"testing"
)

/*
================
watchedCapture

One snapshot's watch and the fields its predicate compares.
================
*/
type watchedCapture struct {
	key      string
	watch    MovementWatch
	expected WorldState
}

/*
================
TestMovementWatchNeverContradictsMovementCurrent
================
*/
func TestMovementWatchNeverContradictsMovementCurrent(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	now := int64(1000)
	st := NewWorldStore()
	// Every fourth step is blocked: the accepted pose stops short.
	steps := 0
	st.ConfigureGroundWalk(GroundWalkConfig{Now: func() int64 { return now }, Step: func(from Spawn, owner NavOwner, to Spawn) (Spawn, NavOwner, bool) {
		steps++
		if steps%4 == 0 {
			return from, owner, true
		}
		return to, owner, false
	}})
	start := Spawn{RegionID: 0x679A, X: 10, Y: 0, Z: 50}
	seed := func() WorldState { return DefaultWorldState(start) }
	keys := []string{WorldKey("test", "a"), WorldKey("test", "b"), WorldKey("test", "c")}
	// A small set of positions so values change back (A -> B -> A).
	spots := []Spawn{start, {RegionID: 0x679A, X: 60, Z: 50}, {RegionID: 0x679A, X: 10, Z: 120}}

	var captures []watchedCapture
	fast, slow := 0, 0
	for step := 0; step < 20000; step++ {
		key := keys[rng.Intn(len(keys))]
		switch op := rng.Intn(10); op {
		case 0, 1:
			var captured WorldState
			watch := st.SnapshotIntoWatched(key, &captured, seed)
			expected := WorldState{LifeRevision: captured.LifeRevision, Spawn: captured.Spawn, MovementMode: captured.MovementMode}
			if captured.MoveSegment != nil {
				segment := *captured.MoveSegment
				expected.MoveSegment = &segment
			}
			captures = append(captures, watchedCapture{key: key, watch: watch, expected: expected})
		case 2:
			st.Update(key, seed, func(w *WorldState) {
				goal := spots[rng.Intn(len(spots))]
				ApplyMove(w, PlayerObjectID(1), MovementRequest{Mode: MovementAckDestinationMode,
					RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z}, RunMode, now)
			})
		case 3:
			st.Update(key, seed, func(w *WorldState) { w.Spawn = spots[rng.Intn(len(spots))]; w.MoveSegment = nil })
		case 4:
			st.Update(key, seed, func(w *WorldState) { w.MovementMode = []uint8{WalkMode, RunMode}[rng.Intn(2)] })
		case 5:
			st.Update(key, seed, func(w *WorldState) { w.LifeRevision++ })
		case 6:
			// Unrelated to movement: must not cost the fast path.
			st.Update(key, seed, func(w *WorldState) { w.Sitting = !w.Sitting })
		case 7:
			st.Forget(key)
		default:
			now += int64(rng.Intn(400))
			st.Snapshot(key, seed)
		}
		for _, c := range captures {
			full := st.MovementCurrent(c.key, c.expected)
			if c.watch.Unchanged() {
				fast++
				if !full {
					t.Fatalf("step %d: the watch says unchanged but MovementCurrent says stale for %s", step, c.key)
				}
			} else if full {
				slow++
			}
		}
		if len(captures) > 64 {
			captures = captures[len(captures)-64:]
		}
	}
	// The fast path must carry the common case, and a change that went
	// back (A -> B -> A) is still answered right by the compare.
	if fast == 0 || slow == 0 {
		t.Fatalf("fast answers %d, compare-only current answers %d: the run did not exercise both", fast, slow)
	}
	t.Logf("%d fast answers, %d current answers left to the compare", fast, slow)
}
