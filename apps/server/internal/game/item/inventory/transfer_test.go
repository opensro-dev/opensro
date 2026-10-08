package inventory

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

const potionCap uint16 = 50

func potionWithCount(slot uint8, count uint16) Item {
	row := potion(slot)
	row.Quantity = count
	return row
}

// The native sub_756a60 arithmetic, all three arms.
func TestTransferSlotStackArms(t *testing.T) {
	cases := []struct {
		name          string
		source, dest  uint16
		cap           uint16
		wantDest      uint16
		wantRemainder uint16
	}{
		{"full merge", 5, 10, 50, 15, 0},
		{"exactly to the cap", 40, 10, 50, 50, 0},
		{"capped spill", 30, 40, 50, 50, 20},
		{"destination at cap swaps counts", 20, 50, 50, 20, 50},
		{"corrupt destination above cap preserves counts", 60_000, 60_001, 50, 60_000, 60_001},
		{"maximum words spill without wrapping", 65_535, 49, 50, 50, 65_534},
		{"cap zero reads as one", 1, 0, 0, 1, 0},
		// dest == cap is the counts-swap arm even at cap 1; real
		// non-stackables never reach the merge leg (the swap leg runs).
		{"at cap one swaps counts", 5, 1, 1, 5, 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := TransferSlotStack(testCase.source, testCase.dest, testCase.cap)
			if got.DestCount != testCase.wantDest || got.SourceRemainder != testCase.wantRemainder {
				t.Fatalf("TransferSlotStack(%d, %d, %d) = %+v, want dest %d remainder %d",
					testCase.source, testCase.dest, testCase.cap, got, testCase.wantDest, testCase.wantRemainder)
			}
		})
	}
}

func TestIsEtcStackableTypeFlags(t *testing.T) {
	if !IsEtcStackableTypeFlags(wire.PackTypeFlags(3, 3, 1, 1)) {
		t.Fatal("a potion word was not stackable-ETC")
	}
	if IsEtcStackableTypeFlags(wire.PackTypeFlags(3, 1, 6, 2)) {
		t.Fatal("a weapon word was stackable-ETC")
	}
}

func TestTransferMergePoursTheWholeStack(t *testing.T) {
	inv := New([]Item{potionWithCount(13, 5), potionWithCount(20, 10)}, domain.DefaultInventorySize)

	applied, fault := inv.Transfer(13, 20, 1, potionCap)
	if fault != nil {
		t.Fatalf("merge refused: %v", fault)
	}
	// The native merge IGNORES the wire quantity (1 here) and combines the
	// full counts; the source row empties and is removed.
	if applied.Leg != LegMerge {
		t.Fatalf("leg = %q, want merge", applied.Leg)
	}
	if applied.DestQuantity != 15 || applied.SourceQuantity != 0 || !applied.SourceRemoved {
		t.Fatalf("merge result = %+v, want dest 15, source 0, removed", applied)
	}
	if _, ok := inv.At(13); ok {
		t.Fatal("the emptied source row survived")
	}
	if dest, _ := inv.At(20); dest.Quantity != 15 {
		t.Fatalf("destination count = %d, want 15", dest.Quantity)
	}
}

func TestTransferMergeCappedSpillsBack(t *testing.T) {
	inv := New([]Item{potionWithCount(13, 30), potionWithCount(20, 40)}, domain.DefaultInventorySize)

	applied, fault := inv.Transfer(13, 20, 0, potionCap)
	if fault != nil {
		t.Fatalf("capped merge refused: %v", fault)
	}
	if applied.Leg != LegMergeCapped {
		t.Fatalf("leg = %q, want merge-capped", applied.Leg)
	}
	if source, _ := inv.At(13); source.Quantity != 20 {
		t.Fatalf("source remainder = %d, want 20", source.Quantity)
	}
	if dest, _ := inv.At(20); dest.Quantity != potionCap {
		t.Fatalf("destination = %d, want the cap %d", dest.Quantity, potionCap)
	}
}

func TestTransferMergeAtCapSwapsCounts(t *testing.T) {
	inv := New([]Item{potionWithCount(13, 20), potionWithCount(20, 50)}, domain.DefaultInventorySize)

	applied, fault := inv.Transfer(13, 20, 0, potionCap)
	if fault != nil {
		t.Fatalf("at-cap merge refused: %v", fault)
	}
	if applied.Leg != LegMergeSwapCounts {
		t.Fatalf("leg = %q, want merge-swapCounts", applied.Leg)
	}
	if source, _ := inv.At(13); source.Quantity != 50 {
		t.Fatalf("source count = %d, want the cap 50", source.Quantity)
	}
	if dest, _ := inv.At(20); dest.Quantity != 20 {
		t.Fatalf("destination count = %d, want the old source 20", dest.Quantity)
	}
}

