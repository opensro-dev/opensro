/*
===========================================================================

jobtarget_test.go - a job monster never strikes its own side (529110)

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestJobMonsterStrikesOnlyTheOpposingSide

CGObjChar_ValidateAttackTarget (529110) at the monster's attack admission:
a thief monster strikes a trader or hunter but never a thief, a hunter
monster strikes a thief but never a trader or hunter, and an ordinary
monster strikes everyone. A refused target ends admission with the zero
result (not alive, not accepted: the monster goes home); an admitted one
passes on to the strike (TargetAlive set). The fixture suit has no item
reference, so the strike itself stops later; admission is what is pinned.
================
*/
func TestJobMonsterStrikesOnlyTheOpposingSide(t *testing.T) {
	for _, tc := range []struct {
		name         string
		typeID4      uint8
		job          int64
		wantAdmitted bool
	}{
		{"thief monster, thief", 2, 2, false},
		{"thief monster, trader", 2, 1, true},
		{"thief monster, hunter", 2, 3, true},
		{"hunter monster, trader", 3, 1, false},
		{"hunter monster, hunter", 3, 3, false},
		{"hunter monster, thief", 3, 2, true},
		{"ordinary monster, thief", 1, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, c, mob := newCombatTestRuntime(t, 100000)
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			c.BattleUntilMs = 0
			mob.Ref.DefaultSkillIDs[0] = 2
			skills := rt.deps.SkillData().(staticSkillSource)
			attack := skills[2]
			attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent = 60, 60, 100
			skills[2] = attack
			suit := &enterworld.ItemRef{RefObjID: 12, Codename: "TEST_JOB_SUIT", TypeIDs: [4]int64{3, 1, 7, tc.job}}
			c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(enterworld.JobSuitSlot),
				RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), VarianceBits: "0", StackCount: 1})
			mob.Ref.TidWord, mob.Ref.TypeID4 = 0x00c6, tc.typeID4
			result := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
			admitted := result.Accepted || result.TargetAlive
			if admitted != tc.wantAdmitted {
				t.Fatalf("admitted %v, want %v: %+v", admitted, tc.wantAdmitted, result)
			}
		})
	}
}
