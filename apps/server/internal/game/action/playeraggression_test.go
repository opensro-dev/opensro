/*
===========================================================================

playeraggression_test.go - hostile-hit registration, countdown and relations

Exercise actor-owned counts through the production tick, including the final
protected tick and party changes while an opponent is still retained.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestPlayerAggressionLifetimeAndRefresh
================
*/
func TestPlayerAggressionLifetimeAndRefresh(t *testing.T) {
	p := newSupportPair(t, enterworld.SkillRow{})
	p.rt.RewardParties = nil
	now := p.clock.NowMs()
	gid := enterworld.ObjectIDForCharacter(p.m)
	if frames := p.rt.registerPlayerAttack(testDivision, p.c, p.m, now); len(frames) != 1 || p.c.PVPState() != 1 {
		t.Fatalf("neutral attack did not publish grey: %v", frames)
	}
	p.rt.advancePlayerAggressions(now + 18000)
	if p.c.Aggressions[gid] != 2 || p.c.PVPState() != 1 {
		t.Fatalf("countdown: %v", p.c.Aggressions)
	}
	if frames := p.rt.advancePlayerAggressions(now + 19000); len(frames) != 1 || p.c.PVPState() != 0 || p.c.Aggressions[gid] != 1 {
		t.Fatalf("last protected tick: state=%d counts=%v frames=%v", p.c.PVPState(), p.c.Aggressions, frames)
	}
	if frames := p.rt.refreshPlayerAggression(testDivision, p.c, p.m, now+19500); len(frames) != 1 {
		t.Fatal("refresh did not restore grey")
	}
	p.rt.advancePlayerAggressions(now + 20000)
	if p.c.Aggressions[gid] != 19 {
		t.Fatal("refresh moved the scheduled tick")
	}
	p.rt.advancePlayerAggressions(now + 40000)
	if len(p.c.Aggressions) != 0 || p.c.PVPState() != 0 {
		t.Fatal("delayed tick retained aggression")
	}
}

/*
================
TestPlayerAggressionPartyAndLegalOpponents
================
*/
func TestPlayerAggressionPartyAndLegalOpponents(t *testing.T) {
	p := scornOpponentPair(t, shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_A_01"))
	now := p.clock.NowMs()
	if frames := p.rt.registerPlayerAttack(testDivision, p.c, p.m, now); len(frames) != 0 || len(p.c.Aggressions) != 0 {
		t.Fatal("opposing capes created criminal aggression")
	}
	p.rt.refreshPlayerAggression(testDivision, p.c, p.m, now)
	p.rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(p.c), enterworld.ObjectIDForCharacter(p.m)}}}
	}
	p.rt.advancePlayerAggressions(now + 1000)
	if len(p.c.Aggressions) != 0 || p.c.PVPState() != 0 {
		t.Fatal("joining the same party retained criminal aggression")
	}
}

/*
================
TestPlayerRelationContextPrecedesCriminalState
================
*/
func TestPlayerRelationContextPrecedesCriminalState(t *testing.T) {
	p := scornOpponentPair(t, shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_A_01"))
	items := p.rt.deps.ItemReferences().(staticItemSource)
	cape := items[p.m.MissionInventory[1].Codename]
	cape.NativeFields = cape.NativeFields.With(freeBattleGroupField, 1)
	p.c.Level, p.m.Level = testInt64(20), testInt64(20)
	p.c.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(p.m): 20}
	if p.rt.hostilePlayerRelation(testDivision, p.c, p.m) || p.rt.playerSkillRelationAllowed(testDivision, p.c, p.m, true) {
		t.Fatal("criminal flag overrode same-cape alliance")
	}
}
