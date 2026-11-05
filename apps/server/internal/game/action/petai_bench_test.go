/*
===========================================================================

petai_bench_test.go - what one full pet sweep costs

BenchmarkAdvancePetsSweep drives advancePets over the synthetic
shard's bound pets: the per-tick constant the live profile pays per
pet - the sub-advance sweep, its store doors and the follow tick
together (#2139).

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

var advancePetsSweepSink []simulation.DivisionFrames

/*
================
BenchmarkAdvancePetsSweep

One advancePets pass over the shard's pets per iteration, after the
warm-up ticks settle the followers.
================
*/
func BenchmarkAdvancePetsSweep(b *testing.B) {
	s := newSyntheticShard(b)
	for i := int64(0); i < tickBudgetWarmupTicks; i++ {
		s.tick(i)
	}
	// The refusal audit never runs in production and is off while
	// counted, as in the tick budget benchmark.
	restore := enterworld.SuspendRefusalAudit(b)
	defer restore()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.clock.Advance(100 * time.Millisecond)
		advancePetsSweepSink = s.rt.advancePets(s.clock.NowMs())
	}
}
