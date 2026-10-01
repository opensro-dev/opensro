/*
===========================================================================

actionstate.go - the native object-command queue response

75BAA0 reads kind, queued count and the kind-three error byte. 6932D7 and
67D140 cancel on a ground click only when that count is exactly one. Combat,
Trace and pickup share this contract; it is not a pickup-specific latch.

===========================================================================
*/
package wire

const OpActionState uint16 = 0xB2CD

// Action-state kinds shared by the native object-action session.
const (
	// Arm reports the count after accepting an executing or queued command.
	ActionStateKindArm uint8 = 0x01
	// Release reports the remaining count, which need not be zero.
	ActionStateKindRelease uint8 = 0x02
	// ActionStateKindNotice is the refusal-notice form; it is the only kind
	// that carries the trailing error byte. Pickup errors do NOT use it -
	// they surface through the 0xB06D notice - but the wire form exists and
	// the parser must know its length.
	ActionStateKindNotice uint8 = 0x03
)

/*
================
ActionState

State is the command count, not the kind or a boolean. The v1.150 notice
reader consumes one error byte; v1.188's two-byte error is a different wire.
================
*/
type ActionState struct {
	Kind      uint8
	State     uint8
	ErrorCode uint8
}

/*
================
Encode
================
*/
func (a ActionState) Encode() []byte {
	w := NewWriter(3).U8(a.Kind).U8(a.State)
	if a.Kind == ActionStateKindNotice {
		w.U8(a.ErrorCode)
	}
	return w.Payload()
}

/*
================
DecodeActionState
================
*/
func DecodeActionState(payload []byte) (ActionState, error) {
	var out ActionState
	r := NewReader(payload)

	kind, err := r.U8()
	if err != nil {
		return out, err
	}
	state, err := r.U8()
	if err != nil {
		return out, err
	}
	out.Kind = kind
	out.State = state

	if kind == ActionStateKindNotice {
		if out.ErrorCode, err = r.U8(); err != nil {
			return out, err
		}
	}
	return out, r.Done()
}

/*
================
ReleaseActionState

Use zero for a terminal release. Owners retaining a command set its count.
================
*/
func ReleaseActionState() ActionState {
	return ActionState{Kind: ActionStateKindRelease, State: 0}
}

/*
================
NoticeActionState

The client shows code under category 0x19 and retains the reported count.
================
*/
func NoticeActionState(state, code uint8) ActionState {
	return ActionState{Kind: ActionStateKindNotice, State: state, ErrorCode: code}
}

/*
================
ArmActionState

The common single-command admission; a queued replacement reports two.
================
*/
func ArmActionState() ActionState {
	return ActionState{Kind: ActionStateKindArm, State: 1}
}
