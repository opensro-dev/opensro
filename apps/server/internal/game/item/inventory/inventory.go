/*
===========================================================================

inventory.go - owns portable item rows and atomic slot operations

===========================================================================
*/
package inventory

import (
	"fmt"
	"opensro.online/server/internal/domain"

	"opensro.online/server/internal/game/item/wire"
)

// Fault is a refused operation: the native 0xB06D error code the client should
// be told, plus the machine-readable reason the fixture logs.
//
// Operations return *Fault rather than error so that a nil result is
// unambiguously "allowed" - a typed nil in an error interface would compare
// non-nil and silently invert every check.
/*
================
Fault
================
*/
type Fault struct {
	// Code is the byte sent as [0x02][Code].
	Code uint8
	// Reason names the specific rule that refused, matching the fixture's
	// reason strings so server logs line up across the two implementations.
	Reason string
}

/*
================
Error
================
*/
func (f *Fault) Error() string {
	return fmt.Sprintf("inventory: %s (0xB06D error 0x%02X)", f.Reason, f.Code)
}

// NewFault builds a refusal for gate implementations living outside this
// package (the action equip-requirement checker).
/*
================
NewFault
================
*/
func NewFault(code uint8, reason string) *Fault {
	return newFault(code, reason)
}

/*
================
newFault
================
*/
func newFault(code uint8, reason string) *Fault {
	return &Fault{Code: code, Reason: reason}
}

// Item is one inventory row.
/*
================
Item
================
*/
type Item struct {
	TradeOwner string // original job alias, preserved when the holder changes
	Summon     *domain.CharacterCOS
	// Slot is the wire slot the row currently occupies.
	Slot         uint8
	RefObjID     uint32
	Codename     string
	TypeFlags    uint16
	Plus         uint8
	VarianceBits uint64
	Durability   uint32
	// Quantity is the stack count. Non-stacking items carry 1.
	Quantity uint16
	// MagicOptions are the encoded magic-option u64 params the row's
	// CSOItem body carries (wire.ItemBody.MagicOptions). Only
	// equipment-family rows carry them; they ride every move/drop/pickup
	// so a bag shuffle can never strip a persisted option.
	MagicOptions      []uint64
	TransformRefObjID uint32 // monster capsule Data
	// RecordID projects the native item-record +20/+24 search identity when
	// supplied by an owner. Its DB allocation/load lifecycle is not implemented
	// by this field. A newly split row starts at zero; RefObjID is not a substitute.
	RecordID uint64
}

// Body returns the CSOItem body for this row, the remainder of a type-0x06
// pickup grant.
/*
================
Body
================
*/
func (i Item) Body() wire.ItemBody {
	return wire.ItemBody{
		TradeOwner:        i.TradeOwner,
		RefObjID:          i.RefObjID,
		TypeFlags:         i.TypeFlags,
		Plus:              i.Plus,
		VarianceBits:      i.VarianceBits,
		Durability:        i.Durability,
		Quantity:          i.Quantity,
		MagicOptions:      append([]uint64(nil), i.MagicOptions...),
		TransformRefObjID: i.TransformRefObjID, Summon: domain.CloneCOS(i.Summon),
	}
}

// Inventory is a character's authoritative slot map.
//
// Rows are held in a slice keyed by their Slot field rather than a fixed
// array, matching the fixture's persisted shape, where absent slots simply
// have no row.
/*
================
Inventory
================
*/
type Inventory struct {
	items             []Item
	bagStart, slotEnd uint8

	// Requirements gates every item entering an equipment socket with the
	// character-vs-itemdata checks (level/stats/gender/country - see
	// EquipRequirements). Installed by the action runtime after New; nil
	// (tests, degraded no-textdata boots) passes everything.
	Requirements EquipRequirements
}

// New returns an Inventory holding a copy of items.
/*
================
New
================
*/
func New(items []Item) *Inventory {
	owned := make([]Item, len(items))
	for n := range items {
		owned[n] = cloneInventoryRow(items[n])
	}
	return &Inventory{items: owned, bagStart: EquipmentSlotEnd, slotEnd: BagSlotEnd}
}

