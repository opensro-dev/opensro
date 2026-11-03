/*
===========================================================================

monsterstate_slot_bench_test.go - the carried-slot door against the gid door

#2133: the step's generation re-read, resolved by gid (the monster
existence probe, the mover lookup and the generation read, each a
storage map probe) against the same read through the slots the
behaviour batch carried (slice places verified against the gid).
Both doors answer for the same seeded 1200-monster division - the
RunMonsterLeg profile's shape - and both pay the same population
resolve and state lock; only the storage probes differ.

===========================================================================
*/

package simulation

import "testing"

/*
================
BenchmarkMoverGenerationCarried
================
*/
func BenchmarkMoverGenerationCarried(b *testing.B) {
	ops, sessions, now := seededLegWorld(b, 1200)
	settle := &fakePusher{}
	for tick := 0; tick < 10; tick++ {
		*now = seededLegBaseMs + int64(tick)*100
		ops.RunMonsterLeg(*now, sessions, settle)
	}
	state := ops.Monsters.division(monsterTestDivision)
	instances := ops.Monsters.InstancesInRegions(monsterTestDivision, []uint16{monsterTestRegion})
	type carried struct{ gid, slot, monsterSlot uint32 }
	refs := make([]carried, 0, len(instances))
	for _, instance := range instances {
		_, monsterSlot, hot := state.instances.hotRowSlot(instance.Gid)
		moverSlot, hasMover := state.movers.slotOf(instance.Gid)
		if !hot || !hasMover {
			continue
		}
		refs = append(refs, carried{instance.Gid, moverSlot, monsterSlot})
	}
	if len(refs) < 1000 {
		b.Fatalf("seeded %d carried actors, want the population", len(refs))
	}
	b.ReportAllocs()
	b.Run("gid", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ref := refs[i%len(refs)]
			if _, _, ok := ops.Monsters.MoverGeneration(monsterTestDivision, ref.gid); !ok {
				b.Fatal("gid door lost a seeded actor")
			}
		}
	})
	b.Run("carried", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ref := refs[i%len(refs)]
			if _, _, ok := ops.Monsters.MoverGenerationAt(monsterTestDivision, ref.gid, ref.slot, ref.monsterSlot); !ok {
				b.Fatal("carried door lost a seeded actor")
			}
		}
	})
}
