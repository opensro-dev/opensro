package wire

import "fmt"

// Action-pane command wire contracts: the 0x324B emote/action channel the
// client's Action-command dispatcher sub_695420 /
// CGInterface_ExecuteActionCommand composes; every layout below is pinned
// to that fold and to the client's inbound handlers (addresses cited per
// constant).

// OpActionEmote is BOTH directions of the emote/action channel - the native
// client sends AND receives opcode 0x324B with different bodies:
//
//   - C->S request: [u8 action]. Emitted by sub_695420's emote arms
//     (@0x695b09 cmd 0xfa0, @0x695b9a jump_table_6960d8 cmds 0xfa1..0xfa6,
//     @0x695ee6 cmd 0xfab) over CanSendOpcode(0x324B)/CMsgStreamBuffer.
//   - S->C push: [u32 gid][u8 action]. Consumed by sub_778190 /
//     CPSMission_HandlePacket324B (registered by sub_74d330 entry #56): the
//     client resolves the gid and plays the action via SetMotionState
//     (state 0xd for the CICUser family, state 7 otherwise).
const OpActionEmote uint16 = 0x324B

// Emote action codes: the complete value space the v1.150 client can emit
// on the C->S 0x324B request. The names pair the actionwnddata.txt emote
// records (4000..4006, group 4) with the wire byte each record's command id
// stages in sub_695420's jump_table_6960d8 (cmd 0xfa0 -> 0, 0xfa1 -> 6,
// 0xfa2 -> 1, 0xfa3 -> 5, 0xfa4 -> 2, 0xfa5 -> 3, 0xfa6 -> 4; cmd 0xfab
// also emits 0). Anything above EmoteActionMax cannot come from a retail
// client and must be refused.
const (
	EmoteActionGreeting uint8 = 0 // 4000 UIIT_CTL_EMOT_GREETING_TT (also cmd 0xfab)
	EmoteActionPokun    uint8 = 1 // 4002 UIIT_CTL_EMOT_POKUN_TT (fist-palm salute)
	EmoteActionRush     uint8 = 2 // 4004 UIIT_CTL_EMOT_RUSH_TT
	EmoteActionJoy      uint8 = 3 // 4005 UIIT_CTL_EMOT_JOY_TT
	EmoteActionNo       uint8 = 4 // 4006 UIIT_CTL_EMOT_NO_TT
	EmoteActionYes      uint8 = 5 // 4003 UIIT_CTL_EMOT_YES_TT
	EmoteActionLaugh    uint8 = 6 // 4001 UIIT_CTL_EMOT_LAUGH_TT

	// EmoteActionMax bounds the client-emittable code space (0..6).
	EmoteActionMax uint8 = 6
)

// DecodeActionEmoteRequest parses a C->S 0x324B body: exactly one action
// byte inside the client-emittable space. Short, over-long and out-of-range
// payloads are all errors - a permissive decode here would let a hostile
// client drive arbitrary SetMotionState params on every peer's client.
func DecodeActionEmoteRequest(payload []byte) (uint8, error) {
	r := NewReader(payload)
	action, err := r.U8()
	if err != nil {
		return 0, err
	}
	if err := r.Done(); err != nil {
		return 0, err
	}
	if action > EmoteActionMax {
		return 0, fmt.Errorf("wire: emote action 0x%02X outside the client-emitted space 0..%d",
			action, EmoteActionMax)
	}
	return action, nil
}

// ActionEmotePush is one S->C 0x324B body: [u32 gid][u8 action], the exact
// read order of sub_778190 / CPSMission_HandlePacket324B (u32 via
// CMsgStreamBuffer_Read then the action byte).
type ActionEmotePush struct {
	Gid    uint32
	Action uint8
}

// Encode returns the S->C 0x324B payload.
func (p ActionEmotePush) Encode() []byte {
	return NewWriter(5).U32(p.Gid).U8(p.Action).Payload()
}

// DecodeActionEmotePush parses an S->C 0x324B body (test support).
func DecodeActionEmotePush(payload []byte) (ActionEmotePush, error) {
	var out ActionEmotePush
	r := NewReader(payload)
	gid, err := r.U32()
	if err != nil {
		return out, err
	}
	action, err := r.U8()
	if err != nil {
		return out, err
	}
	out.Gid = gid
	out.Action = action
	return out, r.Done()
}
