/*
===========================================================================

skillfixeddamage_test.go - pdmg / dmgt admission (Tuning Noise and Sound)

Every shipped Tuning Noise and Tuning Sound tier compiles as one targeted
fixed-damage hit whose damage is drained as MP; near-miss programs stay
on the refused offense gate.

===========================================================================
*/

package enterworld

import (
	"fmt"
	"strconv"
	"testing"
)

/*
================
TestShippedTuningRowsCompileAsFixedDamage

The 12 Tuning Noise and 8 Tuning Sound tiers: pdmg is the amount (130 ..
1115, 1379 .. 3405), dmgt hands 100 percent to MP, one impact, an enemy
target, the harp column, BDMD on the prepared cost and an offensive
execution plan.
================
*/
func TestShippedTuningRowsCompileAsFixedDamage(t *testing.T) {
	source := sharedShippedSkills(t)
	lines := []struct {
		line    string
		amounts []uint32
	}{
		{"SKILL_EU_BARD_FORGETA_MPABSORB_A", []uint32{130, 174, 224, 281, 346, 421, 504, 599, 706, 827, 963, 1115}},
		{"SKILL_EU_BARD_FORGETA_MPABSORB_B", []uint32{1379, 1582, 1809, 2063, 2346, 2661, 3013, 3405}},
	}
	for _, line := range lines {
		for i, amount := range line.amounts {
			code := fmt.Sprintf("%s_%02d", line.line, i+1)
			row, ok := source.SkillByCodename(code)
			if !ok {
				t.Fatalf("missing %s", code)
			}
			want := SkillFixedDamage{Present: true, Amount: amount, MPPercent: 100}
			if row.FixedDamage != want || row.OffenseRefusal != "" || !row.DirectOffensePinned || !row.OffensiveStagePinned {
				t.Fatalf("%s: fixed=%+v refusal=%q direct=%v", code, row.FixedDamage, row.OffenseRefusal, row.DirectOffensePinned)
			}
			if row.Attack.Present || row.Attack.ImpactCount != 1 || !row.TargetRequired || !row.Targets.EnemyM ||
				row.ActionCastingTimeMs != 0 || row.RequiredWeaponKinds[0] != harpWeaponKind ||
				row.StatusCast || row.AreaBurst || row.OffensiveArea != (SkillOffensiveArea{}) {
				t.Fatalf("%s: attack=%+v target=%v cast=%d weapon=%v", code, row.Attack, row.TargetRequired, row.ActionCastingTimeMs, row.RequiredWeaponKinds)
			}
			if !row.Attack.Parameters.Has(ParameterBardMPDecrease) {
				t.Fatalf("%s: getv BDMD lost", code)
			}
			if plan := source.ExecutionPlan(row.ID); plan.Kind() != SkillExecutionOffense {
				t.Fatalf("%s: execution plan %v", code, plan.Kind())
			}
		}
	}
}

/*
================
fixedDamageFields

A synthetic instant enemy-targeted row (Required, Animal, Enemy_M,
Enemy_P) carrying the given program.
================
*/
func fixedDamageFields(program ...int64) []string {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[8] = "1", "2"
	fields[22], fields[23], fields[29], fields[30] = "1", "1", "1", "1"
	for i, value := range program {
		fields[69+i] = strconv.FormatInt(value, 10)
	}
	return fields
}

/*
================
TestFixedDamageRefusesNearMissShapes

Only the complete pdmg + dmgt program is admitted: a missing pdmg or
dmgt, a zero amount, an HP share (word 0, no owner), a share above the
whole damage, an extra instruction, a friendly target or a casting time
leave the row refused.
================
*/
func TestFixedDamageRefusesNearMissShapes(t *testing.T) {
	const (
		ko   = 0x6b6f
		bdmd = 0x42444d44
	)
	base := SkillRow{TimingPinned: true, ActionRangePinned: true, ActionRange: 150, ActionDurationMs: 2000,
		Consumption: SkillConsumption{MP: 29, Pinned: true}, TargetRequired: true, Targets: SkillTargets{Required: true, Animal: true, EnemyM: true, EnemyP: true}}
	friendly := fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, 100)
	friendly[27], friendly[28] = "1", "1"
	cases := []struct {
		name   string
		fields []string
		row    SkillRow
		want   bool
	}{
		{"tuning", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, 100, tagGetv, bdmd), base, true},
		{"no-transfer", fixedDamageFields(tagFixedDamage, 130, tagGetv, bdmd), base, false},
		{"no-damage", fixedDamageFields(tagDamageTransfer, 0, 100), base, false},
		{"zero-amount", fixedDamageFields(tagFixedDamage, 0, tagDamageTransfer, 0, 100), base, false},
		{"hp-share", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 50, 100), base, false},
		{"above-whole", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, maxDamageTransferPercent+1), base, false},
		{"knockdown", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, 100, ko, 1, 50), base, false},
		{"friendly", friendly, base, false},
		{"casting-time", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, 100), func() SkillRow { r := base; r.ActionCastingTimeMs = 1000; return r }(), false},
	}
	for _, tc := range cases {
		row := tc.row
		parseSkillOffense(tc.fields, &row)
		if row.FixedDamage.Present != tc.want || (row.OffenseRefusal == "") != tc.want || row.DirectOffensePinned != tc.want {
			t.Fatalf("%s: fixed=%+v refusal=%q direct=%v", tc.name, row.FixedDamage, row.OffenseRefusal, row.DirectOffensePinned)
		}
		if tc.want && row.FixedDamage != (SkillFixedDamage{Present: true, Amount: 130, MPPercent: 100}) {
			t.Fatalf("%s: fixed=%+v", tc.name, row.FixedDamage)
		}
	}
}
