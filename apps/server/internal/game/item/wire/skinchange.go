package wire

// OpSkinChange is the v1.150 client's
// CPSMission_OnCharacterSkinChangeSuccess0x323A (client 7641D0); the
// v1.188 server sends the same body as 0x3207 from its
// monster-transform and duplicate-transform applications.
const OpSkinChange uint16 = 0x323A

// TransformSkin is the server's transform-skin block: the skin RefObj and,
// for a player skin (a Duplicate), the copied record byte and the worn
// items that are set (u8, u8 count, u32 each). The client restores the own
// model when the msch instance ends, not through this packet.
type TransformSkin struct {
	RefObjID  uint32
	Player    bool
	Shape     uint8
	Equipment [9]uint32
}

func (s TransformSkin) write(w *Writer) *Writer {
	w.U32(s.RefObjID)
	if !s.Player {
		return w
	}
	var worn []uint32
	for _, id := range s.Equipment {
		if id != 0 {
			worn = append(worn, id)
		}
	}
	w.U8(s.Shape).U8(uint8(len(worn)))
	for _, id := range worn {
		w.U32(id)
	}
	return w
}

// SkinChange is [u32 gid][skin block].
type SkinChange struct {
	GID  uint32
	Skin TransformSkin
}

func (s SkinChange) Encode() []byte {
	return s.Skin.write(NewWriter(8).U32(s.GID)).Payload()
}
