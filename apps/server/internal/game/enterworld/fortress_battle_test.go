/*
===========================================================================

fortress_battle_test.go - census of authored fortress ranks and complete skills

===========================================================================
*/
package enterworld

import (
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"path/filepath"
	"strconv"
	"testing"
)

/*
================
TestShippedFortressBattleRanksAreCompleteTimedPrograms
================
*/
func TestShippedFortressBattleRanksAreCompleteTimedPrograms(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	rows := readTextdataFile(filepath.Join(dir, "siegefortressbattlerank.txt"))
	skills := NewTextdataSkills(dir)
	count := 0
	for _, fields := range rows {
		if fields[0] != "1" {
			continue
		}
		ordinal, err := strconv.ParseUint(fields[1], 10, 8)
		if err != nil {
			t.Fatal(err)
		}
		threshold, skillID, ok := fortress.BattleRank(uint8(ordinal))
		if !ok || fields[3] != strconv.FormatUint(uint64(threshold), 10) || fields[4] != strconv.FormatUint(uint64(skillID), 10) {
			t.Fatalf("unaudited rank %+v", fields)
		}
		skill, ok := skills.SkillByID(skillID)
		if !ok || !skill.TimedEffect.Pinned || !skill.TimedJobExecutable() || !skill.Replacement.Cbuf || skill.EffectDurationMs == 0 {
			t.Fatalf("rank %d skill %d not complete", ordinal, skillID)
		}
		if ordinal == 6 && (!skill.MovementModifier.Supported || skill.MovementModifier.Percent != 20 || !skill.BuffModifiers.Dru || !skill.BuffModifiers.Odar || !skill.TimedEffect.HP.Present) {
			t.Fatal("commander lost compound effect")
		}
		count++
	}
	if count != int(fortress.MaxBattleRank) {
		t.Fatalf("audited %d ranks", count)
	}
}
