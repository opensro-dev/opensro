/*
===========================================================================

ignoredefense_test.go - tests for MATTR_AVATAR_MDIA ignore-defense mechanics

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestResolveCalculationIgnoreDefenseSkipsParryAndDefense

Verifies that IgnoreDefense skips both parry division and defense subtraction
in physical and magical lanes and preserves bit 0x20 in ResultFlags, including
the fully covered wall zero-damage branch.
================
*/
func TestResolveCalculationIgnoreDefenseSkipsParryAndDefense(t *testing.T) {
	attacker := Stats{
		Level: 1, MaxLevel: 1,
		Strength: 32, Intellect: 32,
		PhysicalAttackMin: 100, PhysicalAttackMax: 100,
		MagicalAttackMin: 100, MagicalAttackMax: 100,
		HitRate: 50,
	}
	defender := Stats{
		Level: 1, MaxLevel: 1,
		EvasionRate:     50,
		PhysicalDefense: 20, ParryRate: 100,
		MagicalDefense: 20, MagicalParry: 100,
	}

	for _, tc := range []struct {
		name string
		lane uint32
	}{
		{"physical", physicalAttackFlag},
		{"magical", magicalAttackFlag},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attack := enterworld.SkillAttack{Present: true, Flags: tc.lane, Percent: 100}
			normal, err := ResolveCalculation(attacker, defender, AttackCalculation{
				Attack: attack, OriginalFlags: attack.Flags, Lanes: tc.lane, Player: true,
			}, (&sequenceRoll{values: []uint32{0, 0, 0, 0}}).next)
			if err != nil {
				t.Fatalf("normal resolve: %v", err)
			}
			if normal.Damage != 30 || normal.ResultFlags != normalResultFlag {
				t.Fatalf("normal = %+v, want damage 30 flags %#x", normal, normalResultFlag)
			}

			ignored, err := ResolveCalculation(attacker, defender, AttackCalculation{
				Attack: attack, OriginalFlags: attack.Flags, Lanes: tc.lane, Player: true, IgnoreDefense: true,
			}, (&sequenceRoll{values: []uint32{0, 0, 0, 0}}).next)
			if err != nil {
				t.Fatalf("ignore-defense resolve: %v", err)
			}
			wantFlags := normalResultFlag | IgnoreDefenseResultFlag
			if ignored.Damage != 100 || ignored.ResultFlags != wantFlags {
				t.Fatalf("ignored = %+v, want damage 100 flags %#x", ignored, wantFlags)
			}
		})
	}

	// Covered wall path (every lane zeroed) must preserve 0x20 in ResultFlags.
	attack := enterworld.SkillAttack{Present: true, Flags: physicalAttackFlag, Percent: 100}
	wallOut, err := ResolveCalculationAgainstWall(attacker, defender, WallCalculation{
		Attack: AttackCalculation{
			Attack: attack, OriginalFlags: attack.Flags, Lanes: physicalAttackFlag, Player: true, IgnoreDefense: true,
		},
		Wall: enterworld.SkillWall{Mask: physicalAttackFlag, Defense: 20, Parry: 100},
	}, (&sequenceRoll{values: []uint32{0, 0, 0, 0}}).next)
	if err != nil {
		t.Fatalf("wall resolve: %v", err)
	}
	if !wallOut.Covered || wallOut.Absorbed != 100 || wallOut.Defender.Damage != 0 || wallOut.Defender.ResultFlags != normalResultFlag|IgnoreDefenseResultFlag {
		t.Fatalf("wall outcome = %+v, want covered absorbed 100 defender damage 0 flags %#x", wallOut, normalResultFlag|IgnoreDefenseResultFlag)
	}
}

/*
================
TestIgnoreDefenseChanceRequiresPlayerAttackerAndNonPlayerDefender

Verifies that IgnoreDefenseChance reads parameter 0xBC only when the attacker
is a player snapshot (has a graph) and the defender is not (no graph).
================
*/
func TestIgnoreDefenseChanceRequiresPlayerAttackerAndNonPlayerDefender(t *testing.T) {
	c := &domain.Character{Level: pointer(1), Strength: pointer(20), Intellect: pointer(20)}
	playerWithMDIA, _, err := PlayerStatsWithModifiers(
		c,
		Catalogs{Items: itemRefs{}},
		[]paramkeeper.Write{{Parameter: 0xbc, Channel: paramkeeper.Flat, Source: 2, Value: 3}},
		nil,
	)
	if err != nil {
		t.Fatalf("player with mdia: %v", err)
	}
	playerPlain, _, err := PlayerStats(c, Catalogs{Items: itemRefs{}})
	if err != nil {
		t.Fatalf("plain player: %v", err)
	}
	monsterDefender, err := MonsterStats(monster.MonsterRef{Level: 1, CombatPinned: true})
	if err != nil {
		t.Fatalf("monster stats: %v", err)
	}
	literalDefender := Stats{Level: 1}

	for _, tc := range []struct {
		name               string
		attacker, defender Stats
		want               float64
	}{
		{"player vs MonsterStats", playerWithMDIA, monsterDefender, 3},
		{"player vs Stats literal", playerWithMDIA, literalDefender, 3},
		{"player vs player", playerWithMDIA, playerPlain, 0},
		{"non-player vs monster", literalDefender, monsterDefender, 0},
		{"player without 0xbc vs monster", playerPlain, monsterDefender, 0},
	} {
		if got := IgnoreDefenseChance(tc.attacker, tc.defender); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
