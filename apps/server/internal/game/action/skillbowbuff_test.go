/*
===========================================================================

skillbowbuff_test.go - the bow's timed buffs: White Hawk Summon (hr) and
Demon Soul Arrow (ru)

Both were refused as "offensive-shape-unsupported": the timed self-effect
qualifier knew neither tag. 594AC0 installs hr on the hit-rate keeper (0xB)
and ru on the attack-range keeper (0x21), which is the reach of every skill
without its own range (CGObjChar_GetAttackRangeParam 4AC890).

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
castBowBuff

An archer (bow, range 180) casting the shipped first rank of code; the
cast must open and its effect attach.
================
*/
func castBowBuff(t *testing.T, code string) (*Runtime, *enterworld.Character, enterworld.SkillRow) {
	t.Helper()
	rt, c, _, _, _ := arrowFixture(t)
	skill := shippedOffense(t, code)
	if !skill.TimedEffect.Pinned {
		t.Fatalf("%s is not a timed self effect: %+v", code, skill.TimedEffect)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(10000)
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if result.DiagnosticRefusal != "" {
		t.Fatalf("%s refused: %s", code, result.DiagnosticRefusal)
	}
	opened := false
	for _, f := range result.Frames {
		opened = opened || f.Opcode == wire.OpSkillCastResult && len(f.Payload) > 0 && f.Payload[0] == 1
	}
	if !opened {
		t.Fatalf("%s did not open its cast: %+v", code, result.Frames)
	}
	return rt, c, skill
}

/*
================
TestWhiteHawkSummonRaisesHitRate
================
*/
func TestWhiteHawkSummonRaisesHitRate(t *testing.T) {
	plain, c0, _, _, _ := arrowFixture(t)
	before, _, err := plain.playerCombatStats(testDivision, c0)
	if err != nil {
		t.Fatal(err)
	}
	rt, c, skill := castBowBuff(t, "SKILL_CH_BOW_CALL_A_01")
	after, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if want := before.HitRate + float64(skill.BuffModifiers.HrFlat); skill.BuffModifiers.HrFlat == 0 || after.HitRate < want {
		t.Fatalf("hit rate %v -> %v, want at least %v", before.HitRate, after.HitRate, want)
	}
}

/*
================
TestDemonSoulArrowLengthensTheBowsReach
================
*/
func TestDemonSoulArrowLengthensTheBowsReach(t *testing.T) {
	rt, c, skill := castBowBuff(t, "SKILL_CH_BOW_NORMAL_A_01")
	stats, loadout, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	basic := enterworld.SkillRow{}
	reach := skillActionReach(basic, loadout, stats)
	if want := float64(loadout.ActionRange) + float64(skill.BuffModifiers.RuRate); skill.BuffModifiers.RuRate == 0 || float64(reach) != want {
		t.Fatalf("bow reach %+v, want %v (range %v + ru %d)", reach, want, loadout.ActionRange, skill.BuffModifiers.RuRate)
	}
}
