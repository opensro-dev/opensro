/*
===========================================================================

expiry_bench_test.go - the sweep peek's cost at the live plane size

hasExpired gated every 5 s sweep on Registry.All, which detaches each
drop on the plane - pet records and cargo included - only to read the
DroppedAt stamp (#2030). These two benches price the old shape against
the read-only HasExpired at the snapshot bench's plane size (1,600
drops, prod 2026-10-28), with a handful of summon records riding it.

===========================================================================
*/
package grounditem

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
expiryBenchRegistry

1,600 plain drops plus sixteen summon records whose pets carry a
filled thirty-row cargo bag (a dropped transport's shape): the most
expensive record on the plane.
================
*/
func expiryBenchRegistry() *Registry {
	r := NewRegistry()
	now := time.Unix(10_000, 0)
	for i := 0; i < 1600; i++ {
		r.Add("global-official", Item{RefObjID: 3117, StackCount: 1, DroppedAt: now})
	}
	for i := 0; i < 16; i++ {
		rows := make([]domain.InventoryRow, 30)
		for j := range rows {
			rows[j] = domain.InventoryRow{Slot: int64(j), RefObjID: 3117, StackCount: 1}
		}
		r.Add("global-official", Item{RefObjID: 3878, StackCount: 1, DroppedAt: now,
			Summon: &domain.CharacterCOS{Container: &domain.COSContainer{Capacity: 30, Rows: rows}}})
	}
	return r
}

/*
================
BenchmarkRegistryAllExpiryPeek
================
*/
func BenchmarkRegistryAllExpiryPeek(b *testing.B) {
	r := expiryBenchRegistry()
	now := time.Unix(10_000, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		found := false
		for _, item := range r.All("global-official") {
			if !item.DroppedAt.IsZero() && now.Sub(item.DroppedAt) >= FixtureLifetime {
				found = true
			}
		}
		_ = found
	}
}

/*
================
BenchmarkRegistryHasExpired
================
*/
func BenchmarkRegistryHasExpired(b *testing.B) {
	r := expiryBenchRegistry()
	now := time.Unix(10_000, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = r.HasExpired("global-official", now, FixtureLifetime)
	}
}
