/*
===========================================================================

sunevent_test.go - the drop tables say whether Sun is event-only

The published flag follows SRO_SUN_EVENT_ONLY in both settings, so the site
can say why degree-9 Sun is missing from every list.

===========================================================================
*/
package catalogue

import (
	"testing"

	"opensro.online/server/internal/game/progression"
)

/*
================
TestDropTablesPublishSunEventOnly
================
*/
func TestDropTablesPublishSunEventOnly(t *testing.T) {
	for value, want := range map[string]bool{"off": false, "on": true} {
		t.Setenv(progression.EnvSunEventOnly, value)
		if got := BuildDropTables(nil, func(string) uint32 { return 0 }, Header{}).SunEventOnly; got != want {
			t.Fatalf("%s: sunEventOnly %v", value, got)
		}
	}
}
