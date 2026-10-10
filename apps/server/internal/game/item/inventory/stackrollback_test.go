/*
===========================================================================

stackrollback_test.go - retained stacks after operator cap rollback

Exercise the shared bag, warehouse and pet primitives without changing the
native transfer rules for ordinary rows or admitting unrelated corrupt rows.

===========================================================================
*/
package inventory

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestStackRollbackContainerTransfers
================
*/
func TestStackRollbackContainerTransfers(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, tc := range []struct {
			name                                    string
			source, dest, cap, wantSource, wantDest uint16
		}{
			{"oversized-source", 100, 10, 50, 60, 50},
			{"oversized-destination", 10, 100, 50, 100, 10},
			{"both-maximum", 65535, 65535, 50, 65535, 65535},
			{"maximum-source", 65535, 49, 50, 65534, 50},
			{"disabled-elixir", 50, 1, 1, 1, 50},
		} {
			t.Run(tc.name+map[bool]string{false: "/deposit", true: "/withdraw"}[reverse], func(t *testing.T) {
				flags := wire.PackTypeFlags(3, 3, 1, 1)
				if tc.cap == 1 {
					flags = wire.PackTypeFlags(3, 3, 10, 1)
				}
				bag := New(nil, domain.DefaultInventorySize)
				room, fault := NewStorageRoom(nil, 4)
				if fault != nil {
					t.Fatal(fault)
				}
				source, dest := bag, room
				ss, ds := uint8(EquipmentSlotEnd), uint8(0)
				if reverse {
					source, dest, ss, ds = room, bag, ds, ss
				}
				source.items = []Item{{Slot: ss, RefObjID: 1, TypeFlags: flags, Quantity: tc.source, RecordID: 11}}
				dest.items = []Item{{Slot: ds, RefObjID: 1, TypeFlags: flags, Quantity: tc.dest, RecordID: 22}}
				if fault := source.TransferWholeTo(dest, ss, ds, tc.cap); fault != nil {
					t.Fatal(fault)
				}
				a, _ := source.At(ss)
				b, _ := dest.At(ds)
				if a.Quantity != tc.wantSource || b.Quantity != tc.wantDest || uint32(a.Quantity)+uint32(b.Quantity) != uint32(tc.source)+uint32(tc.dest) {
					t.Fatalf("counts: source=%d destination=%d; want %d, %d", a.Quantity, b.Quantity, tc.wantSource, tc.wantDest)
				}
				if a.RecordID != 11 || b.RecordID != 22 {
					t.Fatal("count transfer changed record identities")
				}
			})
		}
	}
}

/*
================
TestStackRollbackSplit
================
*/
func TestStackRollbackSplit(t *testing.T) {
	for _, kind := range []string{"bag", "storage", "pet"} {
		for _, quantity := range []uint16{0, 1, 49, 50, 51} {
			t.Run(kind+"/"+map[uint16]string{0: "zero", 1: "single", 49: "partial", 50: "whole", 51: "over"}[quantity], func(t *testing.T) {
				inv := New(nil, domain.DefaultInventorySize)
				ss := uint8(EquipmentSlotEnd)
				var fault *Fault
				if kind == "storage" {
					inv, fault = NewStorageRoom(nil, 4)
					ss = 0
				}
				if kind == "pet" {
					inv, fault = NewContainer(nil, 4)
					ss = 0
				}
				if fault != nil {
					t.Fatal(fault)
				}
				original := Item{Slot: ss, RefObjID: 3700, TypeFlags: wire.PackTypeFlags(3, 3, 10, 1), Quantity: 50, RecordID: 11}
				inv.items = []Item{original}
				result, fault := inv.Transfer(ss, ss+1, quantity, 1)
				if quantity == 0 || quantity > original.Quantity {
					code := wire.ErrCodePositiveNumberOnly
					if quantity > original.Quantity {
						code = wire.ErrCodeInputFewerThanRemain
					}
					if fault == nil || fault.Code != code || !reflect.DeepEqual(inv.Items(), []Item{original}) {
						t.Fatalf("invalid split mutated stock or wrong refusal: %v %+v", fault, inv.Items())
					}
					return
				}
				if fault != nil {
					t.Fatal(fault)
				}
				a, present := inv.At(ss)
				b, _ := inv.At(ss + 1)
				if b.Quantity != quantity || uint32(a.Quantity)+uint32(b.Quantity) != uint32(original.Quantity) {
					t.Fatalf("split counts: %+v %+v", a, b)
				}
				if quantity == original.Quantity {
					if present || result.Leg != LegMove || b.RecordID != original.RecordID {
						t.Fatal("whole move changed identity")
					}
					return
				}
				if result.Leg != LegSplit || !present || a.RecordID != original.RecordID || b.RecordID != 0 || b.RefObjID != original.RefObjID {
					t.Fatalf("split identity: %+v %+v %+v", result, a, b)
				}
			})
		}
	}
}

