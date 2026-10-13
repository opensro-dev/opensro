/*
===========================================================================

maxplus_test.go - the shard's enhancement cap, on and off

Off (unset) an item reinforces until the native ceiling; on, an item at or
above the cap is refused with the native ceiling's 0x0F before any draw, and
nothing in the bag changes.

===========================================================================
*/

package alchemy

import (
	"errors"
	"reflect"
	"testing"
)

/*
================
TestParseReinforceMaxPlus
================
*/
func TestParseReinforceMaxPlus(t *testing.T) {
	for _, tc := range []struct {
		text string
		want uint8
		bad  bool
	}{
		{"", 0, false},
		{"off", 0, false},
		{" on ", 11, false},
		{"9", 9, false},
		{"249", 249, false},
		{"250", 0, true},
		{"-1", 0, true},
		{"eleven", 0, true},
	} {
		got, err := ParseReinforceMaxPlus(tc.text)
		if (err != nil) != tc.bad || got != tc.want {
			t.Fatalf("%q = %d, %v; want %d, error %v", tc.text, got, err, tc.want, tc.bad)
		}
	}
}

/*
================
TestReinforceMaxPlusOnAndOff

At the cap the attempt is refused before the roll (the roll would fail the
test) and the plan leaves the bag as it was; below it, and with the cap
off, the attempt runs.
================
*/
func TestReinforceMaxPlusOnAndOff(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cap     string
		plus    uint8
		refused bool
	}{
		{"off reinforces past +11", "", 11, false},
		{"on reinforces +10 to +11", "on", 10, false},
		{"on refuses at +11", "on", 11, true},
		{"on refuses above +11", "on", 15, true},
		{"a custom cap refuses at itself", "5", 5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, items := fixture()
			maxPlus, err := ParseReinforceMaxPlus(tc.cap)
			if err != nil {
				t.Fatal(err)
			}
			c.MaxPlus = maxPlus
			items[0].Plus = tc.plus
			before := append(items[:0:0], items...)
			roll := sequence(t, 0, 0)
			if tc.refused {
				roll = func() (uint32, error) { t.Fatal("a capped attempt drew randomness"); return 0, nil }
			}
			_, err = c.Reinforce(items, []uint8{13, 14}, 0, roll)
			if tc.refused {
				if !errors.Is(err, Refusal(0x0f)) {
					t.Fatalf("err = %v, want refusal 0x0f", err)
				}
				if !reflect.DeepEqual(items, before) {
					t.Fatal("a refused attempt changed the bag")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