// NewContainer shares transfer arithmetic with player bags while declaring
// that slot zero is storage, not an equipment socket.
/*
================
NewContainer
================
*/
func NewContainer(items []Item, capacity uint8) (*Inventory, *Fault) {
	return newBoundedContainer(items, capacity, cosContainerMaxSlots)
}

// cosContainerMaxSlots bounds a COS bag.
const cosContainerMaxSlots = 140

/*
================
NewStorageRoom

The account warehouse: a slot-0-based container whose capacity is the
storage list's byte (up to 255 slots).
================
*/
func NewStorageRoom(items []Item, capacity uint8) (*Inventory, *Fault) {
	return newBoundedContainer(items, capacity, 255)
}

/*
================
newBoundedContainer
================
*/
func newBoundedContainer(items []Item, capacity uint8, maxSlots int) (*Inventory, *Fault) {
	if capacity == 0 || int(capacity) > maxSlots {
		return nil, newFault(wire.ErrCodeInvalidRequest, "absentContainer")
	}
	seen := make(map[uint8]bool, len(items))
	for _, row := range items {
		if row.Slot >= capacity || seen[row.Slot] || row.Quantity == 0 || summonerTransferFault(row) != nil {
			return nil, newFault(wire.ErrCodeInvalidRequest, "invalidContainerRows")
		}
		seen[row.Slot] = true
	}
	inv := New(items)
	inv.bagStart, inv.slotEnd = 0, capacity
	return inv, nil
}

/*
================
validSlot
================
*/
func (inv *Inventory) validSlot(n uint8) bool     { return n < inv.slotEnd }
func (inv *Inventory) equipmentSlot(n uint8) bool { return n < inv.bagStart }
func (inv *Inventory) bagSlot(n uint8) bool       { return n >= inv.bagStart && n < inv.slotEnd }

// Items returns a copy of the rows.
/*
================
Items
================
*/
func (inv *Inventory) Items() []Item {
	out := make([]Item, len(inv.items))
	for n := range inv.items {
		out[n] = cloneInventoryRow(inv.items[n])
	}
	return out
}

// Len reports how many rows are occupied.
/*
================
Len
================
*/
func (inv *Inventory) Len() int {
	return len(inv.items)
}

// At returns the row occupying a wire slot.
/*
================
At
================
*/
func (inv *Inventory) At(wireSlot uint8) (Item, bool) {
	if index := inv.indexOf(wireSlot); index >= 0 {
		return cloneInventoryRow(inv.items[index]), true
	}
	return Item{}, false
}

/*
================
indexOf
================
*/
func (inv *Inventory) indexOf(wireSlot uint8) int {
	for index := range inv.items {
		if inv.items[index].Slot == wireSlot {
			return index
		}
	}
	return -1
}

// FirstFreeBagSlot returns the lowest unoccupied bag wire slot. The second
// result is false when the bag is full.
/*
================
FirstFreeBagSlot
================
*/
func (inv *Inventory) FirstFreeBagSlot() (uint8, bool) {
	slot, ok := inv.FirstEmpty(int32(inv.bagStart))
	if !ok {
		return 0, false
	} // Preserve the public API's failure value.
	return slot, true
}

// MoveResult describes an applied type-0x00 move.
/*
================
MoveResult
================
*/
type MoveResult struct {
	SourceSlot uint8
	DestSlot   uint8
	// Swapped is true when the destination was occupied and its row moved
	// back into the source slot.
	Swapped bool
}

// Move applies a type-0x00 move for a non-stacking row: a plain move or swap
// with the full equip gates. It is Transfer with the swap leg forced (cap 1);
// stackable rows go through Transfer with their real cap so the merge and
// split legs can run.
/*
================
Move
================
*/
func (inv *Inventory) Move(sourceSlot, destSlot uint8) (MoveResult, *Fault) {
	var out MoveResult

	applied, fault := inv.Transfer(sourceSlot, destSlot, 0, 1)
	if fault != nil {
		return out, fault
	}
	out.SourceSlot = applied.SourceSlot
	out.DestSlot = applied.DestSlot
	out.Swapped = applied.Leg == LegSwap
	return out, nil
}

