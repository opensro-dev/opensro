/*
===========================================================================

extension_test.go - tests for extension.go

===========================================================================
*/
package nasrun

import "testing"

const testDay = 24 * 60 * 60

/*
================
TestExtendWithinAddsAWeekUpToTheCeiling
================
*/
func TestExtendWithinAddsAWeekUpToTheCeiling(t *testing.T) {
	const now = 1_800_000_000
	for _, c := range []struct {
		name string
		data uint32
		want uint32
		ok   bool
	}{
		{"dormant refuses", 0, 0, false},
		{"awake grows from its expiry", now + 3*testDay, now + 10*testDay, true},
		{"lapsed grows from now", now - 5*testDay, now + 7*testDay, true},
		{"exactly at the ceiling", now + 21*testDay, now + 28*testDay, true},
		{"past the ceiling refuses", now + 21*testDay + 1, now + 21*testDay + 1, false},
	} {
		next, ok := ExtendWithin(c.data, now)
		if next != c.want || ok != c.ok {
			t.Fatalf("%s: got %d %v, want %d %v", c.name, next, ok, c.want, c.ok)
		}
	}
}
