package action

import (
	"encoding/json"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"reflect"
	"testing"
)

func TestCOSShopUsesContainerAndSharedGoldTransaction(t *testing.T) {
	rt, c := merchantFixture(t)
	refs := testCosSource(rt.deps.ItemReferences().(staticItemSource))
	// Reuse the normal dependency implementation, preserving merchant admission.
	withCOS, _ := newTestRuntime(c, refs)
	withCOS.NpcRoster = rt.NpcRoster
	withCOS.NpcSpawn = rt.NpcSpawn
	withCOS.Commerce = rt.Commerce
	withCOS.Selected.Set(testDivision, c.Name, 17)
	withCOS.Selected.OpenFunction(testDivision, c.Name, 17)
	rt = withCOS
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 1, Rows: []domain.InventoryRow{}}}
	beforeBag := c.Snapshot().MissionInventory
	r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: 19, CosGID: gid, NpcGID: 17, ShopSlot: 2, Quantity: 3})
	if len(r.Frames) != 4 || !reflect.DeepEqual(r.Frames[2].Payload, wire.NewWriter(12).U8(1).U8(19).U32(gid).U8(0).U8(2).U8(1).U8(0).U16(3).Payload()) {
		t.Fatalf("COS purchase %v", r)
	}
	if len(c.ActiveCOS.Container.Rows) != 1 || c.ActiveCOS.Container.Rows[0].StackCount != 3 || goldOf(c) != 4820 || !reflect.DeepEqual(beforeBag, c.MissionInventory) {
		t.Fatal("purchase crossed inventory owners")
	}
	var snapshot map[string]interface{}
	if e := json.Unmarshal(r.Frames[1].Payload, &snapshot); e != nil || snapshot["cosGid"] != float64(gid) {
		t.Fatal("missing COS snapshot correlation")
	}
	before := c.Snapshot()
	r = trade(t, rt, c, wire.ItemMoveRequest{MovementType: 19, CosGID: gid, NpcGID: 17, ShopSlot: 2, Quantity: 100})
	if r.Frames[0].Payload[0] != 2 || !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("full COS bag partially charged")
	}
	r = trade(t, rt, c, wire.ItemMoveRequest{MovementType: 20, CosGID: gid, NpcGID: 17, SourceSlot: 0, Quantity: 1})
	if r.Frames[0].Payload[1] != 20 || c.ActiveCOS.Container.Rows[0].StackCount != 2 || goldOf(c) != 4835 || len(c.Buyback) != 1 || !reflect.DeepEqual(beforeBag, c.MissionInventory) {
		t.Fatalf("COS sale not atomic %+v", r)
	}
}

func TestCOSContainerTransferPersistsAndRestores(t *testing.T) {
	c := testCharacter()
	refs := testCosSource(testItems())
	rt, _ := newTestRuntime(c, refs)
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	row := enterworld.InventoryRow{Slot: 0, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 30, VarianceBits: "0"}
	other := row
	other.Slot = 1
	other.StackCount = 40
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{row, other}}}
	q := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosInventory, CosGID: gid, SourceSlot: 0, DestSlot: 1, Quantity: 1}
	p, e := q.Encode()
	if e != nil {
		t.Fatal(e)
	}
	r := rt.HandleItemMove(testDivision, c, p)
	if len(r.Frames) != 1 || !reflect.DeepEqual(r.Frames[0].Payload, append([]byte{1}, p...)) {
		t.Fatalf("bad result %+v", r)
	}
	if c.ActiveCOS.Container.Rows[0].StackCount != 20 || c.ActiveCOS.Container.Rows[1].StackCount != 50 {
		t.Fatal("native merge ignored cap")
	}
	snap := c.Snapshot()
	c.ActiveCOS.Container.Rows[0].StackCount = 9
	if snap.ActiveCOS.Container.Rows[0].StackCount != 20 {
		t.Fatal("snapshot aliases COS rows")
	}
	c.ActiveCOS = snap.ActiveCOS
	data, _ := json.Marshal(c)
	var restored enterworld.Character
	if e = json.Unmarshal(data, &restored); e != nil {
		t.Fatal(e)
	}
	record, e := enterworld.BuildCOSRecord(restored.ActiveCOS, refs.characters["COS_T_DHORSE3"], refs)
	if e != nil || len(record) != 36 || record[16] != 4 || record[17] != 2 {
		t.Fatalf("container restoration %x: %v", record, e)
	}
	before := c.Snapshot()
	for _, bad := range [][]byte{p[:8], append(append([]byte(nil), p...), 0), {16, 1, 0, 0, 0, 0, 1, 1, 0}, {16, 3, 0, 192, 0, 0, 4, 1, 0}} {
		rt.HandleItemMove(testDivision, c, bad)
		if !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatal("invalid command mutated COS")
		}
	}
}

func TestCommerceRejectsExpandedQuantityAndPriceOverflow(t *testing.T) {
	for _, mode := range []string{"quantity", "price"} {
		t.Run(mode, func(t *testing.T) {
			rt, c := merchantFixture(t)
			found := false
			for tab, offers := range rt.Commerce.Tabs {
				for i := range offers {
					if offers[i].Slot != 2 {
						continue
					}
					found = true
					if mode == "quantity" {
						// Four packages wrapped to four units with uint32 multiplication.
						offers[i].Contents = []commerce.Content{{Ref: offers[i].Ref, Stack: offers[i].Stack, Data: 0x40000001}}
					} else {
						// Four prices wrapped to zero with unchecked uint64 multiplication.
						offers[i].Price = uint64(1) << 62
					}
				}
				rt.Commerce.Tabs[tab] = offers
			}
			if !found {
				t.Fatal("fixture offer missing")
			}
			before := c.Snapshot()
			r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopSlot: 2, Quantity: 4})
			if len(r.Frames) != 1 || r.Frames[0].Payload[0] != 2 || !reflect.DeepEqual(before, c.Snapshot()) {
				t.Fatal("overflowing package mutated commerce")
			}
		})
	}
}
