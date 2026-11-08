/*
===========================================================================

inventory_expansion_test.go - the six QSP_*_EXINVENTORY quests

They pay inventory slots (the server's reward-row payment), which grow the
bag at the turn-in itself and are announced with 0x3092. QSP_KT_EXINVENTORY_3
accepts either line's second quest and takes a 10,000 gold fee at its talk.

===========================================================================
*/
package quest

import (
	"bytes"
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
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
the rest; the paid slots are in force as soon as the turn-in completes.
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
	if c.InventoryCapacity() != 55 || c.InventoryExpansion != 0 {
		t.Fatalf("capacity %d (+%d), want 55 (+0) at the turn-in", c.InventoryCapacity(), c.InventoryExpansion)
	}
}

/*
================
TestExpansionRewardPastTheLimitStillCompletes

The server's slot grant refuses slots past the limit and its caller ignores the refusal.
================
*/
func TestExpansionRewardPastTheLimitStillCompletes(t *testing.T) {
	rt := expansionRuntime(t)
	c, def := readyToTurnIn(t, rt, "QSP_CH_EXINVENTORY_1", 0)
	c.InventorySize = 105
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
		t.Fatalf("turn-in: %v", err)
	}
	if !questCompleted(c, def.RefID) || c.InventorySize != 105 || c.InventoryExpansion != 0 {
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
	if !questCompleted(c, def.RefID) || *c.Gold != 5_000 || c.InventoryCapacity() != 47 || c.InventoryExpansion != 0 {
		t.Fatalf("completed %v, gold %d, capacity %d (+%d)", questCompleted(c, def.RefID), *c.Gold, c.InventoryCapacity(), c.InventoryExpansion)
	}
}

/*
================
TestHotanExpansionFollowsEitherLine

The server's quest prerequisite check: one quest of list 0x114 suffices.
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

/*
================
turnInFrames

Turns QSP_CH_EXINVENTORY_1 in on a plain runtime and returns the character
and every 0x3092 frame the turn-in sent.
================
*/
func turnInFrames(t *testing.T) (*enterworld.Character, []wire.Frame) {
	t.Helper()
	rt := expansionRuntime(t)
	c, def := readyToTurnIn(t, rt, "QSP_CH_EXINVENTORY_1", 0)
	result, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename)
	if err != nil {
		t.Fatalf("turn-in: %v", err)
	}
	var announced []wire.Frame
	for _, frame := range result.Frames {
		if frame.Opcode == OpStorageCapacity {
			announced = append(announced, frame)
		}
	}
	return c, announced
}

/*
================
TestExpansionAppliesLiveAtTurnIn

The paid slots are usable in the same session, with no world entry between
the turn-in and the growth, and one 0x3092 [1][capacity] tells the client.
On the pre-port rule this turn-in left the capacity at 45 (+10 waiting)
and announced nothing.
================
*/
func TestExpansionAppliesLiveAtTurnIn(t *testing.T) {
	c, announced := turnInFrames(t)
	if c.InventoryCapacity() != 55 || c.InventoryExpansion != 0 {
		t.Fatalf("capacity %d (+%d), want 55 (+0) at the turn-in", c.InventoryCapacity(), c.InventoryExpansion)
	}
	if len(announced) != 1 || !bytes.Equal(announced[0].Payload, []byte{1, 55}) {
		t.Fatalf("0x3092 frames = %v, want one [01 37]", announced)
	}
}

/*
================
expansionOffer

The offer row for one codename in an NPC's options, if the NPC offers it.
================
*/
func expansionOffer(options []NpcOption, codename string) (NpcOption, bool) {
	for _, option := range options {
		if option.Codename == codename && !option.Complete {
			return option, true
		}
	}
	return NpcOption{}, false
}

