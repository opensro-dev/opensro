/*
===========================================================================

impactfinish_test.go - the shared impact tail against 5874D0 and 58FD41

===========================================================================
*/

package combat

import (
	"math/big"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestMonsterDamageScaleFollowsTheJumpTable

Every grade arm of 5875A8, for a solo (population kind 0) and a party
(kind 1) monster, and an unknown kind falling back to 1.
================
*/
func TestMonsterDamageScaleFollowsTheJumpTable(t *testing.T) {
	party := float32(1.29999995)
	for _, row := range []struct {
		rarity uint8
		want   float32
	}{
		{0x00, 1}, {0x01, 1}, {0x03, 1}, {0x08, 1},
		{0x02, 1},
		{0x04, 1.5}, {0x05, 1.5}, {0x06, 1.5},
		{0x07, float32(1.7999999523162842)},
		{0x09, 1}, {0x0f, 1},
		{0x10, party}, {0x11, party}, {0x13, party}, {0x18, party},
		{0x12, 1},
		{0x14, float32(float64(party) * 1.5)}, {0x16, float32(float64(party) * 1.5)},
		{0x17, float32(float64(party) * 1.7999999523162842)},
		{0x24, 1.5}, // an unknown population kind takes 1 before the grade
	} {
		if got := MonsterDamageScale(row.rarity); got != row.want {
			t.Errorf("rarity %#02x: scale %v, want %v", row.rarity, got, row.want)
		}
	}
}

/*
================
TestScaleDamageExactTruncatesTheExactProduct

58FD0A..58FD3A multiply on the x87 stack, where a 32-bit damage times a
float32 scale is exact, then truncate. Compare against an exact rational
product across the scales 5874D0 can return.
================
*/
func TestScaleDamageExactTruncatesTheExactProduct(t *testing.T) {
	scales := []float32{1, 1.29999995, 1.5, float32(float64(float32(1.29999995)) * 1.5),
		float32(1.7999999523162842), float32(float64(float32(1.29999995)) * 1.7999999523162842)}
	for _, scale := range scales {
		for _, damage := range []uint32{0, 1, 2, 3, 7, 10, 99, 101, 1234, 99999, 4194303, 6000000} {
			exact := new(big.Float).SetPrec(256).SetUint64(uint64(damage))
			exact.Mul(exact, new(big.Float).SetPrec(256).SetFloat64(float64(scale)))
			truncated, _ := exact.Int(nil)
			want := min(truncated.Uint64(), uint64(wire.MaxSkillActionDamage))
			if got := scaleDamageExact(damage, scale); uint64(got) != want {
				t.Errorf("damage %d x %v = %d, want %d", damage, scale, got, want)
			}
		}
	}
}

/*
================
TestFinishImpactAppliesPercentScaleThenFloor

58FC3F..58FD59 in order: percent, monster scale, then the att floor. A
blocked or slain record keeps its zero; a non-att zero stays zero; life
steal skips the percent it already took.
================
*/
func TestFinishImpactAppliesPercentScaleThenFloor(t *testing.T) {
	for _, row := range []struct {
		name string
		in   Result
		tail ImpactTail
		want uint32
	}{
		{"single target unchanged", Result{Damage: 500}, ImpactTail{Attack: true}, 500},
		{"area share truncates", Result{Damage: 4780}, ImpactTail{Percent: 42, Attack: true}, 2007},
		{"att share of one becomes one", Result{Damage: 1}, ImpactTail{Percent: 42, Attack: true}, 1},
		{"att zero becomes one", Result{Damage: 0}, ImpactTail{Attack: true}, 1},
		{"non-att zero stays zero", Result{Damage: 0}, ImpactTail{Percent: 42}, 0},
		{"non-att share of one stays zero", Result{Damage: 1}, ImpactTail{Percent: 42}, 0},
		{"giant scale", Result{Damage: 1000}, ImpactTail{MonsterAttacker: true, AttackerRarity: 0x04, Attack: true}, 1500},
		{"party champion scale", Result{Damage: 1000}, ImpactTail{MonsterAttacker: true, AttackerRarity: 0x11, Attack: true}, 1299},
		{"grade seven scale", Result{Damage: 1000}, ImpactTail{MonsterAttacker: true, AttackerRarity: 0x07, Attack: true}, 1799},
		{"percent before scale", Result{Damage: 1000}, ImpactTail{Percent: 65, MonsterAttacker: true, AttackerRarity: 0x04, Attack: true}, 975},
		{"scale only for a monster attacker", Result{Damage: 1000}, ImpactTail{AttackerRarity: 0x04, Attack: true}, 1000},
		{"life steal keeps its own percent", Result{Damage: 300}, ImpactTail{Percent: 42, PercentApplied: true}, 300},
	} {
		if got := FinishImpact(row.in, row.tail).Damage; got != row.want {
			t.Errorf("%s: damage %d, want %d", row.name, got, row.want)
		}
	}
	for _, row := range []Result{{Blocked: true}, {Slain: true}} {
		if got := FinishImpact(row, ImpactTail{Attack: true, MonsterAttacker: true, AttackerRarity: 4}); got.Damage != 0 {
			t.Errorf("%+v kept damage %d, want 0", row, got.Damage)
		}
	}
}
