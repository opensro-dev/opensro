/*
===========================================================================

attendance.go - the attendance calendar's switch (#3255)

Port-only, not native: neither original has an attendance calendar. The
ledger is platform/attendance and the snapshot feed game/attendancefeed;
this file only names the switch the wiring and the rules page read.

===========================================================================
*/
package action

import (
	"os"
	"strings"
)

// EnvAttendance turns the port-only attendance calendar on ("on", "1",
// "true"). Off, the native default, opens no ledger and sends nothing.
const EnvAttendance = "SRO_ATTENDANCE"

/*
================
AttendanceFromEnv
================
*/
func AttendanceFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAttendance))) {
	case "on", "1", "true":
		return true
	}
	return false
}
