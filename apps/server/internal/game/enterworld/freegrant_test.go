/*
===========================================================================

freegrant_test.go - the free-grant item owner (starterkit.go)

Port-only, not native (#3308): FreeGrantItem answers from the kit's
own grant lists while the switch is on, and for nothing while it is
off.

===========================================================================
*/
package enterworld

import "testing"

/*
================
TestFreeGrantItemAnswersFromTheGrantLists

On: every kit row and every grade of both refill families is a free
grant; an ordinary potion grade the kit never hands out and the mall
scroll an earlier kit retired are not. Off: nothing is.
================
*/
func TestFreeGrantItemAnswersFromTheGrantLists(t *testing.T) {
	var granted []string
	t.Setenv(EnvBetaStarterKit, "on")
	for _, codename := range betaStarterKitCodenames {
		if !FreeGrantItem(codename) {
			t.Fatalf("kit row %s is not a free grant while the kit is on", codename)
		}
		granted = append(granted, codename)
	}
	for _, family := range betaRefillFamilies {
		for _, codename := range family {
			if !FreeGrantItem(codename) {
				t.Fatalf("refill grade %s is not a free grant while the kit is on", codename)
			}
			granted = append(granted, codename)
		}
	}
	// The mall scroll an earlier kit handed out was retired, not
	// granted: the swap on entry replaces it before any merchant can
	// be reached, and a bought one keeps its value until then.
	for _, codename := range []string{"ITEM_MALL_MOVE_SPEED_UP_100", "ITEM_ETC_ARROW_01", ""} {
		if FreeGrantItem(codename) {
			t.Fatalf("%q is a free grant, want not", codename)
		}
	}

	t.Setenv(EnvBetaStarterKit, "off")
	for _, codename := range granted {
		if FreeGrantItem(codename) {
			t.Fatalf("%s is a free grant while the kit is off", codename)
		}
	}
}