// TransferLeg names which native sub_756a60 leg an applied type-0x00 transfer
// took. The values match the reference fixture's log strings so server logs
// line up across the two implementations.
type TransferLeg string

const (
	// LegMove is the plain move into an empty destination.
	LegMove TransferLeg = "move"
	// LegSwap is the full swap with an occupied destination (also how an
	// equip and its swap-back seat).
	LegSwap TransferLeg = "swap"
	// LegMerge poured the whole source stack in; the source row is gone.
	LegMerge TransferLeg = "merge"
	// LegMergeCapped filled the destination to the cap and spilled the
	// remainder back into the source slot.
	LegMergeCapped TransferLeg = "merge-capped"
	// LegMergeSwapCounts hit a destination already AT the cap: the two
	// counts swap.
	LegMergeSwapCounts TransferLeg = "merge-swapCounts"
	// LegSplit took part of the source stack into an empty slot.
	LegSplit TransferLeg = "split"
)

// TransferResult describes an applied type-0x00 transfer.
/*
================
TransferResult
================
*/
type TransferResult struct {
	SourceSlot uint8
	DestSlot   uint8
	Leg        TransferLeg
	// SourceQuantity and DestQuantity are the ABSOLUTE post-transfer counts.
	// SourceQuantity is zero when the source row emptied.
	SourceQuantity uint16
	DestQuantity   uint16
	// SourceRemoved is true when the source ROW was deleted outright, which
	// only a full merge does. A move or swap keeps the row (at its new
	// slot); whether the source SLOT emptied follows from Leg.
	SourceRemoved bool
}

