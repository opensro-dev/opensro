/*
===========================================================================

monsterlegsnapshot.go - the leg-start snapshot view (#2137 slice 1)

One immutable view per batch, built under MonsterState.mu before the
batch dispatches: every due actor's instance and mover value copies
with the mover row's commit generation, keyed by behaviorActorRef.
The step reads its actor from the view instead of resolving it inside
the action lane; the generation it carries is what keeps that safe -
a slower lane's mover set advances the row's generation, and the
step's generation-guarded commits then drop instead of overwriting
the newer value (the actor replans at its next due; the batch build
already rescheduled it). Publication and the value-guarded commit
transactions (commitNavigation, commitActivity) stay exactly as they
were; slices 2-4 widen the view to the actors in sight/help range and
move planning itself onto it.

===========================================================================
*/

package simulation

/*
================
legSnapshot

The batch's actors as the leg-start snapshot read them. An actor
absent from the map was already gone at build time; the dispatch's
existence probe still runs, so a despawn between build and dispatch
drops the actor exactly as before.
================
*/
type legSnapshot struct {
	key    populationKey
	actors map[behaviorActorRef]behaviorActorSnapshot
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
	view := &legSnapshot{key: key, actors: make(map[behaviorActorRef]behaviorActorSnapshot, len(refs))}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(key.division, key.lease)
	if state == nil {
		return view
	}
	for _, ref := range refs {
		if actor := s.behaviorActorMoverLocked(state, ref); actor.ok {
			view.actors[ref] = actor
		}
	}
	return view
}

/*
================
legSnapshot.actor

The view's actor for ref, in the shape the dispatch consumes.
================
*/
func (view *legSnapshot) actor(ref behaviorActorRef) (behaviorActorSnapshot, bool) {
	if view == nil {
		return behaviorActorSnapshot{}, false
	}
	actor, ok := view.actors[ref]
	return actor, ok
}
