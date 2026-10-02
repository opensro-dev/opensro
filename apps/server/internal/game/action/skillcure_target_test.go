/*
===========================================================================

skillcure_target_test.go - independent self and animal cure target flags

An empty action vector falls back to Self, not to the object-kind selector.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestEmptyCureVectorUsesSelfFlag
================
*/
func TestEmptyCureVectorUsesSelfFlag(t *testing.T) {
	for _, self := range []bool{false, true} {
		rt, clock, c, monster := newCombatTestRuntime(t, 100)
		rt.CombatRoll = func() (uint32, error) { return 0, nil }
		seedPlayerStatus(rt, c, abnormal.Stun, 10000, clock.NowMs(), monster.Gid)
		if block := rt.playerAbnormal(testDivision, c.Name); block == nil || !block.Has(abnormal.Stun) {
			t.Fatal("fixture did not install stun")
		}
		skill := enterworld.SkillRow{Targets: enterworld.SkillTargets{Self: self, Animal: !self}}
		skill.Abnormal.Curl = true
		skill.Abnormal.CurlMask = int32(abnormal.Stun.Bit())
		skill.Abnormal.CurlChance = 100
		targets := rt.resolveSkillCureTargets(testDivision, c, c, skill, wire.SkillAction{}, clock.NowMs())
		rt.applySkillCure(testDivision, c, skill, targets, clock.NowMs())
		block := rt.playerAbnormal(testDivision, c.Name)
		stunned := block != nil && block.Has(abnormal.Stun)
		if stunned == self {
			t.Fatalf("self=%v animal=%v stun remained=%v", self, !self, stunned)
		}
	}
}
