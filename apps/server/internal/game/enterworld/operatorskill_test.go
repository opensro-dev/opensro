/*
===========================================================================

operatorskill_test.go - who may cast an operator row unlearned

===========================================================================
*/
package enterworld

import "testing"

/*
================
TestSkillUsableLetsOnlyAGmCastOperatorRows
================
*/
func TestSkillUsableLetsOnlyAGmCastOperatorRows(t *testing.T) {
	operator := SkillRow{ID: 3978, Codename: "SKILL_OP_HARMONY_A_01"}
	player := SkillRow{ID: 100, Codename: "SKILL_CH_WATER_HARMONY_A_01"}
	gm := &Character{Name: "Gm", GMPrivilege: true}
	ordinary := &Character{Name: "Player"}
	learner := &Character{Name: "Learner", Skills: []uint32{player.ID}}
	cases := []struct {
		label string
		c     *Character
		row   SkillRow
		want  bool
	}{
		{"GM, operator row", gm, operator, true},
		{"GM, unlearned player row", gm, player, false},
		{"player, operator row", ordinary, operator, false},
		{"player, learned row", learner, player, true},
		{"no character", nil, operator, false},
	}
	for _, c := range cases {
		if got := SkillUsable(c.c, c.row); got != c.want {
			t.Errorf("%s: SkillUsable = %v, want %v", c.label, got, c.want)
		}
	}
}
