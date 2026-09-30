/*
===========================================================================

passivedefense_test.go - defense passives: learn and restore

===========================================================================
*/

package progression

import (
	"bytes"
	"fmt"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/licensed"
)

func TestDefensePassiveAllRanksLearnAndStoreRestore(t *testing.T) {
	licensed.RequireGameData(t)
	source := enterworld.NewTextdataSkills(licensed.RetailTextdataDir(t))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	for _, family := range []struct {
		name    string
		ranks   int
		mastery uint32
		weapon  int64
	}{{"SKILL_CH_COLD_PASSIVE_A", 9, 273, 0}, {"SKILL_EU_WARRIOR_SHIELDP_DEFENSE_A", 15, 513, 7}, {"SKILL_EU_WARRIOR_FRENZYA_DEFENSE_A", 14, 513, 9}} {
		t.Run(family.name, func(t *testing.T) {
			seed := testCharacter()
			seed.Level = int64Ptr(90)
			seed.MaxLevel = int64Ptr(90)
			seed.SkillPoints = int64Ptr(1000000)
			seed.Masteries = []enterworld.CharacterMastery{{ID: family.mastery, Level: 90}}
			seed.MissionInventory = nil
			dir := t.TempDir()
			rt, c, authority := openDoorRuntime(t, dir, seed)
			deps := rt.deps.(*enterworld.Deps)
			deps.Skills = source
			if family.weapon != 0 {
				ref := &enterworld.ItemRef{RefObjID: 42, Codename: "DEFENSE_WEAPON", TypeIDs: [4]int64{3, 1, 6, family.weapon}, Combat: &enterworld.ItemCombatRef{}}
				deps.Items = passiveItemSource{ref}
				deps.Update(c, "test equipment", func() bool {
					c.MissionInventory = []enterworld.InventoryRow{{Slot: 6, RefObjID: 42, Codename: ref.Codename, VarianceBits: "0", Durability: 100}}
					return true
				})
			}
			byRank := map[[2]int64]enterworld.SkillRow{}
			for _, projection := range source.SpawnSkillRows() {
				r, _ := source.SkillByID(projection.ID)
				if !r.ChainSub {
					key := [2]int64{int64(r.Group), r.Level}
					if _, exists := byRank[key]; !exists {
						byRank[key] = r
					}
				}
			}
			visiting := map[uint32]bool{}
			var learnPrerequisite func(enterworld.SkillRow)
			learnPrerequisite = func(row enterworld.SkillRow) {
				if enterworld.SkillLearned(c, row.ID) {
					return
				}
				if visiting[row.ID] {
					t.Fatal("prerequisite cycle", row.ID)
				}
				visiting[row.ID] = true
				if row.Level > 1 {
					prior, ok := byRank[[2]int64{int64(row.Group), row.Level - 1}]
					if !ok {
						t.Fatal("missing prior rank", row.ID)
					}
					learnPrerequisite(prior)
				}
				for _, req := range row.Prerequisites {
					if req.ID == 0 {
						continue
					}
					r, ok := byRank[[2]int64{int64(req.ID), req.Level}]
					if !ok {
						t.Fatal("missing prerequisite", req)
					}
					learnPrerequisite(r)
				}
				result := rt.HandleSkillLearn(testDivision, c, skillPayload(row.ID))
				if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
					t.Fatal("prerequisite learn refused", row.ID, result)
				}
				delete(visiting, row.ID)
			}

			base, _, err := combat.PlayerStats(c.Snapshot(), combat.Catalogs{Items: deps.Items, Skills: source})
			if err != nil {
				t.Fatal(err)
			}
			var last enterworld.SkillRow
			for rank := 1; rank <= family.ranks; rank++ {
				row, ok := source.SkillByCodename(fmt.Sprintf("%s_%02d", family.name, rank))
				if !ok || !row.PassiveDefense.Pinned {
					t.Fatal("missing admitted rank", rank)
				}
				for _, req := range row.Prerequisites {
					if req.ID != 0 {
						r, ok := byRank[[2]int64{int64(req.ID), req.Level}]
						if !ok {
							t.Fatal("missing prerequisite", req)
						}
						learnPrerequisite(r)
					}
				}
				before := *c.SkillPoints
				r := rt.HandleSkillLearn(testDivision, c, skillPayload(row.ID))
				if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 || *c.SkillPoints != before-row.SPCost {
					t.Fatalf("rank %d refused %+v", rank, r)
				}
				display, e := combat.PlayerBaseStats(c.Snapshot(), combat.Catalogs{Items: deps.Items, Skills: source})
				if e != nil || len(r.Frames) != 3 || r.Frames[2].Opcode != wire.OpBaseStats || !bytes.Equal(r.Frames[2].Payload, enterworld.BuildLoginStatBlock(c.Snapshot(), display)) {
					t.Fatal("learn did not publish committed stats", e, r)
				}
				stats, _, err := combat.PlayerStats(c.Snapshot(), combat.Catalogs{Items: deps.Items, Skills: source})
				want := float64(float32(base.PhysicalDefense) + float32(row.PassiveDefense.Physical))
				if err != nil || stats.PhysicalDefense != want {
					t.Fatalf("rank %d defense %v want %v err %v", rank, stats.PhysicalDefense, want, err)
				}
				if again := rt.HandleSkillLearn(testDivision, c, skillPayload(row.ID)); again.Frames[0].Payload[0] == 1 || *c.SkillPoints != before-row.SPCost {
					t.Fatal("duplicate learn charged")
				}
				last = row
			}
			authority.Close()
			_, restored, _ := openDoorRuntime(t, dir, nil)
			stats, _, err := combat.PlayerStats(restored.Snapshot(), combat.Catalogs{Items: deps.Items, Skills: source})
			want := float64(float32(base.PhysicalDefense) + float32(last.PassiveDefense.Physical))
			if err != nil || stats.PhysicalDefense != want {
				t.Fatalf("restore defense %v want %v err %v", stats.PhysicalDefense, want, err)
			}
			count := 0
			for _, id := range restored.Skills {
				row, _ := source.SkillByID(id)
				if row.Group == last.Group {
					count++
				}
			}
			if count != 1 {
				t.Fatal("historical ranks persisted", count)
			}
		})
	}
}

func TestDefenseLearnInvalidStatsDoesNotSpendOrPublish(t *testing.T) {
	licensed.RequireGameData(t)
	source := enterworld.NewTextdataSkills(licensed.RetailTextdataDir(t))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	seed := testCharacter()
	seed.SkillPoints = int64Ptr(100)
	seed.Masteries = []enterworld.CharacterMastery{{ID: 273, Level: 90}}
	seed.MissionInventory = []enterworld.InventoryRow{{Slot: 6, RefObjID: 999, Codename: "MISSING"}}
	rt, c, _ := openDoorRuntime(t, t.TempDir(), seed)
	rt.deps.(*enterworld.Deps).Skills = source
	before := len(c.Skills)
	r := rt.HandleSkillLearn(testDivision, c, skillPayload(106))
	if len(r.Frames) != 1 || r.Frames[0].Payload[0] == 1 || *c.SkillPoints != 100 || len(c.Skills) != before {
		t.Fatal("invalid stat derivation committed", r)
	}
}
