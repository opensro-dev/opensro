/*
===========================================================================

monsteractivity_queue.go - the monster behaviour queue and its due batches

Every materialized actor has one update entry, due at a millisecond
time. PENDING sleeps until its activity deadline (or earlier movement
arrival); IDLE sleeps until its earliest real due (#2136); combat
events reschedule the same entry. No viewer ring owns actor liveness.

The queue is a timer wheel: buckets of 8 ms over a 4096 ms ring, an
immediate list for dues in the past and a far list for dues beyond the
ring (the activity cadence tops out near 2 s, so the far list is a
safety path, not the common one). set only appends and records the live
due in byGID; the copies an earlier set left behind are stale the
moment byGID moves and die when their bucket drains. Scheduling is
O(1); the old binary heap's sift (less/swap per level, per actor, per
tick) was the measured cost this replaces (#2135). The batch sorts its
actors by gid, so the order monsters act in never depended on the
container - only the due set did, and the wheel keeps it exact.
The doors that arm the queue (the scheduleBehavior family and the
due computation they share) live in monsteractivity_schedule.go.

===========================================================================
*/

package simulation

import (
	"cmp"
	"slices"
	"sort"
	"sync"

	"opensro.online/server/internal/game/world/monster"
)

const (
	// behaviorBucketMs is the wheel's bucket width; behaviorBucketCount
	// buckets ring a horizon a little past twice the longest activity
	// cadence (NewActivityCadence draws 1000-1999 ms).
	behaviorBucketMs    = 8
	behaviorBucketCount = 512
	behaviorHorizonMs   = behaviorBucketMs * behaviorBucketCount

	// behaviorNoMoverSlot marks a carried actor ref with no mover
	// slot: the actor had no mover row at batch build, or no slot
	// was carried at all. The dispatch read resolves it by gid.
	behaviorNoMoverSlot = ^uint32(0)

	// behaviorNoMonsterSlot marks a carried actor ref with no
	// monster slot: the batch build has not stamped one yet. The
	// dispatch read resolves it by gid.
	behaviorNoMonsterSlot = ^uint32(0)

	// behaviorNoBaseGen marks a commitActivityAt caller with no
	// snapshot generation in hand (a lane door whose before was
	// read fresh): the commit guards on the whole row value, as
	// every commit did before the generation guard (#3027).
	behaviorNoBaseGen = ^uint64(0)

	// behaviorNoQueueSlot marks an actor ref built without the
	// drain (a hand-built ref): its queue doors resolve by gid.
	behaviorNoQueueSlot = ^uint32(0)
)

/*
================
behaviorCopy

One placement of a slot in a bucket list. A copy is live only while
its slot is live and still holds the same at; rescheduling leaves the
old copies stale.
================
*/
type behaviorCopy struct {
	slot uint32
	at   int64
}

/*
================
behaviorSlot

One scheduled actor's dense queue row. live is false once the actor
fires or is removed, until set revives the row; copies naming a dead
row are stale whatever at they carry. moverSlot and monsterSlot are
the actor's storage slots as the last batch build resolved them,
carried leg to leg so the next build reads the rows through the
slots instead of resolving the gid through the storage maps again
(#2133); the dispatch reads verify each against the gid before use,
so a row that moved since costs one gid resolve, never a wrong read.
================
*/
type behaviorSlot struct {
	gid         uint32
	at          int64
	live        bool
	moverSlot   uint32
	monsterSlot uint32
}

/*
================
behaviorQueue

The wheel. byGID maps a gid to its slot and is written only when an
actor enters or leaves the queue; the batch build's re-arm verifies
the slot the drain carried (setAt) and reads byGID only when the
carry misses, and the drain itself walks bucket lists of
behaviorCopy and the slot array, no map traffic. now is the last
drain time and the ring's cursor base.
================
*/
type behaviorQueue struct {
	slots   []behaviorSlot
	free    []uint32
	buckets [behaviorBucketCount][]behaviorCopy
	far     []behaviorCopy
	farMin  int64
	due     []behaviorCopy
	byGID   map[uint32]uint32
	now     int64
}

