/*
===========================================================================

attendance.go - the attendance calendar's switch and request (#3255)

Port-only, not native: neither original has an attendance calendar. The
ledger is platform/attendance and the snapshot feed game/attendancefeed.
This file names the switch and answers the request (0x7FD2): with the
flag off the request is ignored; with it on, wiring installs
AttendanceRequest, which checks in or claims a tier and answers with
the month snapshot (0x3FD2). A client that predates 0x7FD2 never sends
it, so no release-protocol bump carries it.

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

Answers 0x7FD2. Off (AttendanceRequest nil) is native: silent. On, the
installed sink checks in or claims and returns the month snapshot; a
refused request stays silent.
================
*/
func (rt *Runtime) HandleAttendanceCheckIn(division string, c *enterworld.Character, payload []byte) OpResult {
	if rt.AttendanceRequest == nil || c == nil {
		return OpResult{}
	}
	frames := rt.AttendanceRequest(division, c, payload)
	if frames == nil {
		return OpResult{DiagnosticRefusal: "attendance request refused"}
	}
	return OpResult{Frames: frames}
}

// AttendanceRequestFunc answers one 0x7FD2: check-in or claim. Nil
// frames refuse the request silently.
type AttendanceRequestFunc func(division string, c *enterworld.Character, payload []byte) []wire.Frame
