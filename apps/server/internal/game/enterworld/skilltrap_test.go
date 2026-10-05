/*
===========================================================================

skilltrap_test.go - complete trap programs and authored item coverage

Reject partial programs before the inventory path can spend an item. Empty
target lists remain valid objects, with no invented capture target.

===========================================================================
*/
package enterworld

import (
	"strconv"
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
questTrapFields
================
*/
func questTrapFields() []string {
	fields := make([]string, 118)
	for index := range fields {
		fields[index] = "0"
	}
	words := []uint32{
		questTrapQuest, 1, 5867, 0, 0,
		questTrapLink, 0, 300, 1, 0,
		questTrapDura, 300000,
		tagEfr, 3, 1, 20, 1, 0, 16,
		questTrapTag,
	}
	for index, word := range words {
		fields[skilldataColEncodedTail+index] = strconv.FormatUint(uint64(word), 10)
	}
	return fields
}

/*
================
TestQuestTrapRequiresTheCompleteSupportedProgram
================
*/
func TestQuestTrapRequiresTheCompleteSupportedProgram(t *testing.T) {
	fields := questTrapFields()
	trap := compileQuestTrap(fields)
	if !trap.Present || trap.DurationMs != 300000 || trap.ScanMs != 300 || trap.Radius != 20 ||
		trap.Targets != [3]uint32{5867, 0, 0} {
		t.Fatal(trap)
	}
	for _, change := range []struct {
		name   string
		offset int
		word   uint32
	}{
		{"unsupported-quest-mode", 1, 2},
		{"linked-secondary-skill", 6, 99},
		{"unsupported-scan-period", 7, 301},
		{"zero-lifetime", 11, 0},
		{"party-targeting", 18, 4},
		{"missing-trap", 19, 0},
		{"duplicate-trap", 20, questTrapTag},
		{"unknown-program", 20, 1234567},
	} {
		t.Run(change.name, func(t *testing.T) {
			changed := append([]string(nil), fields...)
			changed[skilldataColEncodedTail+change.offset] = strconv.FormatUint(uint64(change.word), 10)
			if got := compileQuestTrap(changed); got.Present {
				t.Fatal("partial program admitted", got)
			}
		})
	}
}

/*
================
TestQuestTrapPreservesEmptyAndTerminatedTargetLists
================
*/
func TestQuestTrapPreservesEmptyAndTerminatedTargetLists(t *testing.T) {
	for _, targets := range [][3]uint32{{}, {0, 99, 100}, {5867, 0, 14769}, {5867, 14769, 14764}} {
		fields := questTrapFields()
		for index, target := range targets {
			fields[skilldataColEncodedTail+2+index] = strconv.FormatUint(uint64(target), 10)
		}
		trap := compileQuestTrap(fields)
		if !trap.Present || trap.Targets != targets {
			t.Fatal("decoder rewrote the native target list", trap)
		}
	}
}

/*
================
TestShippedQuestTrapPrograms

Read through the production loader so a missing integration call cannot
pass merely because the isolated program decoder works.
================
*/
func TestShippedQuestTrapPrograms(t *testing.T) {
	licensed.RequireGameData(t)
	dir := licensed.RetailTextdataDir(t)
	source := NewTextdataSkills(dir)
	for _, expected := range []struct {
		id, target, duration uint32
	}{
		{7108, 5867, 300000},
		{7111, 14769, 300000},
		{7115, 14755, 300000},
		{7118, 14764, 300000},
		{30608, 3809, 60000},
		{30609, 0, 60000},
		{30610, 0, 60000},
	} {
		row, found := source.SkillByID(expected.id)
		trap := row.CastGate.QuestTrap
		if !found || !trap.Present || trap.Targets[0] != expected.target ||
			trap.DurationMs != expected.duration || trap.ScanMs != 300 || trap.Radius != 20 {
			t.Fatalf("skill %d trap = %+v", expected.id, trap)
		}
	}
}
