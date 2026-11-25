package enterworld

import (
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"
)

func TestShippedConditionalChildSelfPrograms(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	skills := NewTextdataSkills(dir)
	for _, tc := range []struct{ id, tag, first, cast uint32 }{{10495, 0x64656670, 16, 0}, {10498, 0x647275, 20, 0}, {10507, 0x647275, 20, 929}, {10510, 0x6372, 10, 929}} {
		row, ok := skills.SkillByID(tc.id)
		if !ok || !row.MonsterSelfEffect.Pinned || row.MonsterSelfEffect.Tag != tc.tag || row.MonsterSelfEffect.First != tc.first || row.MonsterSelfEffect.Second != 0 || row.ActionCastingTimeMs != tc.cast || row.EffectDurationMs != 8000 {
			t.Fatalf("%d: self=%+v cast=%d duration=%d timing=%v range=%v replacement=%v", tc.id, row.MonsterSelfEffect, row.ActionCastingTimeMs, row.EffectDurationMs, row.TimingPinned, row.ActionRangePinned, row.ReplacementPinned)
		}
	}
}

/*
================
rocEnrageRow

The VSRO client's MSKILL_RM_ROC_ATTACK10 (id 31102), the row the Roc
import stages: a self-targeted cast whose whole program is mom 600000 10
6 1. program overrides the program columns from 69.
================
*/
func rocEnrageRow(program ...string) string {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	for col, value := range map[int]string{0: "1", 1: "31102", 3: "MSKILL_RM_ROC_ATTACK10", 4: "xxx", 7: "113", 8: "2",
		13: "2633", 14: "3000", 15: "3000", 19: "1", 26: "1", 47: "3", 50: "255", 51: "255", 57: "255", 58: "255",
		59: "255", 60: "255", 61: `skill\monster\monster_buff.ddj`, 62: "UIIT_MSG_STATE_MSKILL_RM_ROC_ATTACK10",
		64: "DE_UIIT_MSG_STATE_MSKILL_RM_ROC_ATTACK10", 67: "66", 68: "3"} {
		fields[col] = value
	}
	copy(fields[69:], program)
	return joinSkillRow(fields)
}

/*
================
TestRocEnrageParsesAsAStackingEffect

The staged row pins as an event-cast stacking buff (ten per step, one
step per ten minutes) and never as an AI self buff. A row with a
duration, or with a zero period, is not this program.
================
*/
func TestRocEnrageParsesAsAStackingEffect(t *testing.T) {
	const mom = "7171949"
	skills := loadDuplicateRows(t,
		rocEnrageRow(mom, "600000", "10", "6", "1"),
	)
	row, ok := skills.SkillByID(31102)
	if !ok {
		t.Fatal("the enrage row did not load")
	}
	if want := (SkillMonsterStackingEffect{Pinned: true, PeriodMs: 600000, StepValue: 10}); row.MonsterStackingEffect != want {
		t.Fatalf("stacking = %+v, want %+v", row.MonsterStackingEffect, want)
	}
	if row.MonsterSelfEffect.Pinned {
		t.Fatal("the enrage pinned as an AI self buff")
	}
	for name, program := range map[string][]string{
		"timed":       {"1685418593", "8000", mom, "600000", "10", "6", "1"},
		"zero period": {mom, "0", "10", "6", "1"},
		"zero step":   {mom, "600000", "0", "6", "1"},
	} {
		row, ok := loadDuplicateRows(t, rocEnrageRow(program...)).SkillByID(31102)
		if ok && row.MonsterStackingEffect.Pinned {
			t.Fatalf("%s: pinned %+v", name, row.MonsterStackingEffect)
		}
	}
}
