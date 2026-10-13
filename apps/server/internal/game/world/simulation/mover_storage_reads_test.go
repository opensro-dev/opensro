/*
===========================================================================

mover_storage_reads_test.go - the visibility reads agree with lookup

has and livePoseAt answer what lookup and lookup().LivePoseAt would, for
every row the storage can hold: pending, spawning, live segments before,
during and after flight, with and without a ground resolver, and across
the transitions that turn one kind of row into another or remove it.

===========================================================================
*/
package simulation

import (
	"math/rand"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
readsTestPose
================
*/
func readsTestPose(r *rand.Rand) monster.Pose {
	return monster.Pose{RegionID: monsterTestRegion + uint16(r.Intn(3)), X: r.Float64() * 1920,
		Y: r.Float64()*200 - 100, Z: r.Float64() * 1920, Heading: uint16(r.Intn(65536))}
}

/*
================
readsTestRow

One random row: pending or spawning (compacted by set) or a live segment.
================
*/
func readsTestRow(r *rand.Rand) monster.MoverState {
	pending := monster.PendingMover{Spawning: r.Intn(2) == 0, BehaviorDeadlineMs: r.Int63n(100000),
		Pose: readsTestPose(r), TransitionSerial: uint64(r.Intn(4)), Channel: uint8(r.Intn(3))}
	row := pending.Expand()
	if r.Intn(2) == 0 {
		return row
	}
	row.From, row.To = readsTestPose(r), readsTestPose(r)
	row.DepartMs = 1000 + r.Int63n(1000)
	row.ArriveMs = row.DepartMs + r.Int63n(3000)
	return row
}

/*
================
TestMoverStorageVisibilityReadsMatchLookup
================
*/
func TestMoverStorageVisibilityReadsMatchLookup(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	ground := func(region uint16, x, z float64) (float64, bool) { return x*0.5 - z + float64(region), x > 500 }
	s := newMoverStorage(nil)
	sawPending, sawLive := false, false
	for step := 0; step < 4000; step++ {
		gid := uint32(1 + r.Intn(64))
		switch r.Intn(5) {
		case 0:
			s.remove(gid)
		default:
			s.set(gid, readsTestRow(r))
		}
		sawPending = sawPending || s.compact(gid)
		sawLive = sawLive || s.has(gid) && !s.compact(gid)
		for probe := uint32(0); probe <= 65; probe++ {
			want, exists := s.lookup(probe)
			if has := s.has(probe); has != exists {
				t.Fatalf("step %d gid %d: has = %v, lookup exists = %v", step, probe, has, exists)
			}
			for _, nowMs := range []int64{0, 1500, 2500, 6000} {
				for _, resolver := range []monster.GroundResolver{nil, ground} {
					got, ok := s.livePoseAt(probe, nowMs, resolver)
					if ok != exists || exists && got != want.LivePoseAt(nowMs, resolver) {
						t.Fatalf("step %d gid %d at %d: livePoseAt = %+v %v, want %+v %v",
							step, probe, nowMs, got, ok, want.LivePoseAt(nowMs, resolver), exists)
					}
				}
			}
		}
	}
	if !sawPending || !sawLive {
		t.Fatalf("the walk never stored both kinds of row (pending %v, live %v)", sawPending, sawLive)
	}
}

/*
================
TestMoverStorageLivePosesMatchValues

livePoses yields exactly the rows values does, each with the pose its
expanded row would report: the population scan's cheap read is the same
read.
================
*/
func TestMoverStorageLivePosesMatchValues(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	ground := func(region uint16, x, z float64) (float64, bool) { return x*0.5 - z + float64(region), x > 500 }
	s := newMoverStorage(nil)
	for step := 0; step < 2000; step++ {
		gid := uint32(1 + r.Intn(64))
		if r.Intn(5) == 0 {
			s.remove(gid)
		} else {
			s.set(gid, readsTestRow(r))
		}
		for _, nowMs := range []int64{0, 1500, 2500, 6000} {
			for _, resolver := range []monster.GroundResolver{nil, ground} {
				want := map[uint32]monster.Pose{}
				for gid, row := range s.values() {
					want[gid] = row.LivePoseAt(nowMs, resolver)
				}
				got := map[uint32]monster.Pose{}
				for gid, pose := range s.livePoses(nowMs, resolver) {
					got[gid] = pose
				}
				if len(got) != len(want) {
					t.Fatalf("step %d at %d: livePoses yielded %d rows, values %d", step, nowMs, len(got), len(want))
				}
				for gid, pose := range want {
					if got[gid] != pose {
						t.Fatalf("step %d gid %d at %d: livePoses = %+v, want %+v", step, gid, nowMs, got[gid], pose)
					}
				}
			}
		}
	}
}

/*
================
BenchmarkMoverStorageVisibilityReads

The visibility path's reads over a resting crowd (pending rows, the common
case): the old lookup-based reads against has and livePoseAt.
================
*/
func BenchmarkMoverStorageVisibilityReads(b *testing.B) {
	r := rand.New(rand.NewSource(2))
	s := newMoverStorage(nil)
	for gid := uint32(1); gid <= 2000; gid++ {
		s.set(gid, monster.PendingMover{Pose: readsTestPose(r), BehaviorDeadlineMs: r.Int63n(100000)}.Expand())
	}
	b.Run("lookup", func(b *testing.B) {
		for range b.N {
			for gid := uint32(1); gid <= 2000; gid++ {
				_ = s.get(gid).LivePoseAt(1500, nil)
				_, _ = s.lookup(gid)
			}
		}
	})
	b.Run("reads", func(b *testing.B) {
		for range b.N {
			for gid := uint32(1); gid <= 2000; gid++ {
				_, _ = s.livePoseAt(gid, 1500, nil)
				_ = s.has(gid)
			}
		}
	})
}

/*
================
BenchmarkMoverStoragePopulationPoses

The area-candidate scan's read of a 3000-row population: every expanded
row's pose (values, before) against livePoses.
================
*/
func BenchmarkMoverStoragePopulationPoses(b *testing.B) {
	r := rand.New(rand.NewSource(3))
	s := newMoverStorage(nil)
	for gid := uint32(1); gid <= 3000; gid++ {
		s.set(gid, readsTestRow(r))
	}
	var sink float64
	b.Run("values", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, row := range s.values() {
				sink += row.LivePoseAt(2000, nil).X
			}
		}
	})
	b.Run("livePoses", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, pose := range s.livePoses(2000, nil) {
				sink += pose.X
			}
		}
	})
	_ = sink
}

/*
================
TestMoverStorageScheduleMatchesLookup

schedule answers what lookup's MoverState would for the behaviour queue's
fields, for every kind of row.
================
*/
func TestMoverStorageScheduleMatchesLookup(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	s := newMoverStorage(nil)
	sawPending, sawLive := false, false
	for step := 0; step < 4000; step++ {
		gid := uint32(1 + r.Intn(64))
		if r.Intn(5) == 0 {
			s.remove(gid)
		} else {
			s.set(gid, readsTestRow(r))
		}
		sawPending = sawPending || s.compact(gid)
		sawLive = sawLive || s.has(gid) && !s.compact(gid)
		for probe := uint32(0); probe <= 65; probe++ {
			m, exists := s.lookup(probe)
			got, ok := s.schedule(probe)
			want := moverSchedule{mode: m.Mode(), activity: m.Activity, arriveMs: m.ArriveMs, regionID: m.Pose.RegionID}
			if ok != exists || got != want {
				t.Fatalf("step %d gid %d: schedule = %+v %v, want %+v %v", step, probe, got, ok, want, exists)
			}
		}
	}
	if !sawPending || !sawLive {
		t.Fatalf("the walk never stored both kinds of row (pending %v, live %v)", sawPending, sawLive)
	}
}
