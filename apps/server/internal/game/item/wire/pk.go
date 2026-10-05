/*
===========================================================================

pk.go - the PK record's live updates

v1.150 CPSMission_OnPkPenaltySeconds0x30F2 (u32 penalty, CICPlayer
+0x188C), ..OnPkDailyCount0x33C4 (u8 daily, +0x1888) and
..OnPkLevelUpdate0x3647 (u16 total, +0x188A). v1.188 sends the same bodies
as 0x30CD, 0x30CE and 0x30D3 (4EAE70, 4EB140, 4EAFD0).

===========================================================================
*/

package wire

const (
	OpPKPenalty uint16 = 0x30f2
	OpPKDaily   uint16 = 0x33c4
	OpPKTotal   uint16 = 0x3647
)

/*
================
PKPenaltyFrame
================
*/
func PKPenaltyFrame(penalty uint32) Frame {
	return Frame{Opcode: OpPKPenalty, Payload: NewWriter(4).U32(penalty).Payload()}
}

/*
================
PKDailyFrame
================
*/
func PKDailyFrame(daily uint8) Frame {
	return Frame{Opcode: OpPKDaily, Payload: NewWriter(1).U8(daily).Payload()}
}

/*
================
PKTotalFrame
================
*/
func PKTotalFrame(total uint16) Frame {
	return Frame{Opcode: OpPKTotal, Payload: NewWriter(2).U16(total).Payload()}
}
