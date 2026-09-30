/*
===========================================================================

passive_defense_native_test.go - passive defense against the native writer

Native writer vectors for passive defense, and passives following their
equipment requirement.

===========================================================================
*/
package combat

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestPassiveDefenseNativeWriterVectors
================
*/
func TestPassiveDefenseNativeWriterVectors(t *testing.T) {
	licensed.RequireGameData(t)
	b, err := os.ReadFile("testdata/native-defp-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Schema string `json:"schema"`
		Cases  []struct {
			ID                uint32 `json:"skill_id"`
			Physical, Magical uint32
			Writes            []struct {
				Parameter, Channel uint32
				Bits               uint32 `json:"float32_bits"`
			}
		}
	}
	if err = json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if report.Schema != "sro-native-defp-vectors-v1" || len(report.Cases) != 43 {
		t.Fatal("invalid native observations")
	}
	source := enterworld.NewTextdataSkills(licensed.RetailTextdataDir(t))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	for i, c := range report.Cases {
		t.Run(fmt.Sprintf("%d/skill-%d", i, c.ID), func(t *testing.T) {
			s := passiveSkills{1: {ID: 1, Group: 1, Level: 1, PassiveDefense: enterworld.SkillPassiveDefense{Pinned: true, Physical: c.Physical, Magical: c.Magical}}}
			if c.ID != 0 {
				row, ok := source.SkillByID(c.ID)
				if !ok || !row.PassiveDefense.Pinned || row.PassiveDefense.Physical != c.Physical || row.PassiveDefense.Magical != c.Magical {
					t.Fatal("authored program not admitted", c.ID)
				}
				row.ID = 1
				row.Reqi = enterworld.SkillReqi{} // values only; gating is TestPassiveDefenseFollowsItsEquipment
				s[1] = row
			}
			writes, _, err := learnedPassives(&domain.Character{Skills: []uint32{1}}, s, nil, 0)
			if err != nil || len(c.Writes) != 2 || len(writes) != 2 {
				t.Fatal(err)
			}
			for i, w := range c.Writes {
				if w.Parameter != uint32(writes[i].Parameter) || w.Channel != uint32(writes[i].Channel) || math.Float32bits(writes[i].Value) != w.Bits {
					t.Fatalf("native %x port %x", w.Bits, math.Float32bits(writes[i].Value))
				}
			}
		})
	}
}

/*
================
TestPassiveDefenseFollowsItsEquipment
================
*/
// 59F0E0: a passive defense with reqi contributes only while the equipment
// passes the walk - here a staff (primary TID4 15) in slot 6.
func TestPassiveDefenseFollowsItsEquipment(t *testing.T) {
	staff := &enterworld.ItemRef{Codename: "ITEM_EU_STAFF_TEST", TypeIDs: [4]int64{3, 1, 6, 15}}
	items := itemRefs{staff.Codename: staff}
	row := enterworld.SkillRow{ID: 1, Group: 1, Level: 1, PassiveDefense: enterworld.SkillPassiveDefense{Pinned: true, Physical: 9}}
	row.Reqi = enterworld.SkillReqi{Present: true, Count: 1}
	row.Reqi.Pairs[0] = enterworld.SkillReqiPair{Kind: 6, Value: 15}
	s := passiveSkills{1: row}
	c := &domain.Character{Skills: []uint32{1}}
	if writes, _, _ := learnedPassives(c, s, items, 0); len(writes) != 0 {
		t.Fatalf("contributed without a staff: %+v", writes)
	}
	c.MissionInventory = []domain.InventoryRow{{Slot: 6, Codename: staff.Codename, TypeFlags: staff.TypeFlags(), Durability: 10}}
	if writes, _, _ := learnedPassives(c, s, items, 15); len(writes) != 2 {
		t.Fatalf("staff equipped, writes %+v", writes)
	}
	c.MissionInventory[0].Durability = 0
	if writes, _, _ := learnedPassives(c, s, items, 15); len(writes) != 0 {
		t.Fatal("a broken staff still carried the passive")
	}
}
