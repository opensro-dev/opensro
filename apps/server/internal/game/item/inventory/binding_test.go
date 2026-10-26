/*
===========================================================================

binding_test.go - character-bound rewards cannot escape through slot operations

===========================================================================
*/
package inventory

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestBoundRewardsRefuseDropsAndContainerSwaps
================
*/
func TestBoundRewardsRefuseDropsAndContainerSwaps(t *testing.T) {
	item := potion(13)
	item.CharacterBound = true
	for _, operation := range []string{"drop", "death", "deposit", "swapBack"} {
		t.Run(operation, func(t *testing.T) {
			bag := New([]Item{item}, domain.DefaultInventorySize)
			room, fault := NewStorageRoom([]Item{potion(0)}, 20)
			if fault != nil {
				t.Fatal(fault)
			}
			beforeBag, beforeRoom := bag.Items(), room.Items()
			switch operation {
			case "drop":
				_, fault = bag.Drop(13)
			case "death":
				_, fault = bag.DeathDrop(13)
			case "deposit":
				fault = bag.TransferWholeTo(room, 13, 1, 50)
			case "swapBack":
				fault = room.TransferWholeTo(bag, 0, 13, 50)
			}
			if fault == nil || fault.Reason != "characterBoundItem" {
				t.Fatalf("%s admitted: %v", operation, fault)
			}
			if !reflect.DeepEqual(beforeBag, bag.Items()) || !reflect.DeepEqual(beforeRoom, room.Items()) {
				t.Fatal("refusal changed a container")
			}
		})
	}
}

/*
================
TestBoundStackSplitMergeAndGrantIdentity
================
*/
func TestBoundStackSplitMergeAndGrantIdentity(t *testing.T) {
	bound := potion(13)
	bound.CharacterBound = true
	bag := New([]Item{bound, potion(14)}, domain.DefaultInventorySize)
	if _, fault := bag.Transfer(13, 15, 5, 50); fault != nil {
		t.Fatal(fault)
	}
	left, _ := bag.At(13)
	split, _ := bag.At(15)
	if !left.CharacterBound || !split.CharacterBound || left.Quantity != 15 || split.Quantity != 5 {
		t.Fatal("split lost binding")
	}
	if _, fault := bag.Transfer(15, 13, 0, 50); fault != nil {
		t.Fatal(fault)
	}
	left, _ = bag.At(13)
	if !left.CharacterBound || left.Quantity != 20 {
		t.Fatal("merge lost binding")
	}
	before := bag.Items()
	if _, fault := bag.Transfer(13, 14, 5, 50); fault == nil || fault.Reason != "characterBoundStackMismatch" {
		t.Fatalf("mixed-binding stacks must refuse: %v", fault)
	}
	if !reflect.DeepEqual(before, bag.Items()) {
		t.Fatal("mixed-binding refusal changed the bag")
	}
	if _, fault := bag.GrantStack(bound, 50); fault != nil {
		t.Fatal(fault)
	}
	left, _ = bag.At(13)
	ordinary, _ := bag.At(14)
	if !left.CharacterBound || left.Quantity != 40 || ordinary.CharacterBound || ordinary.Quantity != 20 {
		t.Fatal("grant changed stock binding")
	}
	if _, fault := bag.GrantStack(potion(0), 50); fault != nil {
		t.Fatal(fault)
	}
	ordinary, _ = bag.At(14)
	if ordinary.CharacterBound || ordinary.Quantity != 40 {
		t.Fatal("ordinary grant merged into bound stock")
	}
}

/*
================
TestBoundWeaponCanEquipAndMove
================
*/
func TestBoundWeaponCanEquipAndMove(t *testing.T) {
	item := sword(13)
	item.CharacterBound = true
	bag := New([]Item{item}, domain.DefaultInventorySize)
	if _, fault := bag.Transfer(13, 6, 0, 1); fault != nil {
		t.Fatal(fault)
	}
	if _, fault := bag.Transfer(6, 20, 0, 1); fault != nil {
		t.Fatal(fault)
	}
	stored, _ := bag.At(20)
	if !stored.CharacterBound || stored.RefObjID != item.RefObjID {
		t.Fatal("equip stripped binding")
	}
}
