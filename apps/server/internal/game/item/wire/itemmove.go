/*
===========================================================================

itemmove.go - encodes and decodes reference-selected item movement bodies

===========================================================================
*/
package wire

import (
	"fmt"
	"opensro.online/server/internal/domain"
)

// ItemBodySize is the encoded size of a CSOItem equipment body carrying NO
// magic options (the header: u32 + u8 + u64 + u32 + the count byte). The
// body is VARIABLE-length - each magic option appends 8 bytes - so sizing
// code must use EncodedSize, never this constant alone.
const ItemBodySize = 18

// MaxMagicOptionsPerItem is the client's MAX_MAGPARAM_PER_ITEM: the parse
// asserts the count byte <= 12 (sub_78b1b0 @0x0078b224, soitem.cpp:0x13b
// int3), so Encode clamps rather than emit an assert-crashing count.
const MaxMagicOptionsPerItem = 12

// ItemBody is the CSOItem body as sub_78c830 reads it. TypeFlags is not on
// the wire: the client resolves it from RefObjID, and the encoder needs the
// same local itemdata word to choose the body:
//
//   - equipment: u32 ref, u8 plus, u64 variance, u32 durability,
//     u8 magic-count + count*u64;
//   - ordinary ETC: u32 ref, u16 quantity, optional subtype plus byte,
//     optional indexed magic-count + count*u64.
//
// A zero TypeFlags retains the historical equipment default for old callers
// and fixtures. New code must always supply the real word.
/*
================
ItemBody
================
*/
type ItemBody struct {
	TradeOwner   string
	Summon       *domain.CharacterCOS
	RefObjID     uint32
	TypeFlags    uint16
	Plus         uint8
	VarianceBits uint64
	Durability   uint32
	Quantity     uint16
	// MagicOptions are the encoded magic-option u64 params (reinforce-leg
	// shape: low u16 = magicoption.txt param id, high u32 = magnitude).
	// The count byte is derived from the length.
	MagicOptions      []uint64
	TransformRefObjID uint32 // monster capsule Data
}

// EncodedSize is the body's variable wire length.
/*
================
EncodedSize
================
*/
func (b ItemBody) EncodedSize() int {
	options := len(b.MagicOptions)
	if options > MaxMagicOptionsPerItem {
		options = MaxMagicOptionsPerItem
	}
	if IsMonsterCapsule(b.TypeFlags) {
		return 4 + 4
	}
	if IsCosSummoner(b.TypeFlags) {
		body, _ := encodeCosSummoner(b.TypeFlags, b.Summon)
		return 4 + len(body)
	}
	if b.TypeFlags != 0 && IsEtcBand(b.TypeFlags) {
		size := 4 + 2
		if EtcCarriesTradeOwner(b.TypeFlags) {
			size += 2 + len(b.TradeOwner)
		}
		if EtcCarriesPlusByte(b.TypeFlags) {
			size++
		}
		if UsesIndexedMagicParams(b.TypeFlags) {
			size += 1 + 8*options
		}
		return size
	}
	return ItemBodySize + 8*options
}

// Encode returns the CSOItem body bytes (EncodedSize long).
/*
================
Encode
================
*/
func (b ItemBody) Encode() []byte {
	options := b.MagicOptions
	if len(options) > MaxMagicOptionsPerItem {
		options = options[:MaxMagicOptionsPerItem]
	}
	w := NewWriter(b.EncodedSize()).U32(b.RefObjID)
	if IsMonsterCapsule(b.TypeFlags) {
		return w.U32(b.TransformRefObjID).Payload()
	}
	if IsCosSummoner(b.TypeFlags) {
		body, err := encodeCosSummoner(b.TypeFlags, b.Summon)
		if err != nil {
			return nil
		}
		return w.Bytes(body).Payload()
	}
	if b.TypeFlags != 0 && IsEtcBand(b.TypeFlags) {
		quantity := b.Quantity
		if quantity == 0 {
			quantity = 1
		}
		w.U16(quantity)
		if EtcCarriesTradeOwner(b.TypeFlags) {
			if len(b.TradeOwner) > 65535 {
				return nil
			}
			w.U16(uint16(len(b.TradeOwner))).Bytes([]byte(b.TradeOwner))
		}
		if EtcCarriesPlusByte(b.TypeFlags) {
			w.U8(b.Plus)
		}
		if UsesIndexedMagicParams(b.TypeFlags) {
			w.U8(uint8(len(options)))
			for _, encoded := range options {
				w.U64(encoded)
			}
		}
		return w.Payload()
	}
	w.U8(b.Plus).
		U64(b.VarianceBits).
		U32(b.Durability).
		U8(uint8(len(options)))
	for _, encoded := range options {
		w.U64(encoded)
	}
	return w.Payload()
}