/*
================
behaviorBucketIndex

The ring bucket a due time falls in.
================
*/
func behaviorBucketIndex(at int64) int {
	return int(at/behaviorBucketMs) % behaviorBucketCount
}

/*
================
behaviorQueue.Len

The scheduled actor count: one live slot per gid.
================
*/
func (q *behaviorQueue) Len() int { return len(q.byGID) }

/*
================
behaviorQueue.place

Appends the slot's copy to the list that holds its due: the immediate
list at or before the last drain, its ring bucket inside the horizon,
the far list beyond it.
================
*/
func (q *behaviorQueue) place(slot uint32, at int64) {
	e := behaviorCopy{slot: slot, at: at}
	switch {
	case at <= q.now:
		q.due = append(q.due, e)
	case at < q.now+behaviorHorizonMs:
		i := behaviorBucketIndex(at)
		q.buckets[i] = append(q.buckets[i], e)
	default:
		if len(q.far) == 0 || at < q.farMin {
			q.farMin = at
		}
		q.far = append(q.far, e)
	}
}

/*
================
behaviorQueue.set

Schedules gid at at: revive (or take) its slot, stamp the due, place
a copy. One byGID read for a known actor; the map is written only for
an actor entering the queue. Copies of earlier dues go stale in place.
A live slot already holding at keeps the copy it placed: re-placing
would only stack a duplicate for the next drain to drop (#2136).
================
*/
func (q *behaviorQueue) set(gid uint32, at int64) {
	if q.byGID == nil {
		q.byGID = make(map[uint32]uint32)
	}
	slot, ok := q.byGID[gid]
	if !ok {
		if n := len(q.free); n > 0 {
			slot = q.free[n-1]
			q.free = q.free[:n-1]
		} else {
			slot = uint32(len(q.slots))
			q.slots = append(q.slots, behaviorSlot{})
		}
		q.byGID[gid] = slot
		q.slots[slot] = behaviorSlot{moverSlot: behaviorNoMoverSlot, monsterSlot: behaviorNoMonsterSlot}
	} else if held := q.slots[slot]; held.live && held.at == at {
		return
	}
	held := q.slots[slot]
	q.slots[slot] = behaviorSlot{gid: gid, at: at, live: true, moverSlot: held.moverSlot, monsterSlot: held.monsterSlot}
	q.place(slot, at)
}

/*
================
behaviorQueue.setAt

set through the ref the drain handed out for the actor (#2133):
when the ref's queue slot still names the actor the re-arm lands on
that row with no byGID probe, and the actor's storage slots are
noted back onto the row for the next leg's drain. A slot recycled
onto another actor, or the no-queue-slot sentinel, delegates to set
and notes the slots on the row set resolved, so the schedule is the
queue's either way.
================
*/
func (q *behaviorQueue) setAt(ref behaviorActorRef, at int64) {
	slot := ref.queueSlot
	if slot >= uint32(len(q.slots)) || q.slots[slot].gid != ref.gid {
		q.set(ref.gid, at)
		if resolved, ok := q.byGID[ref.gid]; ok {
			q.slots[resolved].moverSlot = ref.slot
			q.slots[resolved].monsterSlot = ref.monsterSlot
		}
		return
	}
	if held := q.slots[slot]; held.live && held.at == at {
		q.slots[slot].moverSlot = ref.slot
		q.slots[slot].monsterSlot = ref.monsterSlot
		return
	}
	q.slots[slot] = behaviorSlot{gid: ref.gid, at: at, live: true, moverSlot: ref.slot, monsterSlot: ref.monsterSlot}
	q.place(slot, at)
}

