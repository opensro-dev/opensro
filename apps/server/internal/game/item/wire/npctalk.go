/*
===========================================================================

npctalk.go - Package wire.

===========================================================================
*/

package wire

const (
	OpNpcActionRequest  uint16 = 0x7338
	OpNpcInteractionAck uint16 = 0xB338
	OpNpcDialog         uint16 = 0x3773
)

/*
================
DecodeNpcActionRequest

DecodeNpcActionRequest is the common CIFNPCTalk body:
[boundNpcGid:u32][capabilityMask:u32], exact length.
================
*/
func DecodeNpcActionRequest(payload []byte) (uint32, uint32, error) {
	reader := NewReader(payload)
	gid, err := reader.U32()
	if err != nil {
		return 0, 0, err
	}
	mask, err := reader.U32()
	if err != nil {
		return 0, 0, err
	}
	return gid, mask, reader.Done()
}

/*
================
EncodeNpcInteractionAck

EncodeNpcInteractionAck is CPSMission 0xB338 kind 1: the u32 mask is
dispatched by the client's seven-case native action switch.
================
*/
func EncodeNpcInteractionAck(mask uint32) []byte {
	return NewWriter(5).U8(1).U32(mask).Payload()
}

/*
================
EncodeNpcInteractionRefusal

EncodeNpcInteractionRefusal is CPSMission 0xB338 kind 2 (75AE50): the NPC
refused the function request, and the client shows category 13 for code.
================
*/
func EncodeNpcInteractionRefusal(code uint8) []byte {
	return NewWriter(2).U8(2).U8(code).Payload()
}

/*
================
EncodeNpcDialogConfirm

EncodeNpcDialogConfirm is native kind 3: a prompt followed by Yes/No rows.
The response is one choice byte (2=yes, 3=no).
================
*/
func EncodeNpcDialogConfirm(promptSymbol string) []byte {
	return encodeNpcDialog(3, promptSymbol, nil)
}

// npcDialogExitSymbol is the Exit row of a closing answer.
const npcDialogExitSymbol = "SN_TALK_COMMON_EXIT"

/*
================
EncodeNpcDialogOptions

EncodeNpcDialogOptions is native kind 4: a prompt and up to 251 narrow
option symbols. Client row responses start at byte 5.
================
*/
func EncodeNpcDialogOptions(promptSymbol string, optionSymbols []string) []byte {
	if len(optionSymbols) > 251 {
		optionSymbols = optionSymbols[:251]
	}
	return encodeNpcDialog(4, promptSymbol, optionSymbols)
}

/*
================
EncodeNpcDialogAnswer

A service's closing answer: the line with the one Exit row, kind 4 as the
native talk events send it. The row comes back as the first row's choice
with no conversation open, which ends the talk.
================
*/
func EncodeNpcDialogAnswer(promptSymbol string) []byte {
	return EncodeNpcDialogOptions(promptSymbol, []string{npcDialogExitSymbol})
}

/*
================
encodeNpcDialog
================
*/
func encodeNpcDialog(kind uint8, prompt string, options []string) []byte {
	capacity := 4 + len(prompt)
	for _, option := range options {
		capacity += 2 + len(option)
	}
	w := NewWriter(capacity).U8(kind)
	writeNpcDialogNarrow(w, prompt)
	if kind == 4 {
		w.U8(uint8(len(options)))
		for _, option := range options {
			writeNpcDialogNarrow(w, option)
		}
	}
	return w.Payload()
}

/*
================
writeNpcDialogNarrow
================
*/
func writeNpcDialogNarrow(w *Writer, value string) {
	bytes := []byte(value)
	if len(bytes) > 0xffff {
		bytes = bytes[:0xffff]
	}
	w.U16(uint16(len(bytes))).Bytes(bytes)
}

/*
================
DecodeNpcDialogChoice

DecodeNpcDialogChoice is the exact C->S 0x3773 body: one choice byte.
================
*/
func DecodeNpcDialogChoice(payload []byte) (uint8, error) {
	r := NewReader(payload)
	choice, err := r.U8()
	if err != nil {
		return 0, err
	}
	return choice, r.Done()
}
