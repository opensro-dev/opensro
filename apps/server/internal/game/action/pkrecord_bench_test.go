/*
===========================================================================

pkrecord_bench_test.go - the PK clock sweep's per-tick cost (#1820 K)

Every tracked PK owner is checked every tick. The common case is a clock
that is not due, so the sweep's cost is the check itself; the benchmark
measures it over the real store's crowd.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
BenchmarkPKRecordSweepIdle

One sweep over the crowd, every owner tracked with a decay clock an hour
away: nothing is due, nothing commits.
================
*/
func BenchmarkPKRecordSweepIdle(b *testing.B) {
	rt, st, clock, crowd := crowdStoreRuntime(b, true)
	for _, c := range crowd {
		st.MutateCharacter(c, "seed-pk", func() {
			c.PK = &domain.PKRecord{TotalDecayAt: clock.Now().Unix() + 3600}
		})
		rt.pkOwners.track(testDivision, c.Name)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rt.advancePKRecords()
	}
}