/*
================
behaviorQueue.setAtSlot

set through a carried queue slot alone, verified against the gid
before use (#2133): the activity commit's re-arm holds the slot
its snapshot carried but no storage-slot answer fresher than the
row's own note, so the row keeps the mover and monster slots it
noted - the gid set's preservation, without its byGID probe
(#3027). A slot recycled onto another actor, or the
no-queue-slot sentinel, delegates to set, so the asked actor is
scheduled either way.
================
*/
func (q *behaviorQueue) setAtSlot(queueSlot uint32, gid uint32, at int64) {
	if queueSlot >= uint32(len(q.slots)) || q.slots[queueSlot].gid != gid {
		q.set(gid, at)
		return
	}
	held := q.slots[queueSlot]
	if held.live && held.at == at {
		return
	}
	q.slots[queueSlot] = behaviorSlot{gid: gid, at: at, live: true, moverSlot: held.moverSlot, monsterSlot: held.monsterSlot}
	q.place(queueSlot, at)
}

/*
================
behaviorQueue.removeAt

remove through a carried queue slot, verified against the gid
before use (#2133): a slot that names another actor delegates to
remove, so the asked actor retires either way and the slot owner's
due is never the price of a stale carry.
================
*/
func (q *behaviorQueue) removeAt(queueSlot uint32, gid uint32) {
	if queueSlot >= uint32(len(q.slots)) || q.slots[queueSlot].gid != gid {
		q.remove(gid)
		return
	}
	delete(q.byGID, gid)
	q.slots[queueSlot] = behaviorSlot{}
	q.free = append(q.free, queueSlot)
}

/*
================
behaviorQueue.remove

Unschedules gid: the slot dies and leaves the queue, staling every
copy at once. The row is zeroed, not just stilled: a queue slot a
drained ref still carries can then never verify for the actor a
later set recycles the row onto (#2133).
================
*/
func (q *behaviorQueue) remove(gid uint32) {
	slot, ok := q.byGID[gid]
	if !ok {
		return
	}
	delete(q.byGID, gid)
	q.slots[slot] = behaviorSlot{}
	q.free = append(q.free, slot)
}

/*
================
behaviorQueue.liveCopy

Whether a bucket copy still names its slot's live due.
================
*/
func (q *behaviorQueue) liveCopy(e behaviorCopy) bool {
	s := q.slots[e.slot]
	return s.live && s.at == e.at
}

/*
================
behaviorQueue.armedAt

Whether the actor's queue row still holds a live due: a fired row
stays dead until a set revives it, so this reads the drain's pop
contract back - true when some door has re-armed the actor since
its due fired. The slot is verified against the gid before use
(#2133): a row recycled onto another actor answers for its new
owner, never for the stale ref.
================
*/
func (q *behaviorQueue) armedAt(queueSlot uint32, gid uint32) bool {
	return queueSlot < uint32(len(q.slots)) && q.slots[queueSlot].gid == gid && q.slots[queueSlot].live
}

/*
================
behaviorQueue.filterDue

One list under drain at now: live copies due by now fire (their slot
dies, so a second copy cannot fire again); live copies not yet due
stay; stale copies drop. Array traffic only.
================
*/
func (q *behaviorQueue) filterDue(list []behaviorCopy, now int64, out *[]behaviorActorRef) []behaviorCopy {
	keep := list[:0]
	for _, e := range list {
		if !q.liveCopy(e) {
			continue
		}
		if e.at > now {
			keep = append(keep, e)
			continue
		}
		q.slots[e.slot].live = false
		row := q.slots[e.slot]
		*out = append(*out, behaviorActorRef{gid: row.gid, queueSlot: e.slot, slot: row.moverSlot, monsterSlot: row.monsterSlot})
	}
	return keep
}

