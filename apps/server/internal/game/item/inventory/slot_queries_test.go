package inventory

import (
	"bufio"
	"fmt"
	"opensro.online/server/internal/testsupport/licensed"
	"strings"
	"testing"
)

func TestSlotQueriesAgainstNative(t *testing.T) {
	f, err := licensed.ServerFixtureReader(t, "internal/game/item/inventory/testdata/native-inventory-slots-20260921.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	checked := 0
	for s.Scan() {
		var op, cap, mask, a, b, selector, expected, expectedSlot int32
		if _, err := fmt.Fscan(strings.NewReader(s.Text()), &op, &cap, &mask, &a, &b, &selector, &expected, &expectedSlot); err != nil {
			t.Fatal(err)
		}
		if op != 5 && op != 6 && op != 7 && op != 8 {
			continue
		}
		inv := &Inventory{slotEnd: uint8(cap)}
		for i := int32(0); i < cap; i++ {
			if mask&(1<<i) != 0 {
				inv.items = append(inv.items, Item{Slot: uint8(i), RefObjID: uint32(i + 1), Quantity: 1})
			}
		}
		var result int32
		slot := uint8(123)
		switch op {
		case 5:
			result = inv.CountSlots(a, b, selector)
		case 6:
			var row Item
			row, slot, _ = inv.FirstOccupied(a, b)
			result = int32(row.RefObjID)
		case 7:
			if inv.HasItemFrom(a) {
				result = 1
			}
		case 8:
			free, _ := inv.FirstEmpty(a)
			result = int32(free)
		}
		if result != expected || int32(slot) != expectedSlot {
			t.Fatalf("native mismatch %s: %d slot %d", s.Text(), result, slot)
		}
		checked++
	}
	if s.Err() != nil {
		t.Fatal(s.Err())
	}
	if checked == 0 {
		t.Fatal("no comparisons")
	}
	t.Logf("%d native query traces", checked)
}
