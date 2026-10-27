package wire

// The equipment-visual pushes (the M1 chain): after a type-0x00 move touches
// an equipment socket, the containers update from the 0xB06D row but the doll
// and the world model keep wearing the old item until these land. Native
// sends them as separate per-entity packets behind the move result.

// OpEquipVisual is the 0x3314 equip visual (handler sub_777800). Both visual
// opcodes end in CInterfaceModel_SetEquipmentSlot on the doll plus a
// StartAnimationBySet restart, so a weapon change also restarts the stance.
const OpEquipVisual uint16 = 0x3314

// OpUnequipVisual is the 0x377C unequip visual (handler sub_777980).
const OpUnequipVisual uint16 = 0x377C

// equipVisualHasOptTail reports whether the 0x3314 payload carries the
// trailing opt-level byte: the handler @0x00777840 tests bit1 / TID1 / TID2
// on the ref word's LOW BYTE alone, and only the equipment class reads the
// extra byte.
func equipVisualHasOptTail(typeFlags uint16) bool {
	lowByte := typeFlags & 0xFF
	return lowByte&0x02 == 0 && lowByte&0x1C == 0x0C && lowByte&0x60 == 0x20
}

// EquipVisual is one 0x3314 payload:
//
//	[u32 gid][u8 0][u32 refObjId] (+[u8 optLevel] when the ref's low byte is
//	equipment-class - exactly when the client will try to read it)
type EquipVisual struct {
	Gid      uint32
	RefObjID uint32
	// TypeFlags is the worn item's type word; it decides whether the
	// opt-level tail is present.
	TypeFlags uint16
	OptLevel  uint8
}

// Encode returns the 0x3314 payload.
func (v EquipVisual) Encode() []byte {
	w := NewWriter(10).U32(v.Gid).U8(0).U32(v.RefObjID)
	if equipVisualHasOptTail(v.TypeFlags) {
		w.U8(v.OptLevel)
	}
	return w.Payload()
}

// DecodeEquipVisual parses a 0x3314 payload. typeFlags cannot be recovered
// from the payload - the client knows it from its own itemdata lookup on
// refObjId - so the caller supplies the word the encoder used.
func DecodeEquipVisual(payload []byte, typeFlags uint16) (EquipVisual, error) {
	var out EquipVisual
	out.TypeFlags = typeFlags
	r := NewReader(payload)

	gid, err := r.U32()
	if err != nil {
		return out, err
	}
	if _, err := r.U8(); err != nil {
		return out, err
	}
	refObjID, err := r.U32()
	if err != nil {
		return out, err
	}
	out.Gid = gid
	out.RefObjID = refObjID

	if equipVisualHasOptTail(typeFlags) {
		if out.OptLevel, err = r.U8(); err != nil {
			return out, err
		}
	}
	return out, r.Done()
}

// UnequipVisual is one 0x377C payload, a fixed 9 bytes with no conditional
// tail:
//
//	[u32 gid][u8 slot][u32 refObjId]
//
// The refObjId only selects the avatar-vs-normal arm; a vacated socket is
// cleared with RefObjID 0 either way.
type UnequipVisual struct {
	Gid      uint32
	Slot     uint8
	RefObjID uint32
}

// UnequipVisualSize is the encoded size of a 0x377C payload.
const UnequipVisualSize = 9

// Encode returns the 0x377C payload.
func (v UnequipVisual) Encode() []byte {
	return NewWriter(UnequipVisualSize).U32(v.Gid).U8(v.Slot).U32(v.RefObjID).Payload()
}

// DecodeUnequipVisual parses a 0x377C payload.
func DecodeUnequipVisual(payload []byte) (UnequipVisual, error) {
	var out UnequipVisual
	r := NewReader(payload)

	gid, err := r.U32()
	if err != nil {
		return out, err
	}
	slot, err := r.U8()
	if err != nil {
		return out, err
	}
	refObjID, err := r.U32()
	if err != nil {
		return out, err
	}
	out.Gid = gid
	out.Slot = slot
	out.RefObjID = refObjID
	return out, r.Done()
}

// EquipVisualFrame wraps an equip visual for a burst.
func EquipVisualFrame(visual EquipVisual) Frame {
	return Frame{Opcode: OpEquipVisual, Payload: visual.Encode()}
}

// UnequipVisualFrame wraps an unequip visual (the socket clear) for a burst.
func UnequipVisualFrame(visual UnequipVisual) Frame {
	return Frame{Opcode: OpUnequipVisual, Payload: visual.Encode()}
}

// InventoryMoveFrames is a successful type-0x00 move burst:
//
//	[0xB06D [1][0][src][dst][qty u16][0]] then the visual pushes
//
// The visual frames ride BEHIND the move result whenever an equipment socket
// changed (one 0x3314 per socket that ended up occupied, one 0x377C clear per
// socket that ended up empty); a bag-only move passes none. Callers build
// them from the post-move occupancy (inventory.EquipVisualChanges), because a
// swap must report what each socket ended up holding, not what moved.
func InventoryMoveFrames(sourceSlot, destSlot uint8, quantity uint16, visualFrames ...Frame) []Frame {
	frames := []Frame{
		{Opcode: OpItemMoveResponse, Payload: EncodeInventoryMoveResult(sourceSlot, destSlot, quantity, nil)},
	}
	return append(frames, visualFrames...)
}
