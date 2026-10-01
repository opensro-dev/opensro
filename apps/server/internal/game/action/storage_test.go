/*
===========================================================================

storage_test.go - warehouse admission, fees and transfers

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
memoryStorage

An in-memory StorageAuthority with the store's detached-copy contract.
================
*/
type memoryStorage struct {
	room domain.AccountStorage
}

func (m *memoryStorage) AccountStorage(*domain.Character) (domain.AccountStorage, error) {
	return m.room, nil
}

func (m *memoryStorage) TransactStorage(character *domain.Character, mutate func(*domain.Character, *domain.AccountStorage) error) (domain.AccountStorage, error) {
	next := character.Snapshot()
	room := m.room
	room.Rows = append([]domain.InventoryRow(nil), m.room.Rows...)
	if err := mutate(next, &room); err != nil {
		return m.room, err
	}
	m.room = room
	character.MissionInventory = next.MissionInventory
	character.Gold = next.Gold
	return room, nil
}

/*
================
potionRow

The bag row holding the fixture's HP potions.
================
*/
func potionRow(t *testing.T, c *enterworld.Character) enterworld.InventoryRow {
	t.Helper()
	for _, row := range c.MissionInventory {
		if row.Codename == "ITEM_ETC_HP_POTION_01" && row.Slot >= 13 {
			return row
		}
	}
	t.Fatal("fixture has no bag potion")
	return enterworld.InventoryRow{}
}

/*
================
storageFixture

The merchant fixture's NPC turned into a warehouse keeper; the potion is
storable with a one-gold keeping fee.
================
*/
func storageFixture(t *testing.T) (*Runtime, *enterworld.Character, *memoryStorage) {
	t.Helper()
	rt, c := merchantFixture(t)
	potion := rt.deps.ItemReferences().(staticItemSource)["ITEM_ETC_HP_POTION_01"]
	potion.NativeFields = potion.NativeFields.With("canBorrow", 255).With("keepingFee", 1)
	rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagStorage
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 14, RefObjID: 3630,
		Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 30, VarianceBits: "0"})
	authority := &memoryStorage{room: domain.NewAccountStorage()}
	rt.ConfigureStorage(authority)
	return rt, c, authority
}

func TestStorageListAndOpenAnswerTheNativeShapes(t *testing.T) {
	rt, c, authority := storageFixture(t)
	authority.room.Gold = 77
	frames, refusal := rt.HandleStorageList(testDivision, c, wire.NewWriter(5).U32(17).U8(0).Payload())
	if refusal != "" || len(frames) != 3 || frames[0].Opcode != opCommerceItemReferences || frames[1].Opcode != wire.OpStorageGold || frames[2].Opcode != wire.OpStorageList {
		t.Fatalf("list = %+v / %q", frames, refusal)
	}
	if frames[2].Payload[0] != domain.StorageDefaultCapacity || frames[2].Payload[1] != 0 {
		t.Fatalf("empty room list = %v", frames[2].Payload)
	}
	open, refusal := rt.HandleNpcAction(testDivision, c, wire.NewWriter(8).U32(17).U32(wire.StorageFunctionMask).Payload())
	if refusal != "" || len(open) != 1 || open[0].Opcode != wire.OpNpcInteractionAck {
		t.Fatalf("open = %+v / %q", open, refusal)
	}
	rt.Selected.Set(testDivision, c.Name, 99)
	if _, refusal := rt.HandleStorageList(testDivision, c, wire.NewWriter(5).U32(17).U8(0).Payload()); refusal == "" {
		t.Fatal("listed a warehouse the player does not have selected")
	}
}

func TestStorageDepositChargesTheKeepingFeeAndWithdrawReturns(t *testing.T) {
	rt, c, authority := storageFixture(t)
	row := potionRow(t, c)
	slot, stack := uint8(row.Slot), row.StackCount
	gold := goldOf(c)
	r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeStorageDeposit, SourceSlot: slot, DestSlot: 3, NpcGID: 17})
	if r.Frames[0].Payload[0] != 1 || len(authority.room.Rows) != 1 || authority.room.Rows[0].Slot != 3 {
		t.Fatalf("deposit = %+v, room %+v", r, authority.room)
	}
	if goldOf(c) != gold-uint64(stack) {
		t.Fatalf("keeping fee: gold %d, want %d", goldOf(c), gold-uint64(stack))
	}
	r = trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeStorageWithdraw, SourceSlot: 3, DestSlot: slot, NpcGID: 17})
	if r.Frames[0].Payload[0] != 1 || len(authority.room.Rows) != 0 || len(c.MissionInventory) == 0 {
		t.Fatalf("withdraw = %+v, room %+v", r, authority.room)
	}
}

func TestStorageRefusesUnstorableItemsAndOverdrafts(t *testing.T) {
	rt, c, authority := storageFixture(t)
	potion := rt.deps.ItemReferences().(staticItemSource)["ITEM_ETC_HP_POTION_01"]
	potion.NativeFields = potion.NativeFields.With("canBorrow", 0)
	slot := uint8(potionRow(t, c).Slot)
	r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeStorageDeposit, SourceSlot: slot, DestSlot: 0, NpcGID: 17})
	if r.Frames[0].Payload[0] != 2 || r.Frames[0].Payload[1] != errCodeInvalidStorageTarget || len(authority.room.Rows) != 0 {
		t.Fatalf("unstorable deposit = %v", r.Frames[0].Payload)
	}
	gold := goldOf(c)
	r = trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeStorageGoldDeposit, GoldAmount: 300})
	if r.Frames[0].Payload[0] != 1 || authority.room.Gold != 300 || goldOf(c) != gold-300 {
		t.Fatalf("gold deposit = %+v room %d", r, authority.room.Gold)
	}
	r = trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeStorageGoldWithdraw, GoldAmount: 301})
	if r.Frames[0].Payload[0] != 2 || r.Frames[0].Payload[1] != wire.ErrCodeNotEnoughGold || authority.room.Gold != 300 {
		t.Fatalf("overdraft = %v room %d", r.Frames[0].Payload, authority.room.Gold)
	}
}
