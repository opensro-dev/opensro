/*
===========================================================================

skillrecoveryrates_test.go - the recovery rates of the rows the timed
program never compiles

The fortress structure skills are no player programs: their irgc words
(the headquarters' recovery gift) only reach a reader through
projectTimedRecoveryRates. These pins hold the shipped table's
holder-side standing areas (efr kind 3, select 32) and the one row the
projection claims.

===========================================================================
*/

package enterworld

import (
	"testing"
)

/*
================
TestShippedStructureAuraRows

The nine holder-side standing areas of the shipped table, each with
its authored program and no authored duration: the aggressive flag's
flat attack words, the defensive flag's defense percents, the five
defensive positions tier by tier, the headquarters' recovery rates and
the healing flag's pulsed share-of-maximum heal. Nothing else in the
table selects the holder side there.
================
*/
func TestShippedStructureAuraRows(t *testing.T) {
	source := sharedShippedSkills(t)
	want := map[string]struct {
		radius, max uint32
		check       func(t *testing.T, row SkillRow)
	}{
		"SKILL_FORT_STRUCTURE_AGGRESSIVE_FLAG": {250, 24, func(t *testing.T, row SkillRow) {
			if !row.BuffModifiers.Dru || row.BuffModifiers.DruWords != [2]uint32{10, 10} {
				t.Errorf("aggressive flag modifiers %+v", row.BuffModifiers)
			}
		}},
		"SKILL_FORT_STRUCTURE_DEFENSIVE_FLAG": {200, 24, func(t *testing.T, row SkillRow) {
			if !row.BuffModifiers.Odar || row.BuffModifiers.OdarWord != 10 {
				t.Errorf("defensive flag modifiers %+v", row.BuffModifiers)
			}
		}},
		"SKILL_FORT_STRUCTURE_HEALING_FLAG": {200, 24, func(t *testing.T, row SkillRow) {
			if !row.Heal.Present || row.Heal.HPPercent != 5 || row.Heal.MPPercent != 5 ||
				!row.Abnormal.PulsePresent || row.Abnormal.Pulse != 5000 {
				t.Errorf("healing flag heal %+v pulse %d", row.Heal, row.Abnormal.Pulse)
			}
		}},
		"SKILL_FORT_STRUCTURE_HEADQUARTERS": {330, 50, func(t *testing.T, row SkillRow) {
			if row.TimedEffect.Recovery != (SkillRecoveryRates{Present: true, HP: 100, MP: 100}) {
				t.Errorf("headquarters recovery %+v", row.TimedEffect.Recovery)
			}
		}},
	}
	for tier, value := range []uint32{3, 6, 10, 14, 18} {
		want["SKILL_FORT_STRUCTURE_DEFENSIVE_POSITION_0"+string(rune('1'+tier))] = struct {
			radius, max uint32
			check       func(t *testing.T, row SkillRow)
		}{250, 24, func(t *testing.T, row SkillRow) {
			if !row.BuffModifiers.Dru || row.BuffModifiers.DruWords != [2]uint32{value, value} ||
				!row.BuffModifiers.Odar || row.BuffModifiers.OdarWord != value {
				t.Errorf("position tier %d modifiers %+v", value, row.BuffModifiers)
			}
		}}
	}
	found := 0
	for id := uint32(1); id < 45000; id++ {
		row, ok := source.SkillByID(id)
		if !ok || !row.Abnormal.PersistentArea.Present || row.Abnormal.PersistentArea.Select != 32 {
			continue
		}
		found++
		pin, known := want[row.Codename]
		if !known {
			t.Errorf("unpinned holder-side standing area %s", row.Codename)
			continue
		}
		area := row.Abnormal.PersistentArea
		if area.Radius != pin.radius || area.MaxTargets != pin.max {
			t.Errorf("%s area %+v", row.Codename, area)
		}
		if row.EffectDurationMs != 0 || row.EffectDurationPresent {
			t.Errorf("%s carries an authored duration %d", row.Codename, row.EffectDurationMs)
		}
		pin.check(t, row)
	}
	if found != len(want) {
		t.Errorf("holder-side standing areas = %d, want %d", found, len(want))
	}
}

/*
================
TestProjectedRecoveryRatesReachOnlyTheHeadquarters

Every shipped row whose recovery rates stand outside a compiled timed
program: exactly one, the fortress headquarters. A row the timed
program claimed keeps its own rates, so the projection must never
have manufactured a second set.
================
*/
func TestProjectedRecoveryRatesReachOnlyTheHeadquarters(t *testing.T) {
	source := sharedShippedSkills(t)
	for id := uint32(1); id < 45000; id++ {
		row, ok := source.SkillByID(id)
		if !ok || !row.TimedEffect.Recovery.Present || row.TimedEffect.Pinned {
			continue
		}
		if row.Codename != "SKILL_FORT_STRUCTURE_HEADQUARTERS" {
			t.Errorf("recovery rates outside a timed program on %s", row.Codename)
		}
	}
}
