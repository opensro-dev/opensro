/*
===========================================================================

skilllearn_order_test.go - native first-failure ordering and prerequisite slots

===========================================================================
*/
package progression

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestSkillLearnFirstFailureOrder

Start with every requirement failing and satisfy one gate at a time. Each
refusal must preserve the character, including the final insufficient-SP gate.
================
*/
func TestSkillLearnFirstFailureOrder(t *testing.T) {
	c := testCharacter()
	rt := newSkillTestRuntime(c)
	skills := rt.deps.SkillData().(staticSkills)
	row := skills[skillSmashB1]
	row.SPCost = 0
	row.Masteries[0].ID = euMasteryWarlock
	row.ReqStr, row.ReqInt, row.RequiredRace, row.Level = 200, 200, 1, 2
	*c.SkillPoints = 0
	steps := []struct {
		code byte
		fix  func()
	}{
		{0x09, func() { row.SPCost = 117 }},
		{0x01, func() { row.Masteries[0].ID = chMastery }},
		{0x02, func() { setMastery(c, chMastery, 27) }},
		{0x03, func() { row.ReqStr = 0 }},
		{0x04, func() { row.ReqInt = 0 }},
		{0x05, func() { row.RequiredRace = enterworld.SkillRaceAny }},
		{0x0c, func() { row.Level = 1 }},
		{0x06, func() { c.Skills = append(c.Skills, skillSmashA1) }},
		{0x07, func() { c.Skills[len(c.Skills)-1] = skillSmashA9 }},
		{0x0a, func() {}},
	}
	for _, step := range steps {
		skills[row.ID] = row
		count := len(c.Skills)
		result := rt.HandleSkillLearn(testDivision, c, skillPayload(row.ID))
		if len(result.Frames) != 1 || !bytes.Equal(result.Frames[0].Payload, []byte{wire.ResultError, step.code}) {
			t.Fatalf("expected first refusal %02x, got %+v", step.code, result)
		}
		if *c.SkillPoints != 0 || len(c.Skills) != count {
			t.Fatal("refusal changed learned skills or SP")
		}
		step.fix()
	}
}