// Transfer applies a type-0x00 item operation with the full native routing:
// the equip gates on both directions, then the merge/split/swap legs of
// sub_756a60 / CNetProcessInner_TransferSlotStack.
//
// quantity is the wire quantity field. It only drives the SPLIT leg - the
// native merge ignores it and combines the full counts - and a whole-stack
// quantity into an empty slot is a plain move, not a split.
//
// stackCap is the source item's iMax (RefItemData+0x1a8, the itemdata
// MaxStack column), which this package has no reader for; callers source it
// from their reference data, gated on IsEtcStackableTypeFlags (an item
// outside the stackable-ETC class carries cap 1). The cap is forced to 1
// unless BOTH slots are in the bag band: equipment rows are a different
// native record and keep the plain swap.
//
// Equipment gates, in fixture order:
//
//   - a destination in 0..12 validates the source item's socket class
//     (EquipSocketForTypeFlags) and the clothes/hard exclusivity
//     (ClothesHardExclusivityConflict, bug C).
//   - a source in 0..12 with an OCCUPIED destination is an equip too: the
//     swap seats the occupant in the vacated socket (sub_756a60's full swap
//     writes BOTH records), so the occupant faces the same socket-class and
//     exclusivity gates against the vacated slot. Without this the measured
//     back door put a chest piece in the weapon socket.
//
// DEVIATION: sourceSlot == destSlot is refused. The native client cannot
// compose it (a drag onto itself is a no-op), and the reference fixture's
// merge leg would eat the row.
/*
================
Transfer
================
*/
func (inv *Inventory) Transfer(sourceSlot, destSlot uint8, quantity uint16, stackCap uint16) (TransferResult, *Fault) {
	var out TransferResult

	if !inv.validSlot(sourceSlot) || !inv.validSlot(destSlot) || sourceSlot == destSlot {
		return out, newFault(wire.ErrCodeInvalidRequest, "slotOutOfRange")
	}
	sourceIndex := inv.indexOf(sourceSlot)
	if sourceIndex < 0 {
		return out, newFault(wire.ErrCodeInvalidRequest, "sourceSlotEmpty")
	}

	// Forward equip gates: the source item is moving INTO a socket. The
	// requirement checks bracket the exclusivity scan in the native
	// full-mask order (sub_789c60: level/stats/gender -> 0x080 -> country).
	if inv.equipmentSlot(destSlot) {
		socket, equipable := EquipSocketForTypeFlags(inv.items[sourceIndex].TypeFlags)
		if !equipable {
			return out, newFault(wire.ErrCodeCantEquip, "notEquipable")
		}
		if !SocketAccepts(socket, destSlot) {
			return out, newFault(wire.ErrCodeCantEquip, "wrongSocket")
		}
		if inv.Requirements != nil {
			if fault := inv.Requirements.PreExclusivity(inv.items[sourceIndex]); fault != nil {
				return out, fault
			}
		}
		if _, conflict := ClothesHardExclusivityConflict(inv.items, inv.items[sourceIndex].TypeFlags); conflict {
			return out, newFault(wire.ErrCodeExclusiveArmorMix, "clothesHardArmorExclusivity")
		}
		if inv.Requirements != nil {
			if fault := inv.Requirements.PostExclusivity(inv.items[sourceIndex]); fault != nil {
				return out, fault
			}
		}
	}

	destIndex := inv.indexOf(destSlot)

	// Swap-back equip gates: moving OUT of a socket onto an occupied slot
	// seats that occupant in the socket, so the occupant is an incoming
	// equip too. The exclusivity scan runs against the CURRENT worn rows,
	// the vacating source piece included - native walks all six sockets
	// unconditionally, with no departing-socket exclusion.
	if inv.equipmentSlot(sourceSlot) && destIndex >= 0 {
		socket, equipable := EquipSocketForTypeFlags(inv.items[destIndex].TypeFlags)
		if !equipable {
			return out, newFault(wire.ErrCodeCantEquip, "notEquipable")
		}
		if !SocketAccepts(socket, sourceSlot) {
			return out, newFault(wire.ErrCodeCantEquip, "wrongSocket")
		}
		if inv.Requirements != nil {
			if fault := inv.Requirements.PreExclusivity(inv.items[destIndex]); fault != nil {
				return out, fault
			}
		}
		if _, conflict := ClothesHardExclusivityConflict(inv.items, inv.items[destIndex].TypeFlags); conflict {
			return out, newFault(wire.ErrCodeExclusiveArmorMix, "clothesHardArmorExclusivity")
		}
		if inv.Requirements != nil {
			if fault := inv.Requirements.PostExclusivity(inv.items[destIndex]); fault != nil {
				return out, fault
			}
		}
	}

	if fault := summonerTransferFault(inv.items[sourceIndex]); fault != nil {
		return out, fault
	}
	if destIndex >= 0 {
		if fault := summonerTransferFault(inv.items[destIndex]); fault != nil {
			return out, fault
		}
	}
	if wire.IsCosSummoner(inv.items[sourceIndex].TypeFlags) || (destIndex >= 0 && wire.IsCosSummoner(inv.items[destIndex].TypeFlags)) {
		stackCap = 1
	}
	bothInBag := inv.bagSlot(sourceSlot) && inv.bagSlot(destSlot)
	cap := stackCap
	if !bothInBag || cap < 1 {
		cap = 1
	}
	stackable := cap > 1
	sourceCount := inv.items[sourceIndex].Quantity
	if sourceCount == 0 {
		sourceCount = 1
	}

	// SPLIT-REQUEST VALIDATION. The native quantity dialog can only emit
	// 1..stackCount-1 (the spinner caps at one below the stack), so any
	// other partial amount targeting an EMPTY bag slot is malformed; without
	// this guard it would fall through to the move leg and silently become
	// a whole-stack move. quantity == stackCount is NOT a split: that is the
	// plain two-click move of a whole stackable, and it must keep reaching
	// the swap/move leg.
	if bothInBag && stackable && destIndex < 0 && quantity != sourceCount {
		// Per-cause notice bytes (client sub_689420 copy): zero asks for a
		// positive number (01:29), over-stack says fewer-than-remain
		// (01:14). Retail UI clamps the divide input to [1, stack-1] before
		// composing, so both are hostile/malformed-packet guards - the
		// split keeps the semantically matching notice for each cause
		// instead of collapsing both onto 0x14.
		if quantity < 1 {
			return out, newFault(wire.ErrCodePositiveNumberOnly, "splitQuantityNotPositive")
		}
		if quantity > sourceCount-1 {
			return out, newFault(wire.ErrCodeInputFewerThanRemain, "splitQuantityOverRemain")
		}
	}

	out.SourceSlot = sourceSlot
	out.DestSlot = destSlot

	switch {
	// MERGE: same active ref id, stackable class. Ignores the wire quantity
	// and combines the FULL counts, exactly like native - the qty field only
	// drives the split leg.
	case stackable && destIndex >= 0 && stackIdentityMatches(inv.items[destIndex], inv.items[sourceIndex]):
		destCount := inv.items[destIndex].Quantity
		if destCount == 0 {
			destCount = 1
		}
		transfer := TransferSlotStack(sourceCount, destCount, cap)
		switch {
		case transfer.SourceRemainder == 0:
			// The destination takes everything and the source slot empties.
			out.Leg = LegMerge
			out.SourceRemoved = true
		case destCount != cap:
			// Fill the destination, spill the remainder back to the source.
			out.Leg = LegMergeCapped
		default:
			// A destination already AT the cap swaps counts.
			out.Leg = LegMergeSwapCounts
		}
		inv.items[destIndex].Quantity = transfer.DestCount
		out.DestQuantity = transfer.DestCount
		out.SourceQuantity = transfer.SourceRemainder
		if out.SourceRemoved {
			inv.items = append(inv.items[:sourceIndex], inv.items[sourceIndex+1:]...)
		} else {
			inv.items[sourceIndex].Quantity = transfer.SourceRemainder
		}

	// PARTIAL SPLIT: only into an EMPTY slot and only for less than the
	// whole stack.
	case stackable && destIndex < 0 && quantity > 0 && quantity < sourceCount:
		splitRow := splitInventoryRow(inv.items[sourceIndex], destSlot, quantity)
		inv.items = append(inv.items, splitRow)
		inv.items[sourceIndex].Quantity = sourceCount - quantity
		out.Leg = LegSplit
		out.SourceQuantity = sourceCount - quantity
		out.DestQuantity = quantity

	// FULL SWAP - also how native performs a plain move: it swaps the item
	// with the empty destination record.
	default:
		inv.items[sourceIndex].Slot = destSlot
		out.DestQuantity = sourceCount
		if destIndex >= 0 {
			inv.items[destIndex].Slot = sourceSlot
			swappedCount := inv.items[destIndex].Quantity
			if swappedCount == 0 {
				swappedCount = 1
			}
			out.Leg = LegSwap
			out.SourceQuantity = swappedCount
		} else {
			out.Leg = LegMove
		}
	}

	return out, nil
}