func TestTransferSplitIntoEmptySlot(t *testing.T) {
	inv := New([]Item{potionWithCount(13, 20)}, domain.DefaultInventorySize)

	applied, fault := inv.Transfer(13, 21, 5, potionCap)
	if fault != nil {
		t.Fatalf("split refused: %v", fault)
	}
	if applied.Leg != LegSplit {
		t.Fatalf("leg = %q, want split", applied.Leg)
	}
	source, _ := inv.At(13)
	split, ok := inv.At(21)
	if !ok || source.Quantity != 15 || split.Quantity != 5 {
		t.Fatalf("split rows = source %d, dest %d; want 15 and 5", source.Quantity, split.Quantity)
	}
	if split.RefObjID != source.RefObjID || split.TypeFlags != source.TypeFlags {
		t.Fatal("the split row lost its identity fields")
	}
}

func TestTransferSplitQuantityValidation(t *testing.T) {
	cases := []struct {
		name       string
		quantity   uint16
		wantCode   uint8
		wantReason string
	}{
		// Zero asks for a positive number (01:29); over-stack reads
		// fewer-than-remain (01:14) - each cause keeps its matching notice.
		{"zero", 0, wire.ErrCodePositiveNumberOnly, "splitQuantityNotPositive"},
		{"above the stack", 25, wire.ErrCodeInputFewerThanRemain, "splitQuantityOverRemain"},
		// stackCount - 1 is the ceiling; the whole count is a move, tested
		// separately.
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			inv := New([]Item{potionWithCount(13, 20)}, domain.DefaultInventorySize)
			before := inv.Items()

			_, fault := inv.Transfer(13, 21, testCase.quantity, potionCap)
			if fault == nil || fault.Reason != testCase.wantReason || fault.Code != testCase.wantCode {
				t.Fatalf("quantity %d = %v, want code 0x%02X reason %s",
					testCase.quantity, fault, testCase.wantCode, testCase.wantReason)
			}
			if !reflect.DeepEqual(inv.Items(), before) {
				t.Fatal("a refused split still mutated the inventory")
			}
		})
	}
}

func TestTransferWholeStackQuantityIsAPlainMove(t *testing.T) {
	// quantity == stackCount is NOT a split: the composer auto-resolves the
	// plain two-click move of a whole stackable to the full count.
	inv := New([]Item{potionWithCount(13, 20)}, domain.DefaultInventorySize)

	applied, fault := inv.Transfer(13, 21, 20, potionCap)
	if fault != nil {
		t.Fatalf("whole-stack move refused: %v", fault)
	}
	if applied.Leg != LegMove {
		t.Fatalf("leg = %q, want move", applied.Leg)
	}
	if _, ok := inv.At(13); ok {
		t.Fatal("the source slot is still occupied after a whole-stack move")
	}
	if dest, _ := inv.At(21); dest.Quantity != 20 {
		t.Fatalf("moved count = %d, want 20", dest.Quantity)
	}
}

func TestTransferEquipmentDestinationKeepsTheSwapLeg(t *testing.T) {
	// Ammo is stackable-ETC AND equipable (the quiver socket). With an
	// equipment destination the cap is forced to 1: equipment rows are a
	// different native record, so no merge or split leg can run.
	arrows := Item{Slot: 13, RefObjID: 3900, Codename: "ITEM_ETC_AMMO_ARROW_01_DEF",
		TypeFlags: wire.PackTypeFlags(3, 3, 4, 1), Quantity: 40}
	inv := New([]Item{arrows}, domain.DefaultInventorySize)

	applied, fault := inv.Transfer(13, SocketShield, 40, potionCap)
	if fault != nil {
		t.Fatalf("equipping ammo refused: %v", fault)
	}
	if applied.Leg != LegMove {
		t.Fatalf("leg = %q, want the plain move", applied.Leg)
	}
	if worn, ok := inv.At(SocketShield); !ok || worn.Quantity != 40 {
		t.Fatalf("the quiver socket holds %+v, want the whole 40-stack", worn)
	}
}

func TestTransferRefusesSourceEqualsDest(t *testing.T) {
	// DEVIATION, documented on Transfer: the native client cannot compose a
	// self-move, and the reference fixture's merge leg would eat the row.
	inv := New([]Item{potionWithCount(13, 20)}, domain.DefaultInventorySize)

	if _, fault := inv.Transfer(13, 13, 0, potionCap); fault == nil || fault.Reason != "slotOutOfRange" {
		t.Fatalf("a self-move = %v, want slotOutOfRange", fault)
	}
}

