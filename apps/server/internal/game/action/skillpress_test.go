/*
===========================================================================

skillpress_test.go - SkillPressHandled answers as the press runs (#1771)

The website's catalogue reads SkillPressHandled to list which skills a
press runs. An imbue or instant self effect is run before routing, so it
must report handled even though its route falls through to offensive.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestSkillPressHandledCountsInstantActivitySkills

An imbue with no offensive shape is handled (the press runs it through
acceptInstantSelfEffect); the same row as a chain stage is not, as the
instant admission refuses chain stages; a bare offensive row with no
admitted shape stays unhandled.
================
*/
func TestSkillPressHandledCountsInstantActivitySkills(t *testing.T) {
	imbue := enterworld.SkillRow{ID: 90, Group: 237, Codename: "SKILL_CH_COLD_GIGONGTA_A_01"}
	imbue.Imbue.Pinned = true
	if routeSkillCommand(imbue) != skillRouteOffensive {
		t.Fatal("fixture: the imbue no longer falls through to the offensive route; this test pins that case")
	}
	if !SkillPressHandled(nil, imbue) {
		t.Fatal("an imbue reports unhandled; the press runs it through acceptInstantSelfEffect")
	}
	instant := enterworld.SkillRow{ID: 20060, Group: 810, InstantSelfEffectPinned: true}
	if !SkillPressHandled(nil, instant) {
		t.Fatal("an instant self effect reports unhandled")
	}
	stage := imbue
	stage.ChainSub = true
	if SkillPressHandled(nil, stage) {
		t.Fatal("a chain-stage imbue reports handled; the instant admission refuses it")
	}
	bare := enterworld.SkillRow{ID: 1, Group: 1}
	if SkillPressHandled(nil, bare) {
		t.Fatal("an offensive row with no admitted shape reports handled")
	}
}
