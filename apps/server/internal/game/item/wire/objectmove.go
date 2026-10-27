package wire

// Position is the region-relative placement shared by the movement opcodes and
// the ground-item spawn row.
type Position struct {
	RegionID uint16
	X        float32
	Y        float32
	Z        float32
	// Heading is a full-circle u16 (divided by 65535.0 by the client). This is
	// NOT interchangeable with PickupAnim.Heading, which is a byte divided by
	// 255.0; convert with HeadingByteFromAngle.
	Heading uint16
}

// ObjectSourceMove is the 0x30E3 remote-entity source move.
//
// Handler sub_775cb0 reads the payload in three steps: @0x00775cc1 a 2-byte
// region, @0x00775ccf a single 14-byte block covering x/y/z and the heading,
// then @0x00775cdd the 4-byte gid. The gid therefore arrives LAST.
//
// ObjectSourceCorrection (0xB2F5) carries the same six fields in the mirror
// order with the gid FIRST, and both encode to 20 bytes. Transposing them is
// silent on the wire, so use the right type rather than hand-rolling either.
type ObjectSourceMove struct {
	Position
	Gid uint32
}

// ObjectMoveSize is the encoded size of a 0x30E3 or 0xB2F5 payload.
const ObjectMoveSize = 20

// Encode returns the 0x30E3 payload:
// [u16 region][f32 x][f32 y][f32 z][u16 heading][u32 gid].
func (m ObjectSourceMove) Encode() []byte {
	return NewWriter(ObjectMoveSize).
		U16(m.RegionID).
		F32(m.X).
		F32(m.Y).
		F32(m.Z).
		U16(m.Heading).
		U32(m.Gid).
		Payload()
}

// DecodeObjectSourceMove parses a 0x30E3 payload.
func DecodeObjectSourceMove(payload []byte) (ObjectSourceMove, error) {
	var out ObjectSourceMove
	r := NewReader(payload)

	position, err := readPosition(r)
	if err != nil {
		return out, err
	}
	gid, err := r.U32()
	if err != nil {
		return out, err
	}
	if err := r.Done(); err != nil {
		return out, err
	}

	out.Position = position
	out.Gid = gid
	return out, nil
}

// ObjectSourceCorrection is the 0xB2F5 remote-entity position correction, the
// lenient (strict=0) sibling of the 0xB738 move ack. It hard-stops the
// entity's PathCtl and repositions it.
//
// Layout is asm-pinned at asm.asm @0x00775b62 (sub_775b50): the gid comes
// FIRST here, the mirror image of 0x30E3.
type ObjectSourceCorrection struct {
	Gid uint32
	Position
}

// Encode returns the 0xB2F5 payload:
// [u32 gid][u16 region][f32 x][f32 y][f32 z][u16 heading].
func (c ObjectSourceCorrection) Encode() []byte {
	return NewWriter(ObjectMoveSize).
		U32(c.Gid).
		U16(c.RegionID).
		F32(c.X).
		F32(c.Y).
		F32(c.Z).
		U16(c.Heading).
		Payload()
}

// DecodeObjectSourceCorrection parses a 0xB2F5 payload.
func DecodeObjectSourceCorrection(payload []byte) (ObjectSourceCorrection, error) {
	var out ObjectSourceCorrection
	r := NewReader(payload)

	gid, err := r.U32()
	if err != nil {
		return out, err
	}
	position, err := readPosition(r)
	if err != nil {
		return out, err
	}
	if err := r.Done(); err != nil {
		return out, err
	}

	out.Gid = gid
	out.Position = position
	return out, nil
}

// ObjectStateRefresh is the 0x3122 discrete state channel update
// (handler sub_777b60, asm-pinned at asm.asm @0x00777b62).
type ObjectStateRefresh struct {
	Gid uint32
	// StateType selects the channel; see the StateChannel* constants.
	StateType uint8
	Value     uint8
}

// ObjectStateRefreshSize is the encoded size of a 0x3122 payload.
const ObjectStateRefreshSize = 6

// Encode returns the 0x3122 payload: [u32 gid][u8 stateType][u8 value].
func (s ObjectStateRefresh) Encode() []byte {
	return NewWriter(ObjectStateRefreshSize).
		U32(s.Gid).
		U8(s.StateType).
		U8(s.Value).
		Payload()
}

// DecodeObjectStateRefresh parses a 0x3122 payload.
func DecodeObjectStateRefresh(payload []byte) (ObjectStateRefresh, error) {
	var out ObjectStateRefresh
	r := NewReader(payload)

	gid, err := r.U32()
	if err != nil {
		return out, err
	}
	stateType, err := r.U8()
	if err != nil {
		return out, err
	}
	value, err := r.U8()
	if err != nil {
		return out, err
	}
	if err := r.Done(); err != nil {
		return out, err
	}

	out.Gid = gid
	out.StateType = stateType
	out.Value = value
	return out, nil
}

// readPosition consumes the region/x/y/z/heading run shared by the movement
// opcodes and the ground-item spawn row.
func readPosition(r *Reader) (Position, error) {
	var out Position

	regionID, err := r.U16()
	if err != nil {
		return out, err
	}
	x, err := r.F32()
	if err != nil {
		return out, err
	}
	y, err := r.F32()
	if err != nil {
		return out, err
	}
	z, err := r.F32()
	if err != nil {
		return out, err
	}
	heading, err := r.U16()
	if err != nil {
		return out, err
	}

	out.RegionID = regionID
	out.X = x
	out.Y = y
	out.Z = z
	out.Heading = heading
	return out, nil
}
