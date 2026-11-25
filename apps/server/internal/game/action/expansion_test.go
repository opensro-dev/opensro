/*
===========================================================================

expansion_test.go - the later inventory and warehouse addition items

===========================================================================
*/

package action

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
expansionCharacter

A character holding three of the addition item its codename names.
================
*/
func expansionCharacter(t *testing.T, codename string, refObjID uint32, params map[string]float64) (*enterworld.Character, *Runtime, *enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	fields := map[string]float64{"canUse": 1, "maxStack": 10}
	for key, value := range params {
		fields[key] = value
	}
	item := &enterworld.ItemRef{RefObjID: refObjID, Codename: codename, Country: 3,
		TypeIDs: [4]int64{3, 3, 13, 17}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(fields)}
	items := testItems()
	items[item.Codename] = item
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 25, RefObjID: item.RefObjID,
		Codename: item.Codename, TypeFlags: item.TypeFlags(), StackCount: 3})
	rt, _ := newTestRuntime(c, items)
	return c, rt, item
}

/*
================
TestInventoryAdditionGrowsTheBagOnce

The addition grows the bag by its 32 slots, announced on 0x3092, and
the character's mark refuses a second one with nothing spent.
================
*/
func TestInventoryAdditionGrowsTheBagOnce(t *testing.T) {
	c, rt, item := expansionCharacter(t, "ITEM_MALL_INVENTORY_ADDITION", 24405, map[string]float64{
		"itemParam1_29c": 32})
	use := wire.NewWriter(3).U8(25).U16(item.TypeFlags()).Payload()
	out := rt.HandleItemUse(testDivision, c, use)
	if out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("addition = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if c.InventoryCapacity() != 77 || !c.InventoryAdditionUsed {
		t.Fatalf("capacity %d, used %v", c.InventoryCapacity(), c.InventoryAdditionUsed)
	}
	announced := false
	for _, frame := range out.Frames {
		if frame.Opcode == wire.OpStorageCapacity {
			announced = len(frame.Payload) == 2 && frame.Payload[0] == 1 && frame.Payload[1] == 77
		}
	}
	if !announced {
		t.Fatalf("no 0x3092 bag announce in %+v", out.Frames)
	}
	assertItemUseRefusedUnchanged(t, rt, c, use, wire.ErrCodeInvalidRequest)
}

/*
================
TestInventoryAdditionTopsUpAFlooredBag

Under the inventory-pages floor the bag already holds 93: the
addition tops it up to the 109 ceiling and still spends its one use.
================
*/
func TestInventoryAdditionTopsUpAFlooredBag(t *testing.T) {
	c, rt, item := expansionCharacter(t, "ITEM_MALL_INVENTORY_ADDITION", 24405, map[string]float64{
		"itemParam1_29c": 32})
	c.InventorySize = domain.InventoryPagesBase
	use := wire.NewWriter(3).U8(25).U16(item.TypeFlags()).Payload()
	if out := rt.HandleItemUse(testDivision, c, use); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("addition = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if c.InventoryCapacity() != domain.MaxInventorySize || !c.InventoryAdditionUsed {
		t.Fatalf("capacity %d, used %v", c.InventoryCapacity(), c.InventoryAdditionUsed)
	}
}

/*
================
TestWarehouseAdditionGrowsTheRoomTwice

The warehouse addition grows the account room by 30 a use; the third
use refuses with nothing spent, and the room keeps its 210 slots.
================
*/
func TestWarehouseAdditionGrowsTheRoomTwice(t *testing.T) {
	c, rt, item := expansionCharacter(t, "ITEM_MALL_WAREHOUSE_ADDITION", 24406, map[string]float64{
		"itemParam1_29c": 30, "itemParam2_2a0": 1})
	authority := &memoryStorage{room: domain.NewAccountStorage()}
	rt.ConfigureStorage(authority)
	use := wire.NewWriter(3).U8(25).U16(item.TypeFlags()).Payload()
	for range 2 {
		if out := rt.HandleItemUse(testDivision, c, use); out.Frames[0].Payload[0] != wire.ResultSuccess {
			t.Fatalf("addition = %+v / %q", out.Frames, out.DiagnosticRefusal)
		}
	}
	if authority.room.Capacity != 210 || authority.room.ExpansionUses != 2 {
		t.Fatalf("room = %+v", authority.room)
	}
	assertItemUseRefusedUnchanged(t, rt, c, use, wire.ErrCodeInvalidRequest)
	if authority.room.Capacity != 210 {
		t.Fatalf("refused use grew the room: %+v", authority.room)
	}
}

