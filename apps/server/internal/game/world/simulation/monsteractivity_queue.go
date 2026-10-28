/*
===========================================================================

monsteractivity_queue.go - the monster behaviour queue and its due batches

Every materialized actor has one update entry, ordered by (at, gid).
PENDING sleeps until its activity deadline (or earlier movement arrival);
combat events reschedule the same entry. No viewer ring owns actor
liveness.

The queue sifts with its own typed copies of container/heap's up and down,
step for step, so every operation leaves the same order the generic heap
would; the generic one cost an interface call per compare and swap on the
tick's busiest path. The heap.Interface methods stay for callers that use
container/heap directly.

===========================================================================
*/

package simulation

import (
	"sort"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
behaviorEntry
================
*/
type behaviorEntry struct {
	gid   uint32
	at    int64
	index int
}

/*
================
behaviorQueue
================
*/
type behaviorQueue struct {
	entries []*behaviorEntry
	byGID   map[uint32]*behaviorEntry
}

/*
================
behaviorQueue.Len
================
*/
func (q behaviorQueue) Len() int { return len(q.entries) }

/*
================
behaviorQueue.Less
================
*/
func (q behaviorQueue) Less(i, j int) bool { return q.less(i, j) }

/*
================
behaviorQueue.Swap
================
*/
func (q behaviorQueue) Swap(i, j int) { q.swap(i, j) }

/*
================
behaviorQueue.Push
================
*/
func (q *behaviorQueue) Push(v any) {
	e := v.(*behaviorEntry)
	e.index = len(q.entries)
	q.entries = append(q.entries, e)
	q.byGID[e.gid] = e
}

/*
================
behaviorQueue.Pop
================
*/
func (q *behaviorQueue) Pop() any {
	n := len(q.entries) - 1
	e := q.entries[n]
	q.entries[n] = nil
	q.entries = q.entries[:n]
	delete(q.byGID, e.gid)
	return e
}

/*
================
behaviorQueue.less
================
*/
func (q *behaviorQueue) less(i, j int) bool {
	a, b := q.entries[i], q.entries[j]
	return a.at < b.at || a.at == b.at && a.gid < b.gid
}

/*
================
behaviorQueue.swap
================
*/
func (q *behaviorQueue) swap(i, j int) {
	q.entries[i], q.entries[j] = q.entries[j], q.entries[i]
	q.entries[i].index = i
	q.entries[j].index = j
}

/*
================
behaviorQueue.up

container/heap's up.
================
*/
func (q *behaviorQueue) up(j int) {
	for {
		i := (j - 1) / 2
		if i == j || !q.less(j, i) {
			break
		}
		q.swap(i, j)
		j = i
	}
}

/*
================
behaviorQueue.down

container/heap's down: reports whether the entry moved.
================
*/
func (q *behaviorQueue) down(i0, n int) bool {
	i := i0
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 {
			break
		}
		j := j1
		if j2 := j1 + 1; j2 < n && q.less(j2, j1) {
			j = j2
		}
		if !q.less(j, i) {
			break
		}
		q.swap(i, j)
		i = j
	}
	return i > i0
}

/*
================
behaviorQueue.set

Schedules gid at at: heap.Fix for an existing entry, heap.Push for a new
one.
================
*/
func (q *behaviorQueue) set(gid uint32, at int64) {
	if q.byGID == nil {
		q.byGID = make(map[uint32]*behaviorEntry)
	}
	if e := q.byGID[gid]; e != nil {
		e.at = at
		if !q.down(e.index, len(q.entries)) {
			q.up(e.index)
		}
		return
	}
	q.Push(&behaviorEntry{gid: gid, at: at})
	q.up(len(q.entries) - 1)
}

/*
================
behaviorQueue.remove

heap.Remove of gid's entry, if it has one.
================
*/
func (q *behaviorQueue) remove(gid uint32) {
	e := q.byGID[gid]
	if e == nil {
		return
	}
	i, n := e.index, len(q.entries)-1
	if n != i {
		q.swap(i, n)
		if !q.down(i, n) {
			q.up(i)
		}
	}
	q.Pop()
}

//============================================================================

/*
================
MonsterState.scheduleBehavior
================
*/
func (s *MonsterState) scheduleBehavior(state *divisionMonsterState, gid uint32, now int64) {
	state.forgetDormant(gid)
	m, ok := state.movers.schedule(gid)
	if !ok {
		state.behavior.remove(gid)
		return
	}
	if s.tryDormant(state, gid, m, now) {
		return
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
}

/*
================
behaviorBatch
================
*/
type behaviorBatch struct {
	key    populationKey
	actors []uint32
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
MonsterState.behaviorActorMover

behaviorActor plus the mover the step is about to read, in the one
locked pass: the step otherwise re-pays the lock, a population scan
(populationForObject) and a second storage probe for the same gid
(#2029). The mover half is MonsterState.Mover's value for this
population - the stored row, or the synthesized spawn mover when no
row exists yet - so the step continues with exactly what its own read
would have returned.
================
*/
func (s *MonsterState) behaviorActorMover(key populationKey, gid uint32) behaviorActorSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(key.division, key.lease)
	if state == nil {
		return behaviorActorSnapshot{}
	}
	actor, ok := state.instances.lookup(gid)
	if !ok {
		return behaviorActorSnapshot{}
	}
	if mover, ok := state.movers.lookup(gid); ok {
		if err := mover.Validate(); err != nil {
			panic(err)
		}
		return behaviorActorSnapshot{instance: actor, mover: mover, ok: true}
	}
	mover := monster.NewSpawnMover(actor, s.nowMillis())
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
		for state.behavior.Len() > 0 && state.behavior.entries[0].at <= now {
			// Reschedule the existing queue entry in place. Pop followed by
			// set allocated a new object for every actor on every due tick.
			e := state.behavior.entries[0]
			i, exists := state.instances.hot[e.gid]
			if !exists {
				state.behavior.remove(e.gid)
				continue
			}
			if i.currentHP == 0 && i.ref.Value().MaxHP > 0 {
				state.behavior.remove(e.gid)
				continue
			}
			if i.summonActionUntilMs != 0 && now >= i.summonActionUntilMs {
				state.instances.set(e.gid, finishSummonAction(i.value(e.gid), now))
			}
			batch.actors = append(batch.actors, e.gid)
			s.scheduleBehavior(state, e.gid, now)
		}
		sort.Slice(batch.actors, func(i, j int) bool { return batch.actors[i] < batch.actors[j] })
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
