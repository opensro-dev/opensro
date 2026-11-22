/*
===========================================================================

uniquestatus.go - the Unique Status feed (port-only, not native)

#3531: with SRO_UNIQUE_STATUS on, the server keeps every entered client
told of every unique's state for the in-game Unique Status window: a
full snapshot when a session is first seen, then one update per unique
whose state changed. 0x3FC4 and 0x3FC5 are port-only opcodes: no v1.150
client or v1.188 server uses them, server wire and client dispatch were
both searched for them, and a client that predates them drops the
unknown opcode. With the flag off nothing is ever sent.

The rows are composed from the same two owners the public API's uniques
page reads (the monster state's live timeline and the unique kill
record), so the window and the site can never disagree. No respawn
delay is carried: when a dead unique returns stays secret (owner,
2026-10-11).

Body, little-endian:

	snapshot: u64 serverNowMs, u16 count, count x row
	update:   u64 serverNowMs, row
	row:      u32 refObjID, u8 nameLength, name bytes, u8 level,
	          u16 regionID, u8 alive (0 or 1), u64 diedAtMs (0 = never),
	          u8 killerLength, killer bytes

===========================================================================
*/

package wire

import "encoding/binary"

// OpUniqueStatusSnapshot is the port-only S->C full unique state list.
const OpUniqueStatusSnapshot uint16 = 0x3FC4

// OpUniqueStatusUpdate is the port-only S->C one-unique state change.
const OpUniqueStatusUpdate uint16 = 0x3FC5

// UniqueStatusMaxRows bounds one snapshot; the rest are left out.
const UniqueStatusMaxRows = 256

// uniqueStatusMaxName bounds a name's bytes (a u8 length).
const uniqueStatusMaxName = 64

// UniqueStatusRow is one unique's state for the window: its display
// name, whether it is alive, and its last recorded kill (zero diedAtMs
// and an empty killer until the first one since the record began).
type UniqueStatusRow struct {
	RefObjID uint32
	Name     string
	Level    uint8
	RegionID uint16
	Alive    bool
	DiedAtMs int64
	Killer   string
}

/*
================
appendUniqueStatusRow
================
*/
func appendUniqueStatusRow(out []byte, row UniqueStatusRow) []byte {
	name := row.Name
	if len(name) > uniqueStatusMaxName {
		name = name[:uniqueStatusMaxName]
	}
	killer := row.Killer
	if len(killer) > uniqueStatusMaxName {
		killer = killer[:uniqueStatusMaxName]
	}
	out = binary.LittleEndian.AppendUint32(out, row.RefObjID)
	out = append(out, uint8(len(name)))
	out = append(out, name...)
	out = append(out, row.Level)
	out = binary.LittleEndian.AppendUint16(out, row.RegionID)
	if row.Alive {
		out = append(out, 1)
	} else {
		out = append(out, 0)
	}
	out = binary.LittleEndian.AppendUint64(out, uint64(row.DiedAtMs))
	out = append(out, uint8(len(killer)))
	return append(out, killer...)
}

/*
================
EncodeUniqueStatusSnapshot
================
*/
func EncodeUniqueStatusSnapshot(rows []UniqueStatusRow, serverNowMs int64) []byte {
	if len(rows) > UniqueStatusMaxRows {
		rows = rows[:UniqueStatusMaxRows]
	}
	out := binary.LittleEndian.AppendUint64(nil, uint64(serverNowMs))
	out = binary.LittleEndian.AppendUint16(out, uint16(len(rows)))
	for _, row := range rows {
		out = appendUniqueStatusRow(out, row)
	}
	return out
}

/*
================
EncodeUniqueStatusUpdate
================
*/
func EncodeUniqueStatusUpdate(row UniqueStatusRow, serverNowMs int64) []byte {
	out := binary.LittleEndian.AppendUint64(nil, uint64(serverNowMs))
	return appendUniqueStatusRow(out, row)
}
