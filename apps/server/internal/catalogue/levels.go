/*
===========================================================================

levels.go - levels.json: the level curve and the mastery training prices

The website's skill build planner reads the character level cap, the
mastery caps and, per level, the experience to the next level and the SP
a mastery costs to train from that level to the next. Every number comes
from the server's own constants and level table (progression,
enterworld.TextdataLevels), so the site never hard-codes one.

===========================================================================
*/
package catalogue

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
)

/*
================
LevelRow
================
*/
type LevelRow struct {
	Level     int64 `json:"level"`
	Exp       int64 `json:"exp,omitempty" doc:"Experience to advance from this character level to the next; absent past the table."`
	MasterySP int64 `json:"masterySp" doc:"SP to train a mastery from this level to the next (progression.MasteryLevelUpCost); 0 to 1 is free."`
}

/*
================
MasteryTotal

How many mastery levels a character may hold in all (the 07:05 gate).
================
*/
type MasteryTotal struct {
	Chinese          int64 `json:"chinese" doc:"A Chinese character's flat allowance."`
	EuropeanPerLevel int64 `json:"europeanPerLevel" doc:"A European character's allowance per character level..."`
	EuropeanCeiling  int64 `json:"europeanCeiling" doc:"...clamped to this ceiling."`
	Beta             int64 `json:"beta,omitempty" doc:"The closed beta's flat allowance for everyone (port-only, SRO_BETA_MASTERY); absent when the build ran without it."`
}

/*
================
LevelsFile
================
*/
type LevelsFile struct {
	Header
	LevelCap        int64        `json:"levelCap" doc:"The character level cap."`
	MasteryLevelCap int64        `json:"masteryLevelCap" doc:"One mastery's ceiling; a mastery also never passes the character's level."`
	MasteryTotal    MasteryTotal `json:"masteryTotal"`
	Levels          []LevelRow   `json:"levels"`
}

/*
================
BuildLevels

One row per level up to the mastery cap (mastery prices run past the
character cap), from the level table.
================
*/
func BuildLevels(header Header, levels enterworld.LevelDataSource, betaMastery int64) LevelsFile {
	file := LevelsFile{Header: header, LevelCap: progression.LevelCap, MasteryLevelCap: progression.MasteryLevelCap,
		MasteryTotal: MasteryTotal{Chinese: progression.TotalMasteryCapChina,
			EuropeanPerLevel: progression.TotalMasteryEuropePerLevel,
			EuropeanCeiling:  progression.TotalMasteryCapEuropeCeiling, Beta: betaMastery},
		Levels: []LevelRow{}}
	for level := int64(1); level <= progression.MasteryLevelCap; level++ {
		row := LevelRow{Level: level}
		if exp, ok := levels.ExpRequired(level); ok && level < progression.LevelCap {
			row.Exp = exp
		}
		if sp, ok := progression.MasteryLevelUpCost(levels, level); ok {
			row.MasterySP = sp
		}
		file.Levels = append(file.Levels, row)
	}
	return file
}