// SocketVisual is the post-move occupancy of one equipment socket a type-0x00
// move touched: what the doll and the world model must now show.
/*
================
SocketVisual
================
*/
type SocketVisual struct {
	Socket uint8
	// Worn is false when the socket ended up empty, which the wire clears
	// with a 0x377C carrying RefObjID 0.
	Worn bool
	// Item is the occupant when Worn.
	Item Item
}

// EquipVisualChanges reports the post-move occupancy of every equipment
// socket the move touched, source first then destination when both are
// sockets and distinct. It returns nothing for a bag-only move.
//
// Callers run it AFTER the transfer so a swap reports what each socket ended
// up holding, then push one 0x3314 per worn entry and one 0x377C clear per
// vacated entry behind the 0xB06D result - without them the containers would
// update but the doll would keep wearing the old item (the M1 chain).
/*
================
EquipVisualChanges
================
*/
func (inv *Inventory) EquipVisualChanges(sourceSlot, destSlot uint8) []SocketVisual {
	var out []SocketVisual
	seen := map[uint8]bool{}
	for _, slot := range []uint8{sourceSlot, destSlot} {
		if !inv.equipmentSlot(slot) || seen[slot] {
			continue
		}
		seen[slot] = true
		if item, worn := inv.At(slot); worn {
			out = append(out, SocketVisual{Socket: slot, Worn: true, Item: item})
		} else {
			out = append(out, SocketVisual{Socket: slot})
		}
	}
	return out
}

