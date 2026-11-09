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

The view is a retained slice, not a map (#2030): storing the batch's
fat value copies in a hash map cost 3.6 GB a minute of bucket copies
on the live shard (two heap pulls 60 s apart, 2026-11-03). Every
reader walks the batch in its own order, so the positional read
serves them all, and the view is dead when its batch finishes -
RunAction runs the dispatch closure before returning, and the pooled
leg's workers plan from copied values - so the batch's backing array
returns to the state for the next batch.

The state retains the backings itself instead of a sync.Pool: the
collector empties a pool under exactly the allocation pressure the
leg runs at, and the seeded-leg profile (2026-11-04) still charged
the make below 38% of the leg's allocated bytes - a handful of
multi-megabyte zeroed arrays per hundred legs. A bounded stack on
MonsterState survives every collection; the bound keeps a freak
batch's backing from being pinned forever.

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/world/monster"
)

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

// maxRetainedLegSnapshots bounds the views MonsterState keeps for
// reuse (one per in-flight batch, plus slack for the pooled leg's
// workers); past it a released view goes to the collector.
const maxRetainedLegSnapshots = 4

// maxRetainedLegSnapshotActors bounds a retained view's backing in
// actors: a freak batch's multi-megabyte array is dropped on release
// instead of pinned for the process's life.
const maxRetainedLegSnapshotActors = 16384

/*
================
MonsterState.takeLegView

The next view to build into: a retained one when the state holds
any, a fresh one otherwise. The retained stack rides its own mutex
so a checkout never queues behind the population lock the build
itself takes.
================
*/
func (s *MonsterState) takeLegView() *legSnapshot {
	s.legViewMu.Lock()
	defer s.legViewMu.Unlock()
	if n := len(s.legViews); n > 0 {
		view := s.legViews[n-1]
		s.legViews[n-1] = nil
		s.legViews = s.legViews[:n-1]
		return view
	}
	return &legSnapshot{}
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
	view := s.takeLegView()
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
		s.behaviorActorMoverInto(&view.actors[i], state, ref)
	}
	return view
}

/*
================
legSnapshot.actorAt

The view's actor for the batch's refs[i], in the shape the dispatch
consumes. ok is false when the actor was already gone at build time
- the same answer the dispatch read gives for it.

The actor is returned by pointer into the view, never into storage
(#2982): the view is leg-local, written only by legSnapshot and
cleared only by releaseLegSnapshot after its batch is dispatched, so
the pointer outlives no lock and no slot move. Readers treat it as
read-only; the step copies the mover before it changes it.
================
*/
func (view *legSnapshot) actorAt(i int) (*behaviorActorSnapshot, bool) {
	if view == nil || i < 0 || i >= len(view.actors) {
		return nil, false
	}
	actor := &view.actors[i]
	return actor, actor.ok
}

/*
================
MonsterState.releaseLegSnapshot

Takes the view back once its batch has been dispatched. The wipe
covers only what the next build does not write: the instance's
effect families (fill writes the rest of the instance field by
field, and hotFill joins an effect only where the sparse mask
sets it, so a built slot's effect fields must arrive zero) and
the mover (its expansion writes only the fields it owns). A
retained view so holds no actor (or anything its pointers reach)
alive between batches; a view past the retained bounds is left
to the collector instead.
================
*/
func (s *MonsterState) releaseLegSnapshot(view *legSnapshot) {
	if view == nil || cap(view.actors) > maxRetainedLegSnapshotActors {
		return
	}
	for i := range view.actors {
		actor := &view.actors[i]
		actor.instance.LinkedEffects = nil
		actor.instance.Abnormal = nil
		actor.instance.SelfEffects = monster.SelfEffects{}
		actor.instance.TargetEffects = monster.TargetEffects{}
		actor.mover = monster.MoverState{}
	}
	view.actors = view.actors[:0]
	view.key = populationKey{}
	s.legViewMu.Lock()
	defer s.legViewMu.Unlock()
	if len(s.legViews) < maxRetainedLegSnapshots {
		s.legViews = append(s.legViews, view)
	}
}
