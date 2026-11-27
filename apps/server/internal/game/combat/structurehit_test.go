/*
===========================================================================

structurehit_test.go - the target-side hit group's amount: attack-power
draws scaled by the per-weapon-kind factor

Every kind row of the native factor switch is pinned against a hand
computed amount, so mutating any factor, any pair choice, or the draw
itself turns a row red.

===========================================================================
*/
package combat

import (
	"errors"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

// structureHitAttacker spans 199 on both pairs, so a draw of 100 lands
// on round powers: 10000 physical, 21000 magical.
func structureHitAttacker() Stats {
	return Stats{
		PhysicalAttackMin: 9900, PhysicalAttackMax: 10099,
		MagicalAttackMin: 20900, MagicalAttackMax: 21099,
	}
}

/*
================
TestStructureHitDamageKinds

The factor switch row by row. Kinds 0, 1 and anything above 16 deal
exactly 1 and draw nothing; kind 16 is the bare physical draw; the
staff kinds (10, 11, 14, 15) draw from the magical pair.
================
*/
func TestStructureHitDamageKinds(t *testing.T) {
	for _, tc := range []struct {
		kind  uint32
		want  uint32
		draws int
	}{
		{0, 1, 0},
		{1, 1, 0},
		{2, 3000, 1},  // 10000 x 0.30
		{3, 2700, 1},  // 10000 x 0.90 x 0.30
		{4, 5220, 1},  // 10000 x 0.90 x 0.58
		{5, 4698, 1},  // 10000 x 0.90 x 0.90 x 0.58
		{6, 1260, 1},  // 10000 x 0.30 x 0.42
		{7, 4356, 1},  // 10000 x 0.66 x 0.66
		{8, 6600, 1},  // 10000 x 0.66
		{9, 2508, 1},  // 10000 x 0.66 x 0.38
		{10, 4357, 1}, // 21000 x 0.5 x 0.5 x 0.83
		{11, 4357, 1}, // 21000 x 0.5 x 0.5 x 0.83
		{12, 1410, 1}, // 10000 x 0.47 x 0.30
		{13, 1650, 1}, // 10000 x 0.66 x 0.25
		{14, 5250, 1}, // 21000 x 0.5 x 0.5
		{15, 4357, 1}, // 21000 x 0.5 x 0.5 x 0.83
		{16, 10000, 1},
		{17, 1, 0},
		{31, 1, 0},
	} {
		draws := 0
		got, err := StructureHitDamage(tc.kind, structureHitAttacker(), func() (uint32, error) {
			draws++
			return 100, nil
		})
		if err != nil {
			t.Fatalf("kind %d: %v", tc.kind, err)
		}
		if got != tc.want {
			t.Errorf("kind %d: damage %d, want %d", tc.kind, got, tc.want)
		}
		if draws != tc.draws {
			t.Errorf("kind %d: consumed %d draws, want %d", tc.kind, draws, tc.draws)
		}
	}
}

/*
================
TestStructureHitDamagePairEdges

The pair is read as truncated integers and a pair with no span deals
0 without consuming a draw, natively.
================
*/
func TestStructureHitDamagePairEdges(t *testing.T) {
	// Fractional bounds truncate before the span is taken:
	// 100..200, draw 50 -> 150, x 0.30 -> 45.
	attacker := Stats{PhysicalAttackMin: 100.9, PhysicalAttackMax: 200.9}
	got, err := StructureHitDamage(2, attacker, func() (uint32, error) { return 50, nil })
	if err != nil {
		t.Fatal(err)
	}
	if got != 45 {
		t.Errorf("truncated pair: damage %d, want 45", got)
	}

	for _, tc := range []struct {
		name     string
		min, max float64
	}{
		{"equal bounds", 500, 500},
		{"inverted bounds", 600, 500},
	} {
		draws := 0
		attacker := Stats{PhysicalAttackMin: tc.min, PhysicalAttackMax: tc.max}
		got, err := StructureHitDamage(2, attacker, func() (uint32, error) {
			draws++
			return 7, nil
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != 0 || draws != 0 {
			t.Errorf("%s: damage %d with %d draws, want 0 with none", tc.name, got, draws)
		}
	}
}

/*
================
TestStructureHitDamageRollError

A failing draw source surfaces its error and no damage.
================
*/
func TestStructureHitDamageRollError(t *testing.T) {
	want := errors.New("draw failed")
	got, err := StructureHitDamage(2, structureHitAttacker(), func() (uint32, error) { return 0, want })
	if !errors.Is(err, want) || got != 0 {
		t.Fatalf("got %d, err %v", got, err)
	}
}

/*
================
TestMonsterStatsCarriesStructure

The defender's structure mark travels from the monster ref into its
combat stats, as OneDamageHits does.
================
*/
func TestMonsterStatsCarriesStructure(t *testing.T) {
	marked, err := MonsterStats(monster.MonsterRef{Level: 1, CombatPinned: true, Structure: true})
	if err != nil {
		t.Fatal(err)
	}
	if !marked.Structure {
		t.Fatal("MonsterStats dropped the ref's Structure")
	}
	plain, err := MonsterStats(monster.MonsterRef{Level: 1, CombatPinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Structure {
		t.Fatal("MonsterStats marked an ordinary monster as a structure")
	}
}
