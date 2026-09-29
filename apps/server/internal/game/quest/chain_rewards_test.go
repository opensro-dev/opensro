/*
===========================================================================

chain_rewards_test.go - quest handoff and reward transaction regressions

Exercise predecessor gates, delivery identity, inventory rollback and repeat
exchanges through the runtime used by NPC and reward-window requests.

===========================================================================
*/
package quest

import (
	"errors"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/licensed"
	"reflect"
	"testing"
)

/*
================
TestBanditChainCannotSkipPredecessorAndPaysEachRewardOnce
================
*/
func TestBanditChainCannotSkipPredecessorAndPaysEachRewardOnce(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	exp := int64(0)
	rt.ApplyExperience = func(_ *enterworld.Character, e, s int64, _ uint32) ([]wire.Frame, bool) {
		exp += e
		return nil, true
	}
	if rows := rt.OptionsForNpc(c, "NPC_CH_GENARAL_SP"); len(rows) != 0 {
		t.Fatal("successor offered before predecessor")
	}
	if _, err := rt.StartQuest(c, "QNO_CH_GENARAL_SP_1"); err == nil {
		t.Fatal("direct acceptance bypassed chain")
	}
	for _, q := range []struct {
		code, npc, mob string
		experience     int64
		item           uint32
	}{
		{"QNO_CH_GENARAL_BO_1", "NPC_CH_GENARAL_BO", "MOB_CH_BANDITARCHER", 9500, 3631},
		{"QNO_CH_GENARAL_SP_1", "NPC_CH_GENARAL_SP", "MOB_CH_BANDIT", 24000, 3630},
	} {
		if _, err := rt.StartQuest(c, q.code); err != nil {
			t.Fatal(err)
		}
		for n := 0; n < 49; n++ {
			rt.KillUpdater()(c, q.mob, 0)
		}
		if _, err := rt.AdvanceNpcQuest(c, q.code, q.npc); err == nil {
			t.Fatal("early completion")
		}
		rt.KillUpdater()(c, "MOB_CH_WATERGHOST", 0)
		if _, err := rt.AdvanceNpcQuest(c, q.code, q.npc); err == nil {
			t.Fatal("wrong target counted")
		}
		rt.KillUpdater()(c, q.mob, 0)
		if _, err := rt.AdvanceNpcQuest(c, q.code, "WRONG_NPC"); err == nil {
			t.Fatal("wrong NPC paid reward")
		}
		result, err := rt.AdvanceNpcQuest(c, q.code, q.npc)
		if err != nil {
			t.Fatal(err)
		}
		if exp != q.experience || len(result.Frames) < 3 || result.Frames[0].Opcode != 14 {
			t.Fatalf("bad reward %d / %v", exp, result)
		}
		count := int64(0)
		for _, row := range c.MissionInventory {
			if row.RefObjID == q.item {
				count += row.StackCount
			}
		}
		if count != 50 {
			t.Fatalf("reward quantity=%d", count)
		}
		if _, err := rt.AdvanceNpcQuest(c, q.code, q.npc); err == nil {
			t.Fatal("duplicate reward")
		}
	}
	if len(c.ActiveQuests) != 0 || len(c.CompletedQuestIds) != 2 {
		t.Fatal("chain did not finish")
	}
}

/*
================
TestRewardFullBagLeavesObjectiveAndScalarsAvailableForRetry
================
*/
func TestRewardFullBagLeavesObjectiveAndScalarsAvailableForRetry(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	def, _ := rt.Defs.ByRefID(6)
	c.ActiveQuests = []enterworld.ActiveQuestRecord{BuildActiveQuestRecord(def, 50)}
	for n := 13; n < 45; n++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(n), RefObjID: 3631, Codename: "ITEM_ETC_MP_POTION_01", StackCount: 50, TypeFlags: wire.PackTypeFlags(3, 3, 1, 2)})
	}
	before := append([]enterworld.InventoryRow(nil), c.MissionInventory...)
	paid := 0
	rt.ApplyExperience = func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) {
		paid++
		return nil, true
	}
	result, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename)
	var localized interface {
		error
		DialogueSymbol() string
	}
	if !errors.As(err, &localized) || localized.DialogueSymbol() != def.InventoryFullSymbol || len(result.Frames) != 0 {
		t.Fatalf("no authored refusal: %v %v", result, err)
	}
	if paid != 0 || len(c.ActiveQuests) != 1 || len(c.CompletedQuestIds) != 0 || !reflect.DeepEqual(before, c.MissionInventory) {
		t.Fatal("refusal partially committed")
	}
	c.MissionInventory = c.MissionInventory[1:]
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
		t.Fatal(err)
	}
	if paid != 1 || len(c.ActiveQuests) != 0 {
		t.Fatal("retry did not commit exactly once")
	}
}

