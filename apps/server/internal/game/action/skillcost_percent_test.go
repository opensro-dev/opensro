/*
===========================================================================

skillcost_percent_test.go - the percent part of an HP or MP cost

The native cost code divides before it multiplies (fild vital, fild
percent, fdiv 100.0, fmulp; 58E1D6, 58E214, 583170), so the percent part is
vital * (percent / 100), truncated. That is not (vital * percent) / 100:
0.41 is not exact in binary.

===========================================================================
*/

package action

import "testing"

/*
================
TestVitalPercentDividesFirst
================
*/
func TestVitalPercentDividesFirst(t *testing.T) {
	for _, tc := range []struct {
		vital   int64
		percent uint32
		want    int64
	}{
		{300, 41, 122}, // 0.41 * 300 = 122.99999999999999; (300 * 41) / 100 is 123
		{600, 41, 245},
		{1000, 50, 500},
		{12345, 0, 0},
	} {
		if got := vitalPercent(tc.vital, tc.percent); got != tc.want {
			t.Errorf("vitalPercent(%d, %d) = %d, want %d", tc.vital, tc.percent, got, tc.want)
		}
	}
}
