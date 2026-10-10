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
