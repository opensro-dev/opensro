/*
===========================================================================

operator_grant_test.go - atomic grants, ordinary receipts and durable inventory

===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestOperatorGrantItemsPersistsAndPublishes
================
*/
func TestOperatorGrantItemsPersistsAndPublishes(t *testing.T) {
	d := openDoorRuntime(t, t.TempDir(), testCharacter())
	deps := d.rt.deps.(*enterworld.Deps)
	deps.UpdateCharacter = d.authority.UpdateCharacter
	deps.ReadCharacter = func(_ string, read func()) { d.authority.ReadState(read) }
	before := d.character.Snapshot()
	var sent []wire.Frame
	d.rt.PushCharacterFrames = func(division, name string, frames []wire.Frame) {
		if division != testDivision || name != d.character.Name {
			t.Fatal("receipt sent to another character")
		}
		sent = append(sent, frames...)
	}
	grants := []inventory.ItemAmount{
		{Codename: "ITEM_CH_SWORD_01_A_RARE", Count: 1},
		{Codename: "ITEM_ETC_HP_POTION_01", Count: 78},
	}
	if err := d.rt.OperatorGrantItems(testDivision, d.character.Name, grants); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 4 || sent[0].Opcode != opCommerceItemReferences {
		t.Fatalf("expected metadata and three inventory receipts: %+v", sent)
	}
	for _, frame := range sent[1:] {
		if frame.Opcode != wire.OpItemMoveResponse || frame.Payload[0] != 1 || frame.Payload[1] != 14 {
			t.Fatalf("not an ordinary inventory grant: %+v", frame)
		}
	}
	if d.character.GMPrivilege != before.GMPrivilege || !reflect.DeepEqual(d.character.World, before.World) ||
		!reflect.DeepEqual(d.character.Gold, before.Gold) {
		t.Fatal("grant changed privileges, position or gold")
	}
	rows := d.character.Snapshot().MissionInventory
	if health := d.authority.Health(); health.FailedWrites != 0 {
		t.Fatalf("storage: %+v", health)
	}
	if got := d.reboot(t).character.MissionInventory; !reflect.DeepEqual(rows, got) {
		t.Fatal("inventory did not survive authority restart")
	}
}

/*
================
TestOperatorGrantItemsRefusesWholeBatch
================
*/
func TestOperatorGrantItemsRefusesWholeBatch(t *testing.T) {
	for _, mode := range []string{"unknown", "zero", "full", "deleted", "missing", "empty"} {
		t.Run(mode, func(t *testing.T) {
			c := testCharacter()
			rt, _ := newTestRuntime(c, testItems())
			grants := []inventory.ItemAmount{{Codename: "ITEM_CH_SWORD_01_A_RARE", Count: 1}}
			name := c.Name
			switch mode {
			case "unknown":
				grants = append(grants, inventory.ItemAmount{Codename: "UNKNOWN", Count: 1})
			case "zero":
				grants[0].Count = 0
			case "full":
				c.MissionInventory = nil
				for slot := inventory.EquipmentSlotEnd; slot < inventory.BagSlotEnd; slot++ {
					c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
						Slot: int64(slot), RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 50,
						TypeFlags: testItems()["ITEM_ETC_HP_POTION_01"].TypeFlags(),
					})
				}
			case "deleted":
				c.DeletePending = true
			case "missing":
				name = "missing"
			case "empty":
				grants = nil
			}
			before := c.Snapshot()
			rt.PushCharacterFrames = func(string, string, []wire.Frame) { t.Fatal("refused grant published") }
			if err := rt.OperatorGrantItems(testDivision, name, grants); err == nil {
				t.Fatal("invalid grant accepted")
			}
			if !reflect.DeepEqual(before, c.Snapshot()) {
				t.Fatal("refused grant changed character")
			}
		})
	}
}

/*
================
TestOperatorGrantItemsRefusesExchange
================
*/
func TestOperatorGrantItemsRefusesExchange(t *testing.T) {
	rt, a, b, pushed := exchangeFixture(t)
	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(b)).Payload())
	rt.ExchangeConsent().ApplyConsent(nil, testDivision, b, 1, 1)
	before := a.Snapshot()
	count := len(pushed[a.Name])
	if err := rt.OperatorGrantItems(testDivision, a.Name, []inventory.ItemAmount{{Codename: "ITEM_ETC_HP_POTION_01", Count: 1}}); err == nil {
		t.Fatal("exchange inventory changed")
	}
	if !reflect.DeepEqual(before, a.Snapshot()) || len(pushed[a.Name]) != count {
		t.Fatal("refused grant changed inventory or sent receipts")
	}
}

/*
================
TestOperatorGrantItemsRefusesStall
================
*/
func TestOperatorGrantItemsRefusesStall(t *testing.T) {
	rt, owner, _, _ := stallFixture(t)
	result := rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Inspection").Payload())
	if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
		t.Fatal("stall was not created")
	}
	before := owner.Snapshot()
	if err := rt.OperatorGrantItems(testDivision, owner.Name, []inventory.ItemAmount{{Codename: "ITEM_ETC_HP_POTION_01", Count: 1}}); err == nil {
		t.Fatal("stall inventory changed")
	}
	if !reflect.DeepEqual(before, owner.Snapshot()) {
		t.Fatal("refused grant changed character")
	}
}
