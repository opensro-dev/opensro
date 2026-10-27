/*
===========================================================================

snapshot_bench_test.go - the cost of one ground snapshot

The store snapshots the registry on every ground-dirty commit, on the
tick goroutine; this measures it at the live plane's size (1,600 drops,
prod 2026-10-28).

===========================================================================
*/
package grounditem

import "testing"

/*
================
BenchmarkSnapshotLivePlane
================
*/
func BenchmarkSnapshotLivePlane(b *testing.B) {
	r := NewRegistry()
	for i := 0; i < 1600; i++ {
		r.Add("global-official", Item{RefObjID: 3117, StackCount: 1, MagicOptions: []uint64{uint64(i)}, DroppedBy: "hunter"})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = r.Snapshot()
	}
}
