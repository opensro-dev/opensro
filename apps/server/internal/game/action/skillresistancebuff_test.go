/*
===========================================================================

skillresistancebuff_test.go - Holy Word resists while it lasts

A resistance buff installs the reat and real a resistance passive
(Protection) installs: the six flat status reductions rise by reat's value
and real's flat guards the masked statuses at its grade, for as long as
the instance lives.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
)

// holyWordA1 is SKILL_EU_CLERIC_SAINTA_ABNORMAL_A_01: dura 300000,
// reat 63 48, real 0x17FAFC0 100 2, on one friendly target.
const holyWordA1 = 10364

/*
================
holyWordProbe

A grade-2 status of the real mask at a certain chance, as a monster's
hit would roll it on c.
================
*/
func holyWordProbe(t *testing.T, rt *Runtime, c *enterworld.Character, mask uint32) int {
	t.Helper()
	var params abnormal.SkillParams
	found := false
	for index := 6; index < abnormal.SourceCount && !found; index++ {
		source := abnormal.Sources[index]
		if source.Resist >= 0 && mask&source.Status.Bit() != 0 {
			params.Params[index] = abnormal.Param{Present: true, Args: [6]uint32{10000, 100, 2}}
			found = true
		}
	}
	if !found {
		t.Fatal("no resistible status in the mask")
	}
	defender, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	records, err := rt.rollCreatureOnPlayer(testDivision, 1, 1, &params, c, defender, nil)
	if err != nil {
		t.Fatal(err)
	}
	return len(records)
}

/*
================
TestHolyWordResistsWhileItLasts

Cast on oneself: the flat reductions 0x91..0x96 rise by 48 and a certain
grade-2 status of the mask no longer lands (flat 100 at grade 2); before
the cast the same roll lands.
================
*/
func TestHolyWordResistsWhileItLasts(t *testing.T) {
	rt, _, c := concealmentFixture(t, holyWordA1)
	row := rt.deps.SkillData().(staticSkillSource)[holyWordA1]
	if !row.TimedEffect.Pinned || row.TimedEffect.Reat != (enterworld.SkillPassiveReat{Mask: 63, Value: 48}) ||
		row.TimedEffect.Real != (enterworld.SkillPassiveReal{Mask: 0x17fafc0, Flat: 100, Grade: 2}) {
		t.Fatalf("Holy Word not admitted: %+v", row.TimedEffect)
	}
	// 56 MP is beyond a level-1 caster; the cost is not under test.
	row.Consumption.MP = 10
	rt.deps.SkillData().(staticSkillSource)[holyWordA1] = row
	equipWeaponKind(rt, c, int64(row.RequiredWeaponKinds[0]))
	base, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if holyWordProbe(t, rt, c, row.TimedEffect.Real.Mask) == 0 {
		t.Fatal("the probe status did not land before the buff")
	}

	if r := castSelf(rt, c, holyWordA1); len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 || !hasSkillEffect(rt, c.Name, holyWordA1) {
		t.Fatalf("Holy Word cast: %+v", r)
	}
	stats, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	for id := uint16(0x91); id <= 0x96; id++ {
		before, _ := base.Param(id)
		if after, _ := stats.Param(id); after != before+48 {
			t.Fatalf("parameter %x = %v, want %v", id, after, before+48)
		}
	}
	if n := holyWordProbe(t, rt, c, row.TimedEffect.Real.Mask); n != 0 {
		t.Fatalf("%d status record(s) landed through Holy Word", n)
	}
}
