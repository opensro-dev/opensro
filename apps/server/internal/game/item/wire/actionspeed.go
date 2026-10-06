/*
===========================================================================

actionspeed.go - the native action-speed publication

Server 4AA530 publishes parameter 8C separately from movement. The v1.150
client consumes this layout at 775EB0 under opcode 3453.

===========================================================================
*/
package wire

const OpActionSpeed uint16 = 0x3453
const actionSpeedBytes = 8

/*
================
ActionSpeedFrame

The wire carries a denominator, not the reciprocal animation rate.
================
*/
func ActionSpeedFrame(gid uint32, denominator float32) Frame {
	return Frame{Opcode: OpActionSpeed, Payload: NewWriter(actionSpeedBytes).U32(gid).F32(denominator).Payload()}
}