// Drop removes the row at a wire slot for a type-0x07 ground drop and returns
// it, so the caller can register it as a ground item.
//
// The native type-7 request carries [u8 src] and NOTHING else, so retail
// cannot express "drop N of a stack" - the whole row always goes. This is
// that native whole-row drop; the fixture's out-of-band partial drop is
// DropQuantity.
/*
================
Drop
================
*/
func (inv *Inventory) Drop(sourceSlot uint8) (Item, *Fault) {
	if !inv.validSlot(sourceSlot) {
		return Item{}, newFault(wire.ErrCodeInvalidRequest, "slotOutOfRange")
	}
	// A WORN item cannot be ground-dropped. The retail drop-confirm dialog
	// (sub_68d430 @0x0068d430) opens only for a bag source and routes an
	// equipment source to notice 01:6d @0x0068d606 without composing the
	// type-0x07 packet, so a stock client never reaches here from a socket;
	// a modified client can, and it would unequip-by-dropping the worn row.
	// This gate touches only the type-0x07 drop path (Drop's sole callers):
	// the equip/unequip plane is type-0x00 (Transfer) and pickup is Grant,
	// neither of which routes through here.
	if inv.equipmentSlot(sourceSlot) {
		return Item{}, newFault(wire.ErrCodeCannotDropEquipped, "dropEquippedItem")
	}
	sourceIndex := inv.indexOf(sourceSlot)
	if sourceIndex < 0 {
		return Item{}, newFault(wire.ErrCodeInvalidRequest, "sourceSlotEmpty")
	}

	if fault := summonerTransferFault(inv.items[sourceIndex]); fault != nil {
		return Item{}, fault
	}
	dropped := inv.items[sourceIndex]
	inv.items = append(inv.items[:sourceIndex], inv.items[sourceIndex+1:]...)
	return dropped, nil
}

/*
================
DeathDrop

The death penalty's drop (move 0x17, re-typed to 7 by 5264E0 for any
slot but the weapon): unlike Drop it may take a worn item. The weapon
socket refuses (0x1806, "Can't drop equipped weapon forcely by server").
================
*/
func (inv *Inventory) DeathDrop(sourceSlot uint8) (Item, *Fault) {
	if !inv.validSlot(sourceSlot) {
		return Item{}, newFault(wire.ErrCodeInvalidRequest, "slotOutOfRange")
	}
	if sourceSlot == deathDropWeaponSlot {
		return Item{}, newFault(wire.ErrCodeCannotDropEquipped, "deathDropWeapon")
	}
	sourceIndex := inv.indexOf(sourceSlot)
	if sourceIndex < 0 {
		return Item{}, newFault(wire.ErrCodeInvalidRequest, "sourceSlotEmpty")
	}
	dropped := inv.items[sourceIndex]
	inv.items = append(inv.items[:sourceIndex], inv.items[sourceIndex+1:]...)
	return dropped, nil
}

// deathDropWeaponSlot is the weapon socket 5264E0 refuses.
const deathDropWeaponSlot = 6

