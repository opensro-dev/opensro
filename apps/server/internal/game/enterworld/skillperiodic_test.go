/*
===========================================================================

skillperiodic_test.go - whole-program admission for persistent hostile attacks

Fixtures use native instruction shapes without depending on installed assets.
Incomplete programs must not enable either persistent or instant execution.

===========================================================================
*/

package enterworld

import "testing"

/*
================
periodicProgramFixture
================
*/
func periodicProgramFixture(status []uint32, area bool) ([]string, SkillRow) {
	tail := []uint32{tagNbuf, tagBbuf, tagTimedLink, 9, 0, 2, 0, tagPeriodicPerTarget,
		tagDura, 12000, skillPulseTag, 2000, uint32(skillAttackTag), 8, 37, 4, 6, 0,
		uint32(skillMultiImpactTag), 2, 1}
	if area {
		tail = append(tail, tagEfr, 1, 2, 70, 3, 0, 24)
	}
	tail = append(tail, status...)
	tail = append(tail, tagGetv, parameterDotPower, tagGetv, parameterDotDuration)
	fields := marchProgramFields(tail)
	fields[21], fields[22], fields[23], fields[29], fields[30] = "150", "1", "1", "1", "1"
	fields[50] = "10"
	row := SkillRow{Consumption: SkillConsumption{Pinned: true}, ActionCastingTimePinned: true,
		ActionDurationPinned: true, TimingPinned: true, ReplacementPinned: true,
		ActionRangePinned: true, TargetRequired: true}
	return fields, row
}

/*
================
TestPeriodicProgramVariants
================
*/
func TestPeriodicProgramVariants(t *testing.T) {
	for _, status := range [][]uint32{{0x6275, 28, 20, 4}, {0x7073, 40, 25, 20},
		{0x626c, 10000, 25, 2, 20, 20}, {0x736c, 20000, 25, 4}} {
		for _, area := range []bool{false, true} {
			fields, row := periodicProgramFixture(status, area)
			parseSkillTimedEffect(fields, &row)
			got := row.TimedEffect.Periodic
			if !got.Pinned || got.PeriodMs != 2000 || got.DurationMs != 12000 ||
				(got.Area.Radius != 0) != area || !got.Attack.Parameters.Has(ParameterDotPower) ||
				!got.Attack.Parameters.Has(ParameterDotDuration) {
				t.Fatalf("status %x area %v: %+v", status[0], area, got)
			}
			if row.TimedEffect.Pinned || row.CombatPinned || row.DirectOffensePinned || row.Attack.Present {
				t.Fatal("periodic program leaked into an immediate producer")
			}
		}
	}
}

/*
================
TestPeriodicProgramRefusesIncompleteContracts
================
*/
func TestPeriodicProgramRefusesIncompleteContracts(t *testing.T) {
	for _, tc := range []struct {
		column int
		value  string
	}{
		{68, "1"}, {29, "0"}, {27, "1"}, {76, "0"}, {78, "0"},
		{80, "0"}, {82, "16"}, {87, "0"}, {99, "12345"},
	} {
		fields, row := periodicProgramFixture([]uint32{0x6275, 28, 20, 4}, false)
		fields[tc.column] = tc.value
		if got := compileSkillPeriodicEffect(fields, row); got.Pinned {
			t.Fatalf("column %d=%s admitted: %+v", tc.column, tc.value, got)
		}
	}
}
