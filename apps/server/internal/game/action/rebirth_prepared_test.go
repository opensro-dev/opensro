/*
===========================================================================

rebirth_prepared_test.go - tests for the detached rebirth preparation

The prepared path (#2143) builds a same-world town revival's re-entry
corpus before the publication gate; everything else declines and runs
gated, and a publish whose world moved on refuses instead of reviving
over the newer state.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPrepareLocalRebirthSameWorldTownPublishesRevival

Preparation commits nothing: the corpse stays dead until the publish
half runs, and the publish then produces the same re-entry burst the
gated operation produces.
================
*/
func TestPrepareLocalRebirthSameWorldTownPublishesRevival(t *testing.T) {
	character := rebirthTestCharacter(20, 0)
	appointed := simulation.EuropeStartProfile()
	character.World.RebirthPoint = worldSpawnFromMission(appointed)
	rt, _ := newTestRuntime(character, testItems())
	worldKey := simulation.WorldKey(testDivision, character.Name)
	corpse := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState { return simulation.WorldState{} })

	publish, ok := rt.prepareLocalRebirth(testDivision, character, []byte{wire.RebirthAtSpecifiedPoint})
	if !ok || publish == nil {
		t.Fatalf("same-world town revival did not prepare: ok=%v", ok)
	}
	if enterworld.CharacterAlive(character) {
		t.Fatal("preparation revived the character before the publish half")
	}
	if got := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState { return simulation.WorldState{} }); got.Spawn != corpse.Spawn {
		t.Fatalf("preparation moved the corpse: %+v -> %+v", corpse.Spawn, got.Spawn)
	}

	result := publish()
	if len(result.Frames) < 8 || len(result.Broadcast) != 3 {
		t.Fatalf("prepared rebirth burst = %d private/%d broadcast, want reset corpus + LIFE / 3", len(result.Frames), len(result.Broadcast))
	}
	if result.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("prepared rebirth head = %#x, want the reset client opcode", result.Frames[0].Opcode)
	}
	if !enterworld.CharacterAlive(character) {
		t.Fatal("prepared publish did not revive the character")
	}
}

/*
================
TestPrepareLocalRebirthDeclinesPresentPoint

A present-point revival builds no re-entry corpus, so it declines and
the gated operation runs it whole.
================
*/
func TestPrepareLocalRebirthDeclinesPresentPoint(t *testing.T) {
	character := rebirthTestCharacter(5, 0)
	rt, _ := newTestRuntime(character, testItems())

	publish, ok := rt.prepareLocalRebirth(testDivision, character, []byte{wire.RebirthAtPresentPoint})
	if ok || publish != nil {
		t.Fatalf("present-point revival prepared: ok=%v", ok)
	}
	if enterworld.CharacterAlive(character) {
		t.Fatal("a declined preparation revived the character")
	}
	result := rt.HandleLocalRebirth(testDivision, character, []byte{wire.RebirthAtPresentPoint})
	if len(result.Frames) == 0 || !enterworld.CharacterAlive(character) {
		t.Fatal("the gated present-point revival did not run after the decline")
	}
}

/*
================
TestPrepareLocalRebirthDeclinesCrossWorld

A town revival out of a fortress world must move its membership before
the corpus can be built, so it declines; the gated operation still
lands the character in the field.
================
*/
func TestPrepareLocalRebirthDeclinesCrossWorld(t *testing.T) {
	rt, c, clock := fortressFixtureWithClock(t, testFieldFortGate)
	enterFortress(t, rt, c)
	zero := int64(0)
	c.CurrentHP = &zero
	rt.settlePlayerDeathInDoor(testDivision, c, deathKiller{}, clock.NowMs())

	publish, ok := rt.prepareLocalRebirth(testDivision, c, []byte{wire.RebirthAtSpecifiedPoint})
	if ok || publish != nil {
		t.Fatalf("cross-world town revival prepared: ok=%v", ok)
	}
	if enterworld.CharacterAlive(c) {
		t.Fatal("a declined preparation revived the character")
	}
	out := rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtSpecifiedPoint})
	if len(out.Frames) == 0 {
		t.Fatalf("the gated cross-world revival did not run after the decline: %+v", out)
	}
	inField(t, rt, c, "the declined-preparation town revival")
}

/*
================
TestPrepareLocalRebirthPublishRefusesStaleState

If the gated operation already revived the character while the
prepared publish waited for the gate, the publish refuses quietly
instead of reviving over the newer state.
================
*/
func TestPrepareLocalRebirthPublishRefusesStaleState(t *testing.T) {
	character := rebirthTestCharacter(20, 0)
	character.World.RebirthPoint = worldSpawnFromMission(simulation.EuropeStartProfile())
	rt, _ := newTestRuntime(character, testItems())

	publish, ok := rt.prepareLocalRebirth(testDivision, character, []byte{wire.RebirthAtSpecifiedPoint})
	if !ok || publish == nil {
		t.Fatalf("same-world town revival did not prepare: ok=%v", ok)
	}
	direct := rt.HandleLocalRebirth(testDivision, character, []byte{wire.RebirthAtSpecifiedPoint})
	if len(direct.Frames) == 0 || !enterworld.CharacterAlive(character) {
		t.Fatal("the intervening gated revival did not run")
	}
	landed := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, character.Name),
		func() simulation.WorldState { return simulation.WorldState{} }).Spawn

	if result := publish(); len(result.Frames) != 0 || len(result.Broadcast) != 0 {
		t.Fatalf("stale publish produced frames: %+v", result)
	}
	if got := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, character.Name),
		func() simulation.WorldState { return simulation.WorldState{} }).Spawn; got != landed {
		t.Fatalf("stale publish moved the character: %+v -> %+v", landed, got)
	}
}
