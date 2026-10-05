/*
===========================================================================

skillareaburst_test.go - untargeted caster-centred attack admission

Booming Chord and Booming Wave compile onto the area-burst route; targeted
attacks keep the ordinary offense path, and near-miss shapes refuse.

===========================================================================
*/

package enterworld

import (
	"fmt"
	"strconv"
	"testing"
)

const (
	// harpWeaponKind is column 50 on every Bard Battle Chord row.
	harpWeaponKind = 14
	// boomingImpacts is the authored mc block (mc 2 1): kind 2, count 1
	// (encodedMultiImpactCount), so each victim takes one impact.
	boomingImpacts = 1
)

/*
================
TestShippedBoomingRowsCompileAsAreaBurst

All 15 Booming Chord and 3 Booming Wave tiers are admitted untargeted with
their caster-centred efr (radius 80 / 120, five victims, 35 percent per
victim, select 24), the mc impact count, MUAT and BDMD.
================
*/
func TestShippedBoomingRowsCompileAsAreaBurst(t *testing.T) {
	source := sharedShippedSkills(t)
	lines := []struct {
		line   string
		tiers  int
		radius uint32
	}{
		{"SKILL_EU_BARD_BATTLAA_EXPLOSION_A", 15, 80},
		{"SKILL_EU_BARD_BATTLAA_EXPLOSION_B", 3, 120},
	}
	for _, line := range lines {
		for tier := 1; tier <= line.tiers; tier++ {
			code := fmt.Sprintf("%s_%02d", line.line, tier)
			row, ok := source.SkillByCodename(code)
			if !ok {
				t.Fatalf("missing %s", code)
			}
			want := SkillOffensiveArea{Shape: 1, Radius: line.radius, MaxTargets: 5, ReductionPercent: 35, Select: 24}
			if !row.AreaBurst || row.OffenseRefusal != "" || row.TargetRequired || row.OffensiveArea != want {
				t.Fatalf("%s: burst=%v refusal=%q area=%+v", code, row.AreaBurst, row.OffenseRefusal, row.OffensiveArea)
			}
			if !row.Attack.Present || row.Attack.ImpactCount != boomingImpacts || row.ActionCastingTimeMs != 0 ||
				row.RequiredWeaponKinds[0] != harpWeaponKind || row.StatusCast || row.CombatTrap.Pinned {
				t.Fatalf("%s: attack=%+v cast=%d weapon=%v", code, row.Attack, row.ActionCastingTimeMs, row.RequiredWeaponKinds)
			}
			if !row.Attack.Parameters.Has(ParameterMusicPower) || !row.Attack.Parameters.Has(ParameterBardMPDecrease) {
				t.Fatalf("%s: getv MUAT/BDMD lost", code)
			}
		}
	}
	// A targeted Battle Chord area still takes the ordinary offense path.
	weird, ok := source.SkillByCodename("SKILL_EU_BARD_BATTLAA_DAMAGE_B_01")
	if !ok || weird.AreaBurst || !weird.TargetRequired || !weird.DirectOffensePinned || weird.OffensiveArea.Shape != 2 {
		t.Fatalf("Weird Chord rerouted: burst=%v refusal=%q area=%+v", weird.AreaBurst, weird.OffenseRefusal, weird.OffensiveArea)
	}
}

/*
================
areaBurstFields

A synthetic untargeted row in the Booming Chord shape; tail replaces the
program after att.
================
*/
func areaBurstFields(tail ...int64) []string {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[8] = "1", "2"
	values := append([]int64{skillAttackTag, 8, 368, 106, 130, 0}, tail...)
	for i, value := range values {
		fields[69+i] = strconv.FormatInt(value, 10)
	}
	return fields
}

/*
================
TestAreaBurstRefusesNearMissShapes

Only the complete caster-centred program is admitted: a primary-centred
efr, a missing efr, an extra instruction or a positive casting time leave
the untargeted row on the refused offense gate; a targeted row in the same
shape keeps the ordinary offense path.
================
*/
func TestAreaBurstRefusesNearMissShapes(t *testing.T) {
	const (
		mc   = 0x6d63
		efr  = 0x656672
		getv = 0x67657476
		muat = 0x4d554154
		ko   = 0x6b6f
	)
	base := SkillRow{CombatPinned: true, TimingPinned: true, ActionRangePinned: true, ActionDurationMs: 2000,
		Consumption: SkillConsumption{MP: 312, Pinned: true}, Attack: SkillAttack{Present: true, ImpactCount: 2}}
	good := []int64{mc, 2, 1, efr, 1, 1, 80, 5, 35, 24, getv, muat}
	cases := []struct {
		name     string
		fields   []string
		row      SkillRow
		burst    bool
		admitted bool
	}{
		{"booming", areaBurstFields(good...), base, true, true},
		{"primary-centred", areaBurstFields(mc, 2, 1, efr, 1, 2, 80, 5, 35, 24), base, false, false},
		{"no-area", areaBurstFields(mc, 2, 1, getv, muat), base, false, false},
		{"knockdown", areaBurstFields(append(append([]int64{}, good...), ko, 1, 50)...), base, false, false},
		{"casting-time", areaBurstFields(good...), func() SkillRow { r := base; r.ActionCastingTimeMs = 1000; return r }(), false, false},
		{"targeted", areaBurstFields(good...), func() SkillRow { r := base; r.TargetRequired = true; return r }(), false, true},
	}
	for _, tc := range cases {
		row := tc.row
		parseSkillOffense(tc.fields, &row)
		if row.AreaBurst != tc.burst || (row.OffenseRefusal == "") != tc.admitted || row.DirectOffensePinned != tc.admitted {
			t.Fatalf("%s: burst=%v refusal=%q direct=%v", tc.name, row.AreaBurst, row.OffenseRefusal, row.DirectOffensePinned)
		}
	}
}
