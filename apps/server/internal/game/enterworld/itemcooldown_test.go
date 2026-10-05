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

/*
================
TestItemCooldownGroupByteBoundaries

The native parser stores AL, so values above a byte retain their low bits.
Malformed or out-of-range source pairs cannot replace a preceding valid pair.
================
*/
func TestItemCooldownGroupByteBoundaries(t *testing.T) {
	for _, tc := range []struct {
		group    string
		duration string
		want     float64
		valid    bool
	}{
		{"0x0", "500", 0, true},
		{"0xFF", "500", 255, true},
		{"0x100", "500", 0, true},
		{"0x1FF", "500", 255, true},
		{"0xFFFFFF00", "500", 0, true},
		{"0xFFFFFFFF", "500", 255, true},
		{"0x100000000", "500", 242, false},
		{"-1", "500", 242, false},
		{"invalid", "500", 242, false},
		{"", "500", 242, false},
		{"0xFF", "-1", 242, false},
		{"0xFF", "2147483648", 242, false},
		{"0xFF", "NaN", 242, false},
	} {
		t.Run(tc.group+"/"+tc.duration, func(t *testing.T) {
			fields := make([]string, 160)
			fields[118], fields[119] = "600000", "COOLTIME:0xF2"
			fields[156], fields[157] = tc.duration, "COOLTIME:"+tc.group
			got := buildItemNativeFields(fields)
			if group := got.Get("useCooldownGroup524"); group != tc.want {
				t.Fatalf("group = %v, want %v", group, tc.want)
			}
			wantDuration := float64(600000)
			if tc.valid {
				wantDuration = 500
			}
			if duration := got.Get("useCooldownDuration528"); duration != wantDuration {
				t.Fatalf("duration = %v, want %v", duration, wantDuration)
			}
		})
	}
}
