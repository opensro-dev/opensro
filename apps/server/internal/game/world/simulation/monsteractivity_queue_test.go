/*
===========================================================================

monsteractivity_queue_test.go - the wheel keeps the heap's due set

The behaviour queue was a binary heap; it is a timer wheel now
(monsteractivity_queue.go). The batch never read the heap's order (it
sorts actors by gid), so the contract that survives is the due set:
after any walk of schedules, reschedules, removals and drains, the
wheel must fire exactly the gids the heap would have popped, each
once, and hold the same live dues. legacyBehaviorQueue below is the
old heap, kept verbatim as the oracle.

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
legacyBehaviorEntry
================
*/
type legacyBehaviorEntry struct {
	gid   uint32
	at    int64
	index int
}

/*
================
legacyBehaviorQueue

The queue as it was: a binary heap sifted by container/heap.
================
*/
type legacyBehaviorQueue struct {
	entries []*legacyBehaviorEntry
	byGID   map[uint32]*legacyBehaviorEntry
}

/*
================
legacyBehaviorQueue.Len
================
*/
func (q *legacyBehaviorQueue) Len() int { return len(q.entries) }

/*
================
legacyBehaviorQueue.Less
================
*/
func (q *legacyBehaviorQueue) Less(i, j int) bool {
	a, b := q.entries[i], q.entries[j]
	return a.at < b.at || a.at == b.at && a.gid < b.gid
}

/*
================
legacyBehaviorQueue.Swap
================
*/
func (q *legacyBehaviorQueue) Swap(i, j int) {
	q.entries[i], q.entries[j] = q.entries[j], q.entries[i]
	q.entries[i].index = i
	q.entries[j].index = j
}

/*
================
legacyBehaviorQueue.Push
================
*/
func (q *legacyBehaviorQueue) Push(v any) {
	e := v.(*legacyBehaviorEntry)
	e.index = len(q.entries)
	q.entries = append(q.entries, e)
	q.byGID[e.gid] = e
}

/*
================
legacyBehaviorQueue.Pop
================
*/
func (q *legacyBehaviorQueue) Pop() any {
	n := len(q.entries) - 1
	e := q.entries[n]
	q.entries[n] = nil
	q.entries = q.entries[:n]
	delete(q.byGID, e.gid)
	return e
}

/*
================
legacyBehaviorQueue.set
================
*/
func (q *legacyBehaviorQueue) set(gid uint32, at int64) {
	if q.byGID == nil {
		q.byGID = make(map[uint32]*legacyBehaviorEntry)
	}
	if e := q.byGID[gid]; e != nil {
		e.at = at
		heap.Fix(q, e.index)
		return
	}
	heap.Push(q, &legacyBehaviorEntry{gid: gid, at: at})
}

/*
================
legacyBehaviorQueue.remove
================
*/
func (q *legacyBehaviorQueue) remove(gid uint32) {
	if e := q.byGID[gid]; e != nil {
		heap.Remove(q, e.index)
	}
}