/*
================
behaviorQueue.takeDue

Every actor due by now, as a ref carrying its queue slot and the
storage slots its queue row noted last leg (the batch build verifies
and refreshes them), in no order (callers sort). Their live dues are
consumed: the caller re-arms the ones it keeps (scheduleBehavior) and
the rest stay unscheduled, exactly the heap's pop contract.

Far dues first migrate into the ring (or fire) once the nearest one is
inside the horizon; the ring then sweeps from the last drain's bucket
to now's, or every bucket when the drain gap spans the horizon or time
went backwards. Copies set during the caller's processing land in
future buckets and cannot fire in this drain: scheduleBehavior never
re-arms into the past.
================
*/
func (q *behaviorQueue) takeDue(now int64, out []behaviorActorRef) []behaviorActorRef {
	if len(q.far) > 0 && q.farMin-now <= behaviorHorizonMs {
		rest := q.far[:0]
		var migrate []behaviorCopy
		farMin := int64(0)
		haveFar := false
		for _, e := range q.far {
			if !q.liveCopy(e) {
				continue
			}
			if e.at > now+behaviorHorizonMs {
				rest = append(rest, e)
				if !haveFar || e.at < farMin {
					farMin, haveFar = e.at, true
				}
				continue
			}
			if e.at <= now {
				q.slots[e.slot].live = false
				row := q.slots[e.slot]
				out = append(out, behaviorActorRef{gid: row.gid, queueSlot: e.slot, slot: row.moverSlot, monsterSlot: row.monsterSlot})
				continue
			}
			migrate = append(migrate, e)
		}
		q.far = rest
		q.farMin = farMin
		// Placed only now: place appends to the lists this loop filters.
		for _, e := range migrate {
			q.place(e.slot, e.at)
		}
	}
	q.due = q.filterDue(q.due, now, &out)
	if now < q.now || now-q.now >= behaviorHorizonMs {
		for i := range q.buckets {
			q.buckets[i] = q.filterDue(q.buckets[i], now, &out)
		}
	} else {
		start, end := behaviorBucketIndex(q.now), behaviorBucketIndex(now)
		for i := start; ; i = (i + 1) % behaviorBucketCount {
			q.buckets[i] = q.filterDue(q.buckets[i], now, &out)
			if i == end {
				break
			}
		}
	}
	q.now = now
	return out
}

/*
================
behaviorQueue.earliestDue

The nearest live due, for the dormancy pins and statistics that read
one: a byGID scan, never on the tick's path.
================
*/
func (q *behaviorQueue) earliestDue() (int64, bool) {
	var best int64
	found := false
	for _, s := range q.slots {
		if s.live && (!found || s.at < best) {
			best, found = s.at, true
		}
	}
	return best, found
}

/*
================
behaviorQueue.capacity

Live list entries plus the lists' backing capacity, the excess the
sparse-map compaction shrinks after a mass despawn.
================
*/
func (q *behaviorQueue) capacity() (live int, capacity int) {
	live = len(q.due) + len(q.far)
	capacity = cap(q.due) + cap(q.far) + cap(q.slots) + cap(q.free)
	for i := range q.buckets {
		live += len(q.buckets[i])
		capacity += cap(q.buckets[i])
	}
	return live, capacity
}

/*
================
behaviorQueue.shrinkExcess

Rebuilds the lists with live copies only after a large sustained
contraction, the heap entries shrink's successor in
compactDormantIndexes. Same thresholds: no churn at ordinary region
boundaries. Removal only stales copies, so this is also where a mass
despawn's stale copies leave the buckets.
================
*/
func (q *behaviorQueue) shrinkExcess() {
	live := len(q.byGID)
	_, capacity := q.capacity()
	if capacity < 8192 || live > 4096 || live*4 > capacity {
		return
	}
	slots := make([]behaviorSlot, 0, live)
	for _, s := range q.slots {
		if s.live {
			q.byGID[s.gid] = uint32(len(slots))
			slots = append(slots, s)
		}
	}
	q.slots = slots
	q.free = nil
	q.due = nil
	q.far = nil
	q.farMin = 0
	for i := range q.buckets {
		q.buckets[i] = nil
	}
	for slot, s := range q.slots {
		q.place(uint32(slot), s.at)
	}
}

//============================================================================

