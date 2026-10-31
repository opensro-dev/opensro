/*
===========================================================================

basicattack_spread_test.go - the intent step spreads a burst over ticks

A burst of due intents drains under basicAttackSpreadBudget instead of
inside one tick, the scan resumes past the intent served last so the
tail of the snapshot cannot starve, and an unserved intent is never
dropped (#2143). Follow intents carry the pins: each actor's pursuit
publishes one movement batch naming its own gid.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
spreadFollowFixture

Three admitted actors, each Tracing its own mate. Every mate then steps
200 units further away, so each actor's next advance must publish a
fresh pursuit batch. Returns the actors keyed by name; the snapshot's
own order says who must be served first.
================
*/
func spreadFollowFixture(t *testing.T) (*Runtime, *fakeClock, map[string]*enterworld.Character) {
	t.Helper()
	rt, clock, first, _ := newCombatTestRuntime(t, 100)
	deps := rt.deps.(*enterworld.Deps)

	actors := map[string]*enterworld.Character{}
	for i := 0; i < 3; i++ {
		actor := first
		if i > 0 {
			next := *first
			next.ID, next.Name = first.ID+int64(2*i), "spread-actor-"+string(rune('a'+i))
			world, spawn := *first.World, *first.World.Spawn
			world.Spawn = &spawn
			next.World = &world
			actor = &next
			fixtureCharacters(deps.Characters)[testDivision] = append(
				fixtureCharacters(deps.Characters)[testDivision], actor,
			)
		}
		mate := *actor
		mate.ID, mate.Name = actor.ID+1, actor.Name+"-mate"
		world, spawn := *actor.World, *actor.World.Spawn
		world.Spawn = &spawn
		mate.World = &world
		fixtureCharacters(deps.Characters)[testDivision] = append(
			fixtureCharacters(deps.Characters)[testDivision], &mate,
		)
		for _, c := range []*enterworld.Character{actor, &mate} {
			if err := rt.AdmitCharacterSession(testDivision, c.Name, uint64(c.ID)); err != nil {
				t.Fatal(err)
			}
		}
		rt.Worlds.Update(simulation.WorldKey(testDivision, mate.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(&mate) },
			func(w *simulation.WorldState) { w.Spawn.X += 300 },
		)
		result := rt.HandleTargetInteract(testDivision, actor,
			wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(&mate)}.Encode())
		if result.DiagnosticRefusal != "" {
			t.Fatalf("follow admission for %s refused: %s", actor.Name, result.DiagnosticRefusal)
		}
		rt.Worlds.Update(simulation.WorldKey(testDivision, mate.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(&mate) },
			func(w *simulation.WorldState) { w.Spawn.X += 200 },
		)
		actors[actor.Name] = actor
	}
	if got := len(rt.combatIntentSnapshot()); got != len(actors) {
		t.Fatalf("intents = %d, want %d", got, len(actors))
	}
	// Let every pursuit arrive at its stand-off: a follow advance only
	// resteers (and so publishes) once the actor's move segment ends.
	clock.Advance(30 * time.Second)
	return rt, clock, actors
}

/*
================
TestBasicAttackIntentsSpreadAcrossTicks
================
*/
func TestBasicAttackIntentsSpreadAcrossTicks(t *testing.T) {
	rt, clock, actors := spreadFollowFixture(t)
	saved := basicAttackSpreadBudget
	basicAttackSpreadBudget = 0
	defer func() { basicAttackSpreadBudget = saved }()

	// A zero budget still serves one intent per call, and the rotation
	// serves each actor in snapshot order instead of re-serving the
	// head of the snapshot on every call.
	snapshot := rt.combatIntentSnapshot()
	for i, intent := range snapshot {
		actor := actors[intent.CharacterName]
		out := rt.advanceBasicAttackIntents(clock.NowMs(), nil)
		if len(out) != 1 || out[0].SourceGID != enterworld.ObjectIDForCharacter(actor) {
			t.Fatalf("call %d published %+v, want only %s's pursuit", i+1, out, actor.Name)
		}
		if got := len(rt.combatIntentSnapshot()); got != len(actors) {
			t.Fatalf("call %d left %d intents, want %d (an unserved intent was dropped)", i+1, got, len(actors))
		}
	}
}

/*
================
TestBasicAttackIntentsServeAllInsideBudget
================
*/
func TestBasicAttackIntentsServeAllInsideBudget(t *testing.T) {
	rt, clock, actors := spreadFollowFixture(t)
	out := rt.advanceBasicAttackIntents(clock.NowMs(), nil)
	if len(out) != len(actors) {
		t.Fatalf("one call inside the budget published %d batches, want %d", len(out), len(actors))
	}
	if rt.basicAttackCursor != "" {
		t.Fatalf("a fully served snapshot left cursor %q; the next tick would not start at the head", rt.basicAttackCursor)
	}
	served := map[uint32]bool{}
	for _, batch := range out {
		served[batch.SourceGID] = true
	}
	for _, actor := range actors {
		if !served[enterworld.ObjectIDForCharacter(actor)] {
			t.Fatalf("%s was not served inside the budget", actor.Name)
		}
	}
}