/*
================
TestStackRollbackRejectsUnrelatedRows
================
*/
func TestStackRollbackRejectsUnrelatedRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		item Item
		cap  uint16
	}{
		{"unknown-cap", Item{TypeFlags: wire.PackTypeFlags(3, 3, 10, 1)}, 0},
		{"stone", Item{TypeFlags: wire.PackTypeFlags(3, 3, 11, 1)}, 1},
		{"cargo", Item{TypeFlags: wire.PackTypeFlags(3, 3, 8, 1)}, 1},
		{"metadata", Item{TypeFlags: wire.PackTypeFlags(3, 3, 10, 1), Plus: 1}, 1},
		{"single-potion", Item{TypeFlags: wire.PackTypeFlags(3, 3, 1, 1)}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := tc.item
			item.Slot, item.RefObjID, item.Quantity = EquipmentSlotEnd, 1, 50
			inv := New([]Item{item}, domain.DefaultInventorySize)
			target := item
			target.Slot, target.Quantity = 0, 1
			room, fault := NewStorageRoom([]Item{target}, 4)
			if fault != nil {
				t.Fatal(fault)
			}
			if fault := inv.TransferWholeTo(room, item.Slot, target.Slot, tc.cap); fault == nil {
				t.Fatal("invalid oversized row admitted")
			}
			if !reflect.DeepEqual(inv.Items(), []Item{item}) || !reflect.DeepEqual(room.Items(), []Item{target}) {
				t.Fatal("refusal mutated containers")
			}
		})
	}
}

/*
================
TestStackRollbackFamilies
================
*/
func TestStackRollbackFamilies(t *testing.T) {
	for _, family := range []struct {
		name             string
		typeID3, typeID4 uint8
	}{
		{"hp", 1, 1}, {"mp", 1, 2}, {"vigor", 1, 3},
		{"pet", 1, 4}, {"pet-vigor", 1, 9}, {"elixir", 10, 1}, {"powder", 10, 2},
	} {
		t.Run(family.name, func(t *testing.T) {
			// Package Data can populate Durability even on ordinary ETC rows.
			item := Item{Slot: EquipmentSlotEnd, RefObjID: 1, TypeFlags: wire.PackTypeFlags(3, 3, family.typeID3, family.typeID4), Quantity: 100, Durability: 50}
			bag := New([]Item{item}, domain.DefaultInventorySize)
			target := item
			target.Slot, target.Quantity = 0, 10
			pet, fault := NewContainer([]Item{target}, 4)
			if fault != nil {
				t.Fatal(fault)
			}
			if fault := bag.TransferWholeTo(pet, item.Slot, target.Slot, 50); fault != nil {
				t.Fatal(fault)
			}
			a, _ := bag.At(item.Slot)
			b, _ := pet.At(target.Slot)
			if a.Quantity != 60 || b.Quantity != 50 {
				t.Fatalf("family rollback counts: %+v %+v", a, b)
			}
		})
	}
}

/*
================
TestStackRollbackKeepsNativeSingleSwap
================
*/
func TestStackRollbackKeepsNativeSingleSwap(t *testing.T) {
	first := Item{Slot: EquipmentSlotEnd, RefObjID: 3700, TypeFlags: wire.PackTypeFlags(3, 3, 10, 1), Quantity: 1, RecordID: 11}
	second := first
	second.Slot, second.RecordID = first.Slot+1, 22
	inv := New([]Item{first, second}, domain.DefaultInventorySize)
	result, fault := inv.Transfer(first.Slot, second.Slot, 1, 1)
	if fault != nil || result.Leg != LegSwap {
		t.Fatalf("native singles no longer swap: %+v %v", result, fault)
	}
	a, _ := inv.At(first.Slot)
	b, _ := inv.At(second.Slot)
	if a.Quantity != 1 || b.Quantity != 1 || a.RecordID != second.RecordID || b.RecordID != first.RecordID {
		t.Fatalf("native single swap changed counts or identity: %+v %+v", a, b)
	}
}
