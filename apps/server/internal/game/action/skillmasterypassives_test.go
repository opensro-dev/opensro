/*
===========================================================================

skillmasterypassives_test.go - the Chinese mastery passives that raise a stat

The bow (hr), lightning (er), spear (hpi), water (mpi) and fire (dru)
passives were refused by the passive qualifier and raised nothing. A
learned passive is a standing instance; 594AC0 installs its block.

===========================================================================
*/

package action

import "testing"

/*
================
TestMasteryPassivesRaiseTheirStats
================
*/
func TestMasteryPassivesRaiseTheirStats(t *testing.T) {
	for _, tc := range []struct {
		code  string
		param uint16
		gain  float64
	}{
		{"SKILL_CH_BOW_PASSIVE_A_01", 11, 9},
		{"SKILL_CH_LIGHTNING_PASSIVE_A_01", 9, 9},
		{"SKILL_CH_SPEAR_PASSIVE_A_01", 3, 102},
		{"SKILL_CH_WATER_PASSIVE_A_01", 4, 102},
		{"SKILL_CH_FIRE_PASSIVE_A_01", 0x80, 1},
	} {
		t.Run(tc.code, func(t *testing.T) {
			rt, c, _, _, _ := arrowFixture(t)
			before, _, err := rt.playerCombatStats(testDivision, c)
			if err != nil {
				t.Fatal(err)
			}
			row := shippedOffense(t, tc.code)
			if !row.PassiveParameters.Pinned {
				t.Fatalf("%s is not an admitted passive", tc.code)
			}
			rt.deps.SkillData().(staticSkillSource)[row.ID] = row
			c.Skills = append(c.Skills, row.ID)
			after, _, err := rt.playerCombatStats(testDivision, c)
			if err != nil {
				t.Fatal(err)
			}
			was, _ := before.Param(tc.param)
			now, _ := after.Param(tc.param)
			if float64(now)-float64(was) < tc.gain {
				t.Fatalf("parameter %#x %v -> %v, want at least +%v", tc.param, was, now, tc.gain)
			}
		})
	}
}
