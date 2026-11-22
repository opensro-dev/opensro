/*
===========================================================================

jobhonor.go - the Job Honor ranking feed (port-only, not native)

#3530: with SRO_JOB_HONOR on, every entered session is kept told of the
job honor board and its own standing for the in-game Job Honor Rank
window: one snapshot (0x3FC6) when the session is first seen, and a
fresh one whenever its bytes change. 0x3FC6 is a port-only opcode: no
v1.150 client or v1.188 server uses it (0x3FC0..0x3FC5 and 0x3FCB are
the band's other port-only allocations, server wire and client dispatch
searched), and a client that predates it drops the unknown opcode. With
the flag off nothing is ever sent.

The rows are the public board's (platform/jobhonor's BoardRows over
the ledger's standings): the job alias, never the character name, and
no respawn or timing secret - honor has none to keep.

Body, little-endian:

	u64 serverNowMs, u64 nextChangeMs, u64 previousPeriodEndMs (0 = none)
	u16 currentCount, count x row, u16 previousCount, count x row
	own block (below)
	row: u8 job, u8 aliasLength, alias bytes, u32 points,
	     u8 tierHeld, u8 tierTonight
	own: u8 present, u8 job (the standing the place and tiers belong
	     to, 0 = unranked), u16 place (0 = not in the ranking),
	     u8 tierHeld, u8 tierTonight, u32 capLeft,
	     u32 todayTrader, u32 todayThief, u32 todayHunter,
	     u32 monthTrader, u32 monthThief, u32 monthHunter

===========================================================================
*/

package wire

import "encoding/binary"

// OpJobHonorSnapshot is the port-only S->C job honor board and standing.
const OpJobHonorSnapshot uint16 = 0x3FC6

// JobHonorMaxRows bounds one period's rows in a snapshot.
const JobHonorMaxRows = 256

// jobHonorMaxAlias bounds an alias's bytes (a u8 length), as the
// unique status feed bounds its names.
const jobHonorMaxAlias = 64

/*
================
JobHonorRow

One visible board row (platform/jobhonor's BoardRow, ranked): its job
alias, points and the tiers held now and after tonight's change.
================
*/
type JobHonorRow struct {
	Job     int
	Alias   string
	Points  int64
	Tier    int
	Tonight int
}

/*
================
JobHonorOwn

The viewing character's own standing: its place in the current board
(0 when it has no visible row), the tiers of the standing the place
belongs to, the account's cap left today, and its points today and
this month per job.
================
*/
type JobHonorOwn struct {
	Present bool
	Job     int
	Place   int
	Tier    int
	Tonight int
	CapLeft int
	Today   [3]int64
	Month   [3]int64
}

/*
================
appendJobHonorRow
================
*/
func appendJobHonorRow(out []byte, row JobHonorRow) []byte {
	alias := row.Alias
	if len(alias) > jobHonorMaxAlias {
		alias = alias[:jobHonorMaxAlias]
	}
	out = append(out, uint8(row.Job))
	out = append(out, uint8(len(alias)))
	out = append(out, alias...)
	out = binary.LittleEndian.AppendUint32(out, uint32(max(0, row.Points)))
	out = append(out, uint8(row.Tier), uint8(row.Tonight))
	return out
}

/*
================
EncodeJobHonorSnapshot
================
*/
func EncodeJobHonorSnapshot(serverNowMs, nextChangeMs, previousEndMs int64,
	current, previous []JobHonorRow, own JobHonorOwn) []byte {
	if len(current) > JobHonorMaxRows {
		current = current[:JobHonorMaxRows]
	}
	if len(previous) > JobHonorMaxRows {
		previous = previous[:JobHonorMaxRows]
	}
	out := binary.LittleEndian.AppendUint64(nil, uint64(serverNowMs))
	out = binary.LittleEndian.AppendUint64(out, uint64(nextChangeMs))
	out = binary.LittleEndian.AppendUint64(out, uint64(previousEndMs))
	out = binary.LittleEndian.AppendUint16(out, uint16(len(current)))
	for _, row := range current {
		out = appendJobHonorRow(out, row)
	}
	out = binary.LittleEndian.AppendUint16(out, uint16(len(previous)))
	for _, row := range previous {
		out = appendJobHonorRow(out, row)
	}
	if !own.Present {
		return append(out, 0)
	}
	out = append(out, 1, uint8(own.Job))
	out = binary.LittleEndian.AppendUint16(out, uint16(own.Place))
	out = append(out, uint8(own.Tier), uint8(own.Tonight))
	out = binary.LittleEndian.AppendUint32(out, uint32(max(0, own.CapLeft)))
	for _, points := range own.Today {
		out = binary.LittleEndian.AppendUint32(out, uint32(max(0, points)))
	}
	for _, points := range own.Month {
		out = binary.LittleEndian.AppendUint32(out, uint32(max(0, points)))
	}
	return out
}
