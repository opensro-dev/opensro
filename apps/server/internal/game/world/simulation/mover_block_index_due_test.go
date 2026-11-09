/*
===========================================================================

mover_block_index_due_test.go - the due heap cannot desync from duePos

===========================================================================
*/

package simulation

import (
	"math"
	"math/rand"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
checkDueHeap

The heap contract the reconcile drain relies on: dueHeap and
duePos name the same rows at the same positions, parents are due
no later than their children, and the heap holds exactly the
placed segment rows whose crossing instant is finite. The
2026-11-09 desync (duePos written swapped in the sift pair)
drained forever and grew to 85 GB; this fails in milliseconds
instead.
================
*/
func checkDueHeap(t *testing.T, x *moverBlockIndex) {
	t.Helper()
	seen := make([]bool, len(x.duePos))
	for i, slot := range x.dueHeap {
		if int(slot) >= len(x.duePos) {
			t.Fatalf("heap holds slot %d past duePos (%d)", slot, len(x.duePos))
		}
		if x.duePos[slot] != int32(i) {
			t.Fatalf("duePos[%d] = %d, want its heap position %d", slot, x.duePos[slot], i)
		}
		if seen[slot] {
			t.Fatalf("slot %d is in the heap twice", slot)
		}
		seen[slot] = true
		if x.placeDueMs[slot] == math.MaxInt64 {
			t.Fatalf("settled slot %d is in the heap", slot)
		}
		if i > 0 {
			parent := x.dueHeap[(i-1)/2]
			if x.placeDueMs[parent] > x.placeDueMs[slot] {
				t.Fatalf("heap order: parent slot %d due %d after child slot %d due %d",
					parent, x.placeDueMs[parent], slot, x.placeDueMs[slot])
			}
		}
	}
	finite := 0
	for slot := range x.segBySlot {
		if !x.segBySlot[slot] || x.placeDueMs[slot] == math.MaxInt64 {
			continue
		}
		finite++
		if int(slot) >= len(x.duePos) || x.duePos[slot] < 0 {
			t.Fatalf("segment slot %d due %d is not in the heap", slot, x.placeDueMs[slot])
		}
	}
	if finite != len(x.dueHeap) {
		t.Fatalf("heap holds %d rows, want the %d finite-due segment rows", len(x.dueHeap), finite)
	}
}

/*
================
TestMoverBlockIndexDueHeapInvariant

Random enter, leave, move and remove over one storage, reconciled
at wandering instants (forward, same and backward), with the heap
contract checked after every step.
================
*/
func TestMoverBlockIndexDueHeapInvariant(t *testing.T) {
	rng := rand.New(rand.NewSource(2994))
	storage := newMoverStorage(nil)
	const rows = 48
	pose := func() monster.Pose {
		region := uint16(interestScanRegion + rng.Intn(3) - 1 + (rng.Intn(3)-1)*0x100)
		return monster.Pose{RegionID: region, X: rng.Float64() * 1920, Z: rng.Float64() * 1920}
	}
	row := func(nowMs int64) monster.MoverState {
		from, to := pose(), pose()
		out := monster.MoverState{Pose: from}
		if rng.Intn(3) == 0 {
			return out
		}
		out.From, out.To = from, to
		out.DepartMs = nowMs + int64(rng.Intn(400)) - 100
		out.ArriveMs = out.DepartMs + int64(rng.Intn(2000))
		if rng.Intn(4) == 0 {
			out.ArriveMs = out.DepartMs
		}
		return out
	}
	nowMs := int64(1_000_000)
	for op := 0; op < 900; op++ {
		gid := uint32(1 + rng.Intn(rows))
		switch rng.Intn(5) {
		case 0, 1, 2:
			storage.set(gid, row(nowMs))
		case 3:
			storage.remove(gid)
		case 4:
			storage.remove(gid)
			storage.set(gid, row(nowMs))
		}
		switch rng.Intn(6) {
		case 0:
			nowMs -= 100
		case 1:
		default:
			nowMs += 100
		}
		storage.reconcileBlocks(nowMs)
		checkDueHeap(t, &storage.blocks)
	}
	storage.reconcileBlocks(nowMs + 1<<30)
	checkDueHeap(t, &storage.blocks)
	if len(storage.blocks.dueHeap) != 0 {
		t.Fatalf("heap holds %d rows after every segment settled", len(storage.blocks.dueHeap))
	}
}