/*
================
warehouseCommitStub

A storage authority whose door-side read and commit-side room
differ, or whose commit fails: the two ways the growth can refuse
after the door admitted the use.
================
*/
type warehouseCommitStub struct {
	view domain.AccountStorage
	room domain.AccountStorage
	err  error
}

func (s *warehouseCommitStub) AccountStorage(*domain.Character) (domain.AccountStorage, error) {
	return s.view, nil
}

func (s *warehouseCommitStub) TransactStorage(character *domain.Character, mutate func(*domain.Character, *domain.AccountStorage) error) (domain.AccountStorage, error) {
	if s.err != nil {
		return s.room, s.err
	}
	room := s.room
	if err := mutate(character, &room); err != nil {
		return s.room, err
	}
	s.room = room
	return room, nil
}

/*
================
TestWarehouseAdditionRefusedAtCommitKeepsTheItem

The door admitted the use on a one-use room, but a sibling character
spent the second use before the growth committed: the item stays in
the bag and the room keeps its slots.
================
*/
func TestWarehouseAdditionRefusedAtCommitKeepsTheItem(t *testing.T) {
	c, rt, item := expansionCharacter(t, "ITEM_MALL_WAREHOUSE_ADDITION", 24406, map[string]float64{
		"itemParam1_29c": 30, "itemParam2_2a0": 1})
	authority := &warehouseCommitStub{
		view: domain.AccountStorage{Capacity: 180, ExpansionUses: 1},
		room: domain.AccountStorage{Capacity: 210, ExpansionUses: 2},
	}
	rt.ConfigureStorage(authority)
	use := wire.NewWriter(3).U8(25).U16(item.TypeFlags()).Payload()
	assertItemUseRefusedUnchanged(t, rt, c, use, wire.ErrCodeInvalidRequest)
	if authority.room.Capacity != 210 || authority.room.ExpansionUses != 2 {
		t.Fatalf("refused commit changed the room: %+v", authority.room)
	}
}

/*
================
doorReadRefusingStorage

A storage authority that fails the test on any AccountStorage read.
In the live shard that read takes the store lock, which the item
use's character door already holds; a read from inside the door
deadlocked the GameWorld (tick stall 2026-11-25 22:04Z). Test fakes
take no lock, so the read itself is the observable.
================
*/
type doorReadRefusingStorage struct {
	memoryStorage
	t *testing.T
}

func (s *doorReadRefusingStorage) AccountStorage(*domain.Character) (domain.AccountStorage, error) {
	s.t.Fatalf("the warehouse addition read storage inside the item use's door")
	return domain.AccountStorage{}, nil
}

/*
================
TestWarehouseAdditionReadsNoStorageInsideTheDoor

The addition grows the room through the commit alone: no storage read
happens while the item use's character door is open.
================
*/
func TestWarehouseAdditionReadsNoStorageInsideTheDoor(t *testing.T) {
	c, rt, item := expansionCharacter(t, "ITEM_MALL_WAREHOUSE_ADDITION", 24406, map[string]float64{
		"itemParam1_29c": 30, "itemParam2_2a0": 1})
	authority := &doorReadRefusingStorage{memoryStorage: memoryStorage{room: domain.NewAccountStorage()}, t: t}
	rt.ConfigureStorage(authority)
	use := wire.NewWriter(3).U8(25).U16(item.TypeFlags()).Payload()
	if out := rt.HandleItemUse(testDivision, c, use); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("addition = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if authority.room.Capacity != domain.StorageDefaultCapacity+30 || authority.room.ExpansionUses != 1 {
		t.Fatalf("room = %+v", authority.room)
	}
}

/*
================
TestWarehouseAdditionStoreErrorKeepsTheItem

The growth itself fails to commit: the item stays in the bag and the
client hears the failure.
================
*/
func TestWarehouseAdditionStoreErrorKeepsTheItem(t *testing.T) {
	c, rt, item := expansionCharacter(t, "ITEM_MALL_WAREHOUSE_ADDITION", 24406, map[string]float64{
		"itemParam1_29c": 30, "itemParam2_2a0": 1})
	authority := &warehouseCommitStub{
		view: domain.NewAccountStorage(),
		room: domain.NewAccountStorage(),
		err:  errors.New("store unavailable"),
	}
	rt.ConfigureStorage(authority)
	use := wire.NewWriter(3).U8(25).U16(item.TypeFlags()).Payload()
	assertItemUseRefusedUnchanged(t, rt, c, use, wire.ErrCodeInvalidRequest)
	if authority.room.Capacity != domain.StorageDefaultCapacity {
		t.Fatalf("failed commit changed the room: %+v", authority.room)
	}
}