/*
================
readItemBody
================
*/
func readItemBody(r *Reader, typeFlags uint16) (ItemBody, error) {
	var out ItemBody
	out.TypeFlags = typeFlags

	refObjID, err := r.U32()
	if err != nil {
		return out, err
	}
	out.RefObjID = refObjID
	if IsMonsterCapsule(typeFlags) {
		out.Quantity = 1
		out.TransformRefObjID, err = r.U32()
		return out, err
	}
	if IsCosSummoner(typeFlags) {
		out.Quantity = 1
		out.Summon, err = readCosSummoner(r, typeFlags)
		return out, err
	}
	if typeFlags != 0 && IsEtcBand(typeFlags) {
		if out.Quantity, err = r.U16(); err != nil {
			return out, err
		}
		if EtcCarriesTradeOwner(typeFlags) {
			length, e := r.U16()
			if e != nil {
				return out, e
			}
			owner, e := r.Bytes(int(length))
			if e != nil {
				return out, e
			}
			out.TradeOwner = string(owner)
		}
		if EtcCarriesPlusByte(typeFlags) {
			if out.Plus, err = r.U8(); err != nil {
				return out, err
			}
		}
		if UsesIndexedMagicParams(typeFlags) {
			magicOptionCount, err := r.U8()
			if err != nil {
				return out, err
			}
			if magicOptionCount > MaxMagicOptionsPerItem {
				return out, fmt.Errorf(
					"wire: indexed magic-option count %d exceeds native max %d",
					magicOptionCount,
					MaxMagicOptionsPerItem,
				)
			}
			for i := 0; i < int(magicOptionCount); i++ {
				encoded, err := r.U64()
				if err != nil {
					return out, err
				}
				out.MagicOptions = append(out.MagicOptions, encoded)
			}
		}
		return out, nil
	}
	plus, err := r.U8()
	if err != nil {
		return out, err
	}
	varianceBits, err := r.U64()
	if err != nil {
		return out, err
	}
	durability, err := r.U32()
	if err != nil {
		return out, err
	}
	magicOptionCount, err := r.U8()
	if err != nil {
		return out, err
	}
	var magicOptions []uint64
	for i := 0; i < int(magicOptionCount); i++ {
		encoded, err := r.U64()
		if err != nil {
			return out, err
		}
		magicOptions = append(magicOptions, encoded)
	}

	out.Plus = plus
	out.VarianceBits = varianceBits
	out.Durability = durability
	out.MagicOptions = magicOptions
	return out, nil
}

// SubMove is one follow-up row of a type-0x00 move.
//
// The serializer writes five bytes per row (sub_697e80 @0x00697fed: the op's
// type byte, then the row's +3, +4 and +6 fields) and the parser reads the
// same five back (sub_759a30 @0x00759b04). A plain slot-to-slot move carries
// none; they appear when one logical operation touches several slots, such as
// a stack merge spilling across rows.
/*
================
SubMove
================
*/
type SubMove struct {
	// MovementType echoes the parent operation's type byte.
	MovementType uint8
	SourceSlot   uint8
	DestSlot     uint8
	Quantity     uint16
}

// SubMoveSize is the encoded size of one sub-move row.
const SubMoveSize = 5

