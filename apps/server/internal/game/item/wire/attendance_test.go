package wire

import (
	"bytes"
	"testing"
)

/*
================
TestEncodeAttendanceSnapshotLayout

The body byte for byte, the server clock last.
================
*/
func TestEncodeAttendanceSnapshotLayout(t *testing.T) {
	got := EncodeAttendanceSnapshot(AttendanceSnapshot{
		MonthKey:            202611,
		Today:               26,
		DaysInMonth:         30,
		Checked:             0x0300_0001,
		ContinuityClaimed:   0b01,
		AccumulationClaimed: 0b10,
		ContinuityTiers:     []int{3, 8},
		AccumulationTiers:   []int{5, 300},
		NextResetMs:         0x0102,
		ServerNowMs:         0x0a0b,
	})
	want := []byte{
		0x73, 0x17, 0x03, 0x00, // 202611
		26, 30,
		0x01, 0x00, 0x00, 0x03,
		0b01, 0b10,
		2, 3, 8,
		2, 5, 0xff,
		0x02, 0x01, 0, 0, 0, 0, 0, 0,
		0x0b, 0x0a, 0, 0, 0, 0, 0, 0,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got  % x\nwant % x", got, want)
	}
	if !bytes.Equal(got[len(got)-AttendanceClockBytes:], want[len(want)-8:]) {
		t.Fatal("the server clock is not the trailing field")
	}
}

/*
================
TestEncodeAttendanceSnapshotEmpty

A month with nothing checked still carries its calendar and tiers.
================
*/
func TestEncodeAttendanceSnapshotEmpty(t *testing.T) {
	got := EncodeAttendanceSnapshot(AttendanceSnapshot{MonthKey: 202612, Today: 1, DaysInMonth: 31})
	if len(got) != 4+2+4+2+1+1+8+8 {
		t.Fatalf("empty snapshot is %d bytes", len(got))
	}
}
