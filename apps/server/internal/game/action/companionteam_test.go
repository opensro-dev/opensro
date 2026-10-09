/*
===========================================================================

companionteam_test.go - a companion never strikes its owner's free-battle team

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestCompanionNeverStrikesItsOwnersFreeBattleTeam

CGObjCOS_ValidateAttackTargetThroughOwner (528F40) refuses a player on the
owner's own cape team with 0x3020 before the owner's check, so aggression
that opens the team-mate to the owner still keeps the owner's pet off it.
Group 5 opposes everyone, and different or missing capes are left to the
owner's check.
================
*/
func TestCompanionNeverStrikesItsOwnersFreeBattleTeam(t *testing.T) {
	p := scornOpponentPair(t, shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_A_01"))
	items := p.rt.deps.ItemReferences().(staticItemSource)
	ownerCape, targetCape := items[p.c.MissionInventory[1].Codename], items[p.m.MissionInventory[1].Codename]
	if code := p.rt.companionTeamRefusal(p.c, p.m); code != 0 {
		t.Fatalf("opposing capes refused %#x", code)
	}
	targetCape.NativeFields = targetCape.NativeFields.With(freeBattleGroupField, 1)
	p.c.Level, p.m.Level = testInt64(20), testInt64(20)
	p.c.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(p.m): 20}
	if p.rt.playerAttackTargetRefusal(testDivision, p.c, p.m, p.rt.Now().UnixMilli()) != 0 {
		t.Fatal("fixture: aggression no longer opens the team-mate to the owner")
	}
	if code := p.rt.companionTeamRefusal(p.c, p.m); code != companionTeamRefused {
		t.Fatalf("a team-mate was open to the owner's pet: %#x", code)
	}
	ownerCape.NativeFields = ownerCape.NativeFields.With(freeBattleGroupField, freeBattleAllOpponents)
	targetCape.NativeFields = targetCape.NativeFields.With(freeBattleGroupField, freeBattleAllOpponents)
	if code := p.rt.companionTeamRefusal(p.c, p.m); code != 0 {
		t.Fatalf("group 5 protected its own: %#x", code)
	}
	ownerCape.NativeFields = ownerCape.NativeFields.With(freeBattleGroupField, 0)
	if code := p.rt.companionTeamRefusal(p.c, p.m); code != 0 {
		t.Fatalf("an owner out of free battle refused: %#x", code)
	}
}
