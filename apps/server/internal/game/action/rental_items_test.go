/*
===========================================================================

rental_items_test.go - expired rentals leave at entry and warehouse open

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// rentalTestNow is the one clock these tests read: noon on a fixed day.
const rentalTestNow = int64(1_800_000_000)

/*
================
rentalRuntime
================
*/
func rentalRuntime() *Runtime {
	return &Runtime{Now: func() time.Time { return time.Unix(rentalTestNow, 0) }}
}

/*
================
rentRow
================
*/
func rentRow(slot int64, codename string, expires int64) enterworld.InventoryRow {
	return enterworld.InventoryRow{Slot: slot, RefObjID: 3630, Codename: codename,
		TypeFlags: wire.PackTypeFlags(3, 1, 13, 1), StackCount: 1, VarianceBits: "0",
		RentExpiresUnix: expires}
}

/*
================
TestExpiredRentalsLeaveAtEntry

An expired rental leaves the bag, the avatar rows and a companion bag
at the entry sweep; a live rental and a plain row stay, and a second
sweep changes nothing.
================
*/
func TestExpiredRentalsLeaveAtEntry(t *testing.T) {
	rt := rentalRuntime()
	c := &enterworld.Character{
		Name: "Renter",
		MissionInventory: []enterworld.InventoryRow{
			rentRow(14, "ITEM_EXPIRED_BAG", rentalTestNow-60),
			rentRow(15, "ITEM_LIVE_BAG", rentalTestNow+3600),
			rentRow(16, "ITEM_PLAIN", 0),
		},
		AvatarInventory: &domain.AvatarInventory{Capacity: 4, Rows: []enterworld.InventoryRow{
			rentRow(0, "ITEM_EXPIRED_AVATAR", rentalTestNow),
			rentRow(1, "ITEM_LIVE_AVATAR", rentalTestNow+60),
		}},
		ActiveCOS: &enterworld.CharacterCOS{Container: &domain.COSContainer{Rows: []enterworld.InventoryRow{
			rentRow(0, "ITEM_EXPIRED_COS", rentalTestNow-1),
		}}},
	}
	if !rt.RemoveExpiredRentItems(c) {
		t.Fatal("the sweep removed nothing")
	}
	if len(c.MissionInventory) != 2 || c.MissionInventory[0].Codename != "ITEM_LIVE_BAG" ||
		c.MissionInventory[1].Codename != "ITEM_PLAIN" {
		t.Fatalf("bag rows = %+v, want the live rental and the plain row", c.MissionInventory)
	}
	if len(c.AvatarInventory.Rows) != 1 || c.AvatarInventory.Rows[0].Codename != "ITEM_LIVE_AVATAR" {
		t.Fatalf("avatar rows = %+v, want the live rental", c.AvatarInventory.Rows)
	}
	if len(c.ActiveCOS.Container.Rows) != 0 {
		t.Fatalf("COS rows = %+v, want none", c.ActiveCOS.Container.Rows)
	}
	if rt.RemoveExpiredRentItems(c) {
		t.Fatal("the second sweep removed something")
	}
}

/*
================
TestExpiredRentalRefusesEquip

The equip gate refuses a rental whose period has run on the operation's
clock, before any itemdata leg; a live rental and a plain row pass.
================
*/
func TestExpiredRentalRefusesEquip(t *testing.T) {
	requirements := equipRequirements(testItems(), &enterworld.Character{Name: "Renter"}, nil, rentalTestNow)
	expired := inventory.Item{Codename: "ITEM_CH_SWORD_01_A_RARE", Durability: 100, RentExpiresUnix: rentalTestNow - 1}
	if fault := requirements.PreExclusivity(expired); fault == nil || fault.Reason != "rentExpired" {
		t.Fatalf("expired rental fault = %v, want rentExpired", fault)
	}
	live := inventory.Item{Codename: "ITEM_CH_SWORD_01_A_RARE", Durability: 100, RentExpiresUnix: rentalTestNow + 60}
	if fault := requirements.PreExclusivity(live); fault != nil {
		t.Fatalf("live rental fault = %v, want none", fault)
	}
	plain := inventory.Item{Codename: "ITEM_CH_SWORD_01_A_RARE", Durability: 100}
	if fault := requirements.PreExclusivity(plain); fault != nil {
		t.Fatalf("plain item fault = %v, want none", fault)
	}
}

/*
================
TestStorageListDeletesExpiredRentals

A rental that expires while stored leaves as the warehouse lists, in
the room's stored record too; a live rental and a plain row stay.
================
*/
func TestStorageListDeletesExpiredRentals(t *testing.T) {
	rt, c, authority := storageFixture(t)
	now := rt.Now().Unix()
	authority.room.Rows = append(authority.room.Rows,
		rentRow(0, "ITEM_ETC_HP_POTION_01", now-60),
		rentRow(1, "ITEM_ETC_HP_POTION_01", now+3600),
		enterworld.InventoryRow{Slot: 2, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 1, VarianceBits: "0"},
	)
	frames, refusal := rt.HandleStorageList(testDivision, c, wire.NewWriter(5).U32(17).U8(0).Payload())
	if refusal != "" || len(frames) != 3 {
		t.Fatalf("list = %+v / %q", frames, refusal)
	}
	if len(authority.room.Rows) != 2 {
		t.Fatalf("stored rows = %+v, want the live rental and the plain row", authority.room.Rows)
	}
	for _, row := range authority.room.Rows {
		if row.RentExpiresUnix > 0 && row.RentExpiresUnix <= now {
			t.Fatalf("expired rental survived the list: %+v", row)
		}
	}
}
