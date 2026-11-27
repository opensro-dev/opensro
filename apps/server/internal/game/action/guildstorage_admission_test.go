/*
===========================================================================

guildstorage_admission_test.go - the guild warehouse's own deposit rule

#4490 U6-11: the guild room tests the CanBorrow guild bit where the
account room tests the account bit, and refuses a quest-shaped row and
an untradeable one besides, all with the same 0x43. The account room
keeps its single-bit rule.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// guildAdmissionCase is one bag row judged by both warehouses.
type guildAdmissionCase struct {
	codename string
	slot     uint8
	borrow   float64
	trade    float64
	typeIDs  [4]int64
	account  bool
	guild    bool
}

var guildAdmissionCases = []guildAdmissionCase{
	{"GUILD_CASE_ACCOUNT_BIT", 24, 0x80, 1, [4]int64{3, 3, 1, 1}, true, false},
	{"GUILD_CASE_GUILD_BIT", 25, 0x40, 1, [4]int64{3, 3, 1, 1}, false, true},
	{"GUILD_CASE_QUEST", 26, 255, 1, [4]int64{3, 3, 9, 0}, true, false},
	{"GUILD_CASE_UNTRADEABLE", 27, 255, 0, [4]int64{3, 3, 1, 1}, true, false},
}

/*
================
TestGuildWarehouseAdmitsByItsOwnRule
================
*/
func TestGuildWarehouseAdmitsByItsOwnRule(t *testing.T) {
	rt, a, _, refs := boundPetGuildFixture(t)
	for i, kase := range guildAdmissionCases {
		refs.staticItemSource[kase.codename] = &enterworld.ItemRef{
			Codename: kase.codename, RefObjID: uint32(13001 + i), TypeIDs: kase.typeIDs,
			NativeFields: enterworld.NewNativeFields(map[string]float64{
				"canBorrow": kase.borrow, "canTrade": kase.trade, "keepingFee": 0}),
		}
		a.MissionInventory = append(a.MissionInventory, enterworld.InventoryRow{
			Slot: int64(kase.slot), RefObjID: uint32(13001 + i), Codename: kase.codename,
			TypeFlags:  wire.PackTypeFlags(uint8(kase.typeIDs[0]), uint8(kase.typeIDs[1]), uint8(kase.typeIDs[2]), uint8(kase.typeIDs[3])),
			StackCount: 1})
	}

	// Each room's predicate alone: the two borrow bits are independent,
	// and the quest shape and the trade flag bind only the guild door.
	for _, kase := range guildAdmissionCases {
		item, present := bagOf(a).At(kase.slot)
		if !present {
			t.Fatalf("bag holds no %s", kase.codename)
		}
		if got := rt.storableItem(item); got != kase.account {
			t.Fatalf("%s: account room admits %v, want %v", kase.codename, got, kase.account)
		}
		if got := rt.guildStorableItem(item); got != kase.guild {
			t.Fatalf("%s: guild room admits %v, want %v", kase.codename, got, kase.guild)
		}
	}

	// End to end at the guild door: the three refused rows keep their
	// bag places and answer 0x43; the guild-bit row moves in and comes
	// back out (no admission is asked on a withdraw).
	openBoundPetGuildWarehouse(t, rt, a)
	deposit := func(slot uint8) OpResult {
		return rt.applyGuildStorageMove(testDivision, a, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeGuildStorageDeposit, SourceSlot: slot, DestSlot: 0, NpcGID: guildManagerGid})
	}
	for _, kase := range guildAdmissionCases {
		res := deposit(kase.slot)
		if kase.guild {
			if len(res.Frames) == 0 || res.Frames[0].Payload[0] != 1 || bagRowByCodename(a, kase.codename) != nil {
				t.Fatalf("%s: guild deposit = %+v, want the row stored", kase.codename, res.Frames)
			}
			continue
		}
		if len(res.Frames) != 1 || !bytes.Equal(res.Frames[0].Payload, []byte{2, errCodeInvalidStorageTarget}) {
			t.Fatalf("%s: guild deposit = %+v, want the 0x43 refusal", kase.codename, res.Frames)
		}
		if bagRowByCodename(a, kase.codename) == nil {
			t.Fatalf("%s: the refused deposit took the row", kase.codename)
		}
	}
	res := rt.applyGuildStorageMove(testDivision, a, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGuildStorageWithdraw, SourceSlot: 0, DestSlot: 25, NpcGID: guildManagerGid})
	if len(res.Frames) == 0 || res.Frames[0].Payload[0] != 1 || bagRowByCodename(a, "GUILD_CASE_GUILD_BIT") == nil {
		t.Fatalf("guild withdraw = %+v, want the stored row back", res.Frames)
	}
}