/*
================
TestBehaviorQueueDueSetMatchesLegacyHeap

A random walk of schedules (into the past, the ring and the far list),
removals and drains. At each drain the oracle pops its due and, like
scheduleBehavior, re-arms each popped actor past now; the wheel's
takeDue must return the same set, each gid once. Live dues are
compared in full every 997 steps.
================
*/
func TestBehaviorQueueDueSetMatchesLegacyHeap(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	var got behaviorQueue
	var want legacyBehaviorQueue
	now := int64(0)
	fired := 0
	for step := 0; step < 200000; step++ {
		gid := uint32(1 + r.Intn(400))
		switch op := r.Intn(10); {
		case op < 5:
			at := now - 100 + int64(r.Intn(9000))
			got.set(gid, at)
			want.set(gid, at)
		case op < 7:
			got.remove(gid)
			want.remove(gid)
		default:
			type rearm struct {
				gid uint32
				at  int64
			}
			var rearms []rearm
			wantDue := make(map[uint32]bool)
			for want.Len() > 0 && want.entries[0].at <= now {
				e := heap.Pop(&want).(*legacyBehaviorEntry)
				wantDue[e.gid] = true
				rearms = append(rearms, rearm{e.gid, e.at + 1 + int64(r.Intn(50))})
			}
			gotDue := got.takeDue(now, nil)
			if len(gotDue) != len(wantDue) {
				t.Fatalf("step %d: wheel fired %d, heap popped %d", step, len(gotDue), len(wantDue))
			}
			seen := make(map[uint32]bool, len(gotDue))
			for _, ref := range gotDue {
				if !wantDue[ref.gid] || seen[ref.gid] || ref.slot != behaviorNoMoverSlot || ref.monsterSlot != behaviorNoMonsterSlot {
					t.Fatalf("step %d: wheel fired ref %+v, heap set %v", step, ref, wantDue)
				}
				seen[ref.gid] = true
			}
			fired += len(gotDue)
			for _, a := range rearms {
				got.set(a.gid, a.at)
				want.set(a.gid, a.at)
			}
			now += int64(r.Intn(120))
		}
		if got.Len() != want.Len() {
			t.Fatalf("step %d: %d scheduled, want %d", step, got.Len(), want.Len())
		}
		if step%997 == 0 {
			if len(got.byGID) != len(want.byGID) {
				t.Fatalf("step %d: %d live dues, want %d", step, len(got.byGID), len(want.byGID))
			}
			for g, e := range want.byGID {
				slot, ok := got.byGID[g]
				if !ok || got.slots[slot].gid != g || !got.slots[slot].live || got.slots[slot].at != e.at {
					t.Fatalf("step %d: gid %d due at %v, want %d", step, g, got.byGID[g], e.at)
				}
			}
		}
	}
	if fired < 10000 {
		t.Fatalf("only %d actors fired in the walk", fired)
	}
}

/*
================
TestBehaviorQueueUnchangedDeadlineStaysSingleCopy

Re-scheduling a live row at the deadline it already holds is the
leg's steady state (every commit-side reschedule of the batch's own
at recomputes the same value). It must not stack copies on the
wheel: the copy placed first already names the due, and every
duplicate is a copy the next drain walks and drops (#2136). The due
set itself stays the heap oracle's, pinned above.
================
*/
func TestBehaviorQueueUnchangedDeadlineStaysSingleCopy(t *testing.T) {
	var q behaviorQueue
	placed := func() int {
		n := len(q.due) + len(q.far)
		for _, bucket := range q.buckets {
			n += len(bucket)
		}
		return n
	}
	q.set(7, 1000)
	q.set(7, 1000)
	q.set(7, 1000)
	if got := placed(); got != 1 {
		t.Fatalf("unchanged-deadline reschedules placed %d copies, want 1", got)
	}
	if due := q.takeDue(999, nil); len(due) != 0 {
		t.Fatalf("fired before its deadline: %v", due)
	}
	if due := q.takeDue(1000, nil); len(due) != 1 || due[0].gid != 7 {
		t.Fatalf("deadline fired %v, want gid 7 once", due)
	}
	// A fired row re-scheduled at the same value is a fresh due again.
	q.set(7, 1000)
	if due := q.takeDue(1000, nil); len(due) != 1 || due[0].gid != 7 {
		t.Fatalf("revived deadline fired %v, want gid 7 once", due)
	}

	var deferred behaviorQueue
	deferred.set(8, 500)
	deferred.set(8, 1500)
	if due := deferred.takeDue(1000, nil); len(due) != 0 {
		t.Fatalf("deferred row fired early: %v", due)
	}
	if due := deferred.takeDue(1500, nil); len(due) != 1 || due[0].gid != 8 {
		t.Fatalf("deferred deadline fired %v, want gid 8 once", due)
	}
}

/*
================
BenchmarkBehaviorQueueRescheduleUnchanged

The commit-side reschedule pattern: rows already queued, each
re-set at the very deadline the batch build placed (#2136).
================
*/
func BenchmarkBehaviorQueueRescheduleUnchanged(b *testing.B) {
	var q behaviorQueue
	const rows = 256
	for gid := uint32(1); gid <= rows; gid++ {
		q.set(gid, 5000)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for gid := uint32(1); gid <= rows; gid++ {
			q.set(gid, 5000)
		}
	}
}
