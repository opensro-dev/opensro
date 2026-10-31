package simulation

import (
	"testing"
	"time"
)

func TestSparseQueueCompactionPreservesIdentity(t *testing.T) {
	var state divisionMonsterState
	for i := uint32(0); i < 10000; i++ {
		state.behavior.set(i, int64(i%17))
	}
	state.compactDormantIndexes(time.Now().Add(time.Second))
	for i := uint32(0); i < 9990; i++ {
		state.behavior.remove(i)
	}
	before := state.behavior.slots[state.behavior.byGID[9999]].at
	state.compactDormantIndexes(time.Now().Add(time.Second))
	if _, capacity := state.behavior.capacity(); state.behavior.slots[state.behavior.byGID[9999]].at != before || capacity > 64 || state.sparseMapPeaks[2] != 10 {
		t.Fatal("queue contraction did not preserve entry identity")
	}
	due := state.behavior.takeDue(16, nil)
	if len(due) != 10 {
		t.Fatalf("drained %d actors after contraction, want the surviving 10", len(due))
	}
	seen := make(map[uint32]bool)
	for _, ref := range due {
		if ref.gid < 9990 || seen[ref.gid] {
			t.Fatalf("contraction changed the live set: drained %v", due)
		}
		seen[ref.gid] = true
	}
	for gid, slot := range state.behavior.byGID {
		if state.behavior.slots[slot].gid != gid {
			t.Fatal("index not synchronized")
		}
		state.behavior.remove(gid)
	}
	if len(state.behavior.byGID) != 0 {
		t.Fatal("removal left index entries")
	}
}

func TestSparseMapWaitsForAdmission(t *testing.T) {
	m := map[uint32]int{7: 99}
	peak := 10000
	got := compactSparseMap(m, &peak, false)
	got[8] = 100
	if m[8] != 100 || peak != 10000 {
		t.Fatal("compaction ran without admission")
	}
	got = compactSparseMap(m, &peak, true)
	if len(got) != 2 || got[7] != 99 || got[8] != 100 || peak != 2 {
		t.Fatal("map contraction lost state")
	}
	delete(m, 7)
	if got[7] != 99 {
		t.Fatal("old backing table retained")
	}
}

func BenchmarkSparseMapContraction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		m := make(map[uint32]int64, 53000)
		for gid := uint32(0); gid < 53000; gid++ {
			m[gid] = int64(gid)
		}
		for gid := uint32(0); gid < 51000; gid++ {
			delete(m, gid)
		}
		peak := 53000
		b.StartTimer()
		m = compactSparseMap(m, &peak, true)
		if len(m) != 2000 {
			b.Fatal("lost live entries")
		}
	}
}
