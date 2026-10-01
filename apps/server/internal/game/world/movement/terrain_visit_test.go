/*
===========================================================================

terrain_visit_test.go - native visit order of placed objects (404510)

===========================================================================
*/
package movement

import (
	"math"
	"testing"
)

/*
================
TestTerrainVisitKeyIsFirstRegisteredCellEntry
================
*/
func TestTerrainVisitKeyIsFirstRegisteredCellEntry(t *testing.T) {
	cells := [][4]float32{{100, 0, 200, 50}, {40, 0, 60, 50}}
	if got := terrainVisitKey(&cells, 0, 25, 200, 25); math.Abs(got-0.2) > 1e-12 {
		t.Fatalf("visit = %v, want 0.2 (the nearer registered cell)", got)
	}
	if got := terrainVisitKey(&cells, 50, 25, 150, 25); got != 0 {
		t.Fatalf("visit from inside a cell = %v, want 0", got)
	}
	if got := terrainVisitKey(&cells, 0, 100, 200, 100); !math.IsInf(got, 1) {
		t.Fatalf("visit of a chord that never enters = %v, want +Inf", got)
	}
	if got := terrainVisitKey(nil, 0, 0, 1, 1); got != 0 {
		t.Fatalf("visit without registration data = %v, want 0", got)
	}
}
