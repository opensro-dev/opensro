package action

import "testing"

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
