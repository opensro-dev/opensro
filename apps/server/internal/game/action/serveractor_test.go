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
	rt.ServerActorHit = func(division, victim, attacker string, damage int, _ bool, nowMs int64) {
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

/*
================
TestServerActorAcceptResurrectionRevivesInPlace
================
*/
func TestServerActorAcceptResurrectionRevivesInPlace(t *testing.T) {
	rt, _, caster, actor := newPvpPair(t)
	actor.RuntimeOnly = true
	actor.CurrentHP = testInt64(0)
	var peerFrames []wire.Frame
	rt.PushDivisionPeerFrames = func(_, name string, frames []wire.Frame) {
		if name == actor.Name {
			peerFrames = append(peerFrames, frames...)
		}
	}
	skill := enterworld.SkillRow{
		ID: 7777,
		Heal: enterworld.SkillHeal{
			Present:   true,
			HPPercent: 50,
			MPPercent: 50,
		},
	}
	nowMs := rt.Now().UnixMilli()
	prompt := rt.proposeResurrection(testDivision, caster, actor, skill, nowMs)
	if len(prompt) == 0 {
		t.Fatal("expected resurrection proposal for dead RuntimeOnly actor")
	}
	if !rt.ServerActorAcceptResurrection(testDivision, actor.Name, nowMs+100) {
		t.Fatal("expected ServerActorAcceptResurrection to accept pending proposal")
	}
	if !enterworld.CharacterAlive(actor) || enterworld.CurrentHP(actor) <= 1 {
		t.Fatalf("expected actor revived with healed HP, got alive=%v hp=%d", enterworld.CharacterAlive(actor), enterworld.CurrentHP(actor))
	}
	if len(peerFrames) == 0 {
		t.Fatal("expected native revival peer frames broadcast")
	}
}

/*
================
TestServerActorRetainsOneStarProtection
================
*/
func TestServerActorRetainsOneStarProtection(t *testing.T) {
	rt, _, trader := caravanFixture(t)
	trader.RuntimeOnly = true
	at := simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
	rt.SyncServerActor(testDivision, trader, at, false, 10, false)
	if !rt.levelOneCaravan(trader) || !rt.levelOneCaravan(trader.SnapshotCompanion()) {
		t.Fatal("an AI trader hauling a one-star load lost native protection")
	}
}

/*
================
TestSoloServerActorCargoDropsFromFiveStarsToTwo
================
*/
func TestSoloServerActorCargoDropsFromFiveStarsToTwo(t *testing.T) {
	rt, trader := traderFixture(t, false)
	trader.ActiveCOS.Container.Capacity = 40
	rt.TradeTiers = true
	trader.RuntimeOnly = true
	at := simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
	rt.SyncServerActorCargo(testDivision, trader, at, false, 10, 5, true)
	if tier, stars := rt.caravanTier(trader); tier != 5 || stars != 5 {
		t.Fatalf("fixture should carry five stars, got tier=%d stars=%d", tier, stars)
	}
	rt.SyncServerActorCargo(testDivision, trader, at, false, 10, 3, true)
	if tier, stars := rt.caravanTier(trader); tier != 3 || stars != 3 {
		t.Fatalf("regular transport should be sized down to three stars, got tier=%d stars=%d", tier, stars)
	}
	rt.SyncServerActorCargo(testDivision, trader, at, false, 10, 2, true)
	if tier, stars := rt.caravanTier(trader); tier != 2 || stars != 2 {
		t.Fatalf("solo transport should be sized down to two stars, got tier=%d stars=%d", tier, stars)
	}
}