/*
================
behaviorActorRef

One due actor as the batch carries it: its gid, the queue slot the
drain fired it from, and the mover slot the build's schedule read
resolved. The slot is a hint for the dispatch read
(moverStorage.lookupAt), verified against the gid before use -
remove swaps the last row into the hole, so a carried slot can
name another actor by dispatch time. behaviorNoMoverSlot means no
slot was carried. The monster slot is the same hint for the actor
read (monsterStorage.lookupAt since the hot rows went dense,
#2133): the drain stamps the queue row's note from last leg, the
build verifies and refreshes it, and a recycled slot misses to the
gid resolve. behaviorNoMonsterSlot means no monster slot was
carried. The queue slot rides back to the re-arm (setAt) and the
retire (removeAt) so neither resolves the gid through byGID again;
behaviorNoQueueSlot marks a ref the drain did not build.
================
*/
type behaviorActorRef struct {
	gid         uint32
	queueSlot   uint32
	slot        uint32
	monsterSlot uint32
}

/*
================
behaviorBatch

One population's due actors for one leg. actors rides a pooled
scratch backing (behaviorRefScratch): the leg drops the batch when
it finishes, and a fresh backing per leg per population was the
wheel drain's largest steady allocation - the append growth from
empty, every leg, for every due actor's ref (#2030). The scratch
returns through MonsterState.releaseBehaviorBatches, which the leg
(monstertick.go) calls once its batches are dispatched; callers
that never release (tests, one-shot reads) simply lose the scratch
to the collector and keep today's caller-owned batches.
================
*/
type behaviorBatch struct {
	key     populationKey
	actors  []behaviorActorRef
	scratch *behaviorRefScratch
}

/*
================
behaviorRefScratch

The pooled backing one batch's refs ride on. A ref is three words
with no pointers, so the backing needs no clearing before reuse.
================
*/
type behaviorRefScratch struct {
	refs []behaviorActorRef
	sort []behaviorActorRef
}

// behaviorRefScratchPool holds built refs backings, ready for the
// next batch. A scratch is checked out by behaviorBatchesForDivision
// and returned by releaseBehaviorBatches once its batch is dead.
var behaviorRefScratchPool = sync.Pool{
	New: func() any { return &behaviorRefScratch{} },
}

/*
================
MonsterState.behaviorActor

Dispatch reads do not advance clocks; the due queue already applied the
tick's summon deadline. A retired population or actor is simply absent.
================
*/
func (s *MonsterState) behaviorActor(key populationKey, gid uint32) (monster.Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(key.division, key.lease)
	if state == nil {
		return monster.Instance{}, false
	}
	actor, ok := state.instances.lookup(gid)
	return actor, ok
}

/*
================
behaviorActorSnapshot

One due actor as the leg dispatch reads it: the instance and its mover
value, resolved together. ok is false when the population or the actor
is gone; the mover is then meaningless.
================
*/
type behaviorActorSnapshot struct {
	instance monster.Instance
	mover    monster.MoverState
	// moverGen is the mover row's commit generation read with the
	// mover (zero when no row exists yet and the mover was
	// synthesized): the base generation the step's commits guard on
	// (#2137 slice 1, CommitMoverGen).
	moverGen uint64
	// slot and monsterSlot are the dense storage slots the batch
	// resolved for this actor, carried from the ref the snapshot was
	// read through (#2133): the step arms them on its ops so its
	// doors (monsterpublish.go) read and commit through the verified
	// hints instead of resolving the gid through the storage maps
	// again. behaviorNoMoverSlot/behaviorNoMonsterSlot when the
	// batch carried none.
	slot        uint32
	monsterSlot uint32
	// queueSlot is the behaviour queue slot the same ref carried:
	// the step arms it with the storage slots so the activity
	// commit's re-arm lands on the queue row through it
	// (setAtSlot) instead of resolving the gid through byGID again
	// (#3027). Meaningful only while ok: a snapshot the fill found
	// gone is zeroed whole.
	queueSlot uint32
	ok        bool
}