/*
================
encodeInto
================
*/
func (s SubMove) encodeInto(w *Writer) {
	w.U8(s.MovementType).U8(s.SourceSlot).U8(s.DestSlot).U16(s.Quantity)
}

/*
================
readSubMove
================
*/
func readSubMove(r *Reader) (SubMove, error) {
	var out SubMove

	movementType, err := r.U8()
	if err != nil {
		return out, err
	}
	sourceSlot, err := r.U8()
	if err != nil {
		return out, err
	}
	destSlot, err := r.U8()
	if err != nil {
		return out, err
	}
	quantity, err := r.U16()
	if err != nil {
		return out, err
	}

	out.MovementType = movementType
	out.SourceSlot = sourceSlot
	out.DestSlot = destSlot
	out.Quantity = quantity
	return out, nil
}

// ItemMoveRequest is a 0x706D client item operation (serializer sub_697e80).
//
// Which fields carry meaning depends on MovementType:
//
//	MoveTypeInventory  SourceSlot, DestSlot, Quantity, SubMoves
//	MoveTypeGroundDrop SourceSlot
//	MoveTypeGoldDrop   GoldAmount
const MoveTypeAvatarToPlayer uint8 = 0x23
const MoveTypePlayerToAvatar uint8 = 0x24

/*
================
ItemMoveRequest
================
*/
type ItemMoveRequest struct {
	NpcGID       uint32
	CosGID       uint32
	GroundGID    uint32
	ShopTab      uint8
	ShopSlot     uint8
	MovementType uint8
	SourceSlot   uint8
	DestSlot     uint8
	Quantity     uint16
	GoldAmount   uint32
	SubMoves     []SubMove
}

// ErrUnsupportedMovementType is returned for a movement type this wave has not
// established a layout for. The native jump table covers 0x00..0x24; only the
// types with a pinned layout are implemented here, and guessing
// at the rest would be worse than refusing.
type ErrUnsupportedMovementType uint8

/*
================
Error
================
*/
func (e ErrUnsupportedMovementType) Error() string {
	return fmt.Sprintf("wire: movement type 0x%02X has no established layout", uint8(e))
}

// Encode returns the 0x706D payload: [u8 movementType] then the per-type body.
/*
================
Encode
================
*/
func (q ItemMoveRequest) Encode() ([]byte, error) {
	w := NewWriter(16).U8(q.MovementType)

	switch q.MovementType {
	case MoveTypeAvatarToPlayer, MoveTypePlayerToAvatar:
		w.U8(q.SourceSlot).U8(q.DestSlot)
	case MoveTypeCosPickup, MoveTypeCosDrop:
		if q.CosGID == 0 {
			return nil, fmt.Errorf("missing COS identity")
		}
		w.U32(q.CosGID)
		if q.MovementType == MoveTypeCosPickup {
			if q.GroundGID == 0 {
				return nil, fmt.Errorf("missing ground identity")
			}
			w.U32(q.GroundGID)
		} else {
			w.U8(q.SourceSlot)
		}
	case MoveTypeShopBuy, MoveTypeCosShopBuy, MoveTypeShopSell, MoveTypeCosShopSell:
		if err := q.encodeCommerce(w); err != nil {
			return nil, err
		}
	case MoveTypeCosToPlayer, MoveTypePlayerToCos:
		if q.CosGID == 0 {
			return nil, fmt.Errorf("missing COS identity")
		}
		w.U32(q.CosGID).U8(q.SourceSlot).U8(q.DestSlot)
	case MoveTypeCosInventory:
		if q.CosGID == 0 {
			return nil, fmt.Errorf("missing COS container identity")
		}
		w.U32(q.CosGID).U8(q.SourceSlot).U8(q.DestSlot).U16(q.Quantity)
	case MoveTypeInventory:
		// sub_697e80 @0x00697f7c
		w.U8(q.SourceSlot).U8(q.DestSlot).U16(q.Quantity)
		if len(q.SubMoves) > 0 {
			// sub_697e80 @0x00697f8e
			w.U8(uint8(len(q.SubMoves)))
			for _, subMove := range q.SubMoves {
				subMove.encodeInto(w)
			}
		}
	case MoveTypeGroundDrop:
		// sub_697e80 @0x006980d0
		w.U8(q.SourceSlot)
	case MoveTypeGoldDrop, MoveTypeStorageGoldWithdraw, MoveTypeStorageGoldDeposit,
		MoveTypeGuildStorageGoldDeposit, MoveTypeGuildStorageGoldWithdraw:
		// sub_697e80 @0x006984cb, amount pre-clamped at @0x00697eb5
		w.U32(ClampGold(uint64(q.GoldAmount)))
	case MoveTypeStorage, MoveTypeGuildStorage:
		// ItemMoveRequest_Serialize case 1: [src][dst][u16 count][u32 npc].
		w.U8(q.SourceSlot).U8(q.DestSlot).U16(q.Quantity).U32(q.NpcGID)
	case MoveTypeStorageDeposit, MoveTypeStorageWithdraw, MoveTypeGuildStorageDeposit, MoveTypeGuildStorageWithdraw:
		// Cases 2 and 3 (and 0x1E / 0x1F): [src][dst][u32 npc], no count.
		w.U8(q.SourceSlot).U8(q.DestSlot).U32(q.NpcGID)
	default:
		return nil, ErrUnsupportedMovementType(q.MovementType)
	}

	return w.Payload(), nil
}

