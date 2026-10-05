/*
===========================================================================

itemcooldown_test.go - authored cooldown metadata reaches the client

===========================================================================
*/
package enterworld

import (
	"strconv"
	"testing"
)

/*
================
TestItemCooldownDescriptionSlots
================
*/
func TestItemCooldownDescriptionSlots(t *testing.T) {
	for index := 0; index < 20; index++ {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			fields := make([]string, 160)
			fields[118+index*2] = "600000"
			fields[119+index*2] = "COOLTIME:0x000000F2"
			got := buildItemNativeFields(fields)
			if got.Get("useCooldownGroup524") != 242 || got.Get("useCooldownDuration528") != 600000 {
				t.Fatal("authored cooldown pair was not projected")
			}
		})
	}
	fields := make([]string, 160)
	fields[118], fields[119] = "600000", "COOLTIME:0xF2"
	fields[156], fields[157] = "500", "COOLTIME:0xF3"
	got := buildItemNativeFields(fields)
	if got.Get("useCooldownGroup524") != 243 || got.Get("useCooldownDuration528") != 500 {
		t.Fatal("last cooldown declaration did not replace the first")
	}
}
