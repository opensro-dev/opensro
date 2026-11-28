package wire

import (
	"math"

	"opensro.online/server/internal/domain"
)

// MaxGold is the native ceiling on a gold amount carried by an item
// operation: 0x5F5E100 == 100,000,000.
//
// The client enforces it on both sides of the exchange, which is why it lives
// with the wire layer rather than with the inventory rules:
//
//	The client's ItemMoveRequest_Serialize clamps at 0x00697eb5 before
//	serializing a 0x706D request; its ItemMoveResponse_Deserialize
//	clamps at client 0x0075a2c0 after parsing a 0xB06D result, with the
//	negative arm at client 0x0075a2b3 flooring at zero.
const MaxGold uint32 = domain.GroundGoldAmountMax

// ClampGold applies the native [0, MaxGold] clamp.
//
// It takes a uint64 so a balance that has grown past the ceiling in storage
// clamps rather than wrapping on the way to the wire.
func ClampGold(amount uint64) uint32 {
	if amount > uint64(MaxGold) {
		return MaxGold
	}
	return uint32(amount)
}

/*
================
WideGoldDeposit

Port-only, not native: a warehouse gold deposit (personal or guild) may
carry its amount as a u64 instead of the native u32, so a shard whose
SRO_RAISED_GOLD_CAPS deposit cap passes 4,294,967,295 can take it in one
move. The form is told apart by length alone, as the raised stall price
is (action/stall.go): the native client always sends four bytes, so a
native request decodes as before, and the action still checks every
amount against the shard's cap.
================
*/
func WideGoldDeposit(movementType uint8) bool {
	return movementType == MoveTypeStorageGoldDeposit || movementType == MoveTypeGuildStorageGoldDeposit
}

/*
================
readGoldAmount

A gold move's amount: the native u32, or a wide deposit's u64 when eight
bytes remain.
================
*/
func readGoldAmount(r *Reader, movementType uint8) (uint64, error) {
	if WideGoldDeposit(movementType) && r.Remaining() == 8 {
		return r.U64()
	}
	amount, err := r.U32()
	return uint64(amount), err
}

/*
================
writeGoldAmount

The amount a gold move or its success echo carries: a wide deposit above
the u32 range as a u64, every other amount as the native u32 narrow
(already clamped by the caller where the native client clamps).
================
*/
func writeGoldAmount(w *Writer, movementType uint8, narrow uint32, amount uint64) {
	if WideGoldDeposit(movementType) && amount > uint64(math.MaxUint32) {
		w.U64(amount)
		return
	}
	w.U32(narrow)
}
