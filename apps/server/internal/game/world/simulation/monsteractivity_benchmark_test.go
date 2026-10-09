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
