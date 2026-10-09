/*
===========================================================================

trade_special_test.go - the bandit traders' two favours (TRADE_*_SPECIAL)

Exercise the shipped QNO_TRADE_CH_SPECIAL2_1 definition: the dressed-trader
gate (9262A0 flag 0x1000), a delivery and a hunt in either order, the start
NPC's line naming whichever is left (8CB8A0), and the pay's own bag-full
line.

===========================================================================
*/
package quest

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
dressJob

Wears a suit of job in the job socket: item type 3/1/7 with type 4 naming
the job (enterworld.JobSuitJob).
================
*/
func dressJob(c *enterworld.Character, job uint8) {
	const suitTid1, suitTid2, suitTid3 = 3, 1, 7
	flags := uint16(suitTid1<<2 | suitTid2<<5 | suitTid3<<7 | uint16(job)<<11)
	c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: enterworld.JobSuitSlot,
		Codename: "ITEM_TEST_JOB_SUIT", TypeFlags: flags, StackCount: 1})
}

/*
================
fillBag

Fills every empty bag slot with potions, keeping what is held.
================
*/
func fillBag(c *enterworld.Character) {
	taken := map[int64]bool{}
	for _, row := range c.MissionInventory {
		taken[row.Slot] = true
	}
	for slot := int64(inventory.EquipmentSlotEnd); slot < int64(inventory.BagEnd(c)); slot++ {
		if !taken[slot] {
			c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: 3630,
				Codename: "ITEM_ETC_HP_POTION_01", StackCount: 50})
		}
	}
}

/*
================
TestTradeSpecialNeedsADressedTrader

The offer stands only while a trader's suit is worn; a thief's suit or
none refuses it.
================
*/
func TestTradeSpecialNeedsADressedTrader(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	def := mustQuest(t, rt, "QNO_TRADE_CH_SPECIAL2_1")
	level := int64(40)
	c := &enterworld.Character{ID: 9, Name: "trader", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level}
	if prerequisitesMet(c, def) {
		t.Fatal("the offer stood without a suit")
	}
	dressJob(c, domain.JobThief)
	if prerequisitesMet(c, def) {
		t.Fatal("the offer stood for a dressed thief")
	}
	c.MissionInventory = nil
	dressJob(c, domain.JobTrader)
	if !prerequisitesMet(c, def) {
		t.Fatal("a dressed trader was refused")
	}
	if !jobConditionMet(c, &JobCondition{Job: domain.JobThief}) || jobConditionMet(c, &JobCondition{Job: domain.JobTrader}) {
		t.Fatal("a cleared +0x21 must refuse exactly the named job")
	}
}

/*
================
TestTradeSpecialNamesWhatIsLeft

Seopok answers _06 with both favours open, _05 once the tigers are hunted
(the box is left), and the full bag at the pay with _08, not the
acceptance line _07. The box handed to Ahjin, he pays the camels.
================
*/
func TestTradeSpecialNamesWhatIsLeft(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	def := mustQuest(t, rt, "QNO_TRADE_CH_SPECIAL2_1")
	level, gold := int64(40), int64(0)
	c := &enterworld.Character{ID: 9, Name: "trader", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level, Gold: &gold}
	dressJob(c, domain.JobTrader)
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("accept: %v", err)
	}
	line := func() string {
		row, ok := npcRow(rt, c, def.Codename, def.StartNpcCodename)
		if !ok || !row.Informational {
			t.Fatalf("Seopok's row %+v", row)
		}
		return row.PromptSymbol
	}
	if got := line(); got != "SN_TALK_QNO_TRADE_CH_SPECIAL2_1_06" {
		t.Fatalf("both favours open: %s", got)
	}
	for i := 0; i < 20; i++ {
		rt.KillUpdater()(c, "MOB_CH_WHITETIGER", 0)
	}
	if got := line(); got != "SN_TALK_QNO_TRADE_CH_SPECIAL2_1_05" {
		t.Fatalf("the hunt done, the box left: %s", got)
	}
	if _, err := rt.AdvanceNpcQuest(c, handOverToken(def.Codename), "NPC_CH_KISAENG3"); err != nil {
		t.Fatalf("hand the box to Ahjin: %v", err)
	}
	// Without the confirmation the take-back frees no slot (it removes only
	// what is held), so the bag stays full for the camels.
	kept := c.MissionInventory[:0]
	for _, row := range c.MissionInventory {
		if row.Codename != "ITEM_QNO_TRADE_CH_SPECIAL2_1_02" {
			kept = append(kept, row)
		}
	}
	c.MissionInventory = kept
	held := len(c.MissionInventory)
	fillBag(c)
	_, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename)
	var refusal interface{ DialogueSymbol() string }
	if !errors.As(err, &refusal) || refusal.DialogueSymbol() != "SN_TALK_QNO_TRADE_CH_SPECIAL2_1_08" {
		t.Fatalf("a full bag at the pay answered %v", err)
	}
	c.MissionInventory = c.MissionInventory[:held]
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil || !questCompleted(c, def.RefID) {
		t.Fatalf("Seopok did not pay: %v", err)
	}
	if captureItemCount(c, "ITEM_COS_T_CAMEL1") != 5 {
		t.Fatal("the reward is not five camels", c.MissionInventory)
	}
}

/*
================
TestTradeSpecialNamesTheHuntWhenTheBoxIsDelivered

The other order: the box delivered first, Seopok answers _04.
================
*/
func TestTradeSpecialNamesTheHuntWhenTheBoxIsDelivered(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	def := mustQuest(t, rt, "QNO_TRADE_CH_SPECIAL2_1")
	level := int64(40)
	c := &enterworld.Character{ID: 9, Name: "trader", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level}
	dressJob(c, domain.JobTrader)
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, err := rt.AdvanceNpcQuest(c, handOverToken(def.Codename), "NPC_CH_KISAENG3"); err != nil {
		t.Fatalf("hand the box to Ahjin: %v", err)
	}
	row, ok := npcRow(rt, c, def.Codename, def.StartNpcCodename)
	if !ok || row.PromptSymbol != "SN_TALK_QNO_TRADE_CH_SPECIAL2_1_04" {
		t.Fatalf("the box delivered, the hunt left: %+v", row)
	}
}
