/*
===========================================================================

skillfireshield_test.go - admission of Fire Shield's complete bgra program

The shipped ranks share one timed owner. Partial or malformed programs must
not enable an elemental modifier merely because bgra was indexed.

===========================================================================
*/

package enterworld

import (
	"fmt"
	"testing"
)

/*
================
TestShippedFireShieldRanksCompile
================
*/
func TestShippedFireShieldRanksCompile(t *testing.T) {
	source := sharedShippedSkills(t)
	for _, line := range []struct {
		book  string
		first uint32
		ranks int
	}{
		{"A", 18, 6}, {"B", 38, 6}, {"C", 58, 6}, {"D", 78, 1},
	} {
		for rank := 1; rank <= line.ranks; rank++ {
			code := fmt.Sprintf("SKILL_CH_FIRE_SHIELD_%s_%02d", line.book, rank)
			row, ok := source.SkillByCodename(code)
			if !ok {
				t.Fatalf("missing %s", code)
			}
			m, effect := row.BuffModifiers, row.TimedEffect
			if !effect.Pinned || !effect.ElementResistance || effect.Targeted || effect.Area.Present || effect.Persistent ||
				!m.Bgra || !m.Present() || m.BgraMask != 63 || m.BgraPercent != line.first+3*uint32(rank-1) ||
				row.EffectDurationMs == 0 || !row.Reqi.Present || row.Reqi.Count != 1 ||
				row.Reqi.Pairs[0] != (SkillReqiPair{Kind: 4, Value: 1}) {
				t.Fatalf("%s: modifiers %+v, effect %+v, reqi %+v", code, m, effect, row.Reqi)
			}
		}
	}
}

/*
================
TestFireShieldProgramAdmissionIsAtomic
================
*/
func TestFireShieldProgramAdmissionIsAtomic(t *testing.T) {
	const duration = 300000
	for _, tc := range []struct {
		name  string
		tail  []uint32
		valid bool
	}{
		{"phoenix", []uint32{tagDura, duration, tagTimedElementResistance, 63, 18, 0, tagReqi, 4, 1}, true},
		{"single element", []uint32{tagDura, duration, tagTimedElementResistance, 4, 18}, true},
		{"immunity", []uint32{tagDura, duration, tagTimedElementResistance, 63, 100}, true},
		{"empty mask", []uint32{tagDura, duration, tagTimedElementResistance, 0, 18}, false},
		{"unsupported mask", []uint32{tagDura, duration, tagTimedElementResistance, 64, 18}, false},
		{"invalid percentage", []uint32{tagDura, duration, tagTimedElementResistance, 63, 101}, false},
		{"duplicate", []uint32{tagDura, duration, tagTimedElementResistance, 63, 18, tagTimedElementResistance, 63, 21}, false},
		{"missing duration", []uint32{tagTimedElementResistance, 63, 18}, false},
		{"unknown operation", []uint32{tagDura, duration, tagTimedElementResistance, 63, 18, 0x61626364}, false},
		{"unsupported companion", []uint32{tagDura, duration, tagTimedElementResistance, 63, 18, 0x6572, 10, 0}, false},
		{"area", []uint32{tagDura, duration, tagTimedElementResistance, 63, 18, tagEfr, 1, 1, 300, 8, 0, SelectCaster | SelectParty}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := marchProgramFields(tc.tail)
			row := SkillRow{
				Consumption: SkillConsumption{Pinned: true}, TimingPinned: true,
				ActionCastingTimePinned: true, ActionDurationPinned: true, ReplacementPinned: true,
				EffectDurationMs: duration,
			}
			noteParameterIndex(fields, &row)
			parseSkillTimedEffect(fields, &row)
			if row.TimedEffect.Pinned != tc.valid || row.TimedEffect.ElementResistance != tc.valid {
				t.Fatalf("effect %+v, want admission %v", row.TimedEffect, tc.valid)
			}
		})
	}
}
