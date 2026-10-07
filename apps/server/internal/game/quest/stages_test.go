package quest

import (
	"bytes"
	"encoding/json"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

func verifyStagedQuestLifecycle(t *testing.T, defs *Definitions, items enterworld.ItemRefSource, root *Definition) {
	t.Helper()
	// Both genders of the race the quest admits (country byte 1 is Europe).
	models := []string{"CHAR_CH_MAN_ADVENTURER", "CHAR_CH_WOMAN_ADVENTURER"}
	if root.CountryByte == 1 {
		models = []string{"CHAR_EU_MAN_ADVENTURER", "CHAR_EU_WOMAN_ADVENTURER"}
	}
	for _, model := range models {
		c := questCharacter()
		c.ModelCodename = model
		// A chain stage (Rahid 3) needs its predecessors and level, as the
		// unstaged lifecycle seeds them.
		c.CompletedQuestIds = append([]uint32(nil), root.RequiredQuestIDs...)
		*c.Level = max(*c.Level, int64(root.Level))
		deps := &enterworld.Deps{Items: items}
		rt, err := NewRuntime(deps, defs, func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) { return nil, true })
		if err != nil {
			t.Fatal(err)
		}
		rt.PlanInventory = action.NewRuntime(deps, nil).PlanQuestInventory
		if _, err := rt.StartQuest(c, root.Codename); err != nil {
			t.Fatal(err)
		}
		snapshot := func() []byte {
			b, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
		for stage := range root.Stages {
			def, _ := definitionAtStage(root, uint16(stage))
			// The persistent record resumes every intermediate stage and counter.
			b := snapshot()
			restored := new(enterworld.Character)
			if err := json.Unmarshal(b, restored); err != nil {
				t.Fatal(err)
			}
			c = restored
			if c.ActiveQuests[0].Stage != uint16(stage) {
				t.Fatal("stage lost across persistence")
			}
			token := stageToken(root.Codename, uint16(stage))
			before := snapshot()
			if _, err := rt.CompleteTalkQuest(c, root.Codename); err == nil {
				t.Fatal("legacy talk path bypassed stage")
			}
			if _, err := rt.AdvanceNpcQuest(c, token, "WRONG"); err == nil {
				t.Fatal("wrong NPC advanced")
			}
			if !bytes.Equal(before, snapshot()) {
				t.Fatal("refusal mutated authority")
			}
			if stage > 0 {
				if _, err := rt.HandleGiveUp(c, u32le(root.RefID)); err == nil {
					t.Fatal("abandonment could farm stage gifts")
				}
			}
			if def.Objective != ObjectiveTalk || def.requiredEquippedItem != "" {
				if _, err := rt.AdvanceNpcQuest(c, token, def.EndNpcCodename); err == nil {
					t.Fatal("unfinished objective advanced")
				}
			}
			if def.requiredEquippedItem != "" {
				for i := range c.MissionInventory {
					if c.MissionInventory[i].Codename == def.requiredEquippedItem {
						c.MissionInventory[i].Slot = 11
					}
				}
			}
			if def.Objective == ObjectiveCollect {
				rows, _, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: def.CollectItemCodename, Count: def.CollectCount}})
				if err != nil {
					t.Fatal(err)
				}
				c.MissionInventory = rows
				rt.InventoryUpdater()(c)
			}
			if def.Objective == ObjectiveKill {
				rt.KillUpdater()(c, "MOB_WRONG", 0)
				for n := uint32(0); n < def.KillCount; n++ {
					rt.KillUpdater()(c, def.KillMonsterCodenames[0], 0)
				}
				if recordProgress(c.ActiveQuests[0]) != def.KillCount {
					t.Fatal("fatal hits lost staged progress")
				}
			}
			options := rt.OptionsForNpc(c, def.EndNpcCodename)
			found := false
			for _, option := range options {
				if option.Codename == token {
					found = true
				}
			}
			if !found {
				t.Fatalf("stage %d has no NPC option", stage)
			}
			result, err := rt.AdvanceNpcQuest(c, token, def.EndNpcCodename)
			if err != nil {
				t.Fatalf("stage %d: %v", stage, err)
			}
			want := QuestUpdateOpUpdate
			if stage == len(root.Stages)-1 {
				want = QuestUpdateOpComplete
			}
			found = false
			for _, f := range result.Frames {
				if f.Opcode == OpQuestUpdate && f.Payload[0] == want {
					found = true
				}
			}
			if !found {
				t.Fatal("wrong stage wire transition")
			}
			before = snapshot()
			if _, err := rt.AdvanceNpcQuest(c, token, def.EndNpcCodename); err == nil {
				t.Fatal("stale confirmation replay advanced")
			}
			if !bytes.Equal(before, snapshot()) {
				t.Fatal("replay paid another reward")
			}
		}
		if len(c.ActiveQuests) != 0 || !questCompleted(c, root.RefID) {
			t.Fatal("final stage did not complete")
		}
		// Each stage's gold (the tutorials' 200-gold errand) is paid once.
		gold := int64(0)
		for _, stage := range root.Stages {
			gold += stage.RewardGold
		}
		paid := int64(0)
		if c.Gold != nil {
			paid = *c.Gold
		}
		if paid != gold {
			t.Fatal("errand money not granted exactly once")
		}
		if _, err := rt.StartQuest(c, root.Codename); err == nil {
			t.Fatal("completed tutorial restarted")
		}
	}
}

func TestStageRewardInventoryFailureIsAtomic(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	if _, err := rt.StartQuest(c, "QTUTORIAL_CH"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.AdvanceNpcQuest(c, "QTUTORIAL_CH@0", "NPC_CH_GENARAL"); err != nil {
		t.Fatal(err)
	}
	for slot := inventory.EquipmentSlotEnd; slot < inventory.BagSlotEnd; slot++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(slot), RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 50})
	}
	before, _ := json.Marshal(c)
	if _, err := rt.AdvanceNpcQuest(c, "QTUTORIAL_CH@1", "NPC_CH_ARMOR"); err == nil {
		t.Fatal("full inventory accepted gift")
	}
	after, _ := json.Marshal(c)
	if !bytes.Equal(before, after) {
		t.Fatal("failed grant advanced stage")
	}
	c.MissionInventory = c.MissionInventory[1:]
	if _, err := rt.AdvanceNpcQuest(c, "QTUTORIAL_CH@1", "NPC_CH_ARMOR"); err != nil {
		t.Fatal(err)
	}
}
