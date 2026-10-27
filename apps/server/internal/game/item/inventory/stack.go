package inventory

import "opensro.online/server/internal/game/item/wire"

// Stack arithmetic for the split and merge flows.
//
// The per-item stack ceiling comes from the itemdata MaxStack column, which
// this tree has no reader for, so every function here takes the cap as a
// parameter rather than pretending to know it. Callers pass the cap from
// whatever reference-data source they have; a cap of 0 or 1 means the item
// does not stack.

// IsEtcStackableTypeFlags is the native stackable-ETC family mask: the exact
// predicate sub_4fb260 / RefItemKindWord_IsEtcClass applies to the RefItemData
// type word, and the same three compares sub_756a60 inlines when it decides
// whether a slot-to-slot transfer may MERGE. Callers use it to source the
// stack cap: an item outside the class carries cap 1 regardless of its
// itemdata MaxStack column.
func IsEtcStackableTypeFlags(typeFlags uint16) bool {
	return typeFlags&0x02 == 0 && typeFlags&0x1C == 0x0C && typeFlags&0x60 == 0x60
}

// SlotStackTransfer is the outcome of the native transfer arithmetic:
// ABSOLUTE post-transfer counts, never deltas - the 0xB06D row assigns counts
// client-side, so a delta on the wire would double-apply.
type SlotStackTransfer struct {
	// DestCount is the destination row's post-transfer count.
	DestCount uint16
	// SourceRemainder is what stays on the source side. Zero means the
	// source emptied (a full merge); for a ground pickup a non-zero
	// remainder is what leaves the heap on the ground.
	SourceRemainder uint16
}

// TransferSlotStack is the MERGE leg of native sub_756a60 /
// CNetProcessInner_TransferSlotStack (@0x00756ad9), the single primitive that
// decides how two stacks combine:
//
//	total = dest + source
//	total <= iMax : dest = total, source empties (the full merge "move")
//	total >  iMax : dest != iMax -> dest = iMax, source = total - iMax
//	                dest == iMax -> dest = source, source = iMax (counts swap)
//
// An empty/stale destination is normalized to 0 first, so a merge cannot
// inherit garbage.
func TransferSlotStack(sourceCount, destCount, stackCap uint16) SlotStackTransfer {
	if stackCap < 1 {
		stackCap = 1
	}

	// A row above its reference-data cap is corrupted authority state. Do
	// not pour into it: swapping the two stored words is the only result
	// this value-only primitive can return that preserves every unit and
	// cannot overflow. Normal rows at the cap take the same native arm.
	if destCount >= stackCap {
		return SlotStackTransfer{DestCount: sourceCount, SourceRemainder: destCount}
	}

	room := stackCap - destCount
	if sourceCount <= room {
		return SlotStackTransfer{DestCount: destCount + sourceCount}
	}
	return SlotStackTransfer{
		DestCount:       stackCap,
		SourceRemainder: sourceCount - room,
	}
}

// MergeResult describes the outcome of pouring one stack into another.
type MergeResult struct {
	// Moved is how many units left the source.
	Moved uint16
	// SourceQuantity is what remains on the source row. Zero means the source
	// row is now empty and should be removed.
	SourceQuantity uint16
	// DestQuantity is the destination row's new count.
	DestQuantity uint16
	// Complete is true when the source row ends up empty, which is the signal
	// to remove it. Equivalently: the whole source stack fit.
	Complete bool
}

// MergeStacks pours requested units from a source stack into a destination
// stack without exceeding stackCap.
//
// Passing 0 for requested means "as much as will fit". Anything that does not
// fit stays on the source row, which is why a merge can legitimately leave
// both rows occupied.
//
// NOTE: the native type-0x00 merge IGNORES the wire quantity and combines the
// FULL counts through TransferSlotStack (which also has the at-cap
// counts-swap arm this function lacks) - the move path uses that primitive.
// MergeStacks survives for callers that need a requested-quantity pour.
func MergeStacks(sourceQuantity, destQuantity, requested, stackCap uint16) MergeResult {
	var out MergeResult
	out.SourceQuantity = sourceQuantity
	out.DestQuantity = destQuantity

	if stackCap < 1 {
		stackCap = 1
	}
	if sourceQuantity == 0 || destQuantity >= stackCap {
		out.Complete = out.SourceQuantity == 0
		return out
	}

	moving := sourceQuantity
	if requested > 0 && requested < moving {
		moving = requested
	}
	if room := stackCap - destQuantity; moving > room {
		moving = room
	}

	out.Moved = moving
	out.SourceQuantity = sourceQuantity - moving
	out.DestQuantity = destQuantity + moving
	out.Complete = out.SourceQuantity == 0
	return out
}

// SplitStack takes requested units off a stack for the quantity-msgbox split
// flow.
//
// A split must leave something behind: taking the whole stack is a plain move,
// not a split, and the native quantity dialog caps its spinner at one below
// the stack count for exactly that reason.
func SplitStack(sourceQuantity, requested uint16) (uint16, uint16, *Fault) {
	if requested == 0 {
		return sourceQuantity, 0, newFault(wire.ErrCodePositiveNumberOnly, "invalidSplitQuantity")
	}
	if requested >= sourceQuantity {
		return sourceQuantity, 0, newFault(wire.ErrCodeInputFewerThanRemain, "splitExceedsStack")
	}
	return sourceQuantity - requested, requested, nil
}
