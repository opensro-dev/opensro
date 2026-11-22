/*
===========================================================================

inventorysort_test.go - tests for inventorysort.go

===========================================================================
*/
package action

import "testing"

/*
================
TestInventorySortFromEnv
================
*/
func TestInventorySortFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvInventorySort, value)
		if got := InventorySortFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}
