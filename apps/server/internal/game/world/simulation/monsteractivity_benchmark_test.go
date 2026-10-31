/*
===========================================================================

monsteractivity_benchmark_test.go - simulation monster activity benchmark test ownership

===========================================================================
*/

package simulation

import (
	"container/heap"
	"sort"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
queueBenchmarkState
================
*/
func queueBenchmarkState(count int) (*MonsterState, *divisionMonsterState) {
	s := NewMonsterState(monster.Template{})
	d := &divisionMonsterState{instances: newMonsterStorage(nil), movers: newMoverStorage(nil)}
	s.divs["benchmark"] = d
	for gid := uint32(1); gid <= uint32(count); gid++ {
		d.instances.set(gid, monster.Instance{Gid: gid, CurrentHP: 100, Ref: monster.MonsterRef{MaxHP: 100}})
		d.movers.set(gid, monster.MoverState{})
		d.behavior.set(gid, 0)
	}
	return s, d
}

// Keeps the pre-optimization batch algorithm as a benchmark-only control.
/*
================
BenchmarkBehaviorBatch
================
*/
func BenchmarkBehaviorBatch(b *testing.B) {
	for _, legacy := range []bool{true, false} {
		name := "identities"
		if legacy {
			name = "legacy-snapshots"
		}
		b.Run(name, func(b *testing.B) {
			s, d := queueBenchmarkState(5000)
			b.ReportAllocs()
			b.ResetTimer()
			for tick := 0; tick < b.N; tick++ {
				if !legacy {
					if got := s.behaviorBatches(int64(tick)); len(got) != 1 || len(got[0].actors) != 5000 {
						b.Fatal("lost actors")
					}
					continue
				}
				var actors []monster.Instance
				for d.behavior.Len() > 0 && d.behavior.entries[0].at <= int64(tick) {
					e := heap.Pop(&d.behavior).(*behaviorEntry)
					i := finishSummonAction(d.instances.get(e.gid), int64(tick))
					d.instances.set(e.gid, i)
					actors = append(actors, i)
					s.scheduleBehavior(d, e.gid, int64(tick))
				}
				sort.Slice(actors, func(i, j int) bool { return actors[i].Gid < actors[j].Gid })
				if len(actors) != 5000 {
					b.Fatal("lost actors")
				}
			}
		})
	}
}

/*
================
TestBehaviorDispatchReadsCurrentActorAndSkipsRetiredIdentity
================
*/
func TestBehaviorDispatchReadsCurrentActorAndSkipsRetiredIdentity(t *testing.T) {
	s, d := queueBenchmarkState(3)
	batch := s.behaviorBatches(0)[0]
	for i, gid := range batch.actors {
		if gid != uint32(i+1) {
			t.Fatal("non-deterministic dispatch order")
		}
	}
	actor := d.instances.get(1)
	actor.CurrentHP = 7
	d.instances.set(1, actor)
	d.instances.remove(2)
	if got, ok := s.behaviorActor(batch.key, 1); !ok || got.CurrentHP != 7 {
		t.Fatal("stale actor snapshot")
	}
	if _, ok := s.behaviorActor(batch.key, 2); ok {
		t.Fatal("retired actor dispatched")
	}
	if next := s.behaviorBatches(0); len(next[0].actors) != 0 {
		t.Fatal("same actor scheduled twice in a tick")
	}
	if next := s.behaviorBatches(1); len(next[0].actors) != 2 {
		t.Fatal("survivors were not rescheduled")
	}
}

/*
================
TestBehaviorActorMoverMatchesDispatchReads

TestBehaviorActorMoverMatchesDispatchReads pins behaviorActorMover to
the two reads it fuses (#2029): the instance behaviorActor returns and
the mover MonsterState.Mover returns - the stored row, or the
synthesized spawn mover when no row exists yet - and not-ok for a
retired actor or an unknown population.
================
*/
func TestBehaviorActorMoverMatchesDispatchReads(t *testing.T) {
	s, d := queueBenchmarkState(3)
	s.clock = func() time.Time { return time.UnixMilli(5000) }
	batch := s.behaviorBatches(0)[0]

	snap := s.behaviorActorMover(batch.key, 1)
	actor, actorOK := s.behaviorActor(batch.key, 1)
	mover, moverOK := s.Mover("benchmark", 1)
	if !snap.ok || !actorOK || !moverOK {
		t.Fatal("dispatch reads disagree on a live actor")
	}
	if snap.instance != actor || snap.mover != mover {
		t.Fatal("fused snapshot differs from the separate reads")
	}

	d.movers.remove(3)
	snap = s.behaviorActorMover(batch.key, 3)
	mover, moverOK = s.Mover("benchmark", 3)
	if !snap.ok || !moverOK || snap.mover != mover {
		t.Fatal("synthesized spawn mover differs from Mover's")
	}

	d.instances.remove(2)
	if snap := s.behaviorActorMover(batch.key, 2); snap.ok {
		t.Fatal("retired actor resolved")
	}
	if snap := s.behaviorActorMover(populationKey{division: "nowhere"}, 1); snap.ok {
		t.Fatal("unknown population resolved")
	}
}

/*
================
BenchmarkMoverStorageSetSameRegion

Measures moverStorage.set and commitActivity across same-region pending
updates without expanding the old pending record for interest-bounds checks
or reconstructing monster.Instance twice per commitActivity.
================
*/
func BenchmarkMoverStorageSetSameRegion(b *testing.B) {
	for _, legacy := range []bool{true, false} {
		name := "compact"
		if legacy {
			name = "legacy"
		}
		b.Run(name, func(b *testing.B) {
			s, d := queueBenchmarkState(1000)
			base := monster.MoverState{
				Pose: monster.Pose{
					RegionID: 0x62a8,
					X:        500,
					Z:        500,
				},
				Channel:  2,
				Activity: monster.ActivityCadence{Interval: 1000, LastCheck: 1},
			}
			mustMoverTransition(&base, monster.MoverEventSpawnHoldElapsed, 0)
			mustMoverTransition(&base, monster.MoverEventStartWander, 0)
			mustMoverTransition(&base, monster.MoverEventActivityLost, 0)
			d.movers.set(1, base)
			before := base
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				after := before
				after.Activity.Due(uint32(1001 + i*1000))
				if legacy {
					s.mu.Lock()
					state := s.populationForObject("benchmark", 1)
					current, exists := state.movers.lookup(1)
					if !exists || current != before || state.instances.get(1).Gid == 0 || state.instances.get(1).CurrentHP == 0 {
						s.mu.Unlock()
						b.Fatal("legacy commitActivity rejected")
					}
					if err := after.Validate(); err != nil {
						s.mu.Unlock()
						b.Fatal(err)
					}
					slot, _ := state.movers.slotOf(1)
					old := state.movers.slots[slot].rec
					oldBounds := moverInterestBoundsFor(old.value())
					state.movers.spatial.remove(1, oldBounds)
					row, _ := after.PendingSnapshot()
					old.pending.set(row)
					state.movers.slots[slot].rec = old
					state.movers.spatial.add(1, moverInterestBoundsFor(after))
					state.syncApproachActor(1, after)
					s.scheduleBehavior(state, 1, int64(1000+i))
					s.mu.Unlock()
				} else {
					if !s.commitActivity("benchmark", 1, before, after, int64(1000+i)) {
						b.Fatal("commitActivity rejected")
					}
				}
				before = after
			}
		})
	}
}