// DropQuantity removes count units from the row at a wire slot. FIXTURE
// POLICY, NOT NATIVE: the native type-7 wire has no quantity field, so this
// only serves the fixture's out-of-band partial-drop request; absent that,
// callers use Drop.
//
// Dropping the whole stack removes the row; a partial drop leaves
// stackCount - count behind in the same slot and returns a copy carrying
// count. The bounds check runs on the raw request - a zero must refuse, not
// clamp up to 1 and silently drop a unit.
/*
================
DropQuantity
================
*/
func (inv *Inventory) DropQuantity(sourceSlot uint8, count uint16) (Item, *Fault) {
	if !inv.validSlot(sourceSlot) {
		return Item{}, newFault(wire.ErrCodeInvalidRequest, "slotOutOfRange")
	}
	// Same worn-item guard as Drop (01:6d): an equipment socket is never a
	// legal drop source. Kept on this fixture-only partial-drop path too so
	// the two drop entrypoints refuse identically.
	if inv.equipmentSlot(sourceSlot) {
		return Item{}, newFault(wire.ErrCodeCannotDropEquipped, "dropEquippedItem")
	}
	sourceIndex := inv.indexOf(sourceSlot)
	if sourceIndex < 0 {
		return Item{}, newFault(wire.ErrCodeInvalidRequest, "sourceSlotEmpty")
	}

	if fault := summonerTransferFault(inv.items[sourceIndex]); fault != nil {
		return Item{}, fault
	}
	stack := inv.items[sourceIndex].Quantity
	if stack == 0 {
		stack = 1
	}
	// Same zero/over split as Transfer's divide guard: 01:29 for a
	// non-positive count, 01:14 for more than the stack holds.
	if count < 1 {
		return Item{}, newFault(wire.ErrCodePositiveNumberOnly, "dropQuantityNotPositive")
	}
	if count > stack {
		return Item{}, newFault(wire.ErrCodeInputFewerThanRemain, "dropQuantityOverStack")
	}

	if count >= stack {
		dropped := inv.items[sourceIndex]
		dropped.Quantity = stack
		inv.items = append(inv.items[:sourceIndex], inv.items[sourceIndex+1:]...)
		return dropped, nil
	}

	dropped := splitInventoryRow(inv.items[sourceIndex], sourceSlot, count)
	inv.items[sourceIndex].Quantity = stack - count
	return dropped, nil
}

// Grant places a picked-up item into the first free bag slot and returns that
// slot. The item's Slot field is overwritten with the chosen slot.
//
// A full bag is refused with UIIT_MSG_STRGERR_INVENTORY_FULL, which is what
// the pickup path reports rather than silently dropping the grant.
//
// Grant is the plain occupy-a-free-slot leg; the pickup path proper is
// GrantStack, which also merges onto an existing stack and computes the
// over-cap ground remainder.
/*
================
Grant
================
*/
func (inv *Inventory) Grant(item Item) (uint8, *Fault) {
	if fault := summonerTransferFault(item); fault != nil {
		return 0, fault
	}
	destSlot, ok := inv.FirstFreeBagSlot()
	if !ok {
		return 0, newFault(wire.ErrCodeStorageFull, "inventoryFull")
	}

	item.Slot = destSlot
	if item.Quantity == 0 {
		item.Quantity = 1
	}
	inv.items = append(inv.items, cloneInventoryRow(item))
	return destSlot, nil
}

// MergeTargetSlot returns the bag row a pickup of refObjID would merge onto:
// the LOWEST bag wire slot holding the same ref below stackCap. Lowest slot
// wins, not row order - the persisted row order is an artifact of past moves,
// and the destination must be reproducible. The second result is false when
// no mergeable row exists.
/*
================
MergeTargetSlot
================
*/
func (inv *Inventory) MergeTargetSlot(refObjID uint32, stackCap uint16) (uint8, bool) {
	return inv.mergeTargetSlot(Item{RefObjID: refObjID}, stackCap)
}

/*
================
mergeTargetSlot

Cargo grants carry the owner name; reference-only callers cannot merge into
an owned cargo stack without that identity.
================
*/
func (inv *Inventory) mergeTargetSlot(item Item, stackCap uint16) (uint8, bool) {
	best := -1
	for index := range inv.items {
		row := &inv.items[index]
		if !inv.bagSlot(row.Slot) || !stackIdentityMatches(*row, item) {
			continue
		}
		count := row.Quantity
		if count == 0 {
			count = 1
		}
		if count >= stackCap {
			continue
		}
		if best < 0 || row.Slot < inv.items[best].Slot {
			best = index
		}
	}
	if best < 0 {
		return 0, false
	}
	return inv.items[best].Slot, true
}

