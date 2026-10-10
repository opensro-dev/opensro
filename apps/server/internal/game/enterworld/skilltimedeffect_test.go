/*
===========================================================================

skilltimedeffect_test.go - shipped hunt links (hntp)

===========================================================================
*/

package enterworld

import (
	"fmt"
	"testing"
)

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

/*
================
TestShippedIllusionTiersAreDisguises

Every Illusion and Shade Illusion tier is an untargeted timed disguise
(msch 3) whose skc event mask ends it on the next cast; Duplicate (msch 2)
stays its own owner's.
================
*/
func TestShippedIllusionTiersAreDisguises(t *testing.T) {
	source := sharedShippedSkills(t)
	tiers := 0
	for _, line := range []string{"SKILL_EU_WARLOCK_CONFUSIONA_ILLUSION_A", "SKILL_EU_WARLOCK_CONFUSIONA_ILLUSION_B"} {
		for tier := 1; ; tier++ {
			row, ok := source.SkillByCodename(fmt.Sprintf("%s_%02d", line, tier))
			if !ok {
				break
			}
			tiers++
			d := row.TimedEffect
			if !d.Pinned || !d.Disguise || d.Targeted || d.Area.Present || d.Persistent || row.EffectDurationMs == 0 ||
				row.Replacement.EventCancelMask != 2 || source.ExecutionPlan(row.ID).Kind() != SkillExecutionTimedEffect {
				t.Fatalf("%s: %+v", row.Codename, d)
			}
		}
	}
	if tiers != 6 {
		t.Fatalf("%d Illusion tiers, want 6", tiers)
	}
	if row, _ := source.SkillByCodename("SKILL_EU_ROG_DUPLE_A_01"); row.TimedEffect.Pinned {
		t.Fatal("Duplicate admitted as a timed effect")
	}
}
