/*
===========================================================================

tier.go - which tests run in the everyday gate and which only in the long tier

The everyday server gate keeps every package's tests inside a 30-second
budget. Whole-catalogue and brute-force evidence sweeps (every shipped row
read through every reader, an optimisation compared against a full scan
over tens of thousands of random cases) prove invariants that rarely move
and cost most of the suite's time, so they form the long tier:

  - the everyday gate runs `go test -short`, and Long skips them there;
  - `pnpm task check:server:long` and every release freeze run them all.

Neither tier depends on a hosted CI: both run on our own machines, and the
game data most sweeps read exists only there.

A behaviour test that is merely slow is fixed, never moved here.

===========================================================================
*/
package tier

import "testing"

/*
================
Long

Marks t as a long-tier evidence sweep: skipped under -short, with the
reason naming what the sweep proves.
================
*/
func Long(t testing.TB, proves string) {
	t.Helper()
	if testing.Short() {
		t.Skipf("long tier (check:server:long, release freeze): %s", proves)
	}
}
