/*
===========================================================================

passivecritical_test.go - critical passives: learn, upgrade, restore

===========================================================================
*/

package progression

import (
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
passiveItemSource
================
*/
type passiveItemSource struct{ ref *enterworld.ItemRef }

/*
================
ItemRefByCodename
================
*/
func (s passiveItemSource) ItemRefByCodename(name string) (*enterworld.ItemRef, bool) {
	return s.ref, name == s.ref.Codename
}

/*
================
TestPassiveLearnUpgradeAndStoreRestoration
================
*/
func TestPassiveLearnUpgradeAndStoreRestoration(t *testing.T) {
	licensed.RequireGameData(t)
	source := enterworld.NewTextdataSkills(licensed.RetailTextdataDir(t))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	seed := testCharacter()
	seed.ModelCodename = "CHAR_EU_MAN_ADVENTURER"
	seed.RaceIndex = int64Ptr(enterworld.RaceEurope)
	seed.Level = int64Ptr(90)
	seed.MaxLevel = int64Ptr(90)
	seed.SkillPoints = int64Ptr(1000000)
	seed.Masteries = []enterworld.CharacterMastery{{ID: 513, Level: 90}}
	ref := &enterworld.ItemRef{RefObjID: 42, Codename: "TWOHAND", TypeIDs: [4]int64{3, 1, 6, 8}, Combat: &enterworld.ItemCombatRef{CriticalRate: enterworld.ItemStatRange{Min: 2, Max: 2}}}
	seed.MissionInventory = []enterworld.InventoryRow{{Slot: 6, RefObjID: 42, Codename: ref.Codename, VarianceBits: "0", Durability: 100}}
	dir := t.TempDir()
	rt, c, authority := openDoorRuntime(t, dir, seed)
	deps := rt.deps.(*enterworld.Deps)
	deps.Skills = source
	deps.Items = passiveItemSource{ref}
	for rank := int64(1); rank <= 8; rank++ {
		row, _ := source.SkillByID(uint32(7521 + rank)) // version-qualified fixture; production never uses this range
		// Prerequisite learning is covered by the authenticated probe. Here
		// seed its authored rank to isolate passive replacement/persistence.
		for _, requirement := range row.Prerequisites {
			if requirement.ID == 0 {
				continue
			}
			for id := uint32(7492); id <= 7521; id++ {
				r, ok := source.SkillByID(id)
				if ok && r.Group == requirement.ID && r.Level == requirement.Level {
					deps.Update(c, "test prerequisite", func() bool { c.Skills = append(c.Skills, id); return true })
					break
				}
			}
		}
		before := *c.SkillPoints
		result := rt.HandleSkillLearn(testDivision, c, skillPayload(row.ID))
		if result.Frames[0].Payload[0] != 1 {
			t.Fatalf("rank %d refused %+v", rank, result)
		}
		if *c.SkillPoints != before-row.SPCost {
			t.Fatal("wrong learn cost")
		}
		stats, _, err := combat.PlayerStats(c.Snapshot(), combat.Catalogs{Items: deps.Items, Skills: source})
		if err != nil || stats.CriticalRate != float64(3+rank) {
			t.Fatalf("rank%d stats=%+v err=%v", rank, stats, err)
		}
		again := rt.HandleSkillLearn(testDivision, c, skillPayload(row.ID))
		if again.Frames[0].Payload[0] == 1 || *c.SkillPoints != before-row.SPCost {
			t.Fatal("duplicate learn charged or granted")
		}
	}
	authority.Close()
	_, restored, _ := openDoorRuntime(t, dir, nil)
	stats, _, err := combat.PlayerStats(restored.Snapshot(), combat.Catalogs{Items: passiveItemSource{ref}, Skills: source})
	if err != nil || stats.CriticalRate != 11 {
		t.Fatalf("restored %+v %v", stats, err)
	}
	count := 0
	for _, id := range restored.Skills {
		r, _ := source.SkillByID(id)
		if r.Group == 434 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("restored %d passive ranks", count)
	}
}
