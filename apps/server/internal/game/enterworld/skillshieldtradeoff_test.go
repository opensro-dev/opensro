/*
===========================================================================

skillshieldtradeoff_test.go - complete Flying Heaven Art program admission

All four v1.150 books retain their exact authored units and shield requirement.
Unsupported companion instructions must not admit a partially executed buff.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"testing"
)

/*
================
TestShippedShieldTradeoffEveryRankCompiles
================
*/
func TestShippedShieldTradeoffEveryRankCompiles(t *testing.T) {
	source := sharedShippedSkills(t)
	for _, book := range []struct {
		name   string
		values [][2]uint32
	}{
		{"A", [][2]uint32{{17, 27}, {19, 31}, {22, 35}, {24, 39}, {27, 43}, {29, 47}}},
		{"B", [][2]uint32{{33, 53}, {36, 57}, {38, 61}, {41, 65}, {43, 69}, {46, 73}}},
		{"C", [][2]uint32{{50, 80}, {52, 84}, {55, 88}, {57, 92}, {60, 96}, {62, 100}}},
		{"D", [][2]uint32{{66, 107}, {69, 111}, {71, 115}, {74, 119}}},
	} {
		for rank, expected := range book.values {
			code := fmt.Sprintf("SKILL_CH_SWORD_SHIELDPD_%s_%02d", book.name, rank+1)
			row, ok := source.SkillByCodename(code)
			if !ok {
				t.Fatal(code)
			}
			effect := row.TimedEffect
			if !effect.Pinned || effect.Persistent || effect.Targeted || effect.Area.Present ||
				effect.ShieldTradeoff != (SkillShieldTradeoff{Present: true, DefensePercent: expected[0], PhysicalAttack: expected[1]}) ||
				row.EffectDurationMs != 120000 || row.CoolTimeMs != 180000 || row.ActionCastingTimeMs != 1300 ||
				row.Reqi.Count != 1 || row.Reqi.Pairs[0] != (SkillReqiPair{Kind: 4, Value: 1}) ||
				source.ExecutionPlan(row.ID).Kind() != SkillExecutionTimedEffect {
				t.Fatalf("%s: effect %+v, cooldown %d, duration %d, cast %d, reqi %+v", code, effect, row.CoolTimeMs, row.EffectDurationMs, row.ActionCastingTimeMs, row.Reqi)
			}
		}
	}
}

/*
================
TestShieldTradeoffProgramAdmissionIsAtomic
================
*/
func TestShieldTradeoffProgramAdmissionIsAtomic(t *testing.T) {
	const duration = 120000
	for _, tc := range []struct {
		name  string
		tail  []uint32
		valid bool
	}{
		{"retail", []uint32{tagDura, duration, tagTimedShieldTradeoff, 17, 27, tagReqi, 4, 1}, true},
		{"full shield cut", []uint32{tagDura, duration, tagTimedShieldTradeoff, 100, 27, tagReqi, 4, 1}, true},
		{"missing shield", []uint32{tagDura, duration, tagTimedShieldTradeoff, 17, 27}, false},
		{"wrong shield", []uint32{tagDura, duration, tagTimedShieldTradeoff, 17, 27, tagReqi, 4, 2}, false},
		{"invalid percentage", []uint32{tagDura, duration, tagTimedShieldTradeoff, 101, 27, tagReqi, 4, 1}, false},
		{"duplicate", []uint32{tagDura, duration, tagTimedShieldTradeoff, 17, 27, tagTimedShieldTradeoff, 19, 31, tagReqi, 4, 1}, false},
		{"missing duration", []uint32{tagTimedShieldTradeoff, 17, 27, tagReqi, 4, 1}, false},
		{"job", []uint32{tagDura, duration, tagTimedShieldTradeoff, 17, 27, tagReqi, 4, 1, tagCbuf}, false},
		{"extra attack boost", []uint32{tagDura, duration, tagTimedShieldTradeoff, 17, 27, tagReqi, 4, 1, tagTimedAttack, 20, 0}, false},
		{"unknown operation", []uint32{tagDura, duration, tagTimedShieldTradeoff, 17, 27, tagReqi, 4, 1, 0x61626364}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := marchProgramFields(tc.tail)
			row := SkillRow{Consumption: SkillConsumption{Pinned: true}, TimingPinned: true,
				ActionCastingTimePinned: true, ActionDurationPinned: true, ReplacementPinned: true, EffectDurationMs: duration}
			noteParameterIndex(fields, &row)
			parseSkillTimedEffect(fields, &row)
			if row.TimedEffect.Pinned != tc.valid || row.TimedEffect.ShieldTradeoff.Present != tc.valid {
				t.Fatalf("effect %+v, want admission %v", row.TimedEffect, tc.valid)
			}
		})
	}
}