// DecodeItemMoveRequest parses a 0x706D payload.
//
// A type-0x00 body may or may not carry the sub-move count: the serializer
// only emits it on the branch where the operation has rows to describe, so a
// payload that ends after the quantity is well-formed and yields no SubMoves.
/*
================
DecodeItemMoveRequest
================
*/
func DecodeItemMoveRequest(payload []byte) (ItemMoveRequest, error) {
	var out ItemMoveRequest
	r := NewReader(payload)

	movementType, err := r.U8()
	if err != nil {
		return out, err
	}
	out.MovementType = movementType

	switch movementType {
	case MoveTypeAvatarToPlayer, MoveTypePlayerToAvatar:
		if out.SourceSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.DestSlot, err = r.U8(); err != nil {
			return out, err
		}
	case MoveTypeCosPickup, MoveTypeCosDrop:
		if out.CosGID, err = r.U32(); err != nil {
			return out, err
		}
		if out.CosGID == 0 {
			return out, fmt.Errorf("missing COS identity")
		}
		if movementType == MoveTypeCosPickup {
			if out.GroundGID, err = r.U32(); err != nil {
				return out, err
			}
			if out.GroundGID == 0 {
				return out, fmt.Errorf("missing ground identity")
			}
		} else if out.SourceSlot, err = r.U8(); err != nil {
			return out, err
		}
	case MoveTypeShopBuy, MoveTypeCosShopBuy, MoveTypeShopSell, MoveTypeCosShopSell:
		if err = out.decodeCommerce(r); err != nil {
			return out, err
		}
	case MoveTypeCosToPlayer, MoveTypePlayerToCos:
		if out.CosGID, err = r.U32(); err != nil {
			return out, err
		}
		if out.CosGID == 0 {
			return out, fmt.Errorf("missing COS identity")
		}
		if out.SourceSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.DestSlot, err = r.U8(); err != nil {
			return out, err
		}
	case MoveTypeExchangePut, MoveTypeExchangeTake:
		// 697E80 cases 4 and 5: the bag slot or the exchange slot.
		if out.SourceSlot, err = r.U8(); err != nil {
			return out, err
		}
	case MoveTypeExchangeGold:
		// 697E80 case 0xD: the gold on the table.
		if out.GoldAmount, err = r.U32(); err != nil {
			return out, err
		}
	case MoveTypeCosInventory:
		if out.CosGID, err = r.U32(); err != nil {
			return out, err
		}
		if out.CosGID == 0 {
			return out, fmt.Errorf("missing COS container identity")
		}
		if out.SourceSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.DestSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.Quantity, err = r.U16(); err != nil {
			return out, err
		}
	case MoveTypeInventory:
		if out.SourceSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.DestSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.Quantity, err = r.U16(); err != nil {
			return out, err
		}
		if r.Remaining() > 0 {
			count, err := r.U8()
			if err != nil {
				return out, err
			}
			for i := 0; i < int(count); i++ {
				subMove, err := readSubMove(r)
				if err != nil {
					return out, err
				}
				out.SubMoves = append(out.SubMoves, subMove)
			}
		}
	case MoveTypeGroundDrop:
		if out.SourceSlot, err = r.U8(); err != nil {
			return out, err
		}
	case MoveTypeGoldDrop, MoveTypeStorageGoldWithdraw, MoveTypeStorageGoldDeposit,
		MoveTypeGuildStorageGoldDeposit, MoveTypeGuildStorageGoldWithdraw:
		if out.GoldAmount, err = r.U32(); err != nil {
			return out, err
		}
	case MoveTypeStorage, MoveTypeGuildStorage:
		if out.SourceSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.DestSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.Quantity, err = r.U16(); err != nil {
			return out, err
		}
		if out.NpcGID, err = r.U32(); err != nil {
			return out, err
		}
	case MoveTypeStorageDeposit, MoveTypeStorageWithdraw, MoveTypeGuildStorageDeposit, MoveTypeGuildStorageWithdraw:
		if out.SourceSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.DestSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.NpcGID, err = r.U32(); err != nil {
			return out, err
		}
	default:
		return out, ErrUnsupportedMovementType(movementType)
	}

	if err := r.Done(); err != nil {
		return out, err
	}
	return out, nil
}

