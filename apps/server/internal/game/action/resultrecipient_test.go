/*
===========================================================================

resultrecipient_test.go - a landed hit stands a seated player

593AE8: SkillCombat_ApplyResultRecipients stands a seated recipient
(CGObjChar_RequestMotionChange(recipient, 0)) for any unabsorbed hit.

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
==================
standPushes

The move-channel stand pushes among a hit's observer frames.
==================
*/
func standPushes(frames []simulation.Frame, gid uint32) int {
	stand := string(wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelMove, Value: wire.MoveStateStand}.Encode())
	count := 0
	for _, frame := range frames {
		if frame.Opcode == wire.OpObjectStateRefresh && string(frame.Payload) == stand {
			count++
		}
	}
	return count
}

/*
==================
TestMonsterHitStandsSeatedPlayer
==================
*/
func TestMonsterHitStandsSeatedPlayer(t *testing.T) {
	rt, clock, c, instance := newCombatTestRuntime(t, 100)
	instance.Ref.DefaultSkillIDs[0] = 2
	// A fixed one-point hit with a deterministic roll always lands non-fatally.
	skills := rt.deps.SkillData().(staticSkillSource)
	row := skills[2]
	row.Attack.Min, row.Attack.Max, row.Attack.Percent = 1, 1, 100
	skills[2] = row
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	worldKey := simulation.WorldKey(testDivision, c.Name)
	seed := func() simulation.WorldState { return simulation.SeedWorldState(c) }
	rt.Worlds.Update(worldKey, seed, func(world *simulation.WorldState) {
		world.Sitting = true
		world.PostureTransitionUntilMs = clock.NowMs() - 1
	})
	gid := enterworld.ObjectIDForCharacter(c)
	result := rt.MonsterBasicAttack(testDivision, instance, gid, 2, clock.NowMs())
	if !result.Accepted || !result.TargetAlive {
		t.Fatalf("hit = %+v, want a landed non-fatal hit", result)
	}
	if got := standPushes(result.Frames, gid); got != 1 {
		t.Fatalf("stand pushes = %d in %+v, want the seated victim to stand once", got, result.Frames)
	}
	if world := rt.Worlds.Snapshot(worldKey, seed); world.Sitting {
		t.Fatal("the struck player is still seated")
	}

	// A player already standing gets no stand push from the next hit.
	clock.Advance(5000_000_000)
	again := rt.MonsterBasicAttack(testDivision, instance, gid, 2, clock.NowMs())
	if got := standPushes(again.Frames, gid); got != 0 {
		t.Fatalf("standing victim received %d stand pushes", got)
	}
}
