/*
===========================================================================

moverequest.go - the 0x7738 decoder and the heading-word codec

The client serializer (sub_877cc0) is the only producer of 0x7738, so the
decoder accepts exactly its two forms. The heading-word codec is shared by
every opcode that carries a native 1/65535-circle heading.

===========================================================================
*/
package simulation

import (
	"encoding/binary"
	"fmt"
)

// ClientMovementRequestSize is the encoded size of the native 0x7738 C->S
// ground-click body.
const ClientMovementRequestSize = 9

// ClientAngularMovementRequestSize is the encoded size of the native 0x7738
// angular body: [u8 0][u8 angularMode][u16 headingWord]. The serializer's
// angular arm (sub_877cc0 @0x877ceb) appends exactly 1+2 bytes after the
// mode byte - the xyz words never ride this form (0x877F30 zeroes them in
// the record and the serializer skips them).
const ClientAngularMovementRequestSize = 4

// MaxWadeDepth is the maximum submersion (MAPM water surface above the
// destination y, native units) the retail agent server tolerates for a
// movement destination (server.mjs missionMaxWadeDepth). Wading stays legal
// (dock pools ~0-2u, river fords < ~10u); the Constantinople harbor runs
// 100u+ deep.
const MaxWadeDepth = 12.0

// Angle encoding of the 0x7738/0xB738 heading word, recovered from the
// binary (units, range and wrapping are native, not guessed):
//
//   - UNIT: 1 word unit = 1/65535 of a full 360-degree circle. The client
//     SERIALIZER (sub_877cc0 @0x877ceb; SendSteeringUpdate 0x877540 shares
//     the idiom @0x877621..0x87764d) computes
//     word = trunc(rad * 57.295780181884766 / 360.0 * 65535.0) with the x87
//     rounding control forced to truncate-toward-zero (fnstcw / or 0xc00 /
//     fldcw / fistp). The ACK PARSER (sub_776170 @0x7761c0..0x7761d6: fild /
//     fdiv / fmul / fmul / fstp dword) inverts it as
//     radians = float32(word / 65535.0 * 360.0 * 0.01745329238474369).
//   - CONSTANTS (rizin px against SRO_Client.exe): qword @0xc13df8 =
//     57.295780181884766 (float32(180/pi) promoted to double - a hair ABOVE
//     the exact 57.29577951...), qword @0xc0e450 = 360.0, qword @0xc0e458 =
//     65535.0, qword @0xbc8568 = 0.01745329238474369 (float32(pi/180)
//     promoted to double).
//   - RANGE: producers wrap the yaw into [0, 2*pi) BEFORE converting
//     (sub_853550 Math_YawToHeadingAngle rebases by -pi/2 and wraps), so a
//     live client emits words in [0, 65535]; 2*pi itself lands on 65535
//     because the rad->deg constant's float32 excess pushes 360.0000042
//     degrees / 360 * 65535 = 65535.00077 before the truncation.
//   - WRAPPING: the serializer itself does NOT range-check - the truncated
//     integer stores through a 2-byte append, so an out-of-range yaw wraps
//     through the low 16 bits (two's complement for negatives). The ack
//     consumer re-normalizes with sub_8535a0 Math_NormalizeRadians before
//     applying the yaw (sub_776200 @0x77627d).
const (
	// RadToDegF32 is qword @0xc13df8.
	RadToDegF32 = 57.295780181884766
	// DegToRadF32 is qword @0xbc8568.
	DegToRadF32 = 0.01745329238474369
	// HeadingFullCircleDeg is qword @0xc0e450.
	HeadingFullCircleDeg = 360.0
	// HeadingWordScale is qword @0xc0e458. Note 65535, not 65536: the
	// native codec divides/multiplies by the max word value, so word
	// 65535 IS the full circle, not one unit short of it.
	HeadingWordScale = 65535.0
)

/*
================
HeadingWordFromRadians

Mirrors the client serializer's rad -> wire-word conversion (sub_877cc0
@0x877ceb): truncate toward zero (fistp with RC=0b11), then keep the low 16
bits (the 2-byte append).
================
*/
func HeadingWordFromRadians(rad float64) uint16 {
	deg := rad * RadToDegF32
	return uint16(int64(deg/HeadingFullCircleDeg*HeadingWordScale) & 0xffff)
}

/*
================
RadiansFromHeadingWord

Mirrors the client ack parser's word -> radians conversion (sub_776170
@0x7761c0: fild / fdiv 65535.0 / fmul 360.0 / fmul float32(pi/180) / fstp
dword - the result narrows to float32 on the store, preserved here).
================
*/
func RadiansFromHeadingWord(word uint16) float64 {
	return float64(float32(float64(word) / HeadingWordScale * HeadingFullCircleDeg * DegToRadF32))
}

