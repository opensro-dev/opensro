package quest

import (
	"opensro.online/server/internal/testsupport/clientdata"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

var collectionCatalogFixture = []struct {
	code      string
	id, level uint32
}{
	{"QNO_WC_ARMOR_1", 20, 38},
	{"QNO_CH_POTION_3", 53, 6}, {"QNO_CH_SPECIAL_1", 48, 13}, {"QNO_CH_SPECIAL_2", 61, 18},
	{"QNO_CH_GENARAL_BO_2", 56, 11}, {"QNO_CH_FERRY2_1", 62, 19},
}

func TestCollectionCatalogUsesV150CountsAndAtomicTurnIn(t *testing.T) {
	clientdata.RequireGameData(t)
	for _, tc := range []struct {
		code          string
		count         uint32
		exp, gold, sp int64
	}{
		{"QNO_WC_ARMOR_1", 20, 75000, 13500, 0},
		{"QNO_CH_POTION_3", 20, 6600, 0, 4000},
		{"QNO_CH_SPECIAL_1", 10, 15300, 4000, 5000},
		{"QNO_CH_SPECIAL_2", 100, 42300, 10900, 10000},
		{"QNO_CH_GENARAL_BO_2", 20, 28200, 8300, 10000},
		{"QNO_CH_FERRY2_1", 100, 112000, 28500, 25000},
	} {
		t.Run(tc.code, func(t *testing.T) {
			rt := testRuntime(t)
			c := questCharacter()
			def, _ := rt.Defs.ByCodename(tc.code)
			level := int64(def.Level)
			c.Level = &level
			if def.CollectCount != tc.count || def.RewardExp != tc.exp || def.RewardGold != tc.gold || def.RewardSkillExp != tc.sp {
				t.Fatalf("wrong version contract: %+v", def)
			}
			if len(def.RequiredQuestIDs) > 0 {
				if _, err := rt.StartQuest(c, tc.code); err == nil {
					t.Fatal("missing predecessor admitted")
				}
				c.CompletedQuestIds = append(c.CompletedQuestIds, def.RequiredQuestIDs...)
			}
			if _, err := rt.StartQuest(c, tc.code); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.CompleteNpcQuest(c, tc.code); err == nil {
				t.Fatal("empty inventory completed quest")
			}
			c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: def.CollectItemRefID, Codename: def.CollectItemCodename, StackCount: int64(tc.count)}}
			rt.NotifyInventoryChanged(c)
			if _, err := rt.CompleteNpcQuest(c, tc.code); err != nil {
				t.Fatal(err)
			}
			for _, row := range c.MissionInventory {
				if row.RefObjID == def.CollectItemRefID {
					t.Fatal("turn-in left objective items")
				}
			}
			if tc.gold > 0 && (c.Gold == nil || *c.Gold != tc.gold) {
				t.Fatal("gold reward missing")
			}
			if tc.code == "QNO_CH_POTION_3" && (len(c.MissionInventory) != 1 || c.MissionInventory[0].Codename != "ITEM_ETC_MP_POTION_01" || c.MissionInventory[0].StackCount != 50) {
				t.Fatal("MP herb reward missing")
			}
			if _, err := rt.CompleteNpcQuest(c, tc.code); err == nil {
				t.Fatal("duplicate turn-in paid")
			}
		})
	}
}
