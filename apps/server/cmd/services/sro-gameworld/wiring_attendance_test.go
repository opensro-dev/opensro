/*
===========================================================================

wiring_attendance_test.go - SRO_ATTENDANCE off opens nothing; on wires both

===========================================================================
*/
package main

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/platform/attendance"
)

/*
================
TestInstallAttendanceFollowsTheFlag

Off (the native default): no ledger, no feed, no database file, no
request sink. On: the ledger opens in the shard's authority directory,
the feed and the request sink are built over it, and the shard's
ledger close closes it.
================
*/
func TestInstallAttendanceFollowsTheFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(store.EnvStateDir, dir)

	t.Setenv(action.EnvAttendance, "off")
	application, gameplay := &gameWorldApplication{}, &gameplayPlane{items: &action.Runtime{}}
	if err := installAttendance(application, gameplay, "shard-a"); err != nil {
		t.Fatal(err)
	}
	if application.ledgers.attendance != nil || gameplay.attendance != nil || gameplay.attendanceFeed != nil || gameplay.items.AttendanceRequest != nil {
		t.Fatal("flag off wired the attendance ledger")
	}
	if _, err := os.Stat(filepath.Join(dir, attendance.FileName)); !os.IsNotExist(err) {
		t.Fatalf("flag off created %s (%v)", attendance.FileName, err)
	}

	t.Setenv(action.EnvAttendance, "on")
	if err := installAttendance(application, gameplay, "shard-a"); err != nil {
		t.Fatal(err)
	}
	if application.ledgers.attendance == nil || gameplay.attendance != application.ledgers.attendance || gameplay.attendanceFeed == nil || gameplay.items.AttendanceRequest == nil {
		t.Fatal("flag on did not wire the ledger, the feed and the request")
	}
	if _, err := os.Stat(filepath.Join(dir, attendance.FileName)); err != nil {
		t.Fatal(err)
	}
	if err := application.ledgers.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := gameplay.attendance.Current(1); !errors.Is(err, attendance.ErrClosed) {
		t.Fatalf("ledger still open after close: %v", err)
	}
}

/*
================
TestAttendanceCheckInMarksToday

An empty body (or op 0) marks today and answers with the month
snapshot; a second check-in the same day still answers with today's bit
set; a bad body is refused.
================
*/
func TestAttendanceCheckInMarksToday(t *testing.T) {
	clock := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	service, err := attendance.Open(t.TempDir(), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	request := attendanceRequest(nil, nil, service)
	character := &enterworld.Character{ID: 7, Name: "Tester"}

	if frames := request("", character, []byte{9}); frames != nil {
		t.Fatalf("bad body answered %+v", frames)
	}
	frames := request("", character, nil)
	if len(frames) != 1 || frames[0].Opcode != wire.OpAttendanceSnapshot {
		t.Fatalf("first check-in %+v", frames)
	}
	body := frames[0].Payload
	if key := binary.LittleEndian.Uint32(body); key != 202611 {
		t.Fatalf("month key %d", key)
	}
	if body[4] != 26 || binary.LittleEndian.Uint32(body[6:]) != 1<<25 {
		t.Fatalf("today/checked % x", body[4:10])
	}
	again := request("", character, []byte{wire.AttendanceOpCheckIn})
	if len(again) != 1 || binary.LittleEndian.Uint32(again[0].Payload[6:]) != 1<<25 {
		t.Fatalf("same-day check-in %+v", again)
	}
	if frames := request("", character, []byte{wire.AttendanceOpClaimContinuity, 0}); frames != nil {
		t.Fatalf("uneared claim without days answered %+v", frames)
	}
}
