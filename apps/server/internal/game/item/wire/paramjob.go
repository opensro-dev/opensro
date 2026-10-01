/*
===========================================================================

paramjob.go - the item parameter job board rows (v1.150 client)

An internal param item (EXP and skill-EXP scrolls) runs as a timed
ParamKeeper job on the server. The client shows it on CIFMagicStateBoard as
a kind-4 row keyed by the internal item's reference id
(CGInterface_UpdateMagicStateSlot). Three handlers carry it, registered by
CPSMission_RegisterSecondaryPacketHandlers (774F40):

	0x3602 start   76F6E0  [u32 owner][u32 remaining seconds][u32 item ref]
	0x32AF resume  76F750  the same body, re-raised after world entry
	0x36D4 end     76F7C0  [u32 owner][u32 item ref]

The owner word is read and discarded by all three handlers; the server sends
the character's object id, the identity its own 0x325F job record carries.

===========================================================================
*/
package wire

const (
	OpParamJobStart  uint16 = 0x3602
	OpParamJobResume uint16 = 0x32AF
	OpParamJobEnd    uint16 = 0x36D4
)

/*
================
EncodeParamJobRow

The start/resume body.
================
*/
func EncodeParamJobRow(owner, remainingSeconds, itemRefObjID uint32) []byte {
	return NewWriter(12).U32(owner).U32(remainingSeconds).U32(itemRefObjID).Payload()
}

/*
================
EncodeParamJobEnd
================
*/
func EncodeParamJobEnd(owner, itemRefObjID uint32) []byte {
	return NewWriter(8).U32(owner).U32(itemRefObjID).Payload()
}
