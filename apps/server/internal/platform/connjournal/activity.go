/*
===========================================================================

activity.go - a row's traffic summary (#3307)

The transport counts what each census span did on the wire
(transport/session_activity.go) and hands the summary over when the
span ends. This file owns its stored form: three columns on the row the
summary closes.

	frames_in         every inbound frame of the span; -1 not recorded
	first_handled_ms  the first handled frame's offset from the span's
	                  start; -1 none, or not recorded
	opcodes           JSON {"7034":[count,firstMs],...,"other":[count]}:
	                  the maxStoredOpcodes most frequent handled opcodes
	                  (4-digit hex), the rest summed under "other"

-1 marks a row closed without a summary - a crash closed it, or it was
written before the columns existed - never zero traffic. The columns
are added to an older file with sqlitecol, as every owner does.

===========================================================================
*/
package connjournal

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"opensro.online/server/internal/platform/sqlitecol"
)

// maxStoredOpcodes bounds the opcodes a row names; the rest are summed.
// A row's summary then stays under about 600 bytes.
const maxStoredOpcodes = 32

// otherOpcodesKey names the summed remainder in the stored JSON.
const otherOpcodesKey = "other"

// activityColumns are the summary's columns on the connections table.
var activityColumns = []sqlitecol.Column{
	{Table: "connections", Name: "frames_in", Definition: "INTEGER NOT NULL DEFAULT -1"},
	{Table: "connections", Name: "first_handled_ms", Definition: "INTEGER NOT NULL DEFAULT -1"},
	{Table: "connections", Name: "opcodes", Definition: "TEXT NOT NULL DEFAULT ''"},
}

/*
================
OpcodeActivity

One handled opcode of a span: frames of it, and the first one's offset
from the span's start in milliseconds.
================
*/
type OpcodeActivity struct {
	Opcode  uint16 `json:"opcode"`
	Count   uint32 `json:"count"`
	FirstMs int64  `json:"firstMs"`
}

/*
================
Activity

A span's traffic: what the transport took at its end, and what a read
returns. Other counts the handled frames of the opcodes past the
stored ones.
================
*/
type Activity struct {
	FramesIn       uint32           `json:"framesIn"`
	FirstHandledMs int64            `json:"firstHandledMs"`
	Opcodes        []OpcodeActivity `json:"opcodes"`
	Other          uint32           `json:"other"`
}

/*
================
ensureActivityColumns
================
*/
func ensureActivityColumns(db *sql.DB) error {
	return sqlitecol.Ensure(db, activityColumns)
}

/*
================
encodeOpcodes

The stored JSON: the most frequent opcodes (ties by opcode), the rest
summed into "other" with the summary's own Other.
================
*/
func encodeOpcodes(activity *Activity) string {
	ordered := append([]OpcodeActivity(nil), activity.Opcodes...)
	sortOpcodes(ordered)
	stored := make(map[string][]int64, min(len(ordered), maxStoredOpcodes)+1)
	other := int64(activity.Other)
	for i, opcode := range ordered {
		if i >= maxStoredOpcodes {
			other += int64(opcode.Count)
			continue
		}
		stored[fmt.Sprintf("%04X", opcode.Opcode)] = []int64{int64(opcode.Count), opcode.FirstMs}
	}
	if other > 0 {
		stored[otherOpcodesKey] = []int64{other}
	}
	encoded, _ := json.Marshal(stored)
	return string(encoded)
}

/*
================
decodeActivity

A row's summary, or nil when the row has none (frames_in -1). A stored
list that does not parse is an error: the writer is this package.
================
*/
func decodeActivity(framesIn, firstHandledMs int64, opcodes string) (*Activity, error) {
	if framesIn < 0 {
		return nil, nil
	}
	activity := &Activity{FramesIn: uint32(framesIn), FirstHandledMs: firstHandledMs, Opcodes: []OpcodeActivity{}}
	if opcodes == "" {
		return activity, nil
	}
	var stored map[string][]int64
	if err := json.Unmarshal([]byte(opcodes), &stored); err != nil {
		return nil, fmt.Errorf("connjournal: opcodes %q: %w", opcodes, err)
	}
	for key, values := range stored {
		if key == otherOpcodesKey {
			if len(values) > 0 {
				activity.Other = uint32(values[0])
			}
			continue
		}
		opcode, err := strconv.ParseUint(key, 16, 16)
		if err != nil || len(values) != 2 {
			return nil, fmt.Errorf("connjournal: opcodes entry %q", key)
		}
		activity.Opcodes = append(activity.Opcodes,
			OpcodeActivity{Opcode: uint16(opcode), Count: uint32(values[0]), FirstMs: values[1]})
	}
	sortOpcodes(activity.Opcodes)
	return activity, nil
}

/*
================
sortOpcodes

Most frequent first, ties by opcode: the one order a summary is stored
and read in.
================
*/
func sortOpcodes(opcodes []OpcodeActivity) {
	sort.Slice(opcodes, func(i, j int) bool {
		if opcodes[i].Count != opcodes[j].Count {
			return opcodes[i].Count > opcodes[j].Count
		}
		return opcodes[i].Opcode < opcodes[j].Opcode
	})
}
