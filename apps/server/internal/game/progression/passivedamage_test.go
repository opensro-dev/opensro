/*
===========================================================================

passivedamage_test.go - damage passives: learn and restore

===========================================================================
*/

package progression

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

func TestDamagePassiveAllRanksLearnAndStoreRestore(t *testing.T) {
	licensed.RequireGameData(t)
	source := enterworld.NewTextdataSkills(licensed.RetailTextdataDir(t))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	seed := testCharacter()
	seed.Level = int64Ptr(90)
	seed.MaxLevel = int64Ptr(90)
	seed.SkillPoints = int64Ptr(1000000)
	seed.Masteries = []enterworld.CharacterMastery{{ID: 513, Level: 90}}
	seed.MissionInventory = nil
	dir := t.TempDir()
	rt, c, authority := openDoorRuntime(t, dir, seed)
	deps := rt.deps.(*enterworld.Deps)
	deps.Skills = source
	// All production skill learns, including replacement and native SP costs.
	for rank := 1; rank <= 22; rank++ {
		row, ok := source.SkillByCodename(fmt.Sprintf("SKILL_EU_WARRIOR_TWOHANDP_ATTACK_A_%02d", rank))
		if !ok {
			t.Fatal("authored rank missing", rank)
		}
		before := *c.SkillPoints
		r := rt.HandleSkillLearn(testDivision, c, skillPayload(row.ID))
		if r.Frames[0].Payload[0] != 1 || *c.SkillPoints != before-row.SPCost {
			t.Fatalf("rank%d refused or wrong cost %+v", rank, r)
		}
		stats, _, err := combat.PlayerStats(c.Snapshot(), combat.Catalogs{Items: deps.Items, Skills: source})
		if err != nil || stats.SkillParameters[enterworld.ParameterTwoHandPower] != row.PassiveParameters.Values[enterworld.ParameterTwoHandPower] {
			t.Fatalf("rank%d %+v %v", rank, stats, err)
		}
		if again := rt.HandleSkillLearn(testDivision, c, skillPayload(row.ID)); again.Frames[0].Payload[0] == 1 || *c.SkillPoints != before-row.SPCost {
			t.Fatal("duplicate learn")
		}
	}
	authority.Close()
	_, restored, _ := openDoorRuntime(t, dir, nil)
	stats, _, err := combat.PlayerStats(restored.Snapshot(), combat.Catalogs{Items: deps.Items, Skills: source})
	if err != nil || stats.SkillParameters[enterworld.ParameterTwoHandPower] != 87 {
		t.Fatalf("restore %+v %v", stats, err)
	}
	count := 0
	for _, id := range restored.Skills {
		row, _ := source.SkillByID(id)
		if row.Group == 433 {
			count++
		}
	}
	if count != 1 {
		t.Fatal("historical ranks stacked", count)
	}
}
