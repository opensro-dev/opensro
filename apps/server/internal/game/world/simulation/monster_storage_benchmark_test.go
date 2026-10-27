/*
===========================================================================

monster_storage_benchmark_test.go - gid-keyed monster read benchmarks

Measures the resident read path every tick leans on: one storage lookup,
and the MonsterState doors that first resolve the owning population. Sizes
follow a busy shard: one open-field population of a few thousand actors
with a minority carrying transient effects, plus a handful of world
populations the scan must step over.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

const (
	benchFieldActors    = 5000
	benchWorldCount     = 4
	benchWorldActors    = 200
	benchWorldGIDOffset = 100000
	benchDivision       = "benchmark"
)

/*
================
lookupBenchmarkActor

Deterministic actor gid with a sparse share of each transient effect: 10%
abnormal, 5% self effects, 2% linked and 2% target effects.
================
*/
func lookupBenchmarkActor(gid uint32) monster.Instance {
	row := monster.Instance{Gid: gid, CurrentHP: 100, Ref: monster.MonsterRef{MaxHP: 100, RefObjID: gid % 64}}
	if gid%10 == 0 {
		row.Abnormal = &abnormal.Block{}
	}
	if gid%20 == 1 {
		row.SelfEffects[0].Token = gid
	}
	if gid%50 == 2 {
		row.LinkedEffects = &monster.EffectSnapshot{}
	}
	if gid%50 == 3 {
		row.TargetEffects[0].Token = gid
	}
	return row
}

/*
================
lookupBenchmarkState
================
*/
func lookupBenchmarkState() *MonsterState {
	s := NewMonsterState(monster.Template{})
	field := &divisionMonsterState{instances: newMonsterStorage(nil)}
	for gid := uint32(1); gid <= benchFieldActors; gid++ {
		field.instances.set(gid, lookupBenchmarkActor(gid))
	}
	s.divs[benchDivision] = field
	for w := 0; w < benchWorldCount; w++ {
		lease := instance.Lease{ID: instance.ID(w + 2), Generation: 1}
		world := &divisionMonsterState{lease: lease, instances: newMonsterStorage(nil)}
		for i := 0; i < benchWorldActors; i++ {
			gid := uint32(benchWorldGIDOffset + w*benchWorldActors + i)
			world.instances.set(gid, lookupBenchmarkActor(gid))
		}
		if s.worldPopulations == nil {
			s.worldPopulations = make(map[populationKey]*divisionMonsterState)
		}
		s.worldPopulations[populationKey{benchDivision, lease}] = world
	}
	return s
}

/*
================
BenchmarkMonsterStorageLookup

One op is a read of every field actor.
================
*/
func BenchmarkMonsterStorageLookup(b *testing.B) {
	s := lookupBenchmarkState()
	storage := &s.divs[benchDivision].instances
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for gid := uint32(1); gid <= benchFieldActors; gid++ {
			if _, ok := storage.lookup(gid); !ok {
				b.Fatal("lost actor")
			}
		}
	}
}

/*
================
BenchmarkMonsterStateGet

One op reads every field actor and every world actor through the public door.
================
*/
func BenchmarkMonsterStateGet(b *testing.B) {
	s := lookupBenchmarkState()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for gid := uint32(1); gid <= benchFieldActors; gid++ {
			if _, ok := s.Get(benchDivision, gid); !ok {
				b.Fatal("lost field actor")
			}
		}
		for gid := uint32(benchWorldGIDOffset); gid < benchWorldGIDOffset+benchWorldCount*benchWorldActors; gid++ {
			if _, ok := s.Get(benchDivision, gid); !ok {
				b.Fatal("lost world actor")
			}
		}
	}
}

/*
================
BenchmarkMonsterStateGetInWorld

One op reads every field actor through the world-checked door.
================
*/
func BenchmarkMonsterStateGetInWorld(b *testing.B) {
	s := lookupBenchmarkState()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for gid := uint32(1); gid <= benchFieldActors; gid++ {
			if _, ok := s.GetInWorld(benchDivision, 0, gid); !ok {
				b.Fatal("lost field actor")
			}
		}
	}
}
