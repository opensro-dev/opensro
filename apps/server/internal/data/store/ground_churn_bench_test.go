/*
===========================================================================

ground_churn_bench_test.go - one drop changes, one commit persists it

The live shape of the ground write path: a plane of livePlaneRows drops
attached to the store, one of them changed, then a commit. The whole
cost of persisting that one change - reading the registry, finding the
change, writing it - is what this measures.

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/game/item/grounditem"
)

/*
================
BenchmarkGroundChurnCommit
================
*/
func BenchmarkGroundChurnCommit(b *testing.B) {
	s := openTest(b, b.TempDir(), newTestClock())
	r := grounditem.NewRegistry()
	s.AttachGround(r)
	gids := make([]uint32, 0, livePlaneRows)
	for i := 0; i < livePlaneRows; i++ {
		gids = append(gids, r.Add(testDivision, grounditem.Item{RefObjID: 3117, StackCount: 1,
			Position: grounditem.Point{RegionID: 0x6a9a, X: float32(i % 40), Z: float32(i / 40)}}).Gid)
	}
	s.Mutate("ground-churn-seed", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		r.SetStackCount(testDivision, gids[n%len(gids)], uint16(2+n%7))
		s.Mutate("ground-churn", nil)
	}
}
