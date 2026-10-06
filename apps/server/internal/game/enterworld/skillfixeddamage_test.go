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
TestShippedOverHealingRowsCompileAsFixedDamage

The 12 Over Healing tiers: pdmg is the amount (623 .. 7925), no dmgt
drains anything, one impact on an enemy target after the authored
preparation, and an offensive execution plan.
================
*/
func TestShippedOverHealingRowsCompileAsFixedDamage(t *testing.T) {
	source := sharedShippedSkills(t)
	amounts := []uint32{623, 857, 1141, 1482, 1892, 2381, 2964, 3655, 4475, 5442, 6583, 7925}
	for i, amount := range amounts {
		code := fmt.Sprintf("SKILL_EU_CLERIC_BATTLEA_OVERHEAL_A_%02d", i+1)
		row, ok := source.SkillByCodename(code)
		if !ok {
			t.Fatalf("missing %s", code)
		}
		want := SkillFixedDamage{Present: true, Amount: amount}
		if row.FixedDamage != want || row.OffenseRefusal != "" || !row.DirectOffensePinned || !row.OffensiveStagePinned {
			t.Fatalf("%s: fixed=%+v refusal=%q direct=%v", code, row.FixedDamage, row.OffenseRefusal, row.DirectOffensePinned)
		}
		if row.Attack.Present || row.Attack.ImpactCount != 1 || !row.TargetRequired || !row.Targets.EnemyM ||
			row.ActionCastingTimeMs == 0 || row.StatusCast || row.AreaBurst || row.OffensiveArea != (SkillOffensiveArea{}) {
			t.Fatalf("%s: attack=%+v target=%v cast=%d", code, row.Attack, row.TargetRequired, row.ActionCastingTimeMs)
		}
		if plan := source.ExecutionPlan(row.ID); plan.Kind() != SkillExecutionOffense {
			t.Fatalf("%s: execution plan %v", code, plan.Kind())
		}
	}
}

/*
================
TestShippedGlutHealingRowsCompileAsAreaFixedDamage

The 4 Glut Healing tiers: Over Healing's fixed hit (4780 .. 8422) with
efr(1,2,50,3,35,24), up to three victims around the target, each one
after the first at 35 percent less.
================
*/
func TestShippedGlutHealingRowsCompileAsAreaFixedDamage(t *testing.T) {
	source := sharedShippedSkills(t)
	area := SkillOffensiveArea{Shape: 2, Radius: 50, MaxTargets: 3, ReductionPercent: 35, Select: 24}
	for i, amount := range []uint32{4780, 5802, 7006, 8422} {
		code := fmt.Sprintf("SKILL_EU_CLERIC_BATTLEA_OVERHEAL_B_%02d", i+1)
		row, ok := source.SkillByCodename(code)
		if !ok {
			t.Fatalf("missing %s", code)
		}
		if row.FixedDamage != (SkillFixedDamage{Present: true, Amount: amount}) || row.OffensiveArea != area ||
			row.OffenseRefusal != "" || !row.DirectOffensePinned || row.Attack.ImpactCount != 1 || row.ActionCastingTimeMs == 0 {
			t.Fatalf("%s: fixed=%+v area=%+v refusal=%q", code, row.FixedDamage, row.OffensiveArea, row.OffenseRefusal)
		}
		if plan := source.ExecutionPlan(row.ID); plan.Kind() != SkillExecutionOffense {
			t.Fatalf("%s: execution plan %v", code, plan.Kind())
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

Only a complete pdmg program is admitted: a missing pdmg, a zero amount,
an HP share (word 0, no owner), a share above the whole damage, a cm with
more than one result, an extra instruction, a friendly target or an
unpinned casting time leave the row refused. A prepared row and a row
without dmgt (Over Healing) are admitted, the latter draining nothing.
================
*/
func TestFixedDamageRefusesNearMissShapes(t *testing.T) {
	const (
		ko   = 0x6b6f
		bdmd = 0x42444d44
		cm   = skillMultiImpactTag
	)
	base := SkillRow{TimingPinned: true, ActionCastingTimePinned: true, ActionRangePinned: true, ActionRange: 150, ActionDurationMs: 2000,
		Consumption: SkillConsumption{MP: 29, Pinned: true}, TargetRequired: true, Targets: SkillTargets{Required: true, Animal: true, EnemyM: true, EnemyP: true}}
	friendly := fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, 100)
	friendly[27], friendly[28] = "1", "1"
	cases := []struct {
		name   string
		fields []string
		row    SkillRow
		want   bool
		drain  uint32
	}{
		{"tuning", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, 100, tagGetv, bdmd), base, true, 100},
		{"no-transfer", fixedDamageFields(tagFixedDamage, 130, tagGetv, bdmd), base, true, 0},
		{"one-result", fixedDamageFields(tagFixedDamage, 130, cm, 2, 1), base, true, 0},
		{"area", fixedDamageFields(tagFixedDamage, 130, cm, 2, 1, tagEfr, 1, 2, 50, 3, 35, 24), base, true, 0},
		{"area-drain", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, 100, tagEfr, 1, 2, 50, 3, 35, 24), base, false, 0},
		{"area-kind", fixedDamageFields(tagFixedDamage, 130, tagEfr, 2, 2, 50, 3, 35, 24), base, false, 0},
		{"two-results", fixedDamageFields(tagFixedDamage, 130, cm, 2, 2), base, false, 0},
		{"cm-kind", fixedDamageFields(tagFixedDamage, 130, cm, 1, 1), base, false, 0},
		{"no-damage", fixedDamageFields(tagDamageTransfer, 0, 100), base, false, 0},
		{"zero-amount", fixedDamageFields(tagFixedDamage, 0, tagDamageTransfer, 0, 100), base, false, 0},
		{"hp-share", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 50, 100), base, false, 0},
		{"above-whole", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, maxDamageTransferPercent+1), base, false, 0},
		{"knockdown", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, 100, ko, 1, 50), base, false, 0},
		{"friendly", friendly, base, false, 0},
		{"casting-time", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, 100), func() SkillRow { r := base; r.ActionCastingTimeMs = 1000; return r }(), true, 100},
		{"casting-unpinned", fixedDamageFields(tagFixedDamage, 130, tagDamageTransfer, 0, 100), func() SkillRow { r := base; r.ActionCastingTimePinned = false; return r }(), false, 0},
	}
	for _, tc := range cases {
		row := tc.row
		parseSkillOffense(tc.fields, &row)
		if row.FixedDamage.Present != tc.want || (row.OffenseRefusal == "") != tc.want || row.DirectOffensePinned != tc.want {
			t.Fatalf("%s: fixed=%+v refusal=%q direct=%v", tc.name, row.FixedDamage, row.OffenseRefusal, row.DirectOffensePinned)
		}
		if tc.want && row.FixedDamage != (SkillFixedDamage{Present: true, Amount: 130, MPPercent: tc.drain}) {
			t.Fatalf("%s: fixed=%+v", tc.name, row.FixedDamage)
		}
	}
}
