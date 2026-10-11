/*
===========================================================================

levels_test.go - the level curve and mastery prices the planner reads

===========================================================================
*/
package catalogue

import (
	"testing"

	"opensro.online/server/internal/game/progression"
)

/*
================
tableLevels

A level table where level n needs 10n experience and its mastery costs n
SP; level 100 and above has no row.
================
*/
type tableLevels struct{}

func (tableLevels) SkillPointCost(level int64) (int64, bool) { return level, level < 100 }
func (tableLevels) ExpRequired(level int64) (int64, bool)    { return 10 * level, level < 100 }
func (tableLevels) MonsterExpBasis(int64) (int64, bool)      { return 0, false }

/*
================
TestLevelsReadTheTableAndTheCaps

A row per level to the mastery cap; experience stops at the character cap,
mastery prices at the table's end; the caps are the server's constants.
================
*/
func TestLevelsReadTheTableAndTheCaps(t *testing.T) {
	file := BuildLevels(Header{}, tableLevels{}, 0)
	if file.LevelCap != progression.LevelCap || file.MasteryLevelCap != progression.MasteryLevelCap ||
		file.MasteryTotal.Chinese != progression.TotalMasteryCapChina || file.MasteryTotal.EuropeanPerLevel != progression.TotalMasteryEuropePerLevel {
		t.Fatalf("caps %+v", file)
	}
	if int64(len(file.Levels)) != progression.MasteryLevelCap {
		t.Fatalf("%d rows", len(file.Levels))
	}
	first, atCap, past := file.Levels[0], file.Levels[progression.LevelCap-1], file.Levels[99]
	if first != (LevelRow{Level: 1, Exp: 10, MasterySP: 1}) {
		t.Errorf("level 1 %+v", first)
	}
	if atCap.Exp != 0 || atCap.MasterySP != progression.LevelCap {
		t.Errorf("level cap row %+v", atCap)
	}
	if past.Level != 100 || past.MasterySP != 0 {
		t.Errorf("past the table %+v", past)
	}
}
