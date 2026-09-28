/*
===========================================================================

goldbonus_test.go - merchant blessing through inventory and reward publication

The effect must alter gold quantity for its owner only, preserve non-gold drops,
survive reconnect and disappear at expiry. No random roll belongs to this step.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestGoldItemUsesRewardOwnerAndEffectLifetime
================
*/
func TestGoldItemUsesRewardOwnerAndEffectLifetime(t *testing.T) {
	rt, clock, owner, request := statItemFixture(t, enterworld.SkillTimedEffect{GoldDropPercent: 50})
	if result := rt.HandleItemUse(testDivision, owner, request); result.Frames[0].Payload[0] != 1 {
		t.Fatal("merchant blessing refused", result)
	}
	peer := *owner
	peer.Name = "unbuffed"
	for _, phase := range []string{"active", "restored", "expired"} {
		switch phase {
		case "restored":
			rt.ForgetCharacter(testDivision, owner.Name)
			clock.Advance(time.Hour)
			rt.RestoreTimedSkillJobs(testDivision, owner.Name)
		case "expired":
			clock.Advance(10 * time.Second)
			rt.effects.Expire(clock.NowMs())
			rt.drainStoppedCharacterEffects()
		}
		drops := []grounditem.Item{{GoldAmount: 101}, {StackCount: 3}}
		rt.applyMonsterGoldBonus(testDivision, owner, drops)
		want := uint32(151)
		if phase == "expired" {
			want = 101
		}
		if drops[0].GoldAmount != want || drops[1].StackCount != 3 || drops[1].GoldAmount != 0 {
			t.Fatalf("%s: incorrect drops %+v", phase, drops)
		}
		unbuffed := []grounditem.Item{{GoldAmount: 101}}
		rt.applyMonsterGoldBonus(testDivision, &peer, unbuffed)
		if unbuffed[0].GoldAmount != 101 {
			t.Fatal("bonus leaked to another reward owner")
		}
	}
}

/*
================
TestGoldBonusFollowsWinningContributorThroughFatalHit

Exercise the actual settlement call edge. The final attacker earns its share
of experience but cannot replace the contributor whose bonus owns the heap.
================
*/
func TestGoldBonusFollowsWinningContributorThroughFatalHit(t *testing.T) {
	rt, _, actor, target := newCombatTestRuntime(t, 100)
	owner := *actor
	owner.ID, owner.Name = 4, "gold-owner"
	deps := rt.deps.(*enterworld.Deps)
	source := deps.Characters.(enterworld.StaticCharacterSource)
	source[testDivision] = append(source[testDivision], &owner)
	const skillID = 900001
	deps.Skills.(staticSkillSource)[skillID] = enterworld.SkillRow{
		ID: skillID, Group: skillID, Level: 1, EffectDurationMs: 10000,
		TimedEffect: enterworld.SkillTimedEffect{Pinned: true, Persistent: true, ItemProgram: true, GoldDropPercent: 50},
	}
	if !rt.ApplyCharacterEffect(testDivision, owner.Name, skillID, 1001, statuseffect.StateActive, false) {
		t.Fatal("could not install contributor's blessing")
	}
	ownerGID := enterworld.ObjectIDForCharacter(&owner)
	hits := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, 100, []simulation.MonsterDamagePlan{
		{GID: target.Gid, Damage: 99, CreditGID: ownerGID},
	})
	if len(hits) != 1 || hits[0].Fatal {
		t.Fatal("nonfatal contribution setup failed")
	}
	installSmallGoldRef(rt)
	rt.DropRoll = goldOnlyMonsterDropRoll(0, 0)
	rt.HandleTargetInteract(testDivision, actor, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	drops := rt.Ground.All(testDivision)
	if len(drops) != 1 || drops[0].OwnerJID != ownerGID || drops[0].GoldAmount != 42 {
		t.Fatalf("expected winner's 50%% bonus on the 28-gold heap, got %+v", drops)
	}
}
