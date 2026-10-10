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
