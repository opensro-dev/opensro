/*
===========================================================================

objectselect.go - Package wire.

===========================================================================
*/

package wire

import "fmt"

// Object select/interact plane, C->S opcode 0x745A.
//
// The request is a bare [u32le gid], 4 bytes, with TWO native send sites
// (same opcode, same shape - W3/C3 dual-use pin, server-wave seq 38/51):
//
//   - object select/interact: clicking a world object routes its gid here
//     (live native gateway trace: clicking the spawned NPC_EU_SMITH sent
//     0x745A body 41 0D 03 00 = gid 200001);
//   - stop / clear-marker: sub_692ba0 / CGInterface_ClearLocalDestinationMarker
//     (@0x692c18) sends the marker id after setting the move-stop-pending
//     byte (+0x4fa); the Action pane's case-5 command feeds it the nearest
//     flagged monster gid (sub_861580).
//
// S->C twin 0xB45A (sub_764c60) is the interaction result that opens the
// NPC talk window. The browser reconstruction folded its live-CICNPC arm,
// which retires the old "unconsumable
// response" ruling from server-wave seq 51: this server now answers the
// live roster-NPC and monster grants with the typed encoders below. The
// client fold now also hosts its non-character/local-player and
// CITeleportGate arms, but this server still has no proven response body for
// successful player/ground selects and no static teleport-gate authority.
// It therefore emits only the two authority-backed shapes below. The
// grant/silence policy lives in internal/game/action/select.go.
const (
	// OpObjectSelectRequest is the client's object select/interact request.
	OpObjectSelectRequest uint16 = 0x745A
	// OpObjectSelectResult is the S->C interaction result (0xB45A,
	// sub_764c60), emitted on live roster-NPC and monster grant paths.
	OpObjectSelectResult uint16 = 0xB45A
)

// ObjectSelectRequestSize is the exact 0x745A body size: one u32 gid.
const ObjectSelectRequestSize = 4

/*
================
DecodeObjectSelectRequest

DecodeObjectSelectRequest parses a 0x745A body: exactly [u32le gid].
================
*/
func DecodeObjectSelectRequest(payload []byte) (uint32, error) {
	if len(payload) != ObjectSelectRequestSize {
		return 0, fmt.Errorf("wire: 0x745A body %d bytes, want exactly %d", len(payload), ObjectSelectRequestSize)
	}
	r := NewReader(payload)
	gid, err := r.U32()
	if err != nil {
		return 0, err
	}
	return gid, nil
}

/*
================
EncodeObjectSelectRefusal

EncodeObjectSelectRefusal is the 0xB45A refusal [u8 result=2][u8 code]
(7651F1 consumes it without granting a target; only code 7 shows text).
================
*/
func EncodeObjectSelectRefusal(code uint8) []byte {
	return NewWriter(2).U8(2).U8(code).Payload()
}

/*
================
EncodeNpcObjectSelectResult

EncodeNpcObjectSelectResult composes the S->C 0xB45A grant body for a
live roster NPC, in the folded sub_764c60 field order. A live CICNPC
dynamic-casts to CICharactor (CICNPC -> CICNonuser -> CICharactor), so
the read takes the CHARACTER arm and the vitals-mask byte IS present:

	{u8 result=1}{u32le gid}{u8 vitalsMask=0}{u32le capabilityFlags}{u8 npcExtra}

The bytes must stay identical to the client-side oracle
devComposeSyntheticB45A for the same inputs - the harness parity suite consumes
exactly this shape. Deliberate omissions, all pinned by the fold:

  - vitalsMask stays 0. Bit 1 would append an HP dword this server has
    no vitals plane to source, and bits 2/4 (VITAL_INFO_MP /
    VITAL_INFO_ABNORMAL) are ASSERT-FATAL client-side - mirrored as
    throws in the fold, so they must never ride the wire.
  - capabilityFlags must not carry 0x40000000: that bit appends a u16
    job-transport tail the client correctly expects, but this server has no
    authority value to encode for roster NPCs (the caller's table
    guarantees the bit stays clear).
  - npcExtra 0 appends no menu row; a nonzero value forces the 0x38
    talkbox row (sub_5d3630). DECISION: the roster emits 0 - no roster
    NPC has evidence for the 0x38 row, and forcing it would put a menu
    action on screen that retail never granted these NPCs.

================
*/
func EncodeNpcObjectSelectResult(gid, capabilityFlags uint32, npcExtra uint8) []byte {
	w := NewWriter(11)
	w.U8(1).
		U32(gid).
		U8(0).
		U32(capabilityFlags).
		U8(npcExtra)
	return w.Payload()
}

/*
================
EncodeMonsterObjectSelectResult

EncodeMonsterObjectSelectResult composes the non-CICUser character arm
used when a live monster is selected:

	{u8 result=1}{u32le gid}{u8 vitalsMask=1}{u32le currentHP}{u32le flags=0}

Exact CICMonster is not CICNPC, so there is no npcExtra byte. Flags zero
keeps the @0x00765054 talk-window branch closed while the common
sub_6813e0/sub_67aeb0 selection tail rebuilds the target HUD.
================
*/
func EncodeMonsterObjectSelectResult(gid, currentHP uint32) []byte {
	w := NewWriter(14)
	w.U8(1).
		U32(gid).
		U8(1).
		U32(currentHP).
		U32(0)
	return w.Payload()
}
