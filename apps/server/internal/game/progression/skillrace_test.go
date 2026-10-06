/*
===========================================================================

skillrace_test.go - race admission is independent of mastery membership

===========================================================================
*/
package progression

import (
	"bytes"
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestSkillLearnAuthoredRace
================
*/
func TestSkillLearnAuthoredRace(t *testing.T) {
	for _, country := range []uint8{0, 1} {
		for _, required := range []uint8{0, 1, 2, enterworld.SkillRaceAny} {
			t.Run(fmt.Sprintf("country-%d/required-%d", country, required), func(t *testing.T) {
				c := testCharacter()
				if country == 1 {
					c.ModelCodename = "CHAR_EU_MAN_ADVENTURER"
					c.RaceIndex = int64Ptr(enterworld.RaceEurope)
				}
				// Preserve a satisfied mastery on both characters: the country
				// gate must not depend on the normal starter mastery selection.
				setMastery(c, chMastery, 5)
				rt := newSkillTestRuntime(c)
				skills := rt.deps.SkillData().(staticSkills)
				row := skills[skillSmashA1]
				row.RequiredRace = required
				skills[row.ID] = row
				beforeSP, beforeSkills := *c.SkillPoints, len(c.Skills)
				out := rt.HandleSkillLearn(testDivision, c, skillPayload(row.ID))
				accepted := required == country || required == enterworld.SkillRaceAny
				if accepted {
					if out.Frames[0].Payload[0] != wire.ResultSuccess || *c.SkillPoints != beforeSP-row.SPCost || len(c.Skills) != beforeSkills+1 {
						t.Fatalf("accepted learning: %+v", out)
					}
				} else if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{wire.ResultError, 0x05}) || *c.SkillPoints != beforeSP || len(c.Skills) != beforeSkills {
					t.Fatalf("race refusal mutated character or wrong response: %+v", out)
				}
			})
		}
	}
}
