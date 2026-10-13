/*
===========================================================================

monsteractivity_queue_test.go - the typed queue keeps container/heap's order

The behaviour queue sifts with its own copies of container/heap's up and
down. A random walk of schedules, reschedules, removals and pops drives it
beside a reference queue that uses container/heap itself; after every step
both must hold the same entries in the same slots.

===========================================================================
*/

package simulation

import (
	"container/heap"
	"math/rand"
	"testing"
)

/*
================
referenceQueue

The queue as it was: the same entries and order, sifted by container/heap.
================
*/
type referenceQueue struct{ behaviorQueue }

/*
================
referenceQueue.set
================
*/
func (q *referenceQueue) set(gid uint32, at int64) {
	if q.byGID == nil {
		q.byGID = make(map[uint32]*behaviorEntry)
	}
	if e := q.byGID[gid]; e != nil {
		e.at = at
		heap.Fix(&q.behaviorQueue, e.index)
		return
	}
	heap.Push(&q.behaviorQueue, &behaviorEntry{gid: gid, at: at})
}

/*
================
referenceQueue.remove
================
*/
func (q *referenceQueue) remove(gid uint32) {
	if e := q.byGID[gid]; e != nil {
		heap.Remove(&q.behaviorQueue, e.index)
	}
}

/*
================
TestBehaviorQueueKeepsContainerHeapOrder
================
*/
func TestBehaviorQueueKeepsContainerHeapOrder(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	var got behaviorQueue
	var want referenceQueue
	pops := 0
	for step := 0; step < 200000; step++ {
		gid, at := uint32(1+r.Intn(400)), int64(r.Intn(60))
		switch op := r.Intn(10); {
		case op < 6:
			got.set(gid, at)
			want.set(gid, at)
		case op < 8:
			got.remove(gid)
			want.remove(gid)
		default:
			// The due loop: reschedule the root past now, as scheduleBehavior does.
			if got.Len() > 0 {
				root := got.entries[0].gid
				got.set(root, got.entries[0].at+1+at)
				want.set(root, want.entries[0].at+1+at)
				pops++
			}
		}
		if got.Len() != want.Len() || len(got.byGID) != len(want.byGID) {
			t.Fatalf("step %d: %d entries, want %d", step, got.Len(), want.Len())
		}
		for i := range got.entries {
			g, w := got.entries[i], want.entries[i]
			if g.gid != w.gid || g.at != w.at || g.index != i || w.index != i || got.byGID[g.gid] != g {
				t.Fatalf("step %d slot %d: %+v, want %+v", step, i, *g, *w)
			}
		}
	}
	if pops < 10000 {
		t.Fatalf("only %d root reschedules", pops)
	}
}