/*
================
TestExpansionChainOfferedHuntedAndTurnedIn

The first chain end to end on the shipped catalog: the Jangan accessory
dealer offers QSP_CH_EXINVENTORY_1 at level 5, Mangnyang kills drop its
collect item until the held cap, and the talk turn-in pays the 10 slots
live with one 0x3092.
================
*/
func TestExpansionChainOfferedHuntedAndTurnedIn(t *testing.T) {
	rt := expansionRuntime(t)
	race, level, gold := int64(enterworld.RaceChina), int64(5), int64(0)
	c := &enterworld.Character{ID: 8, Name: "chainwalker", ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex: &race, Level: &level, Gold: &gold}

	offer, offered := expansionOffer(rt.OptionsForNpc(c, "NPC_CH_ACCESSORY"), "QSP_CH_EXINVENTORY_1")
	if !offered || offer.PromptSymbol != "SN_TALK_QSP_CH_EXINVENTORY_1_01" {
		t.Fatalf("Jangan offer = %+v, %v", offer, offered)
	}
	if _, err := rt.StartQuest(c, "QSP_CH_EXINVENTORY_1"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if len(c.ActiveQuests) != 1 {
		t.Fatalf("active quests = %+v", c.ActiveQuests)
	}

	roll := func() (uint32, error) { return 1, nil }
	if drops := rt.MonsterDrops(c, "MOB_CH_GYO", 0, roll); len(drops) != 0 {
		t.Fatalf("a non-target monster dropped: %v", drops)
	}
	drops := rt.MonsterDrops(c, "MOB_CH_MANGNYANG", 0, roll)
	if len(drops) != 1 || drops[0] != (inventory.ItemAmount{Codename: "ITEM_QSP_CH_EXINVENTORY_1_01", Count: 1}) {
		t.Fatalf("Mangnyang drops = %v", drops)
	}

	def, _ := rt.Defs.ByCodename("QSP_CH_EXINVENTORY_1")
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot: 20, RefObjID: def.CollectItemRefID, Codename: def.CollectItemCodename,
		StackCount: int64(def.CollectCount), VarianceBits: "0",
	}}
	if drops := rt.MonsterDrops(c, "MOB_CH_MANGNYANG", 0, roll); len(drops) != 0 {
		t.Fatalf("drops past the held cap: %v", drops)
	}

	result, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename)
	if err != nil {
		t.Fatalf("turn-in: %v", err)
	}
	if !questCompleted(c, def.RefID) || c.InventoryCapacity() != 55 {
		t.Fatalf("completed %v, capacity %d, want true and 55", questCompleted(c, def.RefID), c.InventoryCapacity())
	}
	announced := 0
	for _, frame := range result.Frames {
		if frame.Opcode == OpStorageCapacity {
			announced++
			if !bytes.Equal(frame.Payload, []byte{1, 55}) {
				t.Fatalf("0x3092 payload = % X, want [01 37]", frame.Payload)
			}
		}
	}
	if announced != 1 {
		t.Fatalf("0x3092 frames = %d, want 1", announced)
	}
}

