package enterworld

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestShippedConditionalChildSelfPrograms(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	skills := NewTextdataSkills(dir)
	for _, tc := range []struct{ id, tag, first, cast uint32 }{{10495, 0x64656670, 16, 0}, {10498, 0x647275, 20, 0}, {10507, 0x647275, 20, 929}, {10510, 0x6372, 10, 929}} {
		row, ok := skills.SkillByID(tc.id)
		if !ok || !row.MonsterSelfEffect.Pinned || row.MonsterSelfEffect.Tag != tc.tag || row.MonsterSelfEffect.First != tc.first || row.MonsterSelfEffect.Second != 0 || row.ActionCastingTimeMs != tc.cast || row.EffectDurationMs != 8000 {
			t.Fatalf("%d: self=%+v cast=%d duration=%d timing=%v range=%v replacement=%v", tc.id, row.MonsterSelfEffect, row.ActionCastingTimeMs, row.EffectDurationMs, row.TimingPinned, row.ActionRangePinned, row.ReplacementPinned)
		}
	}
}
