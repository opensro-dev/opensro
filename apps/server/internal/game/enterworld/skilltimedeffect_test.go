/*
===========================================================================

skilltimedeffect_test.go - shipped hunt links (hntp)

===========================================================================
*/

package enterworld

import "testing"

/*
================
TestShippedHuntingPointsCompileAsHuntLinks

Tag Point (four tiers) and Hunting Point carry nbuf bbuf lnks dura hntp
reqi on the targeted shape plus Enemy_P: a hunt link of group 13 whose
range and outgoing count are the row's lnks words.
================
*/
func TestShippedHuntingPointsCompileAsHuntLinks(t *testing.T) {
	source := sharedShippedSkills(t)
	for _, tc := range []struct {
		code     string
		distance uint32
		outgoing uint32
	}{
		{"SKILL_EU_ROG_STEALTHA_POINT_A_01", 10000, 1},
		{"SKILL_EU_ROG_STEALTHA_POINT_A_04", 20000, 1},
		{"SKILL_EU_ROG_STEALTHA_POINT_B_01", 25000, 2},
	} {
		row, ok := source.SkillByCodename(tc.code)
		link := row.TimedEffect.Link
		if !ok || !row.TimedEffect.Pinned || !row.TimedEffect.Targeted || !link.Present || !link.Hunt ||
			link.Group != 13 || link.MaxDistance != tc.distance || link.MaxOutgoing != tc.outgoing ||
			link.Threat || link.Mana {
			t.Fatalf("%s: pinned=%v link=%+v", tc.code, row.TimedEffect.Pinned, link)
		}
	}
}
