/*
===========================================================================

skillrepair_test.go - complete fortress repair program admission

===========================================================================
*/
package enterworld

import (
	"opensro.online/server/internal/testsupport/licensed"
	"path/filepath"
	"strconv"
	"testing"
)

/*
================
TestStructureRepairProgram
================
*/
func TestStructureRepairProgram(t *testing.T) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[8], fields[68] = "1", "1", "3"
	words := []uint32{0x64757261, 12000, 0x70756c73, 3000, 0x6865616c, 0, 1, 0, 0, 0x6c6e6b73, 0, 0, 1, 1, 0x736b63, 15, 8, 0, 0x616f, 0x72706b74}
	for i, v := range words {
		fields[69+i] = strconv.FormatUint(uint64(v), 10)
	}
	row := SkillRow{ReplacementPinned: true, TimingPinned: true, CastGate: SkillCastGate{Rpkt: true}}
	for percent := uint32(1); percent <= 3; percent++ {
		fields[75] = strconv.FormatUint(uint64(percent), 10)
		got := compileStructureRepair(fields, row)
		if !got.Pinned || got.HPPercent != percent || got.DurationMs != 12000 || got.PeriodMs != 3000 {
			t.Fatalf("repair descriptor: %+v", got)
		}
	}
	for _, column := range []int{69, 71, 73, 78, 83, 87, 88} {
		old := fields[column]
		fields[column] = "0"
		if compileStructureRepair(fields, row).Pinned {
			t.Fatalf("missing operation %d admitted", column)
		}
		fields[column] = old
	}
	fields[89] = "1667396966"
	if compileStructureRepair(fields, row).Pinned {
		t.Fatal("extra operation ignored")
	}
}

/*
================
TestShippedStructureRepairDescriptor
================
*/
func TestShippedStructureRepairDescriptor(t *testing.T) {
	licensed.RequireGameData(t)
	dir := licensed.RetailTextdataDir(t)
	skills := NewTextdataSkills(dir)
	for _, fields := range readTextdataFile(filepath.Join(dir, "skilldata_25000.txt")) {
		if len(fields) < 4 || fields[3] != "SKILL_FORT_REPAIR_KIT_01" {
			continue
		}
		row, ok := skills.SkillByCodename(fields[3])
		if !ok {
			t.Fatal("missing skill")
		}
		program, err := CompileSkillProgram(fields)
		if !row.StructureRepair.Pinned {
			t.Fatalf("descriptor absent: columns=%d header=%q,%q,%q replacement=%v timing=%v chain=%d casting=%d rpkt=%v program=%+v err=%v recompile=%+v", len(fields), fields[0], fields[8], fields[68], row.ReplacementPinned, row.TimingPinned, row.ChainNext, row.ActionCastingTimeMs, row.CastGate.Rpkt, program, err, compileStructureRepair(fields, row))
		}
		return
	}
	t.Fatal("missing repair row")
}
