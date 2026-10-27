package wire

// Type-flag band predicates for the CIItem ground spawn row.
//
// The row's shape depends on the item's 16-bit itemdata type-flag word, which
// the client's CPSMission_DeserializeObjectSpawn dispatch cascade tests
// bit-field by bit-field before handing the stream to
// CIItem_ParseGroundSpawn. All three bands
// share the "not a character, item class 0x0C" prefix and then diverge on the
// 0x60 pair and the 0x780 group, so they are mutually exclusive.

func isItemClass(typeFlags uint16) bool {
	return typeFlags&0x02 == 0 && typeFlags&0x1C == 0x0C
}

// PackTypeFlags packs the itemdata TypeID1..4 columns into the RefItemData
// type-flag word the spawn rows and equip rules dispatch on: bit 1 ("not
// classifiable") clear, bits 2-4 TID1, bits 5-6 TID2, bits 7-10 TID3,
// bits 11-15 TID4.
//
// This is the same packing the fixture applies when it builds a gold-heap row
// from the itemdata typeIds (server.mjs moveMissionItem type-0x0A leg).
func PackTypeFlags(tid1, tid2, tid3, tid4 uint8) uint16 {
	return uint16(tid1&0x07)<<2 |
		uint16(tid2&0x03)<<5 |
		uint16(tid3&0x0F)<<7 |
		uint16(tid4&0x1F)<<11
}

// IsEquipmentBand reports whether the row carries the one-byte equipment
// discard field.
func IsEquipmentBand(typeFlags uint16) bool {
	return isItemClass(typeFlags) && typeFlags&0x60 == 0x20
}

// IsEtcBand reports the ordinary ETC/item family parsed by the
// CSOItem_ParseFromStream non-gold branch. Its inventory body carries a u16
// stack count, followed by the small subtype-specific tails below.
func IsEtcBand(typeFlags uint16) bool {
	return isItemClass(typeFlags) && typeFlags&0x60 == 0x60
}

// IsMonsterCapsule tests TID 3.2.2, the monster mask. Its CSOItem body
// is the RefObjID and the captured monster (client CSOItem_Deserialize).
func IsMonsterCapsule(typeFlags uint16) bool {
	return isItemClass(typeFlags) && typeFlags&0x60 == 0x40 && typeFlags&0x780 == 0x100
}

// IsCosSummoner is the client's CItemData_IsCosSummoner (TID 3.2.1, the
// pet flutes and scrolls). Its body is the RefObjID and the native
// summoner record:
// u8 1 when no pet was ever made from it, else the pet's state,
// RefObj, name and timers, which this server does not keep per item.
func IsCosSummoner(typeFlags uint16) bool {
	return isItemClass(typeFlags) && typeFlags&0x60 == 0x40 && typeFlags&0x780 == 0x80
}

// cosSummonerNoRecord is the native byte for a summoner without a pet.
const cosSummonerNoRecord = 1

// UsesIndexedMagicParams is the exact predicate of the client's
// CItemData_IsGachaCard_Tid3_14_Tid4_2. Gacha result
// cards (TID 3.3.14.2) carry two opaque u64 values that the client indexes as
// key 0 = reward RefObjID and key 1 = reward quantity; they are not
// magicoption.txt reinforce rows.
func UsesIndexedMagicParams(typeFlags uint16) bool {
	return IsEtcBand(typeFlags) &&
		typeFlags&0x780 == 0x700 &&
		typeFlags&0xF800 == 0x1000
}

// EtcCarriesPlusByte is the client's CItemData_IsConsumableFamily11Kind1
// and CItemData_IsConsumableFamily11Kind2 pair, consumed by the ETC
// parser after its u16 stack count (TID3 11, TID4 1 or 2).
func EtcCarriesPlusByte(typeFlags uint16) bool {
	return IsEtcBand(typeFlags) &&
		typeFlags&0x780 == 0x580 &&
		(typeFlags&0xF800 == 0x0800 || typeFlags&0xF800 == 0x1000)
}

// IsCodenameBand reports whether the row carries a length-prefixed ASCII
// string (the ETC 0x400 trade goods and 0x480 quest item groups): the owner
// label CIItem_ParseGroundSpawn (client 86E5D8) keeps at +0x288 and the
// name plate shows as "(*owner)" for trade goods (client 86E84D),
// "(owner)" otherwise.
func IsCodenameBand(typeFlags uint16) bool {
	if !isItemClass(typeFlags) || typeFlags&0x60 != 0x60 {
		return false
	}
	group := typeFlags & 0x780
	return group == 0x400 || group == 0x480
}

// IsGoldBand reports whether the row carries a u32 amount: the whole group
// 0x280 (TID 3.3.5), as the v1.150 client parses it (ItemTid_IsEtcType5
// in client CSOItem_Deserialize, 78C9E4). It is a wire width, not a
// meaning:
// the group also holds the later shop tokens (IsGoldHeap).
func IsGoldBand(typeFlags uint16) bool {
	return isItemClass(typeFlags) && typeFlags&0x60 == 0x60 && typeFlags&0x780 == 0x280
}

