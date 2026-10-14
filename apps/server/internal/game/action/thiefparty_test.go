/*
===========================================================================

thiefparty_test.go - a party's thieves raise its monster EXP and gold

Port-only, not native: off, nothing changes; on, each dressed thief in the
party adds 10%, capped at 50%, and only the member's own party counts.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestThiefPartyBonusFollowsTheFlag
================
*/
func TestThiefPartyBonusFollowsTheFlag(t *testing.T) {
	rt, _, actor, _ := newCombatTestRuntime(t, 100)
	deps := rt.deps.(*enterworld.Deps)
	source := fixtureCharacters(deps.Characters)
	members := []*enterworld.Character{actor}
	for i := 0; i < 6; i++ {
		member := *actor
		member.ID, member.Name = int64(10+i), "member"+string(rune('a'+i))
		member.MissionInventory = nil
		source[testDivision] = append(source[testDivision], &member)
		members = append(members, &member)
	}
	outsider := *actor
	outsider.ID, outsider.Name, outsider.MissionInventory = 30, "outsider", nil
	dressThief(&outsider)
	source[testDivision] = append(source[testDivision], &outsider)
	var gids []uint32
	for _, member := range members {
		gids = append(gids, enterworld.ObjectIDForCharacter(member))
	}
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: gids}} }
	party := rt.rewardPartyOf(testDivision, gids[0])

	for _, tc := range []struct {
		on      bool
		thieves int
		percent int
	}{
		{false, 3, 0},
		{true, 0, 0},
		{true, 2, 20},
		{true, 5, 50},
		{true, 6, 50},
	} {
		rt.ThiefPartyBonus = tc.on
		for i, member := range members[1:] {
			member.MissionInventory = nil
			member.Job.Type = 0
			if i < tc.thieves {
				dressThief(member)
			}
		}
		if got := rt.thiefPartyPercent(testDivision, party); got != tc.percent {
			t.Fatalf("on=%v thieves=%d: percent %d, want %d", tc.on, tc.thieves, got, tc.percent)
		}
		if got, want := thiefPartyExp(1000, tc.percent), int64(1000+10*tc.percent); got != want {
			t.Fatalf("on=%v thieves=%d: exp %d, want %d", tc.on, tc.thieves, got, want)
		}
		drops := []grounditem.Item{{GoldAmount: 1000}, {StackCount: 3}}
		rt.applyThiefPartyGold(testDivision, actor, drops)
		if want := uint32(1000 + 10*tc.percent); drops[0].GoldAmount != want || drops[1].GoldAmount != 0 || drops[1].StackCount != 3 {
			t.Fatalf("on=%v thieves=%d: drops %+v, want gold %d", tc.on, tc.thieves, drops, want)
		}
	}

	// A thief outside the party, and a player without one, earn nothing.
	rt.ThiefPartyBonus = true
	if got := rt.thiefPartyPercent(testDivision, nil); got != 0 {
		t.Fatalf("no party: percent %d", got)
	}
	solo := []grounditem.Item{{GoldAmount: 1000}}
	rt.applyThiefPartyGold(testDivision, &outsider, solo)
	if solo[0].GoldAmount != 1000 {
		t.Fatalf("a thief without a party raised its own gold: %+v", solo)
	}
}

/*
================
TestThiefPartyGoldThroughAKill

Through the real kill settlement (HandleTargetInteract): the drop owner's
party holds two dressed thieves, so its 28-gold heap and its kill EXP are
20% larger with the flag on and native with it off.
================
*/
func TestThiefPartyGoldThroughAKill(t *testing.T) {
	earned := map[bool]int64{}
	for on, want := range map[bool]uint32{false: 28, true: 33} {
		rt, _, actor, target := newCombatTestRuntime(t, 100)
		rt.ThiefPartyBonus = on
		deps := rt.deps.(*enterworld.Deps)
		source := fixtureCharacters(deps.Characters)
		owner, thief := *actor, *actor
		owner.ID, owner.Name, owner.MissionInventory = 4, "gold-owner", nil
		thief.ID, thief.Name, thief.MissionInventory = 5, "party-thief", nil
		dressThief(&owner)
		dressThief(&thief)
		source[testDivision] = append(source[testDivision], &owner, &thief)
		ownerGID := enterworld.ObjectIDForCharacter(&owner)
		party := []uint32{ownerGID, enterworld.ObjectIDForCharacter(&thief)}
		rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: party}} }
		hits := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, 100, []simulation.MonsterDamagePlan{
			{GID: target.Gid, Damage: 99, CreditGID: ownerGID},
		})
		if len(hits) != 1 || hits[0].Fatal {
			t.Fatal("nonfatal contribution setup failed")
		}
		installSmallGoldRef(rt)
		rt.DropRoll = goldOnlyMonsterDropRoll(0, 0)
		rt.UpdateExperience = func(c *enterworld.Character, delta, _ int64, _ uint32) ([]wire.Frame, bool) {
			if c.Name == owner.Name {
				earned[on] += delta
			}
			return nil, true
		}
		rt.HandleTargetInteract(testDivision, actor, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
		drops := rt.Ground.All(testDivision)
		if len(drops) != 1 || drops[0].OwnerJID != ownerGID || drops[0].GoldAmount != want {
			t.Fatalf("on=%v: drops %+v, want one %d-gold heap", on, drops, want)
		}
	}
	if earned[false] <= 0 || earned[true] != thiefPartyExp(earned[false], 20) {
		t.Fatalf("owner EXP off %d, on %d, want on = off + 20%%", earned[false], earned[true])
	}
}

/*
================
TestThiefPartyBonusFromEnv
================
*/
func TestThiefPartyBonusFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvThiefPartyBonus, value)
		if got := ThiefPartyBonusFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}
