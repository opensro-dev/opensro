/*
===========================================================================

jobtarget_test.go - a job monster never strikes its own side

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

The native attack target validation at the monster's attack admission:
a thief monster strikes a trader or hunter but never a thief, a hunter
monster strikes a thief but never a trader or hunter, neither strikes a
player without a suit (job state 4), and an ordinary monster strikes
everyone. A refused target ends admission with the zero
result (not accepted: the monster goes home).
================
*/
func TestJobMonsterStrikesOnlyTheOpposingSide(t *testing.T) {
	for _, tc := range []struct {
		name       string
		typeID4    uint8
		job        int64
		wantStruck bool
	}{
		{"thief monster, thief", 2, 2, false},
		{"thief monster, trader", 2, 1, true},
		{"thief monster, hunter", 2, 3, true},
		{"hunter monster, trader", 3, 1, false},
		{"hunter monster, hunter", 3, 3, false},
		{"hunter monster, thief", 3, 2, true},
		{"ordinary monster, thief", 1, 2, true},
		{"thief monster, no suit", 2, 0, false},
		{"hunter monster, no suit", 3, 0, false},
		{"ordinary monster, no suit", 1, 0, true},
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
			if tc.job != 0 {
				dressForJob(rt, c, 9310, "TEST_JOB_SUIT", tc.job)
			}
			mob.Ref.TidWord, mob.Ref.TypeID4 = 0x00c6, tc.typeID4
			result := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
			if result.Accepted != tc.wantStruck {
				t.Fatalf("accepted %v, want %v: %+v", result.Accepted, tc.wantStruck, result)
			}
		})
	}
}
