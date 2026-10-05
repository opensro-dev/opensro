/*
===========================================================================

countjob.go - the premium package's limited-use rows (v1.150 client)

A premium package's limited uses (UIL1: instant return, reverse return,
resurrection N times a day) sit on CIFMagicStateBoard's item count map
(+0x388, keyed by the limited item) and on a kind-5 state slot keyed by
the job. The chat commands /Return, /Reverse Return and /Resurrection
spend them.

	0x3021 start   76F820  [u32 job][u32 remaining seconds][u32 item][u8 uses]
	0x36FC end     76F920  [u32 job][u32 item]
	0x76FD use     7024F0  [u32 job][u32 item] (+[u8 choice] for a reverse return)
	0xB6FD answer  770820  [1][u32 job][u32 item]: one use spent
	                       [2][u8 code]: a category-1 notice

===========================================================================
*/
package wire

const (
	OpCountJobStart  uint16 = 0x3021
	OpCountJobEnd    uint16 = 0x36FC
	OpCountJobUse    uint16 = 0x76FD
	OpCountJobAnswer uint16 = 0xB6FD
)

/*
================
EncodeCountJobStart
================
*/
func EncodeCountJobStart(job, remainingSeconds, item uint32, uses uint8) []byte {
	return NewWriter(13).U32(job).U32(remainingSeconds).U32(item).U8(uses).Payload()
}

/*
================
EncodeCountJobEnd
================
*/
func EncodeCountJobEnd(job, item uint32) []byte {
	return NewWriter(8).U32(job).U32(item).Payload()
}

/*
================
EncodeCountJobSpent
================
*/
func EncodeCountJobSpent(job, item uint32) []byte {
	return NewWriter(9).U8(ResultSuccess).U32(job).U32(item).Payload()
}

/*
================
EncodeCountJobRefusal
================
*/
func EncodeCountJobRefusal(code uint8) []byte {
	return NewWriter(2).U8(ResultError).U8(code).Payload()
}

/*
================
DecodeCountJobUse

The use request; choice is zero when the client sent none.
================
*/
func DecodeCountJobUse(payload []byte) (job, item uint32, choice uint8, err error) {
	r := NewReader(payload)
	if job, err = r.U32(); err != nil {
		return
	}
	if item, err = r.U32(); err != nil {
		return
	}
	if r.Remaining() > 0 {
		if choice, err = r.U8(); err != nil {
			return
		}
	}
	err = r.Done()
	return
}
