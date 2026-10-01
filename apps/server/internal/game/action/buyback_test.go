package action

import (
	"encoding/json"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"reflect"
	"testing"
)

func buyback(t *testing.T, rt *Runtime, c *enterworld.Character, npc, id uint32) OpResult {
	t.Helper()
	return rt.HandleBuyback(testDivision, c, wire.NewWriter(9).U8(1).U32(npc).U32(id).Payload())
}
func TestShopEquipmentCompoundAndBuyback(t *testing.T) {
	rt, c := merchantFixture(t)
	gear := testItems()["ITEM_CH_SWORD_01_A_RARE"]
	// The fixture's reference map belongs to the authority; use its canonical record.
	gear, _ = rt.deps.ItemReferences().ItemRefByCodename(gear.Codename)
	gear.NativeFields = enterworld.NewNativeFields(map[string]float64{"sellPrice": 40})
	gear.NativeFields = gear.NativeFields.With("sellPrice", 40)
	gear.NativeFields = gear.NativeFields.With("canSell", 1)
	rt.Commerce.Magic = emptyBuybackMagic{}
	potion := rt.Commerce.Tabs[1][0].Ref
	rt.Commerce.Tabs[1] = append(rt.Commerce.Tabs[1], commerce.Offer{Slot: 3, Ref: gear, Price: 100, Stack: 1, Contents: []commerce.Content{{Ref: gear, Stack: 1, Plus: 7, Variance: 0x8123456789abcdef, Data: 99, Magic: []uint64{25769804010}}, {Ref: potion, Stack: 50, Data: 3}}})
	r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 3, Quantity: 2})
	if len(r.Frames) != 4 || goldOf(c) != 4800 || len(c.MissionInventory) != 4 {
		t.Fatalf("package: %+v %+v", r, c.MissionInventory)
	}
	var sold enterworld.InventoryRow
	for _, i := range c.MissionInventory {
		if i.Slot == 13 {
			sold = i
		}
	}
	if sold.Plus != 7 || sold.VarianceBits != "9305357566071262703" || sold.Durability != 99 || len(sold.MagicOptions) != 1 {
		t.Fatalf("lost template: %+v", sold)
	}
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: 13, Quantity: 1})
	if len(c.Buyback) != 1 || goldOf(c) != 4840 {
		t.Fatal("sale not retained")
	}
	// A fresh character lifetime cannot reload sold objects from persistence.
	b, _ := json.Marshal(c)
	var restored enterworld.Character
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Buyback) != 0 {
		t.Fatal("buyback survived persisted character reload")
	}
	snap := c.Snapshot()
	c.Buyback[0].Item.MagicOptions[0] = 12
	if snap.Buyback[0].Item.MagicOptions[0] != 25769804010 {
		t.Fatal("snapshot aliases buyback")
	}
	c.Buyback = snap.Buyback
	result := buyback(t, rt, c, 17, c.Buyback[0].ID)
	if len(result.Frames) != 3 || goldOf(c) != 4800 || len(c.Buyback) != 0 {
		t.Fatalf("buyback failed %+v", result)
	}
	var received enterworld.InventoryRow
	for _, i := range c.MissionInventory {
		if i.Slot == 13 {
			received = i
		}
	}
	if !reflect.DeepEqual(sold, received) {
		t.Fatalf("restored %+v want %+v", received, sold)
	}
	before := c.Snapshot()
	r = buyback(t, rt, c, 17, 1)
	if !reflect.DeepEqual(c.Snapshot(), before) || len(r.Frames) != 1 {
		t.Fatal("replayed buyback mutated character")
	}
}
func TestShopPackageFullBagRollsBackAllContents(t *testing.T) {
	rt, c := merchantFixture(t)
	ref := rt.Commerce.Tabs[1][0].Ref
	// First content can fit; the second cannot. Neither may be committed.
	for slot := int64(14); slot < 109; slot++ {
		if slot == 20 {
			continue
		}
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE", TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), StackCount: 1})
	}
	rt.Commerce.Tabs[1][0].Contents = []commerce.Content{{Ref: ref, Stack: 50, Data: 50}, {Ref: ref, Stack: 50, Data: 50}}
	before := c.Snapshot()
	r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 1})
	if r.Frames[0].Payload[0] != 2 || !reflect.DeepEqual(c.Snapshot(), before) {
		t.Fatal("partial package committed")
	}
}
func TestBuybackRefusalsPreserveLedgerAndInventory(t *testing.T) {
	for _, kind := range []string{"funds", "full", "merchant", "stale", "distance"} {
		t.Run(kind, func(t *testing.T) {
			rt, c := merchantFixture(t)
			trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 1})
			trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: 13, Quantity: 1})
			npc, id := uint32(17), c.Buyback[0].ID
			switch kind {
			case "funds":
				setGold(c, 0)
			case "merchant":
				npc = 18
			case "stale":
				id++
			case "distance":
				rt.NpcRoster[0].Spawn.X += 1000
			case "full":
				for slot := int64(13); slot < 109; slot++ {
					if slot != 20 {
						c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE", TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), StackCount: 1})
					}
				}
			}
			before := c.Snapshot()
			r := buyback(t, rt, c, npc, id)
			if len(r.Frames) != 1 || !reflect.DeepEqual(c.Snapshot(), before) {
				t.Fatalf("mutated on refusal: %+v", r)
			}
		})
	}
}

// No matching definition is skipped by native 78B527.
type emptyBuybackMagic struct{}

func (emptyBuybackMagic) MagicOptionByParamID(uint32) (*enterworld.MagicOptionRow, bool) {
	return nil, false
}

func TestBuybackIsCharacterWideAcrossAdmittedMerchants(t *testing.T) {
	rt, c := merchantFixture(t)
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 1})
	trade(t, rt, c, wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: 13, Quantity: 1})
	other := rt.NpcRoster[0]
	other.ObjectID = 18
	other.RefObjID = 101
	rt.NpcRoster = append(rt.NpcRoster, other)
	rt.Selected.Set(testDivision, c.Name, 18)
	rt.Selected.OpenFunction(testDivision, c.Name, 18)
	if len(rt.buybackOffers(c, 101)) != 1 {
		t.Fatal("merchant switch hides retained sale")
	}
	if r := rt.HandleRetailBuyback(testDivision, c, []byte{18, 0, 0, 0, 0}); len(r.Frames) != 5 || len(c.Buyback) != 0 {
		t.Fatalf("other admitted merchant cannot restore: %+v", r)
	}
}
