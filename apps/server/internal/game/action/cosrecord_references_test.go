/*
===========================================================================

cosrecord_references_test.go - a summoned pet's bag is announced first

#bug-reports (2026-10-11): a pickup pet taken out of storage and summoned
ended the session with "Unknown inventory reference" on 0x3158. The record
lists the pet's bag, and its items were never announced to this client.

===========================================================================
*/
package action

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestSummonedPetAnnouncesItsBagBeforeTheRecord
================
*/
func TestSummonedPetAnnouncesItsBagBeforeTheRecord(t *testing.T) {
	items := shippedItems(t)
	summoner, ok := items.ItemRefByCodename("ITEM_COS_P_RABBIT_SCROLL")
	potion, potionOK := items.ItemRefByCodename("ITEM_ETC_HP_POTION_01")
	if !ok || !potionOK {
		t.Fatal("missing shipped items")
	}
	c := testCharacter()
	level := int64(5)
	c.Level = &level
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 23, RefObjID: summoner.RefObjID, Codename: summoner.Codename, TypeFlags: summoner.TypeFlags(), StackCount: 1}}
	rt, _ := newTestRuntime(c, items)
	useSummonerFixture(t, rt, c, 23, summoner)
	useSummonerFixture(t, rt, c, 23, summoner)
	pet := c.MissionInventory[0].Summon
	if pet == nil || pet.Summoned || pet.Container == nil {
		t.Fatal("expected a dismissed pickup pet with a bag")
	}
	// What a pet withdrawn from storage brings: items this client never saw.
	pet.Container.Rows = append(pet.Container.Rows, enterworld.InventoryRow{
		Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7,
	})
	result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(23).U16(summoner.TypeFlags()).Payload())
	announced, record := -1, -1
	for i, frame := range result.Frames {
		switch frame.Opcode {
		case opCommerceItemReferences:
			var delta struct {
				Items []struct {
					ID uint32 `json:"refObjId"`
				} `json:"items"`
			}
			if json.Unmarshal(frame.Payload, &delta) == nil {
				for _, row := range delta.Items {
					if row.ID == potion.RefObjID && announced < 0 {
						announced = i
					}
				}
			}
		case wire.OpCosRecordCreate:
			record = i
		}
	}
	if record < 0 || announced < 0 || announced > record {
		t.Fatalf("potion reference at frame %d, 0x3158 record at %d: the bag must be announced first", announced, record)
	}
}
