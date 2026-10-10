/*
===========================================================================

stonerollback_test.go - native singleton transfers and retained stone stock

Whole-container transfers preserve native count exchange at cap one while
operator-created stacks retain their assimilation values after rollback.

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
TestStoneWholeTransferCapPolicy
================
*/
func TestStoneWholeTransferCapPolicy(t *testing.T) {
	for _, subtype := range []uint8{1, 2} {
		for _, tc := range []struct {
			name                          string
			cap, sourceCount, targetCount uint16
			targetPlus                    uint8
			swapRows                      bool
		}{
			{"native-singles", 1, 1, 1, 90, false},
			{"active-singles", stoneTestCap, 1, 1, 90, true},
			{"active-stacks", stoneTestCap, 5, 2, 90, true},
			{"rollback-same-value", 1, 5, 1, 40, false},
			{"rollback-source-stack", 1, 5, 1, 90, true},
			{"rollback-target-stack", 1, 1, 5, 90, true},
			{"rollback-both-stacks", 1, 5, 2, 90, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				source := stone(EquipmentSlotEnd, 40, tc.sourceCount)
				source.TypeFlags = wire.PackTypeFlags(3, 3, 11, subtype)
				target := source
				target.Slot, target.Plus, target.Quantity, target.RecordID = 0, tc.targetPlus, tc.targetCount, 2
				bag := New([]Item{source}, domain.DefaultInventorySize)
				room, fault := NewStorageRoom([]Item{target}, 4)
				if fault != nil {
					t.Fatal(fault)
				}
				if fault = bag.TransferWholeTo(room, source.Slot, target.Slot, tc.cap); fault != nil {
					t.Fatal(fault)
				}
				wantSource, wantTarget := source, target
				if tc.swapRows {
					wantSource, wantTarget = target, source
					wantSource.Slot, wantTarget.Slot = source.Slot, target.Slot
				} else {
					wantSource.Quantity, wantTarget.Quantity = target.Quantity, source.Quantity
				}
				if !reflect.DeepEqual(bag.Items(), []Item{wantSource}) || !reflect.DeepEqual(room.Items(), []Item{wantTarget}) {
					t.Fatalf("subtype %d: bag %+v room %+v; want %+v %+v", subtype, bag.Items(), room.Items(), wantSource, wantTarget)
				}
			})
		}
	}
}

/*
================
TestStoneRollbackFamiliesSplitAndTransfer
================
*/
func TestStoneRollbackFamiliesSplitAndTransfer(t *testing.T) {
	for _, subtype := range []uint8{1, 2, 7} {
		source := stone(EquipmentSlotEnd, 70, 5)
		source.TypeFlags = wire.PackTypeFlags(3, 3, 11, subtype)
		if subtype == 7 {
			source.Plus = 0
		}
		bag := New([]Item{source}, domain.DefaultInventorySize)
		result, fault := bag.Transfer(source.Slot, source.Slot+1, 2, 1)
		if fault != nil || result.Leg != LegSplit {
			t.Fatalf("subtype %d: split %+v %v", subtype, result, fault)
		}
		a, _ := bag.At(source.Slot)
		b, _ := bag.At(source.Slot + 1)
		if a.Quantity != 3 || b.Quantity != 2 || a.Plus != source.Plus || b.Plus != source.Plus {
			t.Fatalf("subtype %d: split changed stock: %+v", subtype, bag.Items())
		}
		for _, cap := range []uint16{1, 2} {
			bag = New([]Item{source}, domain.DefaultInventorySize)
			target := source
			target.Slot, target.Quantity = 0, 1
			room, fault := NewContainer([]Item{target}, 4)
			if fault != nil {
				t.Fatal(fault)
			}
			if fault = bag.TransferWholeTo(room, source.Slot, target.Slot, cap); fault != nil {
				t.Fatal(fault)
			}
			a, _ = bag.At(source.Slot)
			b, _ = room.At(target.Slot)
			wantSource, wantTarget := uint16(1), uint16(5)
			if cap == 2 {
				wantSource, wantTarget = 4, 2
			}
			if a.Quantity != wantSource || b.Quantity != wantTarget || a.Plus != source.Plus || b.Plus != source.Plus {
				t.Fatalf("subtype %d cap %d: transfer changed stock: %+v %+v", subtype, cap, a, b)
			}
		}
	}
}