// tid4Mask selects TID4; shopTokenTID is TID4 1 inside group 0x280:
// the later shop tokens (ITEM_ETC_SD_TOKEN_*, ITEM_ETC_ARENA_COIN).
const (
	tid4Mask     = 0xF800
	shopTokenTID = 0x800
)

// IsGoldHeap reports a gold heap (TID 3.3.5.0, ITEM_ETC_GOLD_*): the gold
// band without the shop tokens, which v1.188 excludes the same way in its
// gold test and its instance serializer. Native v1.150 data has no tokens,
// so the two agree there.
func IsGoldHeap(typeFlags uint16) bool {
	return IsGoldBand(typeFlags) && typeFlags&tid4Mask != shopTokenTID
}

// GroundItemRow is one CIItem ground-drop entity row.
//
// It rides either the single-object spawn 0x30D7 or an object-list chunk
// 0x3417. The single-object path additionally reads a one-byte appear flag
// through the entity's vtable +0x68 (client CIItem_DeserializeSpawnState)
// that the list path skips, so
// callers select it with WithAppearTail.
type GroundItemRow struct {
	RefObjID uint32
	// TypeFlags is the itemdata type-flag word; it selects which conditional
	// fields are present. See the IsXxxBand predicates.
	TypeFlags uint16
	// Codename is the registry's own bookkeeping; it is not on the wire.
	Codename string
	// OwnerLabel is the IsCodenameBand string: a trade goods drop's original
	// job alias (the goods record's owner), empty otherwise.
	OwnerLabel string
	// GoldAmount is only carried by IsGoldBand rows.
	GoldAmount uint32
	Gid        uint32
	Position
	// HasOwner gates the conditional owner JID dword in
	// client CIItem_ParseGroundSpawn. Omitting OwnerJID while setting this
	// byte shifts tint and the optional appear tail into the dword read.
	HasOwner uint8
	OwnerJID uint32
	Tint     uint8
	// WithAppearTail selects the 0x30D7 single-object form, which appends the
	// appear byte that drives the drop-in presentation.
	WithAppearTail bool
	AppearFlag     uint8
}

// Encode returns the ground-item spawn row.
//
// The codename band emits the owner label, empty when the drop has none.
// The client formats a non-empty label onto the name plate only for this
// band (client 86E82E..86E8AD), so the server that wrote it sent the
// goods' owner:
// dropped trade goods show the trader who bought them.
func (g GroundItemRow) Encode() []byte {
	w := NewWriter(40).U32(g.RefObjID)

	switch {
	case IsEquipmentBand(g.TypeFlags):
		w.U8(0)
	case IsCodenameBand(g.TypeFlags):
		w.Str(g.OwnerLabel)
	case IsGoldBand(g.TypeFlags):
		w.U32(g.GoldAmount)
	}

	w.U32(g.Gid).
		U16(g.RegionID).
		F32(g.X).
		F32(g.Y).
		F32(g.Z).
		U16(g.Heading).
		U8(g.HasOwner)
	if g.HasOwner != 0 {
		w.U32(g.OwnerJID)
	}
	w.U8(g.Tint)

	if g.WithAppearTail {
		w.U8(g.AppearFlag)
	}

	return w.Payload()
}

// DecodeGroundItemRow parses a ground-item spawn row.
//
// typeFlags cannot be recovered from the payload - the client knows it from
// its own itemdata lookup on refObjId - so the caller supplies the same word
// the encoder used. withAppearTail selects the 0x30D7 form.
func DecodeGroundItemRow(payload []byte, typeFlags uint16, withAppearTail bool) (GroundItemRow, error) {
	var out GroundItemRow
	out.TypeFlags = typeFlags
	out.WithAppearTail = withAppearTail

	r := NewReader(payload)

	refObjID, err := r.U32()
	if err != nil {
		return out, err
	}
	out.RefObjID = refObjID

	switch {
	case IsEquipmentBand(typeFlags):
		if _, err := r.U8(); err != nil {
			return out, err
		}
	case IsCodenameBand(typeFlags):
		length, err := r.U16()
		if err != nil {
			return out, err
		}
		label, err := r.Bytes(int(length))
		if err != nil {
			return out, err
		}
		out.OwnerLabel = string(label)
	case IsGoldBand(typeFlags):
		if out.GoldAmount, err = r.U32(); err != nil {
			return out, err
		}
	}

	if out.Gid, err = r.U32(); err != nil {
		return out, err
	}
	position, err := readPosition(r)
	if err != nil {
		return out, err
	}
	out.Position = position

	if out.HasOwner, err = r.U8(); err != nil {
		return out, err
	}
	if out.HasOwner != 0 {
		if out.OwnerJID, err = r.U32(); err != nil {
			return out, err
		}
	}
	if out.Tint, err = r.U8(); err != nil {
		return out, err
	}
	if withAppearTail {
		if out.AppearFlag, err = r.U8(); err != nil {
			return out, err
		}
	}

	if err := r.Done(); err != nil {
		return out, err
	}
	return out, nil
}
