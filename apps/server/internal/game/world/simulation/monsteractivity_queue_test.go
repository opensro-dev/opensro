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
	"cmp"
	"container/heap"
	"math/rand"
	"slices"
	"testing"

	"opensro.online/server/internal/game/world/monster"
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
TestBehaviorBatchBuildAllocations

The batch build rides pooled refs backings: warmed, a build plus
its release allocates a small fixed handful (the batch list, the
population keys), not a fresh refs backing grown from empty for
every due actor - the wheel drain's largest steady allocation
before the pool (#2030; the leg profile charged
behaviorQueue.filterDue's appends 50 MB of a 6,000-leg window, and
the unpooled build measures 19 allocations here against the
pooled 7). The window runs warm first; the pool is only empty once.
================
*/
func TestBehaviorBatchBuildAllocations(t *testing.T) {
	s, _ := queueBenchmarkState(1024)
	tick := int64(0)
	build := func() {
		got := s.behaviorBatches(tick)
		if len(got) != 1 || len(got[0].actors) != 1024 {
			t.Fatalf("batch build lost actors: %d batches", len(got))
		}
		// The leg's full batch cost: arm the remainder it left,
		// then hand the backings back.
		s.armBehaviorRemainders(got, tick)
		tick++
		s.releaseBehaviorBatches(got)
	}
	build()
	fewest := testing.AllocsPerRun(50, build)
	for range 2 {
		if got := testing.AllocsPerRun(50, build); got < fewest {
			fewest = got
		}
	}
	if fewest > 10 {
		t.Fatalf("batch build allocates %.2f per run in steady state, want at most 10", fewest)
	}
}

/*
================
TestSortBehaviorRefsMatchesComparisonSort

The batch build orders its refs by gid in linear time; the order
must be the comparison sort's exactly, at every batch size -
across the counting threshold - and across scratch reuse (the
pooled backing outlives its batch, and the next build sorts a
different set in it). Gids are drawn unique, the queue's own
contract (byGID), from one deterministic shuffle.
================
*/
func TestSortBehaviorRefsMatchesComparisonSort(t *testing.T) {
	rng := rand.New(rand.NewSource(3027))
	scratch := &behaviorRefScratch{}
	for _, n := range []int{0, 1, 2, 63, 64, 127, 128, 129, 255, 600, 1024, 5000} {
		gids := make([]uint32, n)
		for i := range gids {
			gids[i] = uint32(100000+i*7) | uint32(i%3)<<24
		}
		rng.Shuffle(n, func(i, j int) { gids[i], gids[j] = gids[j], gids[i] })
		refs := make([]behaviorActorRef, n)
		for i, gid := range gids {
			refs[i] = behaviorActorRef{gid: gid, queueSlot: uint32(i), slot: uint32(n - i), monsterSlot: uint32(i * 3)}
		}
		want := make([]behaviorActorRef, n)
		copy(want, refs)
		slices.SortFunc(want, func(a, b behaviorActorRef) int { return cmp.Compare(a.gid, b.gid) })
		sortBehaviorRefs(refs, scratch)
		for i := range want {
			if refs[i] != want[i] {
				t.Fatalf("n=%d: refs[%d] = %+v, want %+v", n, i, refs[i], want[i])
			}
		}
	}

	// The two drain shapes the ordered pass special-cases, at
	// sizes on both sides of the threshold: an already ordered
	// batch (a population armed in spawn order) and a reversed
	// one (a descent on the first pair) must both come out in
	// the comparison sort's order.
	for _, n := range []int{2, 63, 127, 128, 600} {
		refs := make([]behaviorActorRef, n)
		for i := range refs {
			refs[i] = behaviorActorRef{gid: uint32(100000 + i*7), queueSlot: uint32(i)}
		}
		want := make([]behaviorActorRef, n)
		copy(want, refs)
		sortBehaviorRefs(refs, scratch)
		for i := range want {
			if refs[i] != want[i] {
				t.Fatalf("ordered n=%d: refs[%d] = %+v, want %+v", n, i, refs[i], want[i])
			}
		}
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			refs[i], refs[j] = refs[j], refs[i]
		}
		want = make([]behaviorActorRef, n)
		copy(want, refs)
		slices.SortFunc(want, func(a, b behaviorActorRef) int { return cmp.Compare(a.gid, b.gid) })
		sortBehaviorRefs(refs, scratch)
		for i := range want {
			if refs[i] != want[i] {
				t.Fatalf("reversed n=%d: refs[%d] = %+v, want %+v", n, i, refs[i], want[i])
			}
		}
	}
}

