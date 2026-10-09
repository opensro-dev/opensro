/*
===========================================================================

passivestats_test.go - learning a stat passive republishes the stat block

===========================================================================
*/

package progression

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestLearningForceIncreasingRaisesTheShownMaximumMP

SKILL_CH_WATER_PASSIVE_A_01 (Force Increasing) is mpi 102 0: flat 102 on
maximum MP (594AC0 0x5954DF). Learning it must resend the stat block with
the raised maximum, as a defense passive does; before, only defense
passives refreshed it and the client kept the old maximum until a relog.
================
*/
func TestLearningForceIncreasingRaisesTheShownMaximumMP(t *testing.T) {
	licensed.RequireGameData(t)
	source := enterworld.NewTextdataSkills(licensed.RetailTextdataDir(t))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	row, ok := source.SkillByCodename("SKILL_CH_WATER_PASSIVE_A_01")
	if !ok || !row.PassiveParameters.MaxMP.Present || row.PassiveDefense.Pinned {
		t.Fatalf("Force Increasing not admitted as an mpi passive: %+v", row.PassiveParameters)
	}
	seed := testCharacter()
	seed.Level = int64Ptr(90)
	seed.MaxLevel = int64Ptr(90)
	seed.SkillPoints = int64Ptr(1000000)
	seed.Masteries = []enterworld.CharacterMastery{{ID: 276, Level: 90}}
	seed.MissionInventory = nil
	rt, c, _ := openDoorRuntime(t, t.TempDir(), seed)
	deps := rt.deps.(*enterworld.Deps)
	deps.Skills = source
	catalogs := combat.Catalogs{Items: deps.Items, Skills: source}
	before, err := combat.PlayerBaseStats(c.Snapshot(), catalogs)
	if err != nil {
		t.Fatal(err)
	}
	r := rt.HandleSkillLearn(testDivision, c, skillPayload(row.ID))
	if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("learn refused %+v", r)
	}
	after, err := combat.PlayerBaseStats(c.Snapshot(), catalogs)
	if err != nil || after.MaxMP != before.MaxMP+102 {
		t.Fatalf("maximum MP %d -> %d, want +102 (%v)", before.MaxMP, after.MaxMP, err)
	}
	if len(r.Frames) != 3 || r.Frames[2].Opcode != wire.OpBaseStats ||
		!bytes.Equal(r.Frames[2].Payload, enterworld.BuildLoginStatBlock(c.Snapshot(), after)) {
		t.Fatalf("learn did not publish the raised stats: %d frames", len(r.Frames))
	}
}
