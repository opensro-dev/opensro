package inventory

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

// Fixture rows carry their real RefItemData TypeID words, the way every
// production row does - the move path resolves sockets from the word.

func sword(slot uint8) Item {
	// TID 3.1.6.2: CH one-hand sword -> the weapon socket.
	return Item{Slot: slot, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), Durability: 100, Quantity: 1}
}

func potion(slot uint8) Item {
	// TID 3.3.1.1: HP potion - stackable ETC, not equipable.
	return Item{Slot: slot, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
		TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), Quantity: 20}
}

func TestMoveIntoEmptyBagSlot(t *testing.T) {
	inv := New([]Item{sword(13)}, domain.DefaultInventorySize)

	got, fault := inv.Move(13, 20)
	if fault != nil {
		t.Fatalf("Move refused: %v", fault)
	}
	if got.Swapped {
		t.Fatal("moving into an empty slot reported a swap")
	}
	if _, occupied := inv.At(13); occupied {
		t.Fatal("the source slot is still occupied")
	}
	moved, occupied := inv.At(20)
	if !occupied {
		t.Fatal("the destination slot is empty")
	}
	if moved.RefObjID != 11459 {
		t.Fatalf("destination holds refObjId %d, want 11459", moved.RefObjID)
	}
}

func TestMoveSwapsWhenDestinationOccupied(t *testing.T) {
	inv := New([]Item{sword(13), potion(20)}, domain.DefaultInventorySize)

	got, fault := inv.Move(13, 20)
	if fault != nil {
		t.Fatalf("Move refused: %v", fault)
	}
	if !got.Swapped {
		t.Fatal("moving onto an occupied slot did not report a swap")
	}

	atDest, _ := inv.At(20)
	atSource, _ := inv.At(13)
	if atDest.RefObjID != 11459 {
		t.Fatalf("slot 20 holds refObjId %d, want the sword 11459", atDest.RefObjID)
	}
	if atSource.RefObjID != 3630 {
		t.Fatalf("slot 13 holds refObjId %d, want the potion 3630", atSource.RefObjID)
	}
	if inv.Len() != 2 {
		t.Fatalf("row count = %d, want 2", inv.Len())
	}
}

func TestMoveRefusals(t *testing.T) {
	cases := []struct {
		name       string
		items      []Item
		sourceSlot uint8
		destSlot   uint8
		reason     string
		wantCode   uint8
	}{
		{
			name:       "source out of range",
			items:      []Item{sword(13)},
			sourceSlot: 45,
			destSlot:   20,
			reason:     "slotOutOfRange",
			wantCode:   wire.ErrCodeInvalidRequest,
		},
		{
			name:       "destination out of range",
			items:      []Item{sword(13)},
			sourceSlot: 13,
			destSlot:   200,
			reason:     "slotOutOfRange",
			wantCode:   wire.ErrCodeInvalidRequest,
		},
		{
			name:       "source empty",
			items:      []Item{sword(13)},
			sourceSlot: 14,
			destSlot:   20,
			reason:     "sourceSlotEmpty",
			wantCode:   wire.ErrCodeInvalidRequest,
		},
		{
			name:       "potion is not equipable",
			items:      []Item{potion(13)},
			sourceSlot: 13,
			destSlot:   SocketWeapon,
			reason:     "notEquipable",
			wantCode:   wire.ErrCodeCantEquip,
		},
		{
			name:       "sword into the shield socket",
			items:      []Item{sword(13)},
			sourceSlot: 13,
			destSlot:   SocketShield,
			reason:     "wrongSocket",
			wantCode:   wire.ErrCodeCantEquip,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			inv := New(testCase.items, domain.DefaultInventorySize)
			before := inv.Items()

			_, fault := inv.Move(testCase.sourceSlot, testCase.destSlot)
			if fault == nil {
				t.Fatal("Move was allowed, want a refusal")
			}
			if fault.Reason != testCase.reason {
				t.Fatalf("reason = %q, want %q", fault.Reason, testCase.reason)
			}
			if fault.Code != testCase.wantCode {
				t.Fatalf("code = 0x%02X, want 0x%02X", fault.Code, testCase.wantCode)
			}
			if !reflect.DeepEqual(inv.Items(), before) {
				t.Fatal("a refused move still mutated the inventory")
			}
		})
	}
}

