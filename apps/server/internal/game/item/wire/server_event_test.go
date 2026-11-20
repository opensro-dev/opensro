/*
===========================================================================

server_event_test.go - the 0x34F2 server event bodies the gauge reads

===========================================================================
*/
package wire

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

/*
================
expectServerEvent
================
*/
func expectServerEvent(t *testing.T, name string, f Frame, want []byte) {
	t.Helper()
	if f.Opcode != OpServerEvent || !bytes.Equal(f.Payload, want) {
		t.Fatalf("%s: opcode=%04x payload=% X, want %04x % X", name, f.Opcode, f.Payload, OpServerEvent, want)
	}
}

/*
================
TestServerEventFullSync

Type 2, sub 3: every row with its count and goal (world entry).
================
*/
func TestServerEventFullSync(t *testing.T) {
	want := []byte{2, 3}
	want = binary.LittleEndian.AppendUint32(want, 1)
	want = binary.LittleEndian.AppendUint32(want, 30001)
	want = binary.LittleEndian.AppendUint32(want, 7)
	want = binary.LittleEndian.AppendUint32(want, 118)
	expectServerEvent(t, "full sync", ServerEventFullSyncFrame([]ServerEventRow{{ID: 30001, Count: 7, Goal: 118}}), want)
}

/*
================
TestServerEventCountSync

Type 2, sub 1: the count alone (the five-minute sync).
================
*/
func TestServerEventCountSync(t *testing.T) {
	want := []byte{2, 1}
	want = binary.LittleEndian.AppendUint32(want, 1)
	want = binary.LittleEndian.AppendUint32(want, 30001)
	want = binary.LittleEndian.AppendUint32(want, 9)
	expectServerEvent(t, "count sync", ServerEventCountSyncFrame([]ServerEventRow{{ID: 30001, Count: 9, Goal: 118}}), want)
}

/*
================
TestServerEventEmptySync
================
*/
func TestServerEventEmptySync(t *testing.T) {
	expectServerEvent(t, "empty full", ServerEventFullSyncFrame(nil), []byte{2, 3, 0, 0, 0, 0})
	expectServerEvent(t, "empty count", ServerEventCountSyncFrame(nil), []byte{2, 1, 0, 0, 0, 0})
}

/*
================
TestServerEventCompletion

Type 1 to every player (target 1), count and goal (DataType 3), then the
ExpUP reward with its ratios.
================
*/
func TestServerEventCompletion(t *testing.T) {
	want := []byte{1}
	want = binary.LittleEndian.AppendUint32(want, 30001)
	want = append(want, 1, 3)
	want = binary.LittleEndian.AppendUint32(want, 2)
	want = binary.LittleEndian.AppendUint32(want, 136)
	want = binary.LittleEndian.AppendUint32(want, 0x1ED)
	want = append(want, 1)
	want = binary.LittleEndian.AppendUint32(want, 3600)
	want = binary.LittleEndian.AppendUint32(want, math.Float32bits(0.2))
	want = binary.LittleEndian.AppendUint32(want, math.Float32bits(0.2))
	f := ServerEventCompletionFrame(ServerEventRow{ID: 30001, Count: 2, Goal: 136},
		ServerEventExpUp{RewardID: 0x1ED, ApplySeconds: 3600, Exp: 0.2, Sp: 0.2})
	expectServerEvent(t, "completion", f, want)
}

/*
================
TestServerEventStart

Type 5 with the flag set and the goal: the window opens at zero.
================
*/
func TestServerEventStart(t *testing.T) {
	want := []byte{5}
	want = binary.LittleEndian.AppendUint32(want, 30001)
	want = append(want, 1)
	want = binary.LittleEndian.AppendUint32(want, 118)
	expectServerEvent(t, "start", ServerEventStartFrame(ServerEventRow{ID: 30001, Count: 4, Goal: 118}), want)
}

/*
================
TestServerEventEnd
================
*/
func TestServerEventEnd(t *testing.T) {
	want := binary.LittleEndian.AppendUint32([]byte{6}, 30001)
	expectServerEvent(t, "end", ServerEventEndFrame(30001), want)
}