// PickupGrant describes an applied ground-item grant.
/*
================
PickupGrant
================
*/
type PickupGrant struct {
	// DestSlot is the bag wire slot the grant landed in.
	DestSlot uint8
	// PostMergeCount is the ABSOLUTE post-merge count on the bag row, never
	// a delta - the 0xB06D row assigns counts client-side.
	PostMergeCount uint16
	// GroundRemainder is what stays on the ground: an over-cap pickup
	// leaves the heap alive under its gid with this count, and the despawn
	// is withheld.
	GroundRemainder uint16
	// Merged is true when the grant landed on an existing stack rather
	// than a fresh slot.
	Merged bool
}

// GrantStack applies a ground-item pickup with the native stack rules: merge
// onto the lowest same-ref bag row below the cap when the item stacks,
// otherwise occupy the first free bag slot, clamping at the cap either way.
//
// stackCap is the item's iMax, sourced by the caller (gated on
// IsEtcStackableTypeFlags; a non-stacking family passes 1). item.Quantity is
// the ground stack count (zero reads as 1).
//
// The merge gate runs first, so a full bag still accepts a pickup that fits
// an existing stack; only the fresh-slot leg refuses with
// UIIT_MSG_STRGERR_INVENTORY_FULL. The destination can never be an
// already-at-cap row, so the counts-swap arm of TransferSlotStack is
// unreachable from a pickup, exactly like native.
/*
================
GrantStack
================
*/
func (inv *Inventory) GrantStack(item Item, stackCap uint16) (PickupGrant, *Fault) {
	var out PickupGrant
	if fault := summonerTransferFault(item); fault != nil {
		return out, fault
	}
	if wire.IsCosSummoner(item.TypeFlags) {
		stackCap = 1
	}

	if stackCap < 1 {
		stackCap = 1
	}
	groundStack := item.Quantity
	if groundStack == 0 {
		groundStack = 1
	}

	if stackCap > 1 {
		if destSlot, ok := inv.mergeTargetSlot(item, stackCap); ok {
			destIndex := inv.indexOf(destSlot)
			destCount := inv.items[destIndex].Quantity
			if destCount == 0 {
				destCount = 1
			}
			transfer := TransferSlotStack(groundStack, destCount, stackCap)
			inv.items[destIndex].Quantity = transfer.DestCount
			out.DestSlot = destSlot
			out.PostMergeCount = transfer.DestCount
			out.GroundRemainder = transfer.SourceRemainder
			out.Merged = true
			return out, nil
		}
	}

	destSlot, ok := inv.FirstFreeBagSlot()
	if !ok {
		return out, newFault(wire.ErrCodeStorageFull, "inventoryFull")
	}
	// A heap larger than one stack cannot land whole in a fresh slot either.
	granted := groundStack
	if granted > stackCap {
		granted = stackCap
	}
	item.Slot = destSlot
	item.Quantity = granted
	if groundStack > granted {
		item.RecordID = 0 // New partial stack must not duplicate the surviving row's identity.
	}
	inv.items = append(inv.items, cloneInventoryRow(item))
	out.DestSlot = destSlot
	out.PostMergeCount = granted
	out.GroundRemainder = groundStack - granted
	return out, nil
}

/*
================
summonerTransferFault

Inference from single-record ownership: an active actor cannot cross an item
container boundary, and a persistent record cannot split or merge. This gate
belongs to the shared inventory owner so warehouse, sale and drop agree.
================
*/
func summonerTransferFault(item Item) *Fault {
	if !wire.IsCosSummoner(item.TypeFlags) && item.Summon == nil {
		return nil
	}
	if !wire.IsCosSummoner(item.TypeFlags) || item.Quantity != 1 || (item.Summon != nil && item.Summon.Summoned) || wire.ValidateCOSItem(item.TypeFlags, item.Summon) != nil {
		return newFault(wire.ErrCodeInvalidRequest, "summonerOwnershipConflict")
	}
	return nil
}
