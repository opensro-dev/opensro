/*
===========================================================================

skillrogue_program_test.go - reject partial Rogue effect programs

Catalog tests cover shipped ranks. These cases prove that an unknown payload
cannot silently become an accepted coating or target constraint.

===========================================================================
*/
package enterworld

import (
	"strconv"
	"testing"
)

/*
================
rogueProgramFields
================
*/
func rogueProgramFields(words []uint32) []string {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[68] = "1", "3"
	for i, word := range words {
		fields[69+i] = strconv.FormatUint(uint64(word), 10)
	}
	return fields
}

/*
================
TestPoisonCoatingWholeProgramAdmission
================
*/
func TestPoisonCoatingWholeProgramAdmission(t *testing.T) {
	fields := rogueProgramFields([]uint32{
		tagDura, 6000, uint32(skillAttackTag), 8, 0, 0, 0, 0,
		tagCoatingPoison, 48, 20, 27,
		tagGetv, parameterPoisonDamage, tagGetv, parameterPoisonDuration, tagGetv, parameterCoatingDuration,
		tagReqi, 6, 12, tagReqi, 6, 13,
	})
	fields[8] = "1"
	row := SkillRow{TimingPinned: true, Consumption: SkillConsumption{Pinned: true}}
	if !compilePoisonCoating(fields, row).Pinned {
		t.Fatal("complete coating refused")
	}
	for column, value := range map[int]string{
		8: "2", 22: "1", 68: "0", 70: "0", 72: "4", 73: "1", 78: "0", 79: "101",
		82: "0", 84: "1380992085", 89: "2", 117: "1",
	} {
		candidate := append([]string(nil), fields...)
		candidate[column] = value
		if compilePoisonCoating(candidate, row).Pinned {
			t.Fatalf("partial or foreign program admitted: column=%d value=%s", column, value)
		}
	}
}

/*
================
TestScornWholeProgramAdmission
================
*/
func TestScornWholeProgramAdmission(t *testing.T) {
	fields := rogueProgramFields([]uint32{tagNbuf, tagBbuf, tagDura, 3000, tagForcedTarget})
	fields[8], fields[22], fields[23], fields[30] = "2", "1", "1", "1"
	row := SkillRow{TimingPinned: true, Consumption: SkillConsumption{Pinned: true}, ReplacementPinned: true,
		TargetRequired: true, ActionRangePinned: true, ActionRange: 150, ActionDurationMs: 500}
	if !compileForcedTarget(fields, row).Pinned {
		t.Fatal("complete Scorn program refused")
	}
	for column, value := range map[int]string{8: "1", 23: "0", 29: "1", 30: "0", 68: "0", 72: "0", 73: "0", 117: "1"} {
		candidate := append([]string(nil), fields...)
		candidate[column] = value
		if compileForcedTarget(candidate, row).Pinned {
			t.Fatalf("partial or foreign Scorn admitted: column=%d value=%s", column, value)
		}
	}
}