func TestMoveAllowsWeaponIntoWeaponSocket(t *testing.T) {
	inv := New([]Item{sword(13)}, domain.DefaultInventorySize)

	if _, fault := inv.Move(13, SocketWeapon); fault != nil {
		t.Fatalf("equipping a sword into the weapon socket was refused: %v", fault)
	}
	if _, occupied := inv.At(SocketWeapon); !occupied {
		t.Fatal("the weapon socket is empty after equipping")
	}
}

func TestMoveAllowsRingIntoSecondHand(t *testing.T) {
	// TID 3.1.5.3: CH ring -> SocketRing, accepted in either hand.
	ring := Item{Slot: 13, RefObjID: 2000, Codename: "ITEM_CH_RING_01_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 5, 3), Quantity: 1}
	inv := New([]Item{ring}, domain.DefaultInventorySize)

	if _, fault := inv.Move(13, SocketRingSecond); fault != nil {
		t.Fatalf("a ring was refused by the second hand: %v", fault)
	}
}

func TestDropRemovesTheRow(t *testing.T) {
	inv := New([]Item{sword(13), potion(20)}, domain.DefaultInventorySize)

	dropped, fault := inv.Drop(13)
	if fault != nil {
		t.Fatalf("Drop refused: %v", fault)
	}
	if dropped.RefObjID != 11459 {
		t.Fatalf("dropped refObjId = %d, want 11459", dropped.RefObjID)
	}
	if _, occupied := inv.At(13); occupied {
		t.Fatal("the dropped slot is still occupied")
	}
	if inv.Len() != 1 {
		t.Fatalf("row count = %d, want 1", inv.Len())
	}
	// The surviving row must keep its slot.
	if survivor, occupied := inv.At(20); !occupied || survivor.RefObjID != 3630 {
		t.Fatalf("the untouched row at slot 20 did not survive: %+v", survivor)
	}
}

func TestDropRefusals(t *testing.T) {
	inv := New([]Item{sword(13)}, domain.DefaultInventorySize)

	if _, fault := inv.Drop(45); fault == nil || fault.Reason != "slotOutOfRange" {
		t.Fatalf("dropping an out-of-range slot = %v, want slotOutOfRange", fault)
	}
	if _, fault := inv.Drop(14); fault == nil || fault.Reason != "sourceSlotEmpty" {
		t.Fatalf("dropping an empty slot = %v, want sourceSlotEmpty", fault)
	}
}

// A WORN item cannot be ground-dropped (notice 01:6d; the retail drop
// dialog sub_68d430 refuses an equipment source before composing the
// packet - LANE-4, COORD seq 93). The gate must refuse ONLY the
// equipment-source drop: a bag drop must still succeed, so a broken drop
// plane can never masquerade as a working gate.
func TestDropRefusesEquippedItemButBagDropStillWorks(t *testing.T) {
	// A sword worn in the weapon socket (equipment band) plus a bag row.
	inv := New([]Item{sword(6), potion(20)}, domain.DefaultInventorySize)

	dropped, fault := inv.Drop(6)
	if fault == nil {
		t.Fatalf("dropping the worn item at socket 6 returned row %+v; want a refusal", dropped)
	}
	if fault.Code != wire.ErrCodeCannotDropEquipped {
		t.Fatalf("equipped-drop refusal code = 0x%02X, want 0x%02X (01:6d)", fault.Code, wire.ErrCodeCannotDropEquipped)
	}
	if fault.Reason != "dropEquippedItem" {
		t.Fatalf("equipped-drop refusal reason = %q, want dropEquippedItem", fault.Reason)
	}
	// The refused drop must NOT have removed the worn row.
	if worn, occupied := inv.At(6); !occupied || worn.RefObjID != 11459 {
		t.Fatalf("the worn row at socket 6 was disturbed by the refused drop: %+v", worn)
	}
	if inv.Len() != 2 {
		t.Fatalf("row count after refused equipped drop = %d, want 2", inv.Len())
	}

	// COORD condition: a normal BAG drop must still succeed.
	if _, fault := inv.Drop(20); fault != nil {
		t.Fatalf("a bag drop at slot 20 must still succeed, got refusal %v", fault)
	}
	if _, occupied := inv.At(20); occupied {
		t.Fatal("the bag row at slot 20 survived a successful drop")
	}

	// DropQuantity guards the same band on the fixture partial-drop path.
	inv2 := New([]Item{sword(6)}, domain.DefaultInventorySize)
	if _, fault := inv2.DropQuantity(6, 1); fault == nil || fault.Code != wire.ErrCodeCannotDropEquipped {
		t.Fatalf("DropQuantity from an equipment socket = %v, want 01:6d refusal", fault)
	}
}

// A grant lands in the lowest free bag slot, never in an equipment socket.
func TestGrantTakesTheFirstFreeBagSlot(t *testing.T) {
	inv := New([]Item{sword(13), sword(14), sword(16)}, domain.DefaultInventorySize)

	slot, fault := inv.Grant(potion(0))
	if fault != nil {
		t.Fatalf("Grant refused: %v", fault)
	}
	if slot != 15 {
		t.Fatalf("granted slot = %d, want the first gap 15", slot)
	}
	granted, occupied := inv.At(15)
	if !occupied {
		t.Fatal("slot 15 is empty after the grant")
	}
	if granted.Slot != 15 {
		t.Fatalf("the granted row carries slot %d, want 15", granted.Slot)
	}
}

func TestGrantIgnoresFreeEquipmentSockets(t *testing.T) {
	// Every bag slot taken, all equipment sockets free.
	items := make([]Item, 0, int((domain.DefaultInventorySize - EquipmentSlotEnd)))
	for wireSlot := EquipmentSlotEnd; wireSlot < domain.DefaultInventorySize; wireSlot++ {
		items = append(items, sword(wireSlot))
	}
	inv := New(items, domain.DefaultInventorySize)

	if _, ok := inv.FirstFreeBagSlot(); ok {
		t.Fatal("a full bag reported a free slot")
	}
	_, fault := inv.Grant(potion(0))
	if fault == nil {
		t.Fatal("granting into a full bag was allowed")
	}
	if fault.Reason != "inventoryFull" {
		t.Fatalf("reason = %q, want inventoryFull", fault.Reason)
	}
	if fault.Code != wire.ErrCodeStorageFull {
		t.Fatalf("code = 0x%02X, want 0x%02X (UIIT_MSG_STRGERR_INVENTORY_FULL)",
			fault.Code, wire.ErrCodeStorageFull)
	}
}

func TestGrantDefaultsQuantityToOne(t *testing.T) {
	inv := New(nil, domain.DefaultInventorySize)

	slot, fault := inv.Grant(Item{RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE"})
	if fault != nil {
		t.Fatalf("Grant refused: %v", fault)
	}
	granted, _ := inv.At(slot)
	if granted.Quantity != 1 {
		t.Fatalf("quantity = %d, want 1", granted.Quantity)
	}
}

// The picked-up row must produce the CSOItem body the type-0x06 grant carries.
func TestItemBodyMirrorsTheRow(t *testing.T) {
	item := Item{RefObjID: 11459, Plus: 5, VarianceBits: 0x1234, Durability: 99}

	got := item.Body()
	want := wire.ItemBody{RefObjID: 11459, Plus: 5, VarianceBits: 0x1234, Durability: 99}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %+v, want %+v", got, want)
	}
}

// New must copy, so a caller's slice cannot be mutated behind its back.
func TestNewCopiesTheRows(t *testing.T) {
	original := []Item{sword(13)}
	inv := New(original, domain.DefaultInventorySize)

	if _, fault := inv.Move(13, 20); fault != nil {
		t.Fatalf("Move refused: %v", fault)
	}
	if original[0].Slot != 13 {
		t.Fatalf("the caller's slice was mutated: slot = %d, want 13", original[0].Slot)
	}
}
