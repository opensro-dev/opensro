package movement

import "testing"

// Native start rule (client CRTNavMeshObj_Move 0x428F40; the server's twin
// move rule agrees): a start just outside the retained cell is nudged 0.2
// toward the cell centroid and walks on from inside it. Wire X/Z
// truncation produces exactly such starts.
func TestRetainedCellSurvivesJustOutsideStart(t *testing.T) {
	mesh := &objectNavMesh{vertices: []float32{0, 5, 0, 100, 5, 0, 0, 5, 100}, cellA: []uint16{0}, cellB: []uint16{1}, cellC: []uint16{2},
		minX: 0, maxX: 100, minY: 5, maxY: 5, minZ: 0, maxZ: 100}
	stand := &objectDeckStand{mesh: mesh, cellIndex: 0}
	inward := traceObjectCells(stand, -0.5, 10, 50, 10)
	if _, owned := inward.cellAt(0); !owned {
		t.Fatal("a start 0.5 outside the retained cell, heading in, lost the cell")
	}
	if _, owned := inward.cellAt(.5); !owned {
		t.Fatal("the walk inside the retained cell lost ownership")
	}
	outward := traceObjectCells(stand, -0.5, 10, -50, 10)
	if _, owned := outward.cellAt(.5); owned {
		t.Fatal("a chord heading away from the cell must not keep it")
	}
}
