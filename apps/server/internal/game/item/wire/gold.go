package wire

import "opensro.online/server/internal/domain"

// MaxGold is the native ceiling on a gold amount carried by an item
// operation: 0x5F5E100 == 100,000,000.
//
// The client enforces it on both sides of the exchange, which is why it lives
// with the wire layer rather than with the inventory rules:
//
//	sub_697e80 @0x00697eb5 clamps before serializing a 0x706D request,
//	sub_759a30 @0x0075a2c0 clamps after parsing a 0xB06D result, with the
//	negative arm @0x0075a2b3 flooring at zero.
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
