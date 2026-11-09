/*
===========================================================================

recallwire.go - the Guild Recall Scroll conversation (0x77C0, 0x7178, 0xB178)

The master spends a Guild Member Summon Scroll (itemuse.go's
itemUseGuildRecall family); every online member is asked with 0x77C0 and
answers with 0x7178; the answer closes with 0xB178. The v1.150 client
stores the request's two dwords verbatim (its summon dialog keeps them
at +0x7C0/+0x7C4 and its seconds at +0x7BC) and echoes them back with
the accept byte, so the port sends its guild id and the master's object
id there and matches the echo against the open window.

===========================================================================
*/

package guild

import (
	"opensro.online/server/internal/game/item/wire"
)

// The recall scroll's opcodes, pinned from the v1.150 client: 0x77C0 is
// registered in the client's second packet table, 0x7178 is composed by
// its summon-request dialog, and 0xB178 is answered by its second-host
// recall handler (result 1 starts the teleport delay bar, result 2 reads
// one notice byte, anything else just closes the dialog).
const (
	OpGuildRecallSummon uint16 = 0x77C0
	OpGuildRecallAnswer uint16 = 0x7178
	OpGuildRecallResult uint16 = 0xB178
)

/*
================
EncodeGuildRecallSummon

The 0x77C0 body the client's recall handler reads: [u32 echoA]
[u32 echoB][u32 window seconds]. The two dwords are opaque to the
client; it refuses to answer while either reads zero.
================
*/
func EncodeGuildRecallSummon(echoA, echoB, windowSeconds uint32) []byte {
	return wire.NewWriter(12).U32(echoA).U32(echoB).U32(windowSeconds).Payload()
}

/*
================
DecodeGuildRecallAnswer

The strict 0x7178 body: [u32 echoA][u32 echoB][u8 accept]. accept is
the dialog's own byte (1 accept, 0 decline); any other value is not a
body the client composes.
================
*/
func DecodeGuildRecallAnswer(payload []byte) (echoA, echoB uint32, accept bool, ok bool) {
	r := wire.NewReader(payload)
	echoA, errA := r.U32()
	echoB, errB := r.U32()
	answer, errC := r.U8()
	if errA != nil || errB != nil || errC != nil || r.Done() != nil {
		return 0, 0, false, false
	}
	if answer > 1 {
		return 0, 0, false, false
	}
	return echoA, echoB, answer == 1, true
}

/*
================
EncodeGuildRecallAccepted

The 0xB178 success arm: [01]. The client starts the teleport delay bar
from the seconds the summon stored, then closes the dialog.
================
*/
func EncodeGuildRecallAccepted() []byte {
	return wire.NewWriter(1).U8(1).Payload()
}

/*
================
EncodeGuildRecallRefused

The 0xB178 refusal arm: [02][u8 notice code]. The later server's answer
carries a u16 code there; the v1.150 client reads one byte, the low
byte of the same notice (wire.ErrCodeGuildRecallRefused).
================
*/
func EncodeGuildRecallRefused(noticeCode uint8) []byte {
	return wire.NewWriter(2).U8(2).U8(noticeCode).Payload()
}

/*
================
EncodeGuildRecallClosed

The 0xB178 closing arm for an answer that arrives after its window
lapsed: any byte but 1 or 2 just closes the dialog.
================
*/
func EncodeGuildRecallClosed() []byte {
	return wire.NewWriter(1).U8(0).Payload()
}
