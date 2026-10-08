/*
===========================================================================

inventory_expansion_test.go - the six QSP_*_EXINVENTORY quests

They pay inventory slots (CBasicQuest_PayRewardRow 924CF0, byte +0x3db),
which wait for the next world entry. QSP_KT_EXINVENTORY_3 accepts either
line's second quest (list 0x114) and takes a 10,000 gold fee (8CF8F0).

===========================================================================
*/
package quest

import (
	"errors"
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
expansionRuntime

The shipped catalog with the action owner planning the inventory.
================
*/
func expansionRuntime(t *testing.T) *Runtime {
	t.Helper()
	defs, items := loadShippedDefinitions(t)
	rt, err := NewRuntime(&enterworld.Deps{Items: items}, defs, func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) {
		return nil, true
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	rt.PlanInventory = action.NewRuntime(&enterworld.Deps{Items: items}, nil).PlanQuestInventory
	return rt
}

/*
================
readyToTurnIn

A character holding the quest's collected items with the quest active.
================
*/
func readyToTurnIn(t *testing.T, rt *Runtime, code string, gold int64) (*enterworld.Character, *Definition) {
	t.Helper()
	def, ok := rt.Defs.ByCodename(code)
	if !ok {
		t.Fatalf("%s is not loaded", code)
	}
	level := int64(40)
	c := &enterworld.Character{ID: 7, Name: "expander", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level, Gold: &gold}
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot: 20, RefObjID: def.CollectItemRefID, Codename: def.CollectItemCodename,
		StackCount: int64(def.CollectCount), VarianceBits: "0",
	}}
	c.ActiveQuests = []enterworld.ActiveQuestRecord{BuildActiveQuestRecord(def, def.CollectCount)}
	return c, def
}

/*
================
TestExpansionQuestsPayTheAdvertisedSlots

The v1.150 popups advertise 10 slots for each line's first quest and 2 for
the rest (the v1.188 rows say 4); the reward waits for a world entry.
================
*/
func TestExpansionQuestsPayTheAdvertisedSlots(t *testing.T) {
	rt := expansionRuntime(t)
	for code, slots := range map[string]uint8{
		"QSP_CH_EXINVENTORY_1": 10, "QSP_WC_EXINVENTORY_2": 2, "QSP_KT_EXINVENTORY_3": 2,
		"QSP_RM_EXINVENTORY_4": 2, "QSP_EU_EXINVENTORY_1": 10, "QSP_CA_EXINVENTORY_2": 2,
	} {
		def, ok := rt.Defs.ByCodename(code)
		if !ok || def.RewardInventorySlots != slots {
			t.Fatalf("%s loaded %v with %d slots, want %d", code, ok, def.RewardInventorySlots, slots)
		}
	}

	c, def := readyToTurnIn(t, rt, "QSP_CH_EXINVENTORY_1", 0)
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
		t.Fatalf("turn-in: %v", err)
	}
	if len(c.ActiveQuests) != 0 || !questCompleted(c, def.RefID) {
		t.Fatal("the quest did not complete")
	}
	if c.InventoryCapacity() != 45 || c.InventoryExpansion != 10 {
		t.Fatalf("capacity %d (+%d), want 45 (+10) until the next world entry", c.InventoryCapacity(), c.InventoryExpansion)
	}
}

/*
================
TestExpansionRewardPastTheLimitStillCompletes

4E19D0 refuses slots past the limit and its caller ignores the refusal.
================
*/
func TestExpansionRewardPastTheLimitStillCompletes(t *testing.T) {
	rt := expansionRuntime(t)
	c, def := readyToTurnIn(t, rt, "QSP_CH_EXINVENTORY_1", 0)
	c.InventorySize = 70
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
		t.Fatalf("turn-in: %v", err)
	}
	if !questCompleted(c, def.RefID) || c.InventorySize != 70 || c.InventoryExpansion != 0 {
		t.Fatalf("completed %v, capacity %d (+%d)", questCompleted(c, def.RefID), c.InventorySize, c.InventoryExpansion)
	}
}

/*
================
TestHotanExpansionTakesItsFee

A character short of 10,000 gold hears the shortage line and keeps the
quest and its items; one who can pay completes it and pays after the reward.
================
*/
func TestHotanExpansionTakesItsFee(t *testing.T) {
	rt := expansionRuntime(t)
	c, def := readyToTurnIn(t, rt, "QSP_KT_EXINVENTORY_3", 9_999)
	_, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename)
	var localized interface {
		error
		DialogueSymbol() string
	}
	if !errors.As(err, &localized) || localized.DialogueSymbol() != "SN_TALK_QSP_KT_EXINVENTORY_3_05" {
		t.Fatalf("short of the fee: %v", err)
	}
	if len(c.ActiveQuests) != 1 || *c.Gold != 9_999 || c.MissionInventory[0].StackCount != int64(def.CollectCount) || c.InventoryExpansion != 0 {
		t.Fatal("a refused turn-in changed the character")
	}

	*c.Gold = 15_000
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
		t.Fatalf("turn-in: %v", err)
	}
	if !questCompleted(c, def.RefID) || *c.Gold != 5_000 || c.InventoryExpansion != 2 {
		t.Fatalf("completed %v, gold %d, waiting %d", questCompleted(c, def.RefID), *c.Gold, c.InventoryExpansion)
	}
}

/*
================
TestHotanExpansionFollowsEitherLine

CBasicQuest_MeetsPrerequisites (9262A0): one quest of list 0x114 suffices.
================
*/
func TestHotanExpansionFollowsEitherLine(t *testing.T) {
	rt := expansionRuntime(t)
	def, _ := rt.Defs.ByCodename("QSP_KT_EXINVENTORY_3")
	c := &enterworld.Character{}
	if prerequisitesMet(c, def) {
		t.Fatal("offered without either second quest")
	}
	for _, code := range []string{"QSP_WC_EXINVENTORY_2", "QSP_CA_EXINVENTORY_2"} {
		line, _ := rt.Defs.ByCodename(code)
		c.CompletedQuestIds = []uint32{line.RefID}
		if !prerequisitesMet(c, def) {
			t.Fatalf("not offered after %s alone", code)
		}
	}
}
