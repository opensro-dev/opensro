/*
===========================================================================

attendance.go - the attendance calendar snapshot and request (port-only)

#3255: with SRO_ATTENDANCE on, every entered session is kept told of its
character's attendance month for the client's calendar window: one
snapshot (0x3FD2) on first sight and whenever the month changes - a
check-in, a claim, the 04:00 UTC day turn. The client marks today or
claims a reward with 0x7FD2. 0x3FD2/0x7FD2 are port-only: no v1.150
client or v1.188 server uses them (0x3FC0..0x3FD1 and 0x7FCA/0x7FCF are
the band's other port-only allocations, server wire and client dispatch
searched). A client that predates them drops 0x3FD2 and never sends
0x7FD2, so no release-protocol bump carries them. With the flag off
nothing is ever sent and 0x7FD2 is ignored.

Snapshot body, little-endian:

	u32 monthKey (YYYYMM of the attendance month)
	u8 today (day of the month, 1..31)
	u8 daysInMonth (the END marker's day)
	u32 checked (bit d-1 set for each checked day d)
	u8 continuityClaimed, u8 accumulationClaimed (bit i set for each
		received tier i)
	u8 continuityTiers, then that many (u8 days, u16 rewardLen, reward)
	u8 accumulationTiers, then that many (u8 days, u16 rewardLen, reward)
	u64 nextResetMs (the next 04:00 UTC day turn)
	u64 serverNowMs (the build instant; the client anchors its
		countdown on it. It rides last so the sender's changed-bytes
		check can ignore it)

Request body (0x7FD2):

	empty or u8 0                 check in today
	u8 1, u8 tierIndex            claim continuity tier
	u8 2, u8 tierIndex            claim accumulation tier

===========================================================================
*/

package wire

import "encoding/binary"

// OpAttendanceSnapshot is the port-only S->C attendance calendar snapshot.
const OpAttendanceSnapshot uint16 = 0x3FD2

// OpAttendanceCheckIn is the port-only C->S attendance request.
const OpAttendanceCheckIn uint16 = 0x7FD2

// The 0x7FD2 operation bytes.
const (
	AttendanceOpCheckIn           = 0
	AttendanceOpClaimContinuity   = 1
	AttendanceOpClaimAccumulation = 2
)

// AttendanceClockBytes is the trailing serverNowMs a changed-bytes check skips.
const AttendanceClockBytes = 8

/*
================
AttendanceTier

One reward row: the day threshold and the item codename it pays.
================
*/
type AttendanceTier struct {
	Days   int
	Reward string
}

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
	ContinuityTiers     []AttendanceTier
	AccumulationTiers   []AttendanceTier
	NextResetMs         int64
	ServerNowMs         int64
}

/*
================
EncodeAttendanceSnapshot

The snapshot's bytes. Tier thresholds clamp to the u8 the body carries,
a tier list is cut at 255 entries and reward names are cut at 0xffff
bytes.
================
*/
func EncodeAttendanceSnapshot(s AttendanceSnapshot) []byte {
	out := make([]byte, 0, 64)
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

func appendAttendanceTiers(out []byte, tiers []AttendanceTier) []byte {
	tiers = tiers[:min(len(tiers), 0xff)]
	out = append(out, byte(len(tiers)))
	for _, tier := range tiers {
		out = append(out, byte(min(max(0, tier.Days), 0xff)))
		reward := []byte(tier.Reward)
		if len(reward) > 0xffff {
			reward = reward[:0xffff]
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(len(reward)))
		out = append(out, reward...)
	}
	return out
}