/*
================
TestBehaviorBuildLeavesRemainderToArm

The build consumes dues without re-arming: a popped actor stays
unscheduled until the dispatcher arms the remainder, an actor a
door re-armed during its dispatch keeps the door's due (the
remainder must not overwrite it with the carried computation), and
the rest land on the carried arm's own due.
================
*/
func TestBehaviorBuildLeavesRemainderToArm(t *testing.T) {
	s, d := queueBenchmarkState(3)
	batches := s.behaviorBatches(0)
	if len(batches) != 1 || len(batches[0].actors) != 3 {
		t.Fatalf("first build batched %v, want 3 actors", batches)
	}
	if next := s.behaviorBatches(1); len(next[0].actors) != 0 {
		t.Fatalf("build re-armed %v; the remainder is the dispatcher's to arm", next[0].actors)
	}
	// Actor 1's step re-arms it through a door, far past the
	// carried due: the remainder leaves the door's due standing.
	d.behavior.set(1, 5000)
	s.armBehaviorRemainders(batches, 0)
	next := s.behaviorBatches(1)
	if len(next) != 1 || len(next[0].actors) != 2 {
		t.Fatalf("remainder build batched %v, want the 2 carried actors", next)
	}
	for _, ref := range next[0].actors {
		if ref.gid == 1 {
			t.Fatal("remainder overwrote the door's due for actor 1")
		}
	}
	next = s.behaviorBatches(5000)
	if len(next) != 1 || len(next[0].actors) != 1 || next[0].actors[0].gid != 1 {
		t.Fatalf("door-armed actor fired as %v, want actor 1 alone at its door due", next)
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
TestBehaviorQueueDrainReadsTheRowItFires

The drain answers every copy from the one queue row it reads: a
duplicate copy of a firing slot fires no second time, a copy left
stale by a later re-arm does not fire at its old time (through the
buckets or the far list), a not-yet-due row survives, and a fired
ref carries its row's slots with the row left dead (#3027).
================
*/
func TestBehaviorQueueDrainReadsTheRowItFires(t *testing.T) {
	var q behaviorQueue
	q.set(1, 100)
	q.set(2, 5000)
	slot1 := q.byGID[1]
	slot2 := q.byGID[2]
	// A second copy of the firing slot, and a stale copy of the
	// re-armed one, injected the way the wheel leaves them behind.
	q.due = append(q.due, behaviorCopy{slot: slot1, at: 100}, behaviorCopy{slot: slot2, at: 100})
	due := q.takeDue(100, nil)
	if len(due) != 1 || due[0].gid != 1 || due[0].queueSlot != slot1 {
		t.Fatalf("drain fired %v, want gid 1 once through its slot", due)
	}
	if q.slots[slot1].live {
		t.Fatal("fired row stayed live")
	}
	if !q.slots[slot2].live || q.slots[slot2].at != 5000 {
		t.Fatal("not-yet-due row changed under the drain")
	}
	// The far list answers from the same one read per copy.
	var far behaviorQueue
	far.set(9, 100000)
	farSlot := far.byGID[9]
	far.far = append(far.far, behaviorCopy{slot: farSlot, at: 100})
	if due := far.takeDue(100, nil); len(due) != 0 {
		t.Fatalf("stale far copy fired: %v", due)
	}
	if due := far.takeDue(100000, nil); len(due) != 1 || due[0].gid != 9 || due[0].queueSlot != farSlot {
		t.Fatalf("far drain fired %v, want gid 9 once through its slot", due)
	}
	if far.slots[farSlot].live {
		t.Fatal("far fired row stayed live")
	}
}

/*
================
TestBehaviorQueueSetAtCarriesAndRecycles

The drain hands each fired actor's queue slot back in its ref, and
the batch build re-arms through setAt without resolving the gid
through byGID again (#2133). The carried slot is verified against
the gid before use: a slot recycled onto another actor must not
take the re-arm - the asked actor is scheduled by the gid resolve
and the slot owner's own due survives. The storage slots noted on
the re-arm ride the queue slot to the next drain's ref, and a plain
set re-arm (the commit path) leaves the notes alone.
================
*/
func TestBehaviorQueueSetAtCarriesAndRecycles(t *testing.T) {
	var q behaviorQueue
	q.set(1, 100)
	q.set(2, 100)
	due := q.takeDue(100, nil)
	if len(due) != 2 {
		t.Fatalf("drain fired %v, want gids 1 and 2", due)
	}
	refs := make(map[uint32]behaviorActorRef, len(due))
	for _, ref := range due {
		if ref.queueSlot == behaviorNoQueueSlot || q.slots[ref.queueSlot].gid != ref.gid {
			t.Fatalf("drained ref %+v does not carry its queue slot", ref)
		}
		refs[ref.gid] = ref
	}
	// Re-arm gid 1 through its carried slot, noting storage slots;
	// the same re-arm again keeps the one placed copy.
	noted := refs[1]
	noted.slot = 7
	noted.monsterSlot = 9
	q.setAt(noted, 200)
	q.setAt(noted, 200)
	// Retire gid 2, then let gid 3 enter: it may take the freed slot.
	q.removeAt(refs[2].queueSlot, 2)
	q.set(3, 300)
	// A stale re-arm for gid 2 through its old slot - recycled or
	// not - must schedule gid 2 by gid and leave gid 3's due alone.
	q.setAt(refs[2], 400)
	if q.Len() != 3 {
		t.Fatalf("%d actors scheduled, want 3", q.Len())
	}
	fired := make(map[uint32]behaviorActorRef)
	for _, ref := range q.takeDue(400, nil) {
		if _, dup := fired[ref.gid]; dup {
			t.Fatalf("gid %d fired twice in one drain", ref.gid)
		}
		fired[ref.gid] = ref
	}
	if len(fired) != 3 {
		t.Fatalf("drain fired %v, want gids 1, 2 and 3 once each", fired)
	}
	if ref := fired[1]; ref.slot != 7 || ref.monsterSlot != 9 {
		t.Fatalf("gid 1 ref carries slots (%d, %d), want the noted (7, 9)", ref.slot, ref.monsterSlot)
	}
	if ref := fired[3]; ref.slot != behaviorNoMoverSlot || ref.monsterSlot != behaviorNoMonsterSlot {
		t.Fatalf("gid 3 ref carries slots (%d, %d), want no carried slots", ref.slot, ref.monsterSlot)
	}
}

/*
================
TestBehaviorQueueSetAtSlotVerified

setAtSlot arms through the carried queue slot alone: the row
keeps the storage slots it noted and a same-at re-arm places no
second copy. A slot that names another actor, and the
no-queue-slot sentinel, delegate to the gid set, so the asked
actor is scheduled and the slot owner's due is untouched
(#3027).
================
*/
func TestBehaviorQueueSetAtSlotVerified(t *testing.T) {
	var q behaviorQueue
	q.set(1, 100)
	q.set(2, 100)
	due := q.takeDue(100, nil)
	if len(due) != 2 {
		t.Fatalf("drain fired %v, want gids 1 and 2", due)
	}
	refs := make(map[uint32]behaviorActorRef, len(due))
	for _, ref := range due {
		refs[ref.gid] = ref
	}
	// Note storage slots on gid 1's row, as a setAt re-arm would.
	noted := refs[1]
	noted.slot = 7
	noted.monsterSlot = 9
	q.setAt(noted, 200)

	q.setAtSlot(noted.queueSlot, 1, 200)
	copies := 0
	countCopies := func(list []behaviorCopy) {
		for _, e := range list {
			if e.slot == noted.queueSlot {
				copies++
			}
		}
	}
	countCopies(q.due)
	countCopies(q.far)
	for i := range q.buckets {
		countCopies(q.buckets[i])
	}
	if copies != 1 {
		t.Fatalf("same-at setAtSlot left %d copies of gid 1's slot, want the one placed copy", copies)
	}

	q.setAtSlot(noted.queueSlot, 1, 300)
	if q.byGID[1] != noted.queueSlot {
		t.Fatalf("gid 1 resolved to slot %d, want its carried slot %d", q.byGID[1], noted.queueSlot)
	}
	if row := q.slots[noted.queueSlot]; row.at != 300 || !row.live || row.moverSlot != 7 || row.monsterSlot != 9 {
		t.Fatalf("gid 1 row %+v, want due 300 live with the noted slots (7, 9)", row)
	}

	// Another actor's slot: gid 2 arms by gid, gid 1 keeps its due.
	q.setAtSlot(noted.queueSlot, 2, 400)
	if got := q.slots[q.byGID[2]].at; got != 400 {
		t.Fatalf("gid 2 due %d, want 400 from the delegated set", got)
	}
	if row := q.slots[noted.queueSlot]; row.at != 300 {
		t.Fatalf("gid 1 row due %d, want its untouched 300", row.at)
	}
	// The sentinel delegates the same way.
	q.setAtSlot(behaviorNoQueueSlot, 2, 500)
	if got := q.slots[q.byGID[2]].at; got != 500 {
		t.Fatalf("gid 2 due %d, want 500 from the sentinel's delegated set", got)
	}

	fired := make(map[uint32]int)
	for _, ref := range q.takeDue(500, nil) {
		fired[ref.gid]++
	}
	if len(fired) != 2 || fired[1] != 1 || fired[2] != 1 {
		t.Fatalf("drain fired %v, want gids 1 and 2 once each", fired)
	}
}

/*
================
TestBehaviorQueueRemoveAtVerified

removeAt through the ref's queue slot retires its own actor; a slot
that names another actor delegates to the gid remove, so the asked
actor retires and the slot owner keeps its due (#2133).
================
*/
func TestBehaviorQueueRemoveAtVerified(t *testing.T) {
	var q behaviorQueue
	q.set(1, 100)
	q.set(2, 100)
	slotOf := func(gid uint32) uint32 {
		t.Helper()
		slot, ok := q.byGID[gid]
		if !ok {
			t.Fatalf("gid %d not scheduled", gid)
		}
		return slot
	}
	// Another actor's slot: gid 2 retires by gid, gid 1 keeps its due.
	q.removeAt(slotOf(1), 2)
	if q.Len() != 1 {
		t.Fatalf("%d actors scheduled, want 1", q.Len())
	}
	if due := q.takeDue(100, nil); len(due) != 1 || due[0].gid != 1 {
		t.Fatalf("drain fired %v, want gid 1 alone", due)
	}
	// A removed actor's slot is unnamed again: it cannot verify for
	// anyone, so a carried re-arm through it resolves by gid.
	q.set(1, 100)
	q.set(2, 100)
	stale := slotOf(2)
	q.removeAt(stale, 2)
	q.setAt(behaviorActorRef{gid: 2, queueSlot: stale, slot: behaviorNoMoverSlot, monsterSlot: behaviorNoMonsterSlot}, 100)
	if due := q.takeDue(100, nil); len(due) != 2 {
		t.Fatalf("drain fired %v, want gids 1 and 2", due)
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

/*
================
idleScheduleFixture

A division holding one idle monster per gid in gids: alive, at rest
in its nest, gate cadence armed at now with the given interval and
idle deadline. scheduleDue reads the live due the queue holds.
================
*/
func idleScheduleFixture(t *testing.T) (*MonsterState, *divisionMonsterState) {
	t.Helper()
	s := NewMonsterState(monster.Template{})
	s.StartDivision("sleep")
	d := s.divs["sleep"]
	d.movers = newMoverStorage(nil)
	return s, d
}

func idleScheduleActor(t *testing.T, s *MonsterState, d *divisionMonsterState, gid uint32, nest monster.NestRow, gateInterval uint16, deadlineMs, now int64) monster.Instance {
	t.Helper()
	actor := monster.Instance{Gid: gid, Ref: monster.MonsterRef{MaxHP: 100}, Nest: nest, CurrentHP: 100}
	mover := monster.PendingMover{Spawning: true, Pose: monster.Pose{RegionID: 0x6060}}.Expand()
	if err := mover.Transition(monster.MoverEventSpawnHoldElapsed, 0); err != nil {
		t.Fatal(err)
	}
	// The entry the spawn transition armed is the first step's to
	// commit; the fixture models the resting monster after it.
	mover.TakeIdleEntry()
	mover.Activity = monster.ActivityCadence{Interval: gateInterval, LastCheck: uint32(now)}
	mover.BehaviorDeadlineMs = deadlineMs
	d.instances.set(gid, actor)
	d.movers.set(gid, mover)
	return actor
}

func scheduleDue(t *testing.T, d *divisionMonsterState, gid uint32) int64 {
	t.Helper()
	slot, ok := d.behavior.byGID[gid]
	if !ok {
		t.Fatalf("gid %d holds no queue row", gid)
	}
	return d.behavior.slots[slot].at
}

/*
================
TestIdleMonsterSchedulesAtItsRealDue

#2136: an idle monster with nothing due re-armed for the next tick,
so every leg snapshotted and stepped it to no effect. Its next due
is the earliest of its idle deadline, the arrival of a segment it
still glides on and, when its nest is aggressive, its acquisition
timer; a mover that is not simply at rest (here: wandering, or
holding a navigation goal) keeps the next tick. The activity-gate
cadence is not a due for an idle monster: the gate reads it only
for a wandering or pending one, so it wakes an idle monster only
while a replacement tactics resolver hides the step's aggression
from the scheduler. Event lanes still wake a sleeping actor at
once (the impact, help and hostility doors set its due to 0).
================
*/
func TestIdleMonsterSchedulesAtItsRealDue(t *testing.T) {
	const now int64 = 100000
	s, d := idleScheduleFixture(t)
	resting := monster.NestRow{Respawn: true}

	// Passive, gate cadence nearer than the deadline: the gate does
	// not read an idle monster's cadence, so the due is the deadline.
	idleScheduleActor(t, s, d, 1, resting, 2000, now+5000, now)
	s.scheduleBehavior(d, 1, now)
	if got, want := scheduleDue(t, d, 1), now+5001; got != want {
		t.Fatalf("passive idle due = %d, want the idle deadline %d", got, want)
	}
	// The same row with its cadence long lapsed: still the deadline,
	// never the next tick.
	lapsed, _ := d.movers.lookup(1)
	lapsed.Activity.LastCheck = uint32(now) - 30000
	d.movers.set(1, lapsed)
	s.scheduleBehavior(d, 1, now)
	if got, want := scheduleDue(t, d, 1), now+5001; got != want {
		t.Fatalf("lapsed-cadence idle due = %d, want the idle deadline %d", got, want)
	}
	// Under a replacement tactics resolver the cadence wake stands:
	// lapsed is the next tick, armed is the gate due.
	s.noteTacticsReplaced(true)
	s.scheduleBehavior(d, 1, now)
	if got, want := scheduleDue(t, d, 1), now+1; got != want {
		t.Fatalf("replaced-resolver lapsed due = %d, want the next tick %d", got, want)
	}
	lapsed.Activity.LastCheck = uint32(now)
	d.movers.set(1, lapsed)
	s.scheduleBehavior(d, 1, now)
	if got, want := scheduleDue(t, d, 1), now+2001; got != want {
		t.Fatalf("replaced-resolver idle due = %d, want the gate due %d", got, want)
	}
	s.noteTacticsReplaced(false)

	// Idle but still gliding (a wander that expired mid-flight): due
	// at the arrival, where the step settles it; matured and not yet
	// settled, due at once.
	idleScheduleActor(t, s, d, 5, resting, 2000, now+5000, now)
	gliding, _ := d.movers.lookup(5)
	gliding.From, gliding.To = gliding.Pose, gliding.Pose
	gliding.To.X += 40
	gliding.DepartMs, gliding.ArriveMs = now-500, now+1200
	d.movers.set(5, gliding)
	s.scheduleBehavior(d, 5, now)
	if got, want := scheduleDue(t, d, 5), now+1200; got != want {
		t.Fatalf("gliding idle due = %d, want the arrival %d", got, want)
	}
	s.scheduleBehavior(d, 5, now+1300)
	if got, want := scheduleDue(t, d, 5), now+1301; got != want {
		t.Fatalf("matured idle due = %d, want the next tick %d", got, want)
	}

	// A held navigation goal: the step commits its next segment.
	idleScheduleActor(t, s, d, 6, resting, 2000, now+5000, now)
	routed, _ := d.movers.lookup(6)
	routed.WaitNavigation(routed.Pose, now+1000)
	d.movers.set(6, routed)
	s.scheduleBehavior(d, 6, now)
	if got, want := scheduleDue(t, d, 6), now+1; got != want {
		t.Fatalf("held-goal idle due = %d, want the next tick %d", got, want)
	}

	// Passive, idle deadline nearest: due at the deadline.
	idleScheduleActor(t, s, d, 2, resting, 9000, now+3000, now)
	s.scheduleBehavior(d, 2, now)
	if got, want := scheduleDue(t, d, 2), now+3001; got != want {
		t.Fatalf("passive idle due = %d, want the idle deadline %d", got, want)
	}

	// Aggressive: the acquisition timer is nearest.
	aggressive := monster.NestRow{Respawn: true, Aggressive: true, PolicyPinned: true}
	actor := idleScheduleActor(t, s, d, 3, aggressive, 2000, now+5000, now)
	timers := s.aiTimersLocked(d, actor, now)
	timers.SetTimer(monster.TimerIDAcquisition, 800, 0, uint32(now), false, func() uint32 { return 0 })
	timers.CheckTimer(monster.TimerIDAcquisition, uint32(now))
	s.scheduleBehavior(d, 3, now)
	if got, want := scheduleDue(t, d, 3), now+800; got != want {
		t.Fatalf("aggressive idle due = %d, want the acquisition due %d", got, want)
	}

	// Wandering is not at rest: the next tick, as before.
	wanderer := idleScheduleActor(t, s, d, 4, resting, 2000, now+5000, now)
	mover, _ := d.movers.lookup(wanderer.Gid)
	if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
		t.Fatal(err)
	}
	d.movers.set(wanderer.Gid, mover)
	s.scheduleBehavior(d, 4, now)
	if got, want := scheduleDue(t, d, 4), now+1; got != want {
		t.Fatalf("wandering due = %d, want the next tick %d", got, want)
	}
}

/*
================
TestQueuedTacticsEventWakesTheActor

A fear or confusion outcome queues a tactics event for the step to
consume; until its step the event just sits there. Queuing one is a
wake like the other event doors (impact, help, hostility): an actor
asleep until a far due fires at once, not at that due (#2136).
================
*/
func TestQueuedTacticsEventWakesTheActor(t *testing.T) {
	const now int64 = 100000
	s, d := idleScheduleFixture(t)
	idleScheduleActor(t, s, d, 1, monster.NestRow{Respawn: true}, 2000, now+5000, now)
	s.scheduleBehavior(d, 1, now)
	d.behavior.set(1, now+60000)
	d.queueAIEvents(1, []monsterAIEvent{{Event: aiEventStart, Kind: 9, Source: 7}})
	if got := scheduleDue(t, d, 1); got > now {
		t.Fatalf("queued tactics event left the actor due at %d, want a wake by %d", got, now)
	}
}
