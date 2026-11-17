/*
===========================================================================

serveractor_test.go - real skill damage on server-owned actors

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
	"time"
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

/*
================
TestServerActorCargoRegistersNativeAmbushesAndCanMount
================
*/
func TestServerActorCargoRegistersNativeAmbushesAndCanMount(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	c.RuntimeOnly = true
	c.ActiveCOS.Mounted = false
	c.BattleUntilMs = 0
	at := simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
	rt.SyncServerActor(testDivision, c, at, false, 1, false)
	if rt.caravans.Len() != 1 {
		t.Fatal("AI transport bypassed the native ambush timer")
	}
	if out := rt.ServerActorRide(testDivision, c.Name, true); !c.ActiveCOS.Mounted || len(out.Broadcast) == 0 {
		t.Fatalf("AI trader did not mount: %+v", out)
	}
	rt.advanceCaravans(clock.Now().UnixMilli())
	clock.Advance(60091 * time.Millisecond)
	rt.advanceCaravans(clock.Now().UnixMilli())
	if len(caravanBandits(rt, caravanBattlefield)) == 0 {
		t.Fatal("native thief NPCs did not spawn for AI cargo")
	}
	if out := rt.ServerActorRide(testDivision, c.Name, false); c.ActiveCOS.Mounted || len(out.Broadcast) == 0 {
		t.Fatalf("AI trader did not dismount: %+v", out)
	}
	c.CurrentMP = testInt64(0)
	rt.HealServerActor(testDivision, c.Name, 1)
	if enterworld.CurrentMP(c) <= 0 {
		t.Fatal("AI potion left the caster without mana")
	}

}
