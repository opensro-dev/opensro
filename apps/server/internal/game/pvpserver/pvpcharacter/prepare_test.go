/*
===========================================================================

prepare_test.go - a PVP character is created finished, in Samarkand

===========================================================================
*/

package pvpcharacter

import (
	"slices"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/progression"
)

const testCap int64 = 110

func newCharacter(model string) *domain.Character {
	level, gold := int64(1), int64(0)
	flags := int64(domain.VisualFlagBeginner)
	return &domain.Character{Name: "Tester", ModelCodename: model, Level: &level, Gold: &gold, VisualFlags: &flags}
}

func TestPrepareMakesAFinishedCharacter(t *testing.T) {
	cases := []struct {
		model     string
		race      uint8
		masteries int
	}{
		{"CHAR_CH_MAN_ADVENTURER", 0, 7},
		{"CHAR_EU_MAN_NOBLE", 1, 6},
	}
	for _, c := range cases {
		character := newCharacter(c.model)
		base := []uint32{900}
		source := fakeSkills{
			skill(1, 10, 1, c.race, need(257, 1), need(0, 0)),
			skill(2, 11, 1, c.race, need(513, 1)),
		}

		Prepare(character, base, source, testCap)

		if *character.Level != testCap || *character.MaxLevel != testCap {
			t.Fatalf("%s: level %d max %d", c.model, *character.Level, *character.MaxLevel)
		}
		if *character.Experience != 0 || *character.SkillExp != 0 {
			t.Fatalf("%s: experience %d skill exp %d", c.model, *character.Experience, *character.SkillExp)
		}
		stat := progression.BaseStatAtLevel(testCap)
		if *character.Strength != stat || *character.Intellect != stat {
			t.Fatalf("%s: STR %d INT %d, want %d", c.model, *character.Strength, *character.Intellect, stat)
		}
		if *character.StatPoints != 327 {
			t.Fatalf("%s: stat points %d, want 327", c.model, *character.StatPoints)
		}
		if len(character.Masteries) != c.masteries {
			t.Fatalf("%s: %d masteries, want %d", c.model, len(character.Masteries), c.masteries)
		}
		for _, mastery := range character.Masteries {
			if mastery.Level != testCap {
				t.Fatalf("%s: mastery %d at %d", c.model, mastery.ID, mastery.Level)
			}
		}
		if !slices.Contains(character.Skills, 900) {
			t.Fatalf("%s: base skill lost: %v", c.model, character.Skills)
		}
		if *character.Gold != 100_000_000 {
			t.Fatalf("%s: gold %d", c.model, *character.Gold)
		}
		if *character.VisualFlags&int64(domain.VisualFlagBeginner) != 0 {
			t.Fatalf("%s: beginner flag still set", c.model)
		}
		world := character.World
		if world == nil || !world.SpawnSet || world.Spawn == nil || world.RebirthPoint == nil {
			t.Fatalf("%s: world %+v", c.model, world)
		}
		if *world.Spawn.RegionID != 27244 || *world.RebirthPoint.RegionID != 27244 {
			t.Fatalf("%s: spawn region %d rebirth %d", c.model, *world.Spawn.RegionID, *world.RebirthPoint.RegionID)
		}
		if world.Spawn.RegionID == world.RebirthPoint.RegionID {
			t.Fatalf("%s: spawn and rebirth share a pointer", c.model)
		}
	}
}

func TestPrepareLearnsTheRaceSkills(t *testing.T) {
	character := newCharacter("CHAR_CH_MAN_ADVENTURER")
	source := fakeSkills{
		skill(1, 10, 1, 0, need(257, 1)),
		skill(2, 10, 2, 0, need(257, 110)),
		skill(3, 20, 1, 1, need(513, 1)),
	}
	Prepare(character, []uint32{900}, source, testCap)
	if !slices.Equal(character.Skills, []uint32{900, 2}) {
		t.Fatalf("skills %v, want [900 2]", character.Skills)
	}
}
