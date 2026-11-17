/*
===========================================================================

serveractor_test.go - real skill damage on server-owned actors

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

/*
================
TestServerActorTakesRealSkillDamageWithoutKillRewards
================
*/
func TestServerActorTakesRealSkillDamageWithoutKillRewards(t *testing.T) {
	rt, _, player, actor := newPvpPair(t)
	actor.RuntimeOnly = true
	actor.CurrentHP = testInt64(1)
	called := false
	rt.ServerActorHit = func(division, victim, attacker string, damage int, nowMs int64) {
		called = victim == actor.Name && attacker == player.Name && damage == 1
	}
	var exp int64
	rt.UpdateExperience = func(c *enterworld.Character, amount, _ int64, _ uint32) ([]wire.Frame, bool) {
		exp += amount
		return nil, true
	}
	out := rt.HandleTargetInteract(testDivision, player, (wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(actor)}).Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || enterworld.CharacterAlive(actor) || !called {
		t.Fatalf("actor did not take a real hit: %s callback=%v", out.DiagnosticRefusal, called)
	}
	if exp != 0 || player.PK != nil && player.PK.TotalCount != 0 {
		t.Fatal("AI kill minted player kill rewards")
	}
}