/*
================
DecodeClientMovementRequest

Parses the native 0x7738 body.

Layout, pinned to the CLIENT SERIALIZER (the only producer): the single
move-request sender CNavigationDeadreckon_SendTargetMovePacket (0x877D80)
feeds every 0x7738 through sub_877cc0 / MoveRequest_SerializePayload, which
emits exactly two forms after the mode byte:

	positional (mode=1): [u8 1][u16 region][i16 x][i16 y][i16 z]  = 9 bytes
	angular    (mode=0): [u8 0][u8 angularMode][u16 headingWord]  = 4 bytes

DUNGEON PIN (negative, closed): the positional arm appends the three int16
offsets UNCONDITIONALLY - rec+0x04..0x08 in one 6-byte append, no
region-conditional branch anywhere in the serializer. The s32
dungeon-coordinate form other SRO builds carry DOES NOT EXIST in v1.150's
client; dungeon-region clicks (regionId & 0x8000) ride this same 9-byte
form, and the sector-grid helpers keep the dungeon bit intact through
interpolation. sub_878080 builds the record with FloatToInt32-packed int16
xyz regardless of region.

The MIDDLE i16 is the HEIGHT (world y), the same slot order the 0xB738 ack
echoes - live capture `01 4F 6B B1 04 50 00 63 01` decodes to mode=1,
region 0x6B4F, x=1201, y=80 (the Europe-start plateau height), z=355.
Community protocol notes name these xOffset/zOffset/yOffset; the axis truth
is x/height/groundZ, which is what MovementRequest carries.

ANGULAR (walk-in-direction) arm: the ONLY producer of the mode-0 form on
this opcode is CNavigationDeadreckon_SendAngleMoveCommand (0x877F30),
reached from a ground-pick miss in CGInterface_MoveToWorldPoint (0x6932A0)
and from the Up/Down keys through MovementController_SetDirectionVector
(0x877FB0). It zeroes the xyz words, sets angularMode = AngularFlagGo and
converts the yaw through the heading-word codec above; the mover then walks
that heading until blocked (direction.go). SendSteeringUpdate (0x877540)
shares the conversion but ships OTHER opcodes (0x72CF, or 0x769E tag 0x04
for a mount), never 0x7738.

Mode bytes the serializer cannot emit (anything but 0/1) stay a loud
unsupported-mode refusal, deliberately DISTINCT from a malformed frame of a
known form.
================
*/
func DecodeClientMovementRequest(payload []byte) (MovementRequest, *MoveError) {
	var out MovementRequest
	if len(payload) == 0 {
		return out, &MoveError{NativeErrorCode: NativeErrorInvalidRequest, Reason: "malformedMovementRequest len=0"}
	}
	switch payload[0] {
	case MovementAckAngularMode:
		if len(payload) != ClientAngularMovementRequestSize {
			return out, &MoveError{
				NativeErrorCode: NativeErrorInvalidRequest,
				Reason:          fmt.Sprintf("malformedMovementRequest angular len=%d", len(payload)),
			}
		}
		out.Mode = MovementAckAngularMode
		out.AngularMode = payload[1]
		out.HeadingWord = binary.LittleEndian.Uint16(payload[2:4])
		return out, nil
	case MovementAckDestinationMode:
		if len(payload) != ClientMovementRequestSize {
			return out, &MoveError{
				NativeErrorCode: NativeErrorInvalidRequest,
				Reason:          fmt.Sprintf("malformedMovementRequest len=%d", len(payload)),
			}
		}
		out.Mode = payload[0]
		out.RegionID = binary.LittleEndian.Uint16(payload[1:3])
		out.X = float64(int16(binary.LittleEndian.Uint16(payload[3:5])))
		out.Y = float64(int16(binary.LittleEndian.Uint16(payload[5:7])))
		out.Z = float64(int16(binary.LittleEndian.Uint16(payload[7:9])))
		return NormalizeMovementRequest(out), nil
	default:
		return out, &MoveError{
			NativeErrorCode: NativeErrorInvalidRequest,
			Reason:          fmt.Sprintf("unsupportedMovementMode 0x%02X (sub_877cc0 emits only 0 angular / 1 positional)", payload[0]),
		}
	}
}

/*
================
EncodeClientMovementRequest

The 0x7738 ground-click body DecodeClientMovementRequest reads: mode 1,
the region and the int16 region-local position the client serializes
(sub_877cc0). Server-issued steps that must take the client's own movement
path, such as a rider's pursuit through the vehicle owner, are built here.
================
*/
func EncodeClientMovementRequest(m MovementRequest) []byte {
	payload := make([]byte, ClientMovementRequestSize)
	payload[0] = MovementAckDestinationMode
	binary.LittleEndian.PutUint16(payload[1:3], m.RegionID)
	binary.LittleEndian.PutUint16(payload[3:5], uint16(int16(m.X)))
	binary.LittleEndian.PutUint16(payload[5:7], uint16(int16(m.Y)))
	binary.LittleEndian.PutUint16(payload[7:9], uint16(int16(m.Z)))
	return payload
}

/*
================
DecodeClientHeadingRequest

Parses the [u16 heading] body shared by 0x72CF (steer) and 0x72F5 (stop),
and by the 0x769E mount arms 0x03 and 0x04 after their gid and tag.
================
*/
func DecodeClientHeadingRequest(payload []byte) (uint16, *MoveError) {
	if len(payload) != ClientHeadingRequestSize {
		return 0, &MoveError{
			NativeErrorCode: NativeErrorInvalidRequest,
			Reason:          fmt.Sprintf("malformedHeadingRequest len=%d", len(payload)),
		}
	}
	return binary.LittleEndian.Uint16(payload), nil
}