/*
================
MonsterState.behaviorActorLive

behaviorActorLiveAt with no slot carried: the gid resolve.
================
*/
func (s *MonsterState) behaviorActorLive(key populationKey, gid uint32) bool {
	return s.behaviorActorLiveAt(key, behaviorActorRef{gid: gid, monsterSlot: behaviorNoMonsterSlot})
}

/*
================
MonsterState.behaviorActorLiveAt

The dispatch's existence probe: whether the population still holds
the actor, without building its instance value. The leg's dispatch
decides only this; the actor itself comes from the batch's leg-start
snapshot (monsterlegsnapshot.go). The probe reads through the
monster slot the batch carried (monsterStorage.containsAt): a slot
that still names the actor answers from its place in the dense
slice, and any other slot resolves by gid, so a despawn between
build and dispatch drops the actor exactly as the gid probe does
(#2133).
================
*/
func (s *MonsterState) behaviorActorLiveAt(key populationKey, ref behaviorActorRef) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(key.division, key.lease)
	return state != nil && state.instances.containsAt(ref.monsterSlot, ref.gid)
}

/*
================
MonsterState.behaviorActorMover

behaviorActor plus the mover the step is about to read, in the one
locked pass: the step otherwise re-pays the lock, a population scan
(populationForObject) and a second storage probe for the same gid
(#2029). The mover half is MonsterState.Mover's value for this
population - the stored row, or the synthesized spawn mover when no
row exists yet - so the step continues with exactly what its own read
would have returned. The leg no longer calls this: it reads
legSnapshot, whose commit generation drops a commit planned from a
value a slower lane has since replaced (#2047 review, #2137 slice 1).
================
*/
func (s *MonsterState) behaviorActorMover(key populationKey, ref behaviorActorRef) behaviorActorSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(key.division, key.lease)
	if state == nil {
		return behaviorActorSnapshot{}
	}
	return s.behaviorActorMoverLocked(state, ref)
}

/*
================
MonsterState.behaviorActorMoverLocked

behaviorActorMover's read with the population resolved and s.mu
held: the leg snapshot (monsterlegsnapshot.go) reads a whole batch
through this one helper under its single locked pass, so a snapshot
actor and a dispatch-read actor are the same values by construction.
================
*/
func (s *MonsterState) behaviorActorMoverLocked(state *divisionMonsterState, ref behaviorActorRef) behaviorActorSnapshot {
	var actor behaviorActorSnapshot
	s.behaviorActorMoverInto(&actor, state, ref)
	return actor
}

/*
================
MonsterState.behaviorActorMoverInto

behaviorActorMoverLocked written in place (#2982): the leg snapshot
builds each actor straight into its view slot, so the 2.7 KB value
is expanded once instead of copied out through two return values.
dst reads as the zero snapshot (ok false) when the actor is gone.
================
*/
func (s *MonsterState) behaviorActorMoverInto(dst *behaviorActorSnapshot, state *divisionMonsterState, ref behaviorActorRef) {
	// The batch carried the monster's slot (#2133): read through it
	// when it still names this actor, else resolve by gid as before.
	// The snapshot stamps the slot the read actually resolved, so a
	// hint left stale by a row swap since the last remainder arm
	// costs the fill one gid resolve, never the step's doors a
	// second one.
	monsterSlot := ref.monsterSlot
	if !state.instances.lookupAtInto(&dst.instance, monsterSlot, ref.gid) {
		actor, ok := state.instances.lookup(ref.gid)
		if !ok {
			*dst = behaviorActorSnapshot{}
			return
		}
		dst.instance = actor
		_, monsterSlot, _ = state.instances.hotRowSlot(ref.gid)
	}
	// The batch carried the mover's slot (#2135): read through it
	// when it still names this actor, else resolve by gid as before.
	slot := ref.slot
	if !state.movers.lookupAtInto(&dst.mover, slot, ref.gid) {
		mover, ok := state.movers.lookup(ref.gid)
		if !ok {
			mover = monster.NewSpawnMover(dst.instance, s.nowMillis())
			slot = behaviorNoMoverSlot
		} else if resolved, found := state.movers.slotOf(ref.gid); found {
			slot = resolved
		}
		dst.mover = mover
	}
	if err := dst.mover.Validate(); err != nil {
		panic(err)
	}
	dst.moverGen = state.movers.generationAt(slot, ref.gid)
	dst.slot, dst.monsterSlot, dst.queueSlot, dst.ok = slot, monsterSlot, ref.queueSlot, true
}