/*
================
TestExpansionChainGatesOffers

The chain's gates on the shipped catalog: the first quest waits for
level 5, the West-China sequel waits for the first quest, and each
country's dealer offers only its own line's first quest.
================
*/
func TestExpansionChainGatesOffers(t *testing.T) {
	rt := expansionRuntime(t)
	race, level, gold := int64(enterworld.RaceChina), int64(4), int64(0)
	young := &enterworld.Character{ID: 9, Name: "tooyoung", ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex: &race, Level: &level, Gold: &gold}
	if _, offered := expansionOffer(rt.OptionsForNpc(young, "NPC_CH_ACCESSORY"), "QSP_CH_EXINVENTORY_1"); offered {
		t.Fatal("the first expansion quest was offered at level 4")
	}
	if _, err := rt.StartQuest(young, "QSP_CH_EXINVENTORY_1"); err == nil {
		t.Fatal("the first expansion quest was accepted at level 4")
	}

	level = 30
	traveller := &enterworld.Character{ID: 10, Name: "westbound", ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex: &race, Level: &level, Gold: &gold}
	if _, offered := expansionOffer(rt.OptionsForNpc(traveller, "NPC_WC_ACCESSORY"), "QSP_WC_EXINVENTORY_2"); offered {
		t.Fatal("the sequel was offered before the first quest")
	}
	first, _ := rt.Defs.ByCodename("QSP_CH_EXINVENTORY_1")
	traveller.CompletedQuestIds = []uint32{first.RefID}
	if _, offered := expansionOffer(rt.OptionsForNpc(traveller, "NPC_WC_ACCESSORY"), "QSP_WC_EXINVENTORY_2"); !offered {
		t.Fatal("the sequel stayed closed after the first quest")
	}

	euRace, euLevel := int64(enterworld.RaceEurope), int64(5)
	european := &enterworld.Character{ID: 11, Name: "constantin", ModelCodename: "CHAR_EU_MAN_ADVENTURER",
		RaceIndex: &euRace, Level: &euLevel, Gold: &gold}
	if _, offered := expansionOffer(rt.OptionsForNpc(european, "NPC_EU_ACCESSORY"), "QSP_EU_EXINVENTORY_1"); !offered {
		t.Fatal("the European first quest was not offered")
	}
	if _, offered := expansionOffer(rt.OptionsForNpc(european, "NPC_CH_ACCESSORY"), "QSP_CH_EXINVENTORY_1"); offered {
		t.Fatal("the Chinese first quest was offered to a European")
	}
}

/*
================
TestExpansionPastTheLimitAnnouncesNothing

A refused grant changes no capacity, so no 0x3092 follows it.
================
*/
func TestExpansionPastTheLimitAnnouncesNothing(t *testing.T) {
	rt := expansionRuntime(t)
	c, def := readyToTurnIn(t, rt, "QSP_CH_EXINVENTORY_1", 0)
	c.InventorySize = 105
	result, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename)
	if err != nil {
		t.Fatalf("turn-in: %v", err)
	}
	for _, frame := range result.Frames {
		if frame.Opcode == OpStorageCapacity {
			t.Fatalf("a refused grant announced % X", frame.Payload)
		}
	}
	if c.InventorySize != 105 || c.InventoryExpansion != 0 {
		t.Fatalf("capacity %d (+%d), want 105 (+0)", c.InventorySize, c.InventoryExpansion)
	}
}

/*
================
TestExpansionChainFromThePagesBaseReachesThreePages

Under SRO_INVENTORY_PAGES the chain still pays its own slots: from the
2.5-page base the four grants land exactly on the three-page maximum,
each announced live.
================
*/
func TestExpansionChainFromThePagesBaseReachesThreePages(t *testing.T) {
	rt := expansionRuntime(t)
	size := domain.InventoryPagesBase
	var completed []uint32
	for _, code := range []string{"QSP_CH_EXINVENTORY_1", "QSP_WC_EXINVENTORY_2", "QSP_KT_EXINVENTORY_3", "QSP_RM_EXINVENTORY_4"} {
		c, def := readyToTurnIn(t, rt, code, 15_000)
		c.InventorySize = size
		c.CompletedQuestIds = completed
		result, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename)
		if err != nil {
			t.Fatalf("%s turn-in: %v", code, err)
		}
		size += def.RewardInventorySlots
		if c.InventoryCapacity() != size {
			t.Fatalf("after %s capacity = %d, want %d", code, c.InventoryCapacity(), size)
		}
		announced := false
		for _, frame := range result.Frames {
			if frame.Opcode == OpStorageCapacity && bytes.Equal(frame.Payload, []byte{1, size}) {
				announced = true
			}
		}
		if !announced {
			t.Fatalf("after %s no 0x3092 announced %d", code, size)
		}
		completed = append(completed, def.RefID)
	}
	if size != domain.MaxInventorySize {
		t.Fatalf("chain reached %d, want the %d maximum", size, domain.MaxInventorySize)
	}
}