/*
================
TestDeliveryRequiresGuardVisitAndConsumesListAtBlacksmith
================
*/
func TestDeliveryRequiresGuardVisitAndConsumesListAtBlacksmith(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	code := "QNO_CH_SMITH_1"
	c.CompletedQuestIds = []uint32{2}
	if _, err := rt.StartQuest(c, code); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.AdvanceNpcQuest(c, code, "NPC_CH_SMITH"); err == nil {
		t.Fatal("skipped guard")
	}
	options := rt.OptionsForNpc(c, "NPC_CH_SOLDIER_SO1")
	if len(options) != 1 || !options[0].Complete {
		t.Fatalf("no delivery option: %v", options)
	}
	if _, err := rt.AdvanceNpcQuest(c, code, "NPC_CH_SOLDIER_SO1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.AdvanceNpcQuest(c, code, "NPC_CH_SOLDIER_SO1"); err == nil {
		t.Fatal("duplicated list")
	}
	if _, err := rt.AdvanceNpcQuest(c, code, "NPC_CH_SMITH"); err != nil {
		t.Fatal(err)
	}
	if len(c.MissionInventory) != 0 || c.Gold == nil || *c.Gold != 205 || len(c.CompletedQuestIds) != 2 {
		t.Fatalf("delivery reward tore: %+v", c)
	}
}

/*
================
TestChainValidationRejectsCyclesAndMissingParents
================
*/
func TestChainValidationRejectsCyclesAndMissingParents(t *testing.T) {
	licensed.RequireGameData(t)
	for _, parent := range []uint32{11, 9999} {
		defs := loadTestDefinitions(t)
		a, _ := defs.ByRefID(10)
		a.RequiredQuestIDs = []uint32{parent}
		if err := validateQuestChains(defs); err == nil {
			t.Fatal("invalid chain admitted")
		}
	}
}

/*
================
TestAbandonDeliveryRemovesItsItemBeforeReacceptance
================
*/
func TestAbandonDeliveryRemovesItsItemBeforeReacceptance(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	c.CompletedQuestIds = []uint32{2}
	if _, err := rt.StartQuest(c, "QNO_CH_SMITH_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.AdvanceNpcQuest(c, "QNO_CH_SMITH_1", "NPC_CH_SOLDIER_SO1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.HandleGiveUp(c, u32le(3)); err != nil {
		t.Fatal(err)
	}
	if len(c.MissionInventory) != 0 {
		t.Fatal("abandoned delivery left a reusable quest item")
	}
	if _, err := rt.StartQuest(c, "QNO_CH_SMITH_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.AdvanceNpcQuest(c, "QNO_CH_SMITH_1", "NPC_CH_SMITH"); err == nil {
		t.Fatal("abandon/reaccept skipped delivery NPC")
	}
}

/*
================
TestResuscitationExchangesHeartsNotPotionsAndCanRepeat

One completion exchanges both full groups; the remaining five hearts seed
the next acceptance without allowing a replay of the paid reward.
================
*/
func TestResuscitationExchangesHeartsNotPotionsAndCanRepeat(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 20, RefObjID: 3673, Codename: "ITEM_QSP_ALL_POTION_1_01", StackCount: 10, TypeFlags: wire.PackTypeFlags(3, 3, 9, 0)}}
	if _, err := rt.StartQuest(c, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}
	if recordProgress(c.ActiveQuests[0]) != 0 {
		t.Fatal("finished potions counted as hearts")
	}
	if _, err := rt.HandleRewardSelect(c, u32le(29)); err == nil {
		t.Fatal("exchanged potions for themselves")
	}
	c.MissionInventory = append(c.MissionInventory, potionInventory(25)[0])
	c.MissionInventory[1].Slot = 21
	rt.NotifyInventoryChanged(c)
	if _, err := rt.HandleRewardSelect(c, u32le(29)); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.HandleRewardSelect(c, u32le(29)); err == nil {
		t.Fatal("replayed a completed exchange")
	}
	if _, err := rt.StartQuest(c, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal("repeat acceptance refused", err)
	}
	hearts, potions := int64(0), int64(0)
	for _, row := range c.MissionInventory {
		if row.RefObjID == 3673 {
			potions += row.StackCount
		}
		if row.RefObjID == 3674 {
			hearts += row.StackCount
		}
	}
	if hearts != 5 || potions != 12 || len(c.CompletedQuestIds) != 1 {
		t.Fatalf("exchange lost or duplicated items/history: %d %d %v", hearts, potions, c.CompletedQuestIds)
	}
	if recordProgress(c.ActiveQuests[0]) != 5 {
		t.Fatal("repeated objective did not retain remaining hearts")
	}
}
