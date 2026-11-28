package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestAttendanceFromEnv

Off unless the operator says on; unset and anything else are native.
================
*/
func TestAttendanceFromEnv(t *testing.T) {
	for value, want := range map[string]bool{
		"": false, "off": false, "0": false, "yes": false,
		"on": true, "ON": true, " 1 ": true, "true": true,
	} {
		t.Setenv(EnvAttendance, value)
		if got := AttendanceFromEnv(); got != want {
			t.Fatalf("%s=%q: %v, want %v", EnvAttendance, value, got, want)
		}
	}
}

/*
================
TestHandleAttendanceCheckIn

Off (nil sink) is silent. On, the sink's frames go out; a nil return
from the sink is a silent refusal; a nil character is ignored.
================
*/
func TestHandleAttendanceCheckIn(t *testing.T) {
	rt := &Runtime{}
	if got := rt.HandleAttendanceCheckIn("", &enterworld.Character{ID: 1}, nil); len(got.Frames) != 0 || got.DiagnosticRefusal != "" {
		t.Fatalf("flag off answered %+v", got)
	}

	var sawDiv string
	var sawID int64
	var sawPayload []byte
	rt.AttendanceRequest = func(division string, c *enterworld.Character, payload []byte) []wire.Frame {
		sawDiv, sawID, sawPayload = division, c.ID, payload
		return []wire.Frame{{Opcode: wire.OpAttendanceSnapshot, Payload: []byte{1, 2, 3}}}
	}
	got := rt.HandleAttendanceCheckIn("div", &enterworld.Character{ID: 7}, []byte{9})
	if sawDiv != "div" || sawID != 7 || len(sawPayload) != 1 || sawPayload[0] != 9 {
		t.Fatalf("sink saw div=%q id=%d payload=%v", sawDiv, sawID, sawPayload)
	}
	if len(got.Frames) != 1 || got.Frames[0].Opcode != wire.OpAttendanceSnapshot {
		t.Fatalf("success frames %+v", got.Frames)
	}

	rt.AttendanceRequest = func(string, *enterworld.Character, []byte) []wire.Frame { return nil }
	if got := rt.HandleAttendanceCheckIn("", &enterworld.Character{ID: 1}, nil); len(got.Frames) != 0 || got.DiagnosticRefusal == "" {
		t.Fatalf("refused sink %+v", got)
	}
	if got := rt.HandleAttendanceCheckIn("", nil, nil); len(got.Frames) != 0 {
		t.Fatalf("nil character answered %+v", got)
	}
}