/*
================
MonsterState.behaviorBatches
================
*/
func (s *MonsterState) behaviorBatches(now int64) []behaviorBatch {
	return s.behaviorBatchesForDivision(now, "")
}

/*
================
MonsterState.behaviorBatchesForDivision

The due batches for one leg. Building a batch consumes the actors'
dues WITHOUT re-arming them: the carried re-arm used to run here,
for every due actor, before its step - and an actor whose step then
committed (the gate's activity commit, a mover wake, an event door)
re-armed itself from its post-step rows, so the build's computation
was pure waste for exactly the actors that acted, while actors the
dispatch dropped paid it too. The dispatcher now arms the remainder
(armBehaviorRemainders) once the batches are through: actors a door
re-armed meanwhile are skipped, the rest are armed from the same
computation the build ran, on the rows their step left.
================
*/
func (s *MonsterState) behaviorBatchesForDivision(now int64, division string) []behaviorBatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []behaviorBatch
	for _, key := range s.populationKeys() {
		if division != "" && key.division != division {
			continue
		}
		state := s.populationForLease(key.division, key.lease)
		scratch := behaviorRefScratchPool.Get().(*behaviorRefScratch)
		batch := behaviorBatch{key: key, scratch: scratch}
		batch.actors = state.behavior.takeDue(now, scratch.refs[:0])
		actors := batch.actors[:0]
		for _, ref := range batch.actors {
			gid := ref.gid
			// The row reads through the monster slot the drain
			// carried (#2133); a slot that no longer names the
			// actor resolves by gid, as before the carry.
			hp, maxHP, summonUntil, hot := state.instances.hotProbeAt(ref.monsterSlot, gid)
			if !hot {
				var exists bool
				ref.monsterSlot, hp, maxHP, summonUntil, exists = state.instances.hotProbe(gid)
				if !exists {
					state.behavior.removeAt(ref.queueSlot, gid)
					continue
				}
			}
			if hp == 0 && maxHP > 0 {
				state.behavior.removeAt(ref.queueSlot, gid)
				continue
			}
			if summonUntil != 0 && now >= summonUntil {
				i, _ := state.instances.hotRowAt(ref.monsterSlot, gid)
				state.instances.set(gid, finishSummonAction(i.value(gid), now))
			}
			actors = append(actors, ref)
		}
		batch.actors = actors
		sortBehaviorRefs(batch.actors, scratch)
		out = append(out, batch)
	}
	return out
}

// behaviorRefSortThreshold is the batch size where ordering the
// refs by counting the gid's bytes passes the comparison sort:
// below it the counting passes' fixed cost (four 256-wide tallies)
// is the larger bill.
const behaviorRefSortThreshold = 128

