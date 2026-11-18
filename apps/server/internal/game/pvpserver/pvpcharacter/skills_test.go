/*
===========================================================================

skills_test.go - every learnable skill of the character's masteries

===========================================================================
*/

package pvpcharacter

import (
	"slices"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

type fakeSkills []enterworld.SkillRow

func (f fakeSkills) EachSkill(visit func(enterworld.SkillRow)) {
	for _, row := range f {
		visit(row)
	}
}

func skill(id, group uint32, rank int64, race uint8, masteries ...enterworld.SkillRequirement) enterworld.SkillRow {
	row := enterworld.SkillRow{ID: id, Group: group, Level: rank, RequiredRace: race, SPCost: 10}
	copy(row.Masteries[:], masteries)
	return row
}

func need(id uint32, level int64) enterworld.SkillRequirement {
	return enterworld.SkillRequirement{ID: id, Level: level}
}

func TestFullSkillsTakesTheTopRankPerGroup(t *testing.T) {
	chain := skill(5, 30, 1, enterworld.SkillRaceAny, need(257, 1))
	chain.ChainSub = true
	free := skill(6, 31, 1, enterworld.SkillRaceAny, need(257, 1))
	free.SPCost = 0
	source := fakeSkills{
		skill(1, 10, 1, enterworld.SkillRaceAny, need(257, 1)),
		skill(2, 10, 2, enterworld.SkillRaceAny, need(257, 20)),
		skill(3, 10, 3, enterworld.SkillRaceAny, need(257, 40)),
		skill(4, 20, 1, 1, need(257, 1)), // European only
		chain,
		free,
		skill(7, 40, 1, enterworld.SkillRaceAny, need(257, 10), need(258, 10)),
		skill(8, 50, 1, enterworld.SkillRaceAny, need(257, 10), need(259, 10)),
	}
	masteries := []domain.CharacterMastery{{ID: 257, Level: 30}, {ID: 258, Level: 30}}

	got := FullSkills(source, 0, masteries)

	want := []uint32{2, 7}
	if !slices.Equal(got, want) {
		t.Fatalf("FullSkills = %v, want %v", got, want)
	}
}

func TestFullSkillsKeepsEuropeanSkillsForEuropeans(t *testing.T) {
	source := fakeSkills{
		skill(1, 10, 1, 0, need(513, 1)),
		skill(2, 20, 1, 1, need(513, 1)),
	}
	got := FullSkills(source, 1, []domain.CharacterMastery{{ID: 513, Level: 110}})
	if !slices.Equal(got, []uint32{2}) {
		t.Fatalf("FullSkills = %v, want [2]", got)
	}
}
