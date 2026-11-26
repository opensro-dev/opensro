/*
===========================================================================

attendance.go - the attendance calendar snapshot (port-only, not native)

#3255: with SRO_ATTENDANCE on, every entered session is kept told of its
character's attendance month for the client's calendar window: one
snapshot (0x3FD2) on first sight and whenever the month changes - a
check-in, a claim, the 04:00 UTC day turn. 0x3FD2 is a port-only opcode:
no v1.150 client or v1.188 server uses it (0x3FC0..0x3FD1 are the band's
other port-only allocations, server wire and client dispatch searched),
and a client that predates it drops the unknown opcode, so no
release-protocol bump carries it. With the flag off nothing is ever
sent.

Body, little-endian:

	u32 monthKey (YYYYMM of the attendance month)
	u8 today (day of the month, 1..31)
	u8 daysInMonth (the END marker's day)
	u32 checked (bit d-1 set for each checked day d)
	u8 continuityClaimed, u8 accumulationClaimed (bit i set for each
		received tier i)
	u8 continuityTiers, then that many u8 day thresholds
	u8 accumulationTiers, then that many u8 day thresholds
	u64 nextResetMs (the next 04:00 UTC day turn)
	u64 serverNowMs (the build instant; the client anchors its
		countdown on it. It rides last so the sender's changed-bytes
		check can ignore it)

The tiers' reward items are not carried yet: the owner has not named
them (#3255).

===========================================================================
*/

package wire

import "encoding/binary"

// OpAttendanceSnapshot is the port-only S->C attendance calendar snapshot.
const OpAttendanceSnapshot uint16 = 0x3FD2

// AttendanceClockBytes is the trailing serverNowMs a changed-bytes check skips.
const AttendanceClockBytes = 8

/*
================
AttendanceSnapshot

One character's attendance month, encoded by EncodeAttendanceSnapshot.
================
*/
type AttendanceSnapshot struct {
	MonthKey            int
	Today               int
	DaysInMonth         int
	Checked             uint32
	ContinuityClaimed   uint8
	AccumulationClaimed uint8
	ContinuityTiers     []int
	AccumulationTiers   []int
	NextResetMs         int64
	ServerNowMs         int64
}

/*
================
EncodeAttendanceSnapshot

The snapshot's bytes. Tier thresholds clamp to the u8 the body carries
and a tier list is cut at 255 entries.
================
*/
func EncodeAttendanceSnapshot(s AttendanceSnapshot) []byte {
	out := make([]byte, 0, 32+len(s.ContinuityTiers)+len(s.AccumulationTiers))
	out = binary.LittleEndian.AppendUint32(out, uint32(max(0, s.MonthKey)))
	out = append(out, byte(min(max(0, s.Today), 0xff)), byte(min(max(0, s.DaysInMonth), 0xff)))
	out = binary.LittleEndian.AppendUint32(out, s.Checked)
	out = append(out, s.ContinuityClaimed, s.AccumulationClaimed)
	out = appendAttendanceTiers(out, s.ContinuityTiers)
	out = appendAttendanceTiers(out, s.AccumulationTiers)
	out = binary.LittleEndian.AppendUint64(out, uint64(max(0, s.NextResetMs)))
	out = binary.LittleEndian.AppendUint64(out, uint64(max(0, s.ServerNowMs)))
	return out
}

func appendAttendanceTiers(out []byte, tiers []int) []byte {
	tiers = tiers[:min(len(tiers), 0xff)]
	out = append(out, byte(len(tiers)))
	for _, days := range tiers {
		out = append(out, byte(min(max(0, days), 0xff)))
	}
	return out
}
