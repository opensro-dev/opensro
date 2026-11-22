/*
===========================================================================

uniquestatus_test.go - the Unique Status frames' bytes

===========================================================================
*/
package wire

import (
	"encoding/binary"
	"testing"
)

/*
================
TestEncodeUniqueStatusFrames

The snapshot and update layouts the browser client decodes: a server
time, the row count on a snapshot, and each row's fields in order.
================
*/
func TestEncodeUniqueStatusFrames(t *testing.T) {
	row := UniqueStatusRow{RefObjID: 1954, Name: "Tiger Girl", Level: 20, RegionID: 25001,
		Alive: false, DiedAtMs: 1_700_000_100_000, Killer: "Lune"}
	snapshot := EncodeUniqueStatusSnapshot([]UniqueStatusRow{row}, 1_700_000_200_000)
	if got := binary.LittleEndian.Uint64(snapshot); got != 1_700_000_200_000 {
		t.Fatalf("snapshot serverNowMs = %d", got)
	}
	if got := binary.LittleEndian.Uint16(snapshot[8:]); got != 1 {
		t.Fatalf("snapshot count = %d", got)
	}
	body := snapshot[10:]
	if got := binary.LittleEndian.Uint32(body); got != 1954 {
		t.Fatalf("row ref = %d", got)
	}
	nameLen := int(body[4])
	if name := string(body[5 : 5+nameLen]); name != "Tiger Girl" {
		t.Fatalf("row name = %q", name)
	}
	at := 5 + nameLen
	if body[at] != 20 {
		t.Fatalf("row level = %d", body[at])
	}
	if got := binary.LittleEndian.Uint16(body[at+1:]); got != 25001 {
		t.Fatalf("row region = %d", got)
	}
	if body[at+3] != 0 {
		t.Fatalf("row alive = %d, want 0 (dead)", body[at+3])
	}
	if got := int64(binary.LittleEndian.Uint64(body[at+4:])); got != 1_700_000_100_000 {
		t.Fatalf("row diedAtMs = %d", got)
	}
	killerLen := int(body[at+12])
	if killer := string(body[at+13 : at+13+killerLen]); killer != "Lune" {
		t.Fatalf("row killer = %q", killer)
	}
	if len(body) != at+13+killerLen {
		t.Fatalf("row has %d trailing bytes", len(body)-(at+13+killerLen))
	}

	update := EncodeUniqueStatusUpdate(row, 1_700_000_200_000)
	if len(update) != 8+(len(body)) {
		t.Fatalf("update = %d bytes, want %d", len(update), 8+len(body))
	}
	if got := binary.LittleEndian.Uint32(update[8:]); got != 1954 {
		t.Fatalf("update ref = %d", got)
	}
}
