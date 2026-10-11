/*
===========================================================================

tether_test.go - 4FD1D0's refusal rule

===========================================================================
*/
package simulation

import "testing"

/*
================
TestTetherRefusesOnlyOutwardStepsPastTheRange
================
*/
func TestTetherRefusesOnlyOutwardStepsPastTheRange(t *testing.T) {
	tether := Tether{Anchor: Spawn{RegionID: 0x6B4F, X: 100, Z: 100}, Range: TradeTransportTetherRange}
	at := func(x float64) Spawn { return Spawn{RegionID: 0x6B4F, X: x, Z: 100} }
	for _, tc := range []struct {
		name     string
		from, to float64
		refused  bool
	}{
		{"inside, stepping past the range", 1050, 1150, false},
		{"past the range, stepping further", 1150, 1200, true},
		{"past the range, stepping back", 1150, 1100, false},
		{"past the range, standing", 1150, 1150, false},
		{"exactly at the range, stepping out", 1100, 1200, false},
	} {
		if got := tether.Refuses(at(tc.from), at(tc.to)); got != tc.refused {
			t.Errorf("%s: refused = %v, want %v", tc.name, got, tc.refused)
		}
	}
	// The anchor may stand in the next region over; distance crosses it.
	across := Tether{Anchor: Spawn{RegionID: 0x6B4E, X: 1900, Z: 100}, Range: TradeTransportTetherRange}
	if !across.Refuses(at(1000), at(1100)) {
		t.Error("a transport in the neighbouring region did not hold its trader")
	}
}