func TestDropQuantityPartialLeavesTheRemainder(t *testing.T) {
	inv := New([]Item{potionWithCount(13, 20)}, domain.DefaultInventorySize)

	dropped, fault := inv.DropQuantity(13, 5)
	if fault != nil {
		t.Fatalf("partial drop refused: %v", fault)
	}
	if dropped.Quantity != 5 {
		t.Fatalf("dropped count = %d, want 5", dropped.Quantity)
	}
	if remaining, _ := inv.At(13); remaining.Quantity != 15 {
		t.Fatalf("remaining count = %d, want 15", remaining.Quantity)
	}
}

func TestDropQuantityWholeStackRemovesTheRow(t *testing.T) {
	inv := New([]Item{potionWithCount(13, 20)}, domain.DefaultInventorySize)

	dropped, fault := inv.DropQuantity(13, 20)
	if fault != nil {
		t.Fatalf("whole drop refused: %v", fault)
	}
	if dropped.Quantity != 20 {
		t.Fatalf("dropped count = %d, want 20", dropped.Quantity)
	}
	if _, ok := inv.At(13); ok {
		t.Fatal("the dropped row survived")
	}
}

func TestDropQuantityBounds(t *testing.T) {
	inv := New([]Item{potionWithCount(13, 20)}, domain.DefaultInventorySize)

	if _, fault := inv.DropQuantity(13, 0); fault == nil || fault.Reason != "dropQuantityNotPositive" || fault.Code != wire.ErrCodePositiveNumberOnly {
		t.Fatalf("dropQuantity 0 = %v, want [29 dropQuantityNotPositive] (a zero must refuse, not clamp)", fault)
	}
	if _, fault := inv.DropQuantity(13, 21); fault == nil || fault.Reason != "dropQuantityOverStack" || fault.Code != wire.ErrCodeInputFewerThanRemain {
		t.Fatalf("dropQuantity above the stack = %v, want [14 dropQuantityOverStack]", fault)
	}
}

func TestGrantStackMergesOntoTheLowestSlot(t *testing.T) {
	inv := New([]Item{potionWithCount(20, 10), potionWithCount(14, 10)}, domain.DefaultInventorySize)

	grant, fault := inv.GrantStack(potionWithCount(0, 5), potionCap)
	if fault != nil {
		t.Fatalf("grant refused: %v", fault)
	}
	if !grant.Merged || grant.DestSlot != 14 {
		t.Fatalf("grant = %+v, want a merge onto the LOWEST slot 14", grant)
	}
	if grant.PostMergeCount != 15 || grant.GroundRemainder != 0 {
		t.Fatalf("grant = %+v, want post 15, remainder 0", grant)
	}
	if row, _ := inv.At(14); row.Quantity != 15 {
		t.Fatalf("slot 14 count = %d, want 15", row.Quantity)
	}
	if row, _ := inv.At(20); row.Quantity != 10 {
		t.Fatalf("slot 20 count = %d, want the untouched 10", row.Quantity)
	}
}

func TestGrantStackSkipsRowsAtTheCap(t *testing.T) {
	inv := New([]Item{potionWithCount(14, 50), potionWithCount(20, 10)}, domain.DefaultInventorySize)

	grant, fault := inv.GrantStack(potionWithCount(0, 5), potionCap)
	if fault != nil {
		t.Fatalf("grant refused: %v", fault)
	}
	if grant.DestSlot != 20 {
		t.Fatalf("dest slot = %d, want 20 (slot 14 is at cap)", grant.DestSlot)
	}
}

func TestGrantStackOverCapMergeLeavesTheGroundRemainder(t *testing.T) {
	inv := New([]Item{potionWithCount(14, 45)}, domain.DefaultInventorySize)

	grant, fault := inv.GrantStack(potionWithCount(0, 10), potionCap)
	if fault != nil {
		t.Fatalf("grant refused: %v", fault)
	}
	if grant.PostMergeCount != 50 || grant.GroundRemainder != 5 {
		t.Fatalf("grant = %+v, want post 50, remainder 5 back to the ground", grant)
	}
}

func TestGrantStackFreshSlotClampsAtTheCap(t *testing.T) {
	inv := New(nil, domain.DefaultInventorySize)

	grant, fault := inv.GrantStack(potionWithCount(0, 60), potionCap)
	if fault != nil {
		t.Fatalf("grant refused: %v", fault)
	}
	if grant.Merged {
		t.Fatal("an empty inventory reported a merge")
	}
	if grant.DestSlot != 13 || grant.PostMergeCount != 50 || grant.GroundRemainder != 10 {
		t.Fatalf("grant = %+v, want slot 13, post 50, remainder 10", grant)
	}
	if row, _ := inv.At(13); row.Quantity != 50 {
		t.Fatalf("granted row count = %d, want 50", row.Quantity)
	}
}

