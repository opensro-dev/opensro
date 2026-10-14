/*
===========================================================================

placementguard_test.go - tests for placementguard.go

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestInsideClosedObjectFindsTheReportedTree

BR-261014-1425-CE1E: the player stood at (1765, 1358) in region 0x5F6A,
inside asset 1160 (a closed tree-trunk pentagon). Its centre is inside;
points just outside each side, and the open ground nearby, are not.
================
*/
func TestInsideClosedObjectFindsTheReportedTree(t *testing.T) {
	v := realAuthorityValidator(t)
	pose := func(x, z float64) simulation.Spawn {
		return simulation.Spawn{RegionID: 0x5F6A, X: x, Y: 4015, Z: z}
	}
	for _, in := range [][2]float64{{1765, 1358}, {1770, 1360}, {1774, 1360}} {
		if asset, inside := v.InsideClosedObject(pose(in[0], in[1])); !inside || asset != 1160 {
			t.Fatalf("(%v, %v): asset %d inside %v, want asset 1160 inside", in[0], in[1], asset, inside)
		}
	}
	for _, out := range [][2]float64{{1764, 1358}, {1776, 1360}, {1770, 1353}, {1770, 1367}, {1750, 1358}} {
		if asset, inside := v.InsideClosedObject(pose(out[0], out[1])); inside {
			t.Fatalf("(%v, %v): reported inside asset %d", out[0], out[1], asset)
		}
	}
}

/*
================
TestClosedMeshNeedsEveryOutlineEdgeBlocking
================
*/
func TestClosedMeshNeedsEveryOutlineEdgeBlocking(t *testing.T) {
	mesh := &objectNavMesh{
		vertices: []float32{0, 0, 0, 10, 0, 0, 0, 0, 10},
		cellA:    []uint16{0}, cellB: []uint16{1}, cellC: []uint16{2},
		outline: objectNavEdges{flags: []byte{0x03, 0x02, 0x01}},
	}
	if !closedMesh(mesh) || !meshCellContains(mesh, 2, 2) {
		t.Fatal("a fully blocking triangle must be closed and contain (2, 2)")
	}
	if meshCellContains(mesh, 8, 8) || meshCellContains(mesh, 5, 0) {
		t.Fatal("outside or on an edge is not inside")
	}
	mesh.outline.flags[1] = 0
	if closedMesh(mesh) {
		t.Fatal("an open outline edge makes the mesh walkable from outside")
	}
}
