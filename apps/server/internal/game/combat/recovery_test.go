/*
===========================================================================

recovery_test.go - native reduction, truncation, and maximum boundaries.

===========================================================================
*/

package combat

import (
	"math"
	"testing"
)

/*
================
TestRecoverVital

Non-integral reductions distinguish native truncation from rounding. Invalid
float conversions follow the native negative sentinel and add no recovery.
================
*/
func TestRecoverVital(t *testing.T) {
	for _, test := range []struct {
		name      string
		amount    int64
		reduction float32
		want      int64
	}{
		{name: "full deficit", amount: 183, want: 200},
		{name: "half recovery", amount: 183, reduction: 50, want: 108},
		{name: "blocked recovery", amount: 183, reduction: 100, want: 17},
		{name: "excess reduction", amount: 183, reduction: 150, want: 17},
		{name: "maximum clamp", amount: 500, want: 200},
		{name: "negative amount", amount: -100, want: 17},
		{name: "invalid reduction", amount: 183, reduction: float32(math.NaN()), want: 17},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := RecoverVital(17, 200, test.amount, test.reduction); got != test.want {
				t.Fatalf("recovered current = %d, want %d", got, test.want)
			}
		})
	}
}
