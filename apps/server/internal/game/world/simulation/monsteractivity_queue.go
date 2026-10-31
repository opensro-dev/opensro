/*
===========================================================================

monsteractivity_queue.go - the monster behaviour queue and its due batches

Every materialized actor has one update entry, due at a millisecond
time. PENDING sleeps until its activity deadline (or earlier movement
arrival); combat events reschedule the same entry. No viewer ring owns
actor liveness.

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

===========================================================================
*/

package simulation

import (
	"sort"

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
row are stale whatever at they carry.
================
*/
type behaviorSlot struct {
	gid  uint32
	at   int64
	live bool
}

/*
================
behaviorQueue

The wheel. byGID maps a gid to its slot and is read on the re-arm
path and written only when an actor enters or leaves the queue; the
drain itself walks bucket lists of behaviorCopy and the slot array,
no map traffic. now is the last drain time and the ring's cursor base.
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
	}
	q.slots[slot] = behaviorSlot{gid: gid, at: at, live: true}
	q.place(slot, at)
}

/*
================
behaviorQueue.remove

Unschedules gid: the slot dies and leaves the queue, staling every
copy at once.
================
*/
func (q *behaviorQueue) remove(gid uint32) {
	slot, ok := q.byGID[gid]
	if !ok {
		return
	}
	delete(q.byGID, gid)
	q.slots[slot].live = false
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
		*out = append(*out, behaviorActorRef{gid: q.slots[e.slot].gid, slot: behaviorNoMoverSlot})
	}
	return keep
}

/*
================
behaviorQueue.takeDue

Every actor due by now, as a ref carrying no mover slot yet (the
batch build fills it), in no order (callers sort). Their live dues are
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
				out = append(out, behaviorActorRef{gid: q.slots[e.slot].gid, slot: behaviorNoMoverSlot})
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
MonsterState.scheduleBehavior
================
*/
func (s *MonsterState) scheduleBehavior(state *divisionMonsterState, gid uint32, now int64) {
	s.scheduleBehaviorSlot(state, gid, now)
}

/*
================
MonsterState.scheduleBehaviorSlot

scheduleBehavior, also returning the mover slot the read resolved.
The batch build carries the slot to the dispatch read
(behaviorActorMover), which verifies it against the gid before use
and falls back to the gid resolve when a despawn recycled it
(#2135). behaviorNoMoverSlot means the actor had no mover row: it
still joins the batch, and its mover is synthesized at the read,
exactly as before the carry.
================
*/
func (s *MonsterState) scheduleBehaviorSlot(state *divisionMonsterState, gid uint32, now int64) uint32 {
	state.forgetDormant(gid)
	m, slot, ok := state.movers.scheduleSlot(gid)
	if !ok {
		state.behavior.remove(gid)
		return behaviorNoMoverSlot
	}
	if s.tryDormant(state, gid, m, now) {
		return slot
	}
	at := now + 1
	commandPending := state.instances.hot[gid].help.HasPending()
	if m.mode == monster.MoverPending && m.activity.Interval != 0 && !commandPending {
		elapsed := uint32(now) - m.activity.LastCheck
		if elapsed <= uint32(m.activity.Interval) {
			at = now + int64(uint32(m.activity.Interval)-elapsed) + 1
		}
		if m.arriveMs > now && m.arriveMs < at {
			at = m.arriveMs
		}
	}
	state.behavior.set(gid, at)
	return slot
}

/*
================
behaviorActorRef

One due actor as the batch carries it: its gid, and the mover slot
the build's schedule read resolved. The slot is a hint for the
dispatch read (moverStorage.lookupAt), verified against the gid
before use - remove swaps the last row into the hole, so a carried
slot can name another actor by dispatch time. behaviorNoMoverSlot
means no slot was carried.
================
*/
type behaviorActorRef struct {
	gid  uint32
	slot uint32
}

/*
================
behaviorBatch
================
*/
type behaviorBatch struct {
	key    populationKey
	actors []behaviorActorRef
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
	ok       bool
}

/*
================
MonsterState.behaviorActorLive

The dispatch's existence probe: whether the population still holds
the actor, without building its instance value. The leg's dispatch
decides only this; the actor itself is read inside the action lane
(behaviorActorMover), under the lane's division lock (#2047 review).
================
*/
func (s *MonsterState) behaviorActorLive(key populationKey, gid uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(key.division, key.lease)
	return state != nil && state.instances.contains(gid)
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
would have returned. Called from inside the action lane (or the
adjacent direct dispatch), never ahead of its lock: a mover set by a
slower lane while the leg waits for the lock must be the value the
step starts from, or the step's whole-value commits overwrite it
(#2047 review).
================
*/
func (s *MonsterState) behaviorActorMover(key populationKey, ref behaviorActorRef) behaviorActorSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(key.division, key.lease)
	if state == nil {
		return behaviorActorSnapshot{}
	}
	actor, ok := state.instances.lookup(ref.gid)
	if !ok {
		return behaviorActorSnapshot{}
	}
	// The batch carried the mover's slot (#2135): read through it
	// when it still names this actor, else resolve by gid as before.
	mover, ok := state.movers.lookupAt(ref.slot, ref.gid)
	if !ok {
		mover, ok = state.movers.lookup(ref.gid)
	}
	if ok {
		if err := mover.Validate(); err != nil {
			panic(err)
		}
		return behaviorActorSnapshot{instance: actor, mover: mover, ok: true}
	}
	mover = monster.NewSpawnMover(actor, s.nowMillis())
	if err := mover.Validate(); err != nil {
		panic(err)
	}
	return behaviorActorSnapshot{instance: actor, mover: mover, ok: true}
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
		batch := behaviorBatch{key: key}
		batch.actors = state.behavior.takeDue(now, batch.actors)
		actors := batch.actors[:0]
		for _, ref := range batch.actors {
			gid := ref.gid
			i, exists := state.instances.hot[gid]
			if !exists {
				state.behavior.remove(gid)
				continue
			}
			if i.currentHP == 0 && i.ref.Value().MaxHP > 0 {
				state.behavior.remove(gid)
				continue
			}
			if i.summonActionUntilMs != 0 && now >= i.summonActionUntilMs {
				state.instances.set(gid, finishSummonAction(i.value(gid), now))
			}
			ref.slot = s.scheduleBehaviorSlot(state, gid, now)
			actors = append(actors, ref)
		}
		batch.actors = actors
		sort.Slice(batch.actors, func(i, j int) bool { return batch.actors[i].gid < batch.actors[j].gid })
		out = append(out, batch)
	}
	return out
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
