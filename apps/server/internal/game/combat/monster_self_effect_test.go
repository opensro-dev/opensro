/*
===========================================================================

monster_self_effect_test.go - tests for monster_self_effect.go

===========================================================================
*/

package combat

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestMonsterSelfEffectParametersAndDamagePlacement(t *testing.T) {
	s := Stats{PhysicalDefense: 100, MagicalDefense: 200, CriticalRate: 90}
	e := monster.SelfEffects{{Token: 1, Tag: 0x64656670, First: 16, Second: 7}, {Token: 2, Tag: 0x6372, First: 10, Second: 20}, {Token: 3, Tag: 0x647275, First: 20, Second: 30}}
	if err := applyMonsterSelfEffects(&s, e, monster.TargetEffects{}, nil); err != nil {
		t.Fatal(err)
	}
	if s.PhysicalDefense != 116 || s.MagicalDefense != 207 || s.CriticalRate != 100 || s.PhysicalBasicRate != 20 || s.PhysicalSkillRate != 20 || s.MagicalBasicRate != 30 || s.MagicalSkillRate != 30 {
		t.Fatal("native parameter writes", s)
	}
	roll := func() (uint32, error) { return 0, nil }
	attacker := Stats{Level: 1, HitRate: 1, PhysicalAttackMin: 100, PhysicalAttackMax: 100, MagicalAttackMin: 100, MagicalAttackMax: 100, PhysicalBasicRate: 20, PhysicalSkillRate: 50, MagicalBasicRate: 30, MagicalSkillRate: 40}
	defender := Stats{Level: 1, PhysicalDefense: 80, MagicalDefense: 80}
	for _, tc := range []struct{ flags, want uint32 }{{5, 24}, {6, 30}, {9, 26}, {10, 28}, {4, 20}, {8, 20}} {
		result, err := ResolveMonster(attacker, defender, enterworld.SkillAttack{Present: true, Flags: tc.flags, Percent: 100}, roll)
		if err != nil || result.Damage != tc.want {
			t.Fatal("post-defense rate", tc, result, err)
		}
	}
}

/*
================
TestRocEnrageStepsGrowEveryChannel

The Roc's mom enrage (ten per step): each step adds ten to the four
damage rates (flat), ten percent to both defences, and both to evasion
and hit rate. Three steps write thirty.
================
*/
func TestRocEnrageStepsGrowEveryChannel(t *testing.T) {
	base := Stats{PhysicalDefense: 1000, MagicalDefense: 2000, EvasionRate: 100, HitRate: 200,
		PhysicalBasicRate: 100, PhysicalSkillRate: 100, MagicalBasicRate: 100, MagicalSkillRate: 100}
	for _, tc := range []struct {
		steps uint32
		want  Stats
	}{
		{1, Stats{PhysicalDefense: 1100, MagicalDefense: 2200, EvasionRate: 121, HitRate: 231,
			PhysicalBasicRate: 110, PhysicalSkillRate: 110, MagicalBasicRate: 110, MagicalSkillRate: 110}},
		{3, Stats{PhysicalDefense: 1300, MagicalDefense: 2600, EvasionRate: 169, HitRate: 299,
			PhysicalBasicRate: 130, PhysicalSkillRate: 130, MagicalBasicRate: 130, MagicalSkillRate: 130}},
	} {
		s := base
		e := monster.SelfEffects{{Token: 9, Tag: enterworld.TagMonsterStacking, First: 600000, Second: 10, Steps: tc.steps}}
		if err := applyMonsterSelfEffects(&s, e, monster.TargetEffects{}, nil); err != nil {
			t.Fatal(err)
		}
		got := [8]float64{s.PhysicalDefense, s.MagicalDefense, s.EvasionRate, s.HitRate,
			float64(s.PhysicalBasicRate), float64(s.PhysicalSkillRate), float64(s.MagicalBasicRate), float64(s.MagicalSkillRate)}
		want := [8]float64{tc.want.PhysicalDefense, tc.want.MagicalDefense, tc.want.EvasionRate, tc.want.HitRate,
			float64(tc.want.PhysicalBasicRate), float64(tc.want.PhysicalSkillRate), float64(tc.want.MagicalBasicRate), float64(tc.want.MagicalSkillRate)}
		if got != want {
			t.Fatalf("%d steps: defences, evasion, hit, rates = %v, want %v", tc.steps, got, want)
		}
	}
}
