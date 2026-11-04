/*
===========================================================================

monsterlegsnapshot.go - the leg-start snapshot view (#2137 slice 1, #2030)

One immutable view per batch, built under MonsterState.mu before the
batch dispatches: every due actor's instance and mover value copies
with the mover row's commit generation, in the batch's own order -
actors[i] is the snapshot of the batch's refs[i]. The step reads its
actor from the view instead of resolving it inside the action lane;
the generation it carries is what keeps that safe - a slower lane's
mover set advances the row's generation, and the step's
generation-guarded commits then drop instead of overwriting the newer
value (the actor replans at its next due; the batch build already
rescheduled it). Publication and the value-guarded commit
transactions (commitNavigation, commitActivity) stay exactly as they
were; slices 2-4 widen the view to the actors in sight/help range and
move planning itself onto it.

The view is a pooled slice, not a map (#2030): storing the batch's
fat value copies in a hash map cost 3.6 GB a minute of bucket copies
on the live shard (two heap pulls 60 s apart, 2026-11-03). Every
reader walks the batch in its own order, so the positional read
serves them all, and the view is dead when its batch finishes -
RunAction runs the dispatch closure before returning, and the pooled
leg's workers plan from copied values - so the batch's backing array
returns to the pool for the next batch.

===========================================================================
*/

package simulation

import "sync"

/*
================
legSnapshot

The batch's actors as the leg-start snapshot read them, in batch
order: actors[i] belongs to the batch's refs[i], and its ok is false
when the actor was already gone at build time; the dispatch's
existence probe still runs, so a despawn between build and dispatch
drops the actor exactly as before.
================
*/
type legSnapshot struct {
	key    populationKey
	actors []behaviorActorSnapshot
}

/*
================
legSnapshotPool

Built views, ready for the next batch. A view is checked out by
MonsterState.legSnapshot and returned by release once its batch has
been dispatched; no reader may touch it afterwards.
================
*/
var legSnapshotPool = sync.Pool{
	New: func() any { return &legSnapshot{} },
}

/*
================
MonsterState.legSnapshot

Builds the batch's view in one locked pass. Every actor resolves
through behaviorActorMoverLocked, the same read the dispatch used
before, so a snapshot actor and a dispatch-read actor are the same
values by construction - only their timing differs, and the commit
generation covers that difference.
================
*/
func (s *MonsterState) legSnapshot(key populationKey, refs []behaviorActorRef) *legSnapshot {
	view := legSnapshotPool.Get().(*legSnapshot)
	view.key = key
	if cap(view.actors) < len(refs) {
		view.actors = make([]behaviorActorSnapshot, len(refs))
	} else {
		view.actors = view.actors[:len(refs)]
	}
	if view.actors == nil {
		view.actors = []behaviorActorSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(key.division, key.lease)
	if state == nil {
		for i := range view.actors {
			view.actors[i] = behaviorActorSnapshot{}
		}
		return view
	}
	for i, ref := range refs {
		view.actors[i] = s.behaviorActorMoverLocked(state, ref)
	}
	return view
}

/*
================
legSnapshot.actorAt

The view's actor for the batch's refs[i], in the shape the dispatch
consumes. ok is false when the actor was already gone at build time
- the same answer the dispatch read gives for it.
================
*/
func (view *legSnapshot) actorAt(i int) (behaviorActorSnapshot, bool) {
	if view == nil || i < 0 || i >= len(view.actors) {
		return behaviorActorSnapshot{}, false
	}
	actor := view.actors[i]
	return actor, actor.ok
}

/*
================
legSnapshot.release

Hands the view back to the pool once its batch has been dispatched.
The stored copies are cleared, so a pooled view holds no actor (or
anything its pointers reach) alive between batches.
================
*/
func (view *legSnapshot) release() {
	if view == nil {
		return
	}
	clear(view.actors)
	view.actors = view.actors[:0]
	view.key = populationKey{}
	legSnapshotPool.Put(view)
}
