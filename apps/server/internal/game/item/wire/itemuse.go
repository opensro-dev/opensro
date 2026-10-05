/*
===========================================================================

itemuse.go - the item-use request and reply (0x75BD / 0xB5BD)

===========================================================================
*/

package wire

/*
==================
ItemUseRequest

ItemUseRequest is the v1.150 0x75BD body composed by
sub_6961b0_CGInterface_ExecuteSelectedInteractionAction:

	[u8 inventory wire slot][u16 RefItem type word, little-endian]

Bag UI slot zero is wire slot 0x0D. The type word is echoed from the
selected RefItemData row and lets the server reject a stale/spoofed use.
==================
*/
type ItemUseRequest struct {
	Slot     uint8
	TypeWord uint16
}

// DecodeItemUseRequest strictly decodes the three-byte native body.
func DecodeItemUseRequest(payload []byte) (ItemUseRequest, error) {
	var out ItemUseRequest
	r := NewReader(payload)

	slot, err := r.U8()
	if err != nil {
		return out, err
	}
	typeWord, err := r.U16()
	if err != nil {
		return out, err
	}
	if err := r.Done(); err != nil {
		return out, err
	}

	out.Slot = slot
	out.TypeWord = typeWord
	return out, nil
}

/*
==================
ReadItemUseRequest

ReadItemUseRequest reads the three-byte header and returns any tail.
49D240 pet potions and the pet cure append the COS gid (u32). Revival
(TID4 6) appends the pet-item slot (u8). Other families must have an
empty tail; DecodeItemUseRequest still rejects a tail on its own.
==================
*/
func ReadItemUseRequest(payload []byte) (ItemUseRequest, []byte, error) {
	var out ItemUseRequest
	r := NewReader(payload)
	slot, err := r.U8()
	if err != nil {
		return out, nil, err
	}
	typeWord, err := r.U16()
	if err != nil {
		return out, nil, err
	}
	out.Slot = slot
	out.TypeWord = typeWord
	tail := make([]byte, r.Remaining())
	copy(tail, payload[len(payload)-len(tail):])
	return out, tail, nil
}

// OpItemUseVisual is v1.150's external item effect (client 74F540): [u32
// gid][u32 item reference id]. v1.188 numbers it 0x305C.
const OpItemUseVisual uint16 = 0x3449

/*
==================
EncodeItemUseSuccess

EncodeItemUseSuccess builds the successful v1.150 0xB5BD body parsed by
sub_755e40:

	[01][u8 slot][u16 remaining quantity][u16 type word]

==================
*/
func EncodeItemUseSuccess(slot uint8, remaining uint16, typeWord uint16) []byte {
	return NewWriter(6).
		U8(ResultSuccess).
		U8(slot).
		U16(remaining).
		U16(typeWord).
		Payload()
}

/*
==================
EncodeItemUseError

EncodeItemUseError builds the failure arm parsed by sub_755e40:

	[02][u8 notice code]

==================
*/
func EncodeItemUseError(noticeCode uint8) []byte {
	return NewWriter(2).U8(ResultError).U8(noticeCode).Payload()
}
