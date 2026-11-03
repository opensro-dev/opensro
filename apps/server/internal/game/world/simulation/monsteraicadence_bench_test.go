/*
===========================================================================

monsteraicadence_bench_test.go - the AI timer check, gid door vs carried door

BenchmarkCheckAITimerDoor is the A/B for #2029's narrow timer read:
the gid arm resolves the actor through the storage maps, the carried
arm goes through the slot the behaviour batch stamped. Both arms ask
the same acquisition question of the same seeded actors; the carried
arm must not pay for expanding the actor's full Instance.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
BenchmarkCheckAITimerDoor
================
*/
func BenchmarkCheckAITimerDoor(b *testing.B) {
	ops, _, now := seededLegWorld(b, 1200)
	batches := ops.Monsters.behaviorBatchesForDivision(*now+100_000, monsterTestDivision)
	var refs []behaviorActorRef
	for _, batch := range batches {
		refs = append(refs, batch.actors...)
	}
	if len(refs) < 1000 {
		b.Fatalf("seeded division too small: %d actors", len(refs))
	}
	// Build every actor's timer bank before timing, so neither arm
	// pays the first-bank setup inside the loop.
	for _, ref := range refs {
		ops.Monsters.checkAITimerAt(monsterTestDivision, ref.gid, ref.monsterSlot, monster.TimerIDAcquisition, *now)
	}
	b.ReportAllocs()
	b.Run("gid", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ref := refs[i%len(refs)]
			ops.Monsters.checkAITimer(monsterTestDivision, ref.gid, monster.TimerIDAcquisition, *now+int64(i%997))
		}
	})
	b.Run("carried", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ref := refs[i%len(refs)]
			ops.Monsters.checkAITimerAt(monsterTestDivision, ref.gid, ref.monsterSlot, monster.TimerIDAcquisition, *now+int64(i%997))
		}
	})
}
