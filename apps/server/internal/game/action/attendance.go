/*
===========================================================================

attendance.go - the attendance calendar's switch and check-in (#3255)

Port-only, not native: neither original has an attendance calendar. The
ledger is platform/attendance and the snapshot feed game/attendancefeed.
This file names the switch and answers the check-in request (0x7FD2):
with the flag off the request is ignored; with it on, wiring installs
AttendanceCheckIn, which marks today and answers with the month
snapshot (0x3FD2). A client that predates 0x7FD2 never sends it, so no
release-protocol bump carries it.

===========================================================================
*/
package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
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

/*
================
HandleAttendanceCheckIn

Answers 0x7FD2. Off (AttendanceCheckIn nil) is native: silent. On, the
installed sink marks today and returns the month snapshot; a refused
request (malformed body or a ledger error) stays silent.
================
*/
func (rt *Runtime) HandleAttendanceCheckIn(_ string, c *enterworld.Character, payload []byte) OpResult {
	if rt.AttendanceCheckIn == nil || c == nil {
		return OpResult{}
	}
	frames := rt.AttendanceCheckIn(c.ID, payload)
	if frames == nil {
		return OpResult{DiagnosticRefusal: "attendance check-in refused"}
	}
	return OpResult{Frames: frames}
}

// AttendanceCheckInFunc marks today for a character and returns the
// month snapshot frames, or nil when the request is refused.
type AttendanceCheckInFunc func(characterID int64, payload []byte) []wire.Frame