func TestGrantStackFullBagStillMerges(t *testing.T) {
	// Every bag slot taken; one row is a same-ref stack below cap. The
	// merge gate runs before the free-slot check, so the pickup fits.
	items := make([]Item, 0, int((domain.DefaultInventorySize - EquipmentSlotEnd)))
	items = append(items, potionWithCount(13, 10))
	for wireSlot := uint8(14); wireSlot < domain.DefaultInventorySize; wireSlot++ {
		items = append(items, sword(wireSlot))
	}
	inv := New(items, domain.DefaultInventorySize)

	grant, fault := inv.GrantStack(potionWithCount(0, 5), potionCap)
	if fault != nil {
		t.Fatalf("a merge into a full bag was refused: %v", fault)
	}
	if !grant.Merged || grant.DestSlot != 13 || grant.PostMergeCount != 15 {
		t.Fatalf("grant = %+v, want a merge onto slot 13 totalling 15", grant)
	}

	// A non-mergeable item still refuses on the fresh-slot leg.
	if _, fault := inv.GrantStack(sword(0), 1); fault == nil || fault.Reason != "inventoryFull" {
		t.Fatalf("granting into a full bag = %v, want inventoryFull", fault)
	}
}

// The M1 chain's occupancy source: what each touched socket ended up holding.
func TestEquipVisualChanges(t *testing.T) {
	t.Run("equip reports the worn occupant", func(t *testing.T) {
		inv := New([]Item{sword(13)}, domain.DefaultInventorySize)
		if _, fault := inv.Transfer(13, SocketWeapon, 1, 1); fault != nil {
			t.Fatalf("equip refused: %v", fault)
		}
		changes := inv.EquipVisualChanges(13, SocketWeapon)
		if len(changes) != 1 || !changes[0].Worn || changes[0].Socket != SocketWeapon || changes[0].Item.RefObjID != 11459 {
			t.Fatalf("changes = %+v, want the weapon socket worn by the sword", changes)
		}
	})

	t.Run("unequip reports the vacated socket", func(t *testing.T) {
		inv := New([]Item{sword(SocketWeapon)}, domain.DefaultInventorySize)
		if _, fault := inv.Transfer(SocketWeapon, 20, 1, 1); fault != nil {
			t.Fatalf("unequip refused: %v", fault)
		}
		changes := inv.EquipVisualChanges(SocketWeapon, 20)
		if len(changes) != 1 || changes[0].Worn || changes[0].Socket != SocketWeapon {
			t.Fatalf("changes = %+v, want the weapon socket reported empty", changes)
		}
	})

	t.Run("a ring hand swap reports both sockets post-move", func(t *testing.T) {
		left := Item{Slot: SocketRing, RefObjID: 2000, TypeFlags: wire.PackTypeFlags(3, 1, 5, 3), Quantity: 1}
		right := Item{Slot: SocketRingSecond, RefObjID: 2001, TypeFlags: wire.PackTypeFlags(3, 1, 5, 3), Quantity: 1}
		inv := New([]Item{left, right}, domain.DefaultInventorySize)
		if _, fault := inv.Transfer(SocketRing, SocketRingSecond, 1, 1); fault != nil {
			t.Fatalf("ring swap refused: %v", fault)
		}
		changes := inv.EquipVisualChanges(SocketRing, SocketRingSecond)
		if len(changes) != 2 {
			t.Fatalf("changes = %+v, want both hands, source first", changes)
		}
		if changes[0].Socket != SocketRing || changes[0].Item.RefObjID != 2001 {
			t.Fatalf("first hand = %+v, want the swapped-in 2001", changes[0])
		}
		if changes[1].Socket != SocketRingSecond || changes[1].Item.RefObjID != 2000 {
			t.Fatalf("second hand = %+v, want the swapped-in 2000", changes[1])
		}
	})

	t.Run("a bag-only move reports nothing", func(t *testing.T) {
		inv := New([]Item{sword(13)}, domain.DefaultInventorySize)
		if changes := inv.EquipVisualChanges(13, 20); len(changes) != 0 {
			t.Fatalf("changes = %+v, want none for a bag-only move", changes)
		}
	})
}

func TestMergeTargetSlot(t *testing.T) {
	inv := New([]Item{potionWithCount(20, 10), potionWithCount(14, 50), sword(SocketWeapon)}, domain.DefaultInventorySize)

	if slot, ok := inv.MergeTargetSlot(3630, potionCap); !ok || slot != 20 {
		t.Fatalf("merge target = (%d, %v), want slot 20 (14 is at cap)", slot, ok)
	}
	if _, ok := inv.MergeTargetSlot(11459, 1); ok {
		t.Fatal("a non-stackable found a merge target")
	}
}
