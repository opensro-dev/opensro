/*
===========================================================================

rates_test.go - the shard's reinforcement table, on and off

Off (unset) every attempt keeps the elixir's authored chance and the native
10% floor; on, the table replaces the authored chance, has no floor, and
the powder, Luck charge and bonuses still add on top.

===========================================================================
*/

package alchemy

import (
	"reflect"
	"testing"
)

/*
================
TestParseReinforceRates
================
*/
func TestParseReinforceRates(t *testing.T) {
	for _, tc := range []struct {
		text string
		want ReinforceRates
		bad  bool
	}{
		{"", nil, false},
		{"off", nil, false},
		{" on ", requestedReinforceRates, false},
		{"100, 50,0", ReinforceRates{100, 50, 0}, false},
		{"101", nil, true},
		{"-1", nil, true},
		{"50,,40", nil, true},
		{"fast", nil, true},
	} {
		got, err := ParseReinforceRates(tc.text)
		if (err != nil) != tc.bad || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: %v, %v; want %v (error %v)", tc.text, got, err, tc.want, tc.bad)
		}
	}
	if _, err := ParseReinforceRates("1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1"); err == nil {
		t.Fatal("accepted a table longer than maxReinforceRates")
	}
}

/*
================
TestRequestedReinforceRates

The table "on" selects is the one the owner approved, +1 through +11.
================
*/
func TestRequestedReinforceRates(t *testing.T) {
	want := []int{100, 80, 70, 60, 50, 40, 30, 20, 15, 10, 5}
	got, _ := ParseReinforceRates("on")
	if !reflect.DeepEqual([]int(got), want) {
		t.Fatalf("on = %v, want %v", got, want)
	}
}

/*
================
TestReinforceRatesOnAndOff

The fixture elixir is authored at 25% for +1 and 10% from +8; its powder
adds 8% from +8.
================
*/
func TestReinforceRatesOnAndOff(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rates   string
		plus    uint8
		powder  bool
		draws   []uint32
		success bool
	}{
		{"off keeps the authored +1", "", 0, false, []uint32{25}, false},
		{"on makes +1 certain", "on", 0, false, []uint32{99}, true},
		{"on +10 succeeds under 10", "on", 9, false, []uint32{9}, true},
		{"on +10 fails at 10", "on", 9, false, []uint32{10, 49}, false},
		{"on +11 has no native floor", "on", 10, false, []uint32{5, 49}, false},
		{"on +11 succeeds under 5", "on", 10, false, []uint32{4}, true},
		{"on holds the last entry above the table", "on", 20, false, []uint32{4}, true},
		{"off keeps the native floor", "", 20, false, []uint32{9}, true},
		{"on, a powder still adds", "on", 9, true, []uint32{17}, true},
		{"on, a powder adds only its own chance", "on", 9, true, []uint32{18, 49}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, items := fixture()
			rates, err := ParseReinforceRates(tc.rates)
			if err != nil {
				t.Fatal(err)
			}
			c.Rates = rates
			items[0].Plus = tc.plus
			slots := []uint8{13, 14}
			if tc.powder {
				slots = append(slots, 15)
			}
			got, err := c.Reinforce(items, slots, 0, sequence(t, tc.draws...))
			if err != nil {
				t.Fatal(err)
			}
			if got.Success != tc.success {
				t.Fatalf("success = %v, want %v", got.Success, tc.success)
			}
		})
	}
}
