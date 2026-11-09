package simulation

import (
	"math"
	"opensro.online/server/internal/game/world/monster"
	"testing"
	"unsafe"
)

func TestResidentPendingPreservesIndependentActorState(t *testing.T) {
	a := monster.PendingMover{Spawning: true, BehaviorDeadlineMs: 918273, Pose: monster.Pose{RegionID: monsterTestRegion, X: 12.125, Y: -3, Z: 98, Heading: 123}, Activity: monster.ActivityCadence{Interval: 1739, LastCheck: 0xfffffffe}, PreviousEvent: 2, LastEvent: 3, TransitionSerial: 919, RetaliationRevision: 17, AttackIntervalMs: 2000, LastBattleActivityMs: 123, HomingStartedMs: 345, HomingAcquireAfterMs: 678, Channel: 2, PursuitChannel: 3, NavigationChannel: 4, NavigationSpeed: math.Copysign(0, -1)}
	b := a
	b.Pose.X = 45.5
	b.Activity.LastCheck = 9
	var x, y residentPendingMover
	x.set(a)
	y.set(b)
	if x.shared != y.shared {
		t.Fatal("identical immutable fields were not shared")
	}
	check := func(p *residentPendingMover, want monster.PendingMover) {
		t.Helper()
		got, ok := p.value().PendingSnapshot()
		if !ok || got != want || math.Float64bits(got.NavigationSpeed) != math.Float64bits(want.NavigationSpeed) {
			t.Fatalf("actor state changed: got %+v want %+v", got, want)
		}
	}
	check(&x, a)
	check(&y, b)
	a.TransitionSerial++
	a.Pose.Z = 77
	x.set(a)
	check(&x, a)
	check(&y, b)
	detached := y.value()
	detached.Pose.X = 999
	check(&y, b)
	if unsafe.Sizeof(residentPendingMover{}) >= unsafe.Sizeof(monster.PendingMover{}) {
		t.Fatal("resident representation did not shrink")
	}
}

/*
================
TestResidentPendingFillTracksRowChanges

The fill expands from the shared row cached at first read: a
per-actor cadence stamp between fills is read live, and a set
with a new row drops the cache so the next fill expands the new
row - never the stale one (#3027). The interned row's signed-zero
navigation speed survives the cache.
================
*/
func TestResidentPendingFillTracksRowChanges(t *testing.T) {
	row := monster.PendingMover{BehaviorDeadlineMs: 42, Pose: monster.Pose{RegionID: monsterTestRegion, X: 3, Z: 4},
		Activity: monster.ActivityCadence{Interval: 1000, LastCheck: 7}, TransitionSerial: 1,
		NavigationSpeed: math.Copysign(0, -1), Stuck: true}
	var p residentPendingMover
	p.set(row)
	snapshot := func() monster.PendingMover {
		t.Helper()
		var m monster.MoverState
		p.fill(&m)
		got, ok := m.PendingSnapshot()
		if !ok {
			t.Fatal("pending fill did not round-trip")
		}
		return got
	}
	if got := snapshot(); got != row || math.Float64bits(got.NavigationSpeed) != math.Float64bits(row.NavigationSpeed) {
		t.Fatalf("first fill = %+v, want the set row %+v", got, row)
	}
	if got := snapshot(); got != row {
		t.Fatalf("cached fill = %+v, want the set row %+v", got, row)
	}

	// A per-actor cadence stamp (stampActivity's write) is not in
	// the cached row: the next fill reads it live.
	p.activity = monster.ActivityCadence{Interval: 5, LastCheck: 6}
	stamped := row
	stamped.Activity = p.activity
	if got := snapshot(); got != stamped {
		t.Fatalf("fill after the cadence stamp = %+v, want %+v", got, stamped)
	}

	// A new row drops the cache.
	next := stamped
	next.TransitionSerial = 2
	next.Pose.X = 9
	p.set(next)
	if got := snapshot(); got != next {
		t.Fatalf("fill after re-set = %+v, want the new row %+v", got, next)
	}
}

/*
================
TestPendingScheduleModeReadsTheCachedBit

scheduleRow derives a pending row's mode from the record's cached
spawning bit, not from the interned shared row: the bit must track
what set last wrote, in both directions, or a spawning actor would
schedule as merely pending (and the other way around).
================
*/
func TestPendingScheduleModeReadsTheCachedBit(t *testing.T) {
	s := newMoverStorage(nil)
	pending := func(spawning bool) monster.MoverState {
		row := monster.PendingMover{Spawning: spawning, Pose: monster.Pose{RegionID: monsterTestRegion, X: 1, Z: 2},
			Activity: monster.ActivityCadence{Interval: 1000, LastCheck: 7}}
		var m monster.MoverState
		row.ExpandInto(&m)
		return m
	}
	s.set(7, pending(true))
	if got, ok := s.schedule(7); !ok || got.mode != monster.MoverSpawning {
		t.Fatalf("spawning row scheduled as mode %v (ok %v), want MoverSpawning", got.mode, ok)
	}
	s.set(7, pending(false))
	if got, ok := s.schedule(7); !ok || got.mode != monster.MoverPending {
		t.Fatalf("re-settled row scheduled as mode %v (ok %v), want MoverPending", got.mode, ok)
	}
}