// ItemMoveResult is a decoded 0xB06D result row.
//
// Result discriminates first: on ResultError only ErrorCode is meaningful. On
// ResultSuccess, MovementType selects which of the remaining fields carry
// meaning:
//
//	MoveTypeInventory  SourceSlot, DestSlot, Quantity, SubMoves
//	MoveTypePickup     PickupSlot, then either GoldAmount (when the slot is
//	                   PickupGoldSlot) or Item
//	MoveTypeGroundDrop SourceSlot
//	MoveTypeGoldDrop   GoldAmount
/*
================
ItemMoveResult
================
*/
type ItemMoveResult struct {
	Result       uint8
	ErrorCode    uint8
	MovementType uint8
	SourceSlot   uint8
	DestSlot     uint8
	Quantity     uint16
	SubMoves     []SubMove
	PickupSlot   uint8
	GoldAmount   uint32
	Item         ItemBody
}

// IsGoldPickup reports whether a successful pickup granted gold rather than an
// item, which the native signals with the PickupGoldSlot sentinel.
/*
================
IsGoldPickup
================
*/
func (r ItemMoveResult) IsGoldPickup() bool {
	return r.Result == ResultSuccess &&
		r.MovementType == MoveTypePickup &&
		r.PickupSlot == PickupGoldSlot
}

// EncodeItemMoveError returns a failed 0xB06D payload: [0x02][errorCode].
/*
================
EncodeItemMoveError
================
*/
func EncodeItemMoveError(errorCode uint8) []byte {
	return NewWriter(2).U8(ResultError).U8(errorCode).Payload()
}

// EncodeInventoryMoveResult returns a successful type-0x00 payload:
// [0x01][0x00][src][dst][qty u16][subMoveCount].
/*
================
EncodeInventoryMoveResult
================
*/
func EncodeInventoryMoveResult(sourceSlot, destSlot uint8, quantity uint16, subMoves []SubMove) []byte {
	w := NewWriter(7 + len(subMoves)*SubMoveSize).
		U8(ResultSuccess).
		U8(MoveTypeInventory).
		U8(sourceSlot).
		U8(destSlot).
		U16(quantity).
		U8(uint8(len(subMoves)))
	for _, subMove := range subMoves {
		subMove.encodeInto(w)
	}
	return w.Payload()
}

