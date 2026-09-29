/*
===========================================================================

resuscitation_test.go - herbalist window admission without quest mutation

Opening the window is a read of authoritative inventory. Both successful and
missing-potion responses leave quests, stacks, gold and learned ranks intact.

===========================================================================
*/
package quest

import (
	"bytes"
	"encoding/json"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestResuscitationHerbalistsDoNotRequireAnActiveQuest
================
*/
func TestResuscitationHerbalistsDoNotRequireAnActiveQuest(t *testing.T) {
	rt := &Runtime{deps: &enterworld.Deps{}}
	c := questCharacter()
	for _, npc := range []string{"NPC_CH_POTION", "NPC_WC_POTION", "NPC_KT_POTION", "NPC_EU_POTION", "NPC_CA_POTION"} {
		if !ResuscitationAvailable(c, npc) {
			t.Fatal("missing native herbalist", npc)
		}
		for _, quantity := range []int64{0, 1} {
			c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, Codename: "ITEM_QSP_ALL_POTION_1_01", StackCount: quantity}}
			before, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			result, err := rt.OpenResuscitation(c, npc)
			if err != nil || len(result.Frames) != 1 {
				t.Fatal(npc, quantity, result, err)
			}
			want := uint16(resuscitationOpenOpcode)
			if quantity == 0 {
				want = wire.OpNpcDialog
			}
			if result.Frames[0].Opcode != want || quantity > 0 && len(result.Frames[0].Payload) != 0 {
				t.Fatal(result)
			}
			after, err := json.Marshal(c)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("opening withdrawal mutated character", err)
			}
		}
	}
	if ResuscitationAvailable(c, "NPC_CH_SMITH") {
		t.Fatal("unrelated NPC offered restoration")
	}
	*c.Level = 19
	if _, err := rt.OpenResuscitation(c, "NPC_CH_POTION"); err == nil {
		t.Fatal("under-level service accepted")
	}
}

/*
================
TestResuscitationBatchCapsPotionsAndPreservesUnspentHearts

The cap counts every bag stack. A refused exchange must leave both the
inventory and the active journal record available for a later retry.
================
*/
func TestResuscitationBatchCapsPotionsAndPreservesUnspentHearts(t *testing.T) {
	licensed.RequireGameData(t)
	for _, held := range []int64{0, 28, 30, 31} {
		rt := testRuntime(t)
		c := questCharacter()
		c.MissionInventory = potionInventory(300)
		if held > 0 {
			c.MissionInventory = append(c.MissionInventory,
				enterworld.InventoryRow{Slot: 21, RefObjID: 3673, Codename: resuscitationPotion, StackCount: held - 1, TypeFlags: wire.PackTypeFlags(3, 3, 9, 0)},
				enterworld.InventoryRow{Slot: 22, RefObjID: 3673, Codename: resuscitationPotion, StackCount: 1, TypeFlags: wire.PackTypeFlags(3, 3, 9, 0)})
		}
		if _, err := rt.StartQuest(c, resuscitationQuest); err != nil {
			t.Fatal(err)
		}
		before, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		_, err = rt.HandleRewardSelect(c, u32le(29))
		if held >= resuscitationPotionLimit {
			after, marshalErr := json.Marshal(c)
			if err == nil || marshalErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("cap refusal changed character: held=%d err=%v marshal=%v", held, err, marshalErr)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var hearts, potions int64
		for _, row := range c.MissionInventory {
			if row.RefObjID == 3674 {
				hearts += row.StackCount
			}
			if row.RefObjID == 3673 {
				potions += row.StackCount
			}
		}
		if potions != resuscitationPotionLimit || hearts != held*10 {
			t.Fatalf("held=%d produced potions=%d hearts=%d", held, potions, hearts)
		}
	}
}

/*
================
TestResuscitationCollectionUsesEveryHerbalist

Acceptance and turn-in use the same merchant set as the withdrawal service.
================
*/
func TestResuscitationCollectionUsesEveryHerbalist(t *testing.T) {
	licensed.RequireGameData(t)
	for _, npc := range []string{"NPC_CH_POTION", "NPC_WC_POTION", "NPC_KT_POTION", "NPC_EU_POTION", "NPC_CA_POTION"} {
		rt := testRuntime(t)
		c := questCharacter()
		found := false
		for _, option := range rt.OptionsForNpc(c, npc) {
			found = found || option.Codename == resuscitationQuest && !option.Complete
		}
		if !found {
			t.Fatal("collection offer missing", npc)
		}
		if _, err := rt.StartQuest(c, resuscitationQuest); err != nil {
			t.Fatal(err)
		}
		c.MissionInventory = potionInventory(20)
		found = false
		for _, option := range rt.OptionsForNpc(c, npc) {
			found = found || option.Codename == resuscitationQuest && option.Complete
		}
		if !found {
			t.Fatal("collection completion missing", npc)
		}
		if _, err := rt.AdvanceNpcQuest(c, resuscitationQuest, "NPC_CH_SMITH"); err == nil {
			t.Fatal("unrelated merchant completed collection")
		}
		if _, err := rt.AdvanceNpcQuest(c, resuscitationQuest, npc); err != nil {
			t.Fatal(npc, err)
		}
	}
}
