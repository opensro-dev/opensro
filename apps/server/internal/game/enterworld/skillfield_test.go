/*
===========================================================================

skillfield_test.go - efr kind 3 buff fields on the shipped rows

Harmony therapy is the one buff field v1.150 ships: every tier must pin its
timed program with the field, its pola guard and its irgc recovery, and no
other row may turn into a field.

===========================================================================
*/

package enterworld

import (
	"strings"
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

const harmonyPrefix = "SKILL_CH_WATER_HARMONY_"

/*
================
TestShippedHarmonyTherapyField
================
*/
func TestShippedHarmonyTherapyField(t *testing.T) {
	source := NewTextdataSkills(gamedatatest.TextdataDir(t))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	harmony := 0
	for _, row := range source.rows.values() {
		effect := row.TimedEffect
		if !strings.HasPrefix(row.Codename, harmonyPrefix) {
			if effect.Field.Present {
				t.Errorf("%s turned into a buff field: %+v", row.Codename, effect.Field)
			}
			continue
		}
		harmony++
		want := SkillRecipientArea{Present: true, Radius: 60, Select: SelectCaster | SelectCharacter | SelectParty}
		if !effect.Pinned || effect.Field != want || effect.Area.Present || effect.Targeted {
			t.Errorf("%s pinned %v field %+v area %+v targeted %v", row.Codename, effect.Pinned, effect.Field, effect.Area, effect.Targeted)
		}
		if !effect.Preemptive.Present || effect.Preemptive.Mask&^3 != 0 || effect.Preemptive.Level == 0 {
			t.Errorf("%s pola %+v", row.Codename, effect.Preemptive)
		}
		if !effect.Recovery.Present || effect.Recovery.HP == 0 || effect.Recovery.MP == 0 {
			t.Errorf("%s irgc %+v", row.Codename, effect.Recovery)
		}
		if row.EffectDurationMs == 0 {
			t.Errorf("%s has no duration", row.Codename)
		}
	}
	if harmony != 20 {
		t.Fatalf("%d %s rows, want 20", harmony, harmonyPrefix)
	}
}

/*
================
TestFieldProgramAdmission

Harmony therapy's program shape on the synthetic envelope. A kind 3 efr
with the hostile bit is a trap field, which this program does not own; a
second efr, a nonzero reduction or a missing irgc payload stay unpinned.
================
*/
func TestFieldProgramAdmission(t *testing.T) {
	const duration = 300000
	for _, tc := range []struct {
		name  string
		tail  []uint32
		valid bool
	}{
		{"harmony", []uint32{tagEfr, efrKindField, 1, 60, 0, 0, 7, tagTimedPreemptive, 1, 33, tagTimedRecovery, 20, 20}, true},
		{"recovery only", []uint32{tagEfr, efrKindField, 1, 60, 0, 0, SelectParty, tagTimedRecovery, 20, 20}, true},
		{"hostile selector", []uint32{tagEfr, efrKindField, 1, 60, 0, 0, 15, tagTimedRecovery, 20, 20}, false},
		{"handler selector", []uint32{tagEfr, efrKindField, 1, 60, 0, 0, 0x21, tagTimedRecovery, 20, 20}, false},
		{"empty selector", []uint32{tagEfr, efrKindField, 1, 60, 0, 0, 0, tagTimedRecovery, 20, 20}, false},
		{"reduction", []uint32{tagEfr, efrKindField, 1, 60, 0, 10, 7, tagTimedRecovery, 20, 20}, false},
		{"field and area", []uint32{tagEfr, efrKindField, 1, 60, 0, 0, 7, tagEfr, efrKindArea, 1, 60, 0, 0, 7, tagTimedRecovery, 20, 20}, false},
		{"duplicate irgc", []uint32{tagEfr, efrKindField, 1, 60, 0, 0, 7, tagTimedRecovery, 20, 20, tagTimedRecovery, 20, 20}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := marchProgramFields(append([]uint32{tagDura, duration}, tc.tail...))
			row := SkillRow{
				Consumption: SkillConsumption{Pinned: true}, TimingPinned: true,
				ActionCastingTimePinned: true, ActionDurationPinned: true, ReplacementPinned: true,
				EffectDurationMs: duration,
			}
			parseSkillTimedEffect(fields, &row)
			got := row.TimedEffect.Pinned && row.TimedEffect.Field.Present
			if got != tc.valid {
				t.Fatalf("pinned %v field %+v recovery %+v, want valid %v", row.TimedEffect.Pinned, row.TimedEffect.Field, row.TimedEffect.Recovery, tc.valid)
			}
		})
	}
}