// EncodeGroundDropResult returns a successful type-0x07 payload:
// [0x01][0x07][src].
/*
================
EncodeGroundDropResult
================
*/
func EncodeGroundDropResult(sourceSlot uint8) []byte {
	return NewWriter(3).U8(ResultSuccess).U8(MoveTypeGroundDrop).U8(sourceSlot).Payload()
}

// EncodeGoldDropResult returns a successful type-0x0A payload:
// [0x01][0x0A][amount u32]. The amount is clamped to the native ceiling.
/*
================
EncodeGoldDropResult
================
*/
func EncodeGoldDropResult(amount uint32) []byte {
	return NewWriter(6).
		U8(ResultSuccess).
		U8(MoveTypeGoldDrop).
		U32(ClampGold(uint64(amount))).
		Payload()
}

// EncodePickupItemResult returns a successful type-0x06 item grant:
// [0x01][0x06][slot][CSOItem body].
/*
================
EncodePickupItemResult
================
*/
func EncodePickupItemResult(slot uint8, item ItemBody) []byte {
	return NewWriter(3 + item.EncodedSize()).
		U8(ResultSuccess).
		U8(MoveTypePickup).
		U8(slot).
		Bytes(item.Encode()).
		Payload()
}

// EncodePickupGoldResult returns a successful type-0x06 gold grant:
// [0x01][0x06][0xFE][amount u32].
/*
================
EncodePickupGoldResult
================
*/
func EncodePickupGoldResult(amount uint32) []byte {
	return NewWriter(7).
		U8(ResultSuccess).
		U8(MoveTypePickup).
		U8(PickupGoldSlot).
		U32(amount).
		Payload()
}

// DecodeItemMoveResult parses a 0xB06D payload. pickupTypeFlags is the
// authoritative RefObj/itemdata type word for a non-gold type-0x06 body; the
// wire does not carry this discriminator. It is ignored for every other shape.
/*
================
DecodeItemMoveResult
================
*/
func DecodeItemMoveResult(payload []byte, pickupTypeFlags uint16) (ItemMoveResult, error) {
	var out ItemMoveResult
	r := NewReader(payload)

	result, err := r.U8()
	if err != nil {
		return out, err
	}
	out.Result = result

	switch result {
	case ResultError:
		if out.ErrorCode, err = r.U8(); err != nil {
			return out, err
		}
		return out, r.Done()
	case ResultSuccess:
	default:
		return out, fmt.Errorf("wire: unknown 0xB06D result byte 0x%02X", result)
	}

	movementType, err := r.U8()
	if err != nil {
		return out, err
	}
	out.MovementType = movementType

	switch movementType {
	case MoveTypeInventory:
		if out.SourceSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.DestSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.Quantity, err = r.U16(); err != nil {
			return out, err
		}
		count, err := r.U8()
		if err != nil {
			return out, err
		}
		for i := 0; i < int(count); i++ {
			subMove, err := readSubMove(r)
			if err != nil {
				return out, err
			}
			out.SubMoves = append(out.SubMoves, subMove)
		}
	case MoveTypePickup:
		if out.PickupSlot, err = r.U8(); err != nil {
			return out, err
		}
		if out.PickupSlot == PickupGoldSlot {
			if out.GoldAmount, err = r.U32(); err != nil {
				return out, err
			}
		} else {
			if out.Item, err = readItemBody(r, pickupTypeFlags); err != nil {
				return out, err
			}
		}
	case MoveTypeGroundDrop:
		if out.SourceSlot, err = r.U8(); err != nil {
			return out, err
		}
	case MoveTypeGoldDrop:
		if out.GoldAmount, err = r.U32(); err != nil {
			return out, err
		}
	default:
		return out, ErrUnsupportedMovementType(movementType)
	}

	if err := r.Done(); err != nil {
		return out, err
	}
	return out, nil
}

/*
================
EtcCarriesTradeOwner

78C830: TID 3.3.8 reads the original trader string after the quantity.
================
*/
func EtcCarriesTradeOwner(flags uint16) bool { return flags&0x7fe == 0x46c }
