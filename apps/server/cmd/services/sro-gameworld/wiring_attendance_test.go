/*
===========================================================================

wiring_attendance_test.go - SRO_ATTENDANCE off opens nothing; on wires both

===========================================================================
*/
package main

import (
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/platform/attendance"
)

/*
================
TestInstallAttendanceFollowsTheFlag

Off (the native default): no ledger, no feed, no database file. On: the
ledger opens in the shard's authority directory, the feed is built over
it, and the shard's ledger close closes it.
================
*/
func TestInstallAttendanceFollowsTheFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(store.EnvStateDir, dir)

	t.Setenv(action.EnvAttendance, "off")
	application, gameplay := &gameWorldApplication{}, &gameplayPlane{}
	if err := installAttendance(application, gameplay, "shard-a"); err != nil {
		t.Fatal(err)
	}
	if application.ledgers.attendance != nil || gameplay.attendance != nil || gameplay.attendanceFeed != nil {
		t.Fatal("flag off wired the attendance ledger")
	}
	if _, err := os.Stat(filepath.Join(dir, attendance.FileName)); !os.IsNotExist(err) {
		t.Fatalf("flag off created %s (%v)", attendance.FileName, err)
	}

	t.Setenv(action.EnvAttendance, "on")
	if err := installAttendance(application, gameplay, "shard-a"); err != nil {
		t.Fatal(err)
	}
	if application.ledgers.attendance == nil || gameplay.attendance != application.ledgers.attendance || gameplay.attendanceFeed == nil {
		t.Fatal("flag on did not wire the ledger and the feed")
	}
	if _, err := os.Stat(filepath.Join(dir, attendance.FileName)); err != nil {
		t.Fatal(err)
	}
	if err := application.ledgers.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := gameplay.attendance.Current(1); err != attendance.ErrClosed {
		t.Fatalf("ledger still open after close: %v", err)
	}
}