/*
================
sortBehaviorRefs

Order a batch's refs by gid, the total order the dispatch walks.
Gids are unique within a batch (the queue's byGID), so four stable
counting passes over the gid's bytes produce exactly the sequence
a comparison sort does, in linear time, ping-ponging between the
refs and the scratch's sort backing (kept pooled with the refs, so
a warmed build allocates neither). The comparison sort this
replaces was half the batch build in the seeded profile (#3027);
small batches keep it, under the threshold above.

The drain often hands the batch its refs in gid order already -
a population armed in spawn order (server start, a region load) -
so every size first walks one pass of adjacent compares: an
ordered drain costs that pass (an ordered batch is a fixed point
of the sort, gids being unique), and an unordered one pays only
the compares up to its first descent (two in expectation) before
the sort it would have paid anyway. The pass used to guard only
the counting path; the seeded steady state hands the small
batches ordered too (2800 of 2800 over 350 legs, #3027), and
they paid the closure comparison sort for a sequence they
already held.
================
*/
func sortBehaviorRefs(refs []behaviorActorRef, scratch *behaviorRefScratch) {
	ordered := true
	for i := 1; i < len(refs); i++ {
		if refs[i-1].gid > refs[i].gid {
			ordered = false
			break
		}
	}
	if ordered {
		return
	}
	if len(refs) < behaviorRefSortThreshold {
		// slices.SortFunc, not sort.Slice: no reflection swapper
		// per batch (#2985).
		slices.SortFunc(refs, func(a, b behaviorActorRef) int { return cmp.Compare(a.gid, b.gid) })
		return
	}
	if cap(scratch.sort) < len(refs) {
		scratch.sort = make([]behaviorActorRef, len(refs))
	}
	src, dst := refs, scratch.sort[:len(refs)]
	for shift := uint(0); shift < 32; shift += 8 {
		var count [256]int
		for _, ref := range src {
			count[byte(ref.gid>>shift)]++
		}
		sum := 0
		for i, c := range count {
			count[i], sum = sum, sum+c
		}
		for _, ref := range src {
			d := byte(ref.gid >> shift)
			dst[count[d]] = ref
			count[d]++
		}
		src, dst = dst, src
	}
}

/*
================
MonsterState.armBehaviorRemainders

The deferred half of the batch build: arm every actor the dispatch
left unscheduled. behaviorBatchesForDivision consumed the dues
without re-arming; an actor whose step committed through a door
(the activity commit, a mover wake, any event lane's set) already
holds its next due from its post-step rows and is skipped - the
carried computation the build used to spend on it was overwritten
by that due anyway. The rest get the build's own carried arm, on
the rows their step left: a step that committed nothing scheduling
left those rows as they were, and a step that moved its actor
without arming is covered by the idle due's own proof (the earliest
of deadline, gate and acquisition is the actor's next act time
either way).

An actor a door put dormant during the dispatch keeps the door's
decision: it is registered in state.dormant, and re-running the
carried arm would forget the registration and re-evaluate a sleep
the door already settled. A population that left mid-leg is
skipped whole.
================
*/
func (s *MonsterState) armBehaviorRemainders(batches []behaviorBatch, now int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range batches {
		state := s.populationForLease(batches[i].key.division, batches[i].key.lease)
		if state == nil {
			continue
		}
		for j := range batches[i].actors {
			ref := &batches[i].actors[j]
			if _, sleeping := state.dormant[ref.gid]; sleeping {
				continue
			}
			if state.behavior.armedAt(ref.queueSlot, ref.gid) {
				continue
			}
			s.scheduleBehaviorCarried(state, ref, now)
		}
	}
}

/*
================
MonsterState.releaseBehaviorBatches

Hands each batch's refs backing back to the scratch pool. The one
production caller is the leg (monstertick.go), after every batch is
dispatched: nothing the dispatch or its deferred commits plan from
keeps a ref - the snapshot and the plans copy the values out - so
the backings are dead there. A batch whose scratch was already
returned, or never carried one, is skipped.
================
*/
func (s *MonsterState) releaseBehaviorBatches(batches []behaviorBatch) {
	for i := range batches {
		scratch := batches[i].scratch
		if scratch == nil {
			continue
		}
		scratch.refs = batches[i].actors[:0]
		scratch.sort = scratch.sort[:0]
		batches[i].scratch = nil
		behaviorRefScratchPool.Put(scratch)
	}
}

/*
================
MonsterState.behaviorDivisions
================
*/
func (s *MonsterState) behaviorDivisions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]bool)
	for _, key := range s.populationKeys() {
		seen[key.division] = true
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
