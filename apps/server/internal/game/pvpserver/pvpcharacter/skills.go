/*
===========================================================================

skills.go - every skill a finished PVP character holds

Port-only, not native. A PVP character is created with every mastery of
its race at the level cap, and with every skill those masteries reach,
each group at its highest rank. The native learn check
(progression.HandleSkillLearn) decides what a trainer would teach; this
file applies the same tests in one pass over the table, minus SP and the
exact-next-rank rule, which only pace learning one rank at a time.

===========================================================================
*/

package pvpcharacter

import (
	"slices"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
SkillSource

The skill table, row by row (enterworld.TextdataSkills).
================
*/
type SkillSource interface {
	EachSkill(visit func(row enterworld.SkillRow))
}

/*
================
FullSkills

The id of the highest learnable rank of every group the masteries reach,
by group. A row is learnable when a trainer could teach it: it costs SP
(SPCost 0 rows are granted, never taught), it is no chain follow-up, its
race is the character's or any, and each mastery it names is held at its
required level. race is the native country byte: 0 China, 1 Europe.

Prerequisite groups are not checked: every group the masteries reach is
taken at its top rank, so a prerequisite inside them is met by
construction.
================
*/
func FullSkills(source SkillSource, race uint8, masteries []domain.CharacterMastery) []uint32 {
	held := make(map[uint32]int64, len(masteries))
	for _, mastery := range masteries {
		held[mastery.ID] = mastery.Level
	}
	type best struct {
		id   uint32
		rank int64
	}
	top := map[uint32]best{}
	source.EachSkill(func(row enterworld.SkillRow) {
		if row.SPCost <= 0 || row.ChainSub {
			return
		}
		if row.RequiredRace != enterworld.SkillRaceAny && row.RequiredRace != race {
			return
		}
		for _, requirement := range row.Masteries {
			if requirement.ID == 0 {
				continue
			}
			level, ok := held[requirement.ID]
			if !ok || level < requirement.Level {
				return
			}
		}
		if current, ok := top[row.Group]; !ok || row.Level > current.rank {
			top[row.Group] = best{id: row.ID, rank: row.Level}
		}
	})
	groups := make([]uint32, 0, len(top))
	for group := range top {
		groups = append(groups, group)
	}
	slices.Sort(groups)
	ids := make([]uint32, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, top[group].id)
	}
	return ids
}
