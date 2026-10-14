/*
===========================================================================

conservation_fuzz_test.go - no bag or warehouse operation mints or burns

Random sequences of moves, splits, merges and bag<->room transfers over a
mixed bag (stackable potions, an over-cap retained elixir stack, single
swords). Every operation either refuses and leaves both containers
untouched, or succeeds with each reference's total count unchanged, no
two rows on one slot, and no empty row.

===========================================================================
*/
package inventory

import (
	"math/rand"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

const (
	fuzzBagEnd   uint8  = 45
	fuzzRoomSize uint8  = 20
	potionRef    uint32 = 1
	swordRef     uint32 = 2
	elixirRef    uint32 = 3
)

/*
================
fuzzStackCap

The stack cap each fuzz reference uses: potions 50, an elixir's operator
cap of 1 under which a retained stack of 7 lives, swords 1.
================
*/
func fuzzStackCap(ref uint32) uint16 {
	if ref == potionRef {
		return 50
	}
	return 1
}

/*
================
fuzzStart
================
*/
func fuzzStart() (*Inventory, *Inventory) {
	potion := wire.PackTypeFlags(3, 3, 1, 1)
	elixir := wire.PackTypeFlags(3, 3, 10, 1)
	sword := wire.PackTypeFlags(3, 1, 6, 2)
	bag := New([]Item{
		{Slot: 13, RefObjID: potionRef, TypeFlags: potion, Quantity: 30},
		{Slot: 14, RefObjID: potionRef, TypeFlags: potion, Quantity: 45},
		{Slot: 15, RefObjID: elixirRef, TypeFlags: elixir, Quantity: 7},
		{Slot: 16, RefObjID: swordRef, TypeFlags: sword, Quantity: 1, Plus: 5},
		{Slot: 17, RefObjID: swordRef, TypeFlags: sword, Quantity: 1},
	}, fuzzBagEnd)
	room, fault := NewStorageRoom([]Item{
		{Slot: 0, RefObjID: potionRef, TypeFlags: potion, Quantity: 20},
		{Slot: 1, RefObjID: swordRef, TypeFlags: sword, Quantity: 1, Plus: 9},
	}, fuzzRoomSize)
	if fault != nil {
		panic(fault)
	}
	return bag, room
}

/*
================
fuzzCensus

Each reference's total count across both containers, and an error when a
container holds two rows on one slot or an empty row.
================
*/
func fuzzCensus(bag, room *Inventory) (map[uint32]int, string) {
	totals := map[uint32]int{}
	pluses := map[uint8]int{}
	for _, inv := range []*Inventory{bag, room} {
		seen := map[uint8]bool{}
		for _, row := range inv.items {
			if seen[row.Slot] {
				return nil, "two rows on one slot"
			}
			seen[row.Slot] = true
			if row.Quantity == 0 {
				return nil, "empty row"
			}
			totals[row.RefObjID] += int(row.Quantity)
			if row.RefObjID == swordRef {
				pluses[row.Plus]++
			}
		}
	}
	// The three swords are distinguishable by their plus: none may be copied.
	for plus, n := range pluses {
		if n > 1 && plus != 0 {
			return nil, "a unique sword was copied"
		}
	}
	return totals, ""
}

/*
================
fuzzStep

Applies one random operation and reports whether it succeeded.
================
*/
func fuzzStep(r *rand.Rand, bag, room *Inventory) bool {
	bagSlot := func() uint8 { return 13 + uint8(r.Intn(int(fuzzBagEnd-13)+2)) - 1 }
	roomSlot := func() uint8 { return uint8(r.Intn(int(fuzzRoomSize) + 2)) }
	quantity := func() uint16 {
		switch r.Intn(4) {
		case 0:
			return 0
		case 1:
			return uint16(r.Intn(80))
		case 2:
			return 65535
		}
		return uint16(1 + r.Intn(50))
	}
	capOf := func(inv *Inventory, slot uint8) uint16 {
		if item, ok := inv.At(slot); ok {
			return fuzzStackCap(item.RefObjID)
		}
		return 1
	}
	switch r.Intn(6) {
	case 0:
		src, dst := bagSlot(), bagSlot()
		_, fault := bag.Transfer(src, dst, quantity(), capOf(bag, src))
		return fault == nil
	case 1:
		src, dst := roomSlot(), roomSlot()
		_, fault := room.Transfer(src, dst, quantity(), capOf(room, src))
		return fault == nil
	case 2:
		src, dst := bagSlot(), roomSlot()
		return bag.TransferWholeTo(room, src, dst, capOf(bag, src)) == nil
	case 3:
		src, dst := roomSlot(), bagSlot()
		return room.TransferWholeTo(bag, src, dst, capOf(room, src)) == nil
	case 4:
		_, fault := bag.Move(bagSlot(), bagSlot())
		return fault == nil
	default:
		_, fault := bag.SplitItem(bagSlot(), bagSlot(), quantity(), nil)
		return fault == nil
	}
}

/*
================
TestBagAndRoomOperationsConserveItems
================
*/
func TestBagAndRoomOperationsConserveItems(t *testing.T) {
	for seed := int64(1); seed <= 400; seed++ {
		r := rand.New(rand.NewSource(seed))
		bag, room := fuzzStart()
		want, problem := fuzzCensus(bag, room)
		if problem != "" {
			t.Fatal(problem)
		}
		for step := 0; step < 300; step++ {
			beforeBag, beforeRoom := bag.Items(), room.Items()
			ok := fuzzStep(r, bag, room)
			got, problem := fuzzCensus(bag, room)
			if problem != "" {
				t.Fatalf("seed %d step %d: %s\nbag %+v\nroom %+v", seed, step, problem, bag.items, room.items)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("seed %d step %d: totals %v, want %v\nbefore bag %+v room %+v\nafter bag %+v room %+v",
					seed, step, got, want, beforeBag, beforeRoom, bag.items, room.items)
			}
			if !ok && (!reflect.DeepEqual(bag.Items(), beforeBag) || !reflect.DeepEqual(room.Items(), beforeRoom)) {
				t.Fatalf("seed %d step %d: a refused operation changed the containers", seed, step)
			}
		}
	}
}
