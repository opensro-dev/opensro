/*
===========================================================================

formula_test.go - tests for formula.go

===========================================================================
*/

package combat

import (
	"errors"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

type sequenceRoll struct {
	values []uint32
	calls  int
}

func (s *sequenceRoll) next() (uint32, error) {
	if s.calls >= len(s.values) {
		return 0, errors.New("sequence exhausted")
	}
	value := s.values[s.calls]
	s.calls++
	return value, nil
}

func TestResolvePhysicalAndMagicalNormalLanes(t *testing.T) {
	attacker := Stats{
		Level: 1, MaxLevel: 1,
		Strength: 32, Intellect: 32,
		PhysicalAttackMin: 100, PhysicalAttackMax: 200,
		MagicalAttackMin: 100, MagicalAttackMax: 200,
		HitRate: 50,
	}
	defender := Stats{Level: 1, MaxLevel: 1, EvasionRate: 50}

	physicalRolls := &sequenceRoll{values: []uint32{0, 0, 0, 0}}
	physical, err := Resolve(attacker, defender, enterworld.SkillAttack{
		Present: true,
		Flags:   physicalAttackFlag,
		Percent: 100,
	}, physicalRolls.next)
	if err != nil {
		t.Fatalf("physical resolve: %v", err)
	}
	if physical.Damage != 150 || physical.ResultFlags != normalResultFlag {
		t.Fatalf("physical result = %+v, want damage 150 flags 1", physical)
	}
	if physicalRolls.calls != 4 {
		t.Fatalf("physical RNG calls = %d, want three spread + one direction", physicalRolls.calls)
	}

	magicalRolls := &sequenceRoll{values: []uint32{0, 0, 0, 0}}
	magical, err := Resolve(attacker, defender, enterworld.SkillAttack{
		Present: true,
		Flags:   magicalAttackFlag,
		Percent: 100,
	}, magicalRolls.next)
	if err != nil {
		t.Fatalf("magical resolve: %v", err)
	}
	if magical.Damage != 150 || magical.ResultFlags != normalResultFlag {
		t.Fatalf("magical result = %+v, want damage 150 flags 1", magical)
	}
	if magicalRolls.calls != 4 {
		t.Fatalf("magical RNG calls = %d, want three spread + one direction", magicalRolls.calls)
	}
}

func TestResolveUsesTheRetailMinimumDamageReplacement(t *testing.T) {
	rolls := &sequenceRoll{values: []uint32{0, 0, 0, 0, 32767}}
	result, err := Resolve(
		Stats{
			Level: 1, MaxLevel: 1,
			Strength:          32,
			PhysicalAttackMin: 100, PhysicalAttackMax: 100,
			HitRate: 1,
		},
		Stats{
			Level: 1, MaxLevel: 1,
			PhysicalDefense: 10000,
			EvasionRate:     1,
		},
		enterworld.SkillAttack{
			Present: true,
			Flags:   physicalAttackFlag,
			Percent: 100,
		},
		rolls.next,
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Damage != 10 {
		t.Fatalf("minimum-damage replacement = %d, want trunc(10%% of 100)=10", result.Damage)
	}
	if rolls.calls != 5 {
		t.Fatalf("minimum-damage RNG calls = %d, want four attack-point + one floor", rolls.calls)
	}
}

func TestHitCenterTreatsZeroEvasionAsTheNativeInfiniteRatio(t *testing.T) {
	got := hitCenter(
		Stats{Level: 1, HitRate: 10},
		Stats{Level: 1, EvasionRate: 0},
	)
	if got != 90 {
		t.Fatalf("hit center with positive HR / zero ER = %v, want upper clamp 90", got)
	}
}

func TestResolveRejectsUnpinnedAndInvalidInputs(t *testing.T) {
	roll := func() (uint32, error) { return 0, nil }
	for name, attack := range map[string]enterworld.SkillAttack{
		"absent":         {},
		"no damage lane": {Present: true, Flags: 1, Percent: 100},
		"negative":       {Present: true, Flags: 4, Percent: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Resolve(Stats{}, Stats{}, attack, roll); err == nil {
				t.Fatal("invalid formula input was accepted")
			}
		})
	}
}

func TestAtcaScalesDamageOnlyWhenTargetFlagMatches(t *testing.T) {
	attacker := Stats{Level: 1, MaxLevel: 1, Strength: 32, HitRate: 50, PhysicalAttackMin: 100, PhysicalAttackMax: 200}
	attack := enterworld.SkillAttack{Present: true, Flags: physicalAttackFlag, Percent: 100, Atca: true, AtcaMask: 0x41c0, AtcaPercent: 50}
	plain, err := Resolve(attacker, Stats{Level: 1, MaxLevel: 1, EvasionRate: 50}, attack, (&sequenceRoll{values: []uint32{0, 0, 0, 0}}).next)
	if err != nil || plain.Damage != 150 {
		t.Fatalf("flag clear %+v %v", plain, err)
	}
	got, err := Resolve(attacker, Stats{Level: 1, MaxLevel: 1, EvasionRate: 50, AbnormalMask: 0x4000}, attack, (&sequenceRoll{values: []uint32{0, 0, 0, 0}}).next)
	if err != nil || got.Damage != 225 {
		t.Fatalf("flag set %+v %v", got, err)
	}
}

func TestBerserkDamageFlagBothLanes(t *testing.T) {
	for _, flags := range []uint32{physicalAttackFlag, magicalAttackFlag} {
		a := Stats{Berserk: true, Level: 1, MaxLevel: 1, Strength: 32, Intellect: 32, PhysicalAttackMin: 100, PhysicalAttackMax: 200, MagicalAttackMin: 100, MagicalAttackMax: 200, HitRate: 50}
		r := &sequenceRoll{values: []uint32{0, 0, 0, 0}}
		got, err := Resolve(a, Stats{Level: 1, MaxLevel: 1, EvasionRate: 50}, enterworld.SkillAttack{Present: true, Flags: flags, Percent: 100}, r.next)
		if err != nil || got.Damage != 300 || got.ResultFlags != 5 {
			t.Fatalf("%+v %v", got, err)
		}
	}
}

/*
================
TestDefenderTakenFactorFollowsLaneAndAttackKind

AE..B1 scale only the matching lane and attack kind: a physical skill hit
on a defender with a 0.7 physical-skill factor drops from 150 to 105.
================
*/
func TestDefenderTakenFactorFollowsLaneAndAttackKind(t *testing.T) {
	attacker := Stats{Level: 1, MaxLevel: 1, Strength: 32, Intellect: 32, HitRate: 50,
		PhysicalAttackMin: 100, PhysicalAttackMax: 200, MagicalAttackMin: 100, MagicalAttackMax: 200}
	defender := Stats{Level: 1, MaxLevel: 1, EvasionRate: 50, PhysicalSkillTaken: 0.7}
	for _, tc := range []struct {
		flags uint32
		want  uint32
	}{
		{physicalAttackFlag | 2, 105},
		{physicalAttackFlag | 1, 150},
		{magicalAttackFlag | 2, 150},
	} {
		got, err := Resolve(attacker, defender, enterworld.SkillAttack{Present: true, Flags: tc.flags, Percent: 100},
			(&sequenceRoll{values: []uint32{0, 0, 0, 0}}).next)
		if err != nil || got.Damage != tc.want {
			t.Errorf("flags %#x: %+v %v, want damage %d", tc.flags, got, err, tc.want)
		}
	}
}
