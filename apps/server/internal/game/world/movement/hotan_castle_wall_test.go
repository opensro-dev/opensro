/*
===========================================================================

hotan_castle_wall_test.go - the Hotan castle precinct wall is native

#4156 reported an invisible barrier in Hotan at region 0x5C87
(837.7, 256.8, 1532.9). The blocker is the castle compound's own
navigation: the precinct's south outline runs along world z=1509, wall
sections (outline flag 3) between authored gates (flag 0). The reported
point stands on the precinct apron inside the wall - its height is the
apron cell's plane (256.78), not the flat terrain (243.58) - so the
reporter was inside the grounds, walking out through a wall section.
The native client walls exactly here: entry from outside crosses the
outline only at open edges (client CRTNavMeshObj_EnterFromOutside,
client 0x00428300; client CRTNavMeshTerrain_StepPlacedObject,
client 0x00403FB0), and a
walker who owns the apron cells meets the same outline as a hard stop.
The shipped assets are the native bytes (payload sha-verified against
the retail BMS), so there is nothing to repair; this test pins the
behaviour at the reported spot so a future "fix" cannot quietly open
the precinct wall or seal its gates.

===========================================================================
*/
package movement

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
)

/*
================
TestHotanCastlePrecinctWall

On the real region bundle: the wall section at x=837.7 refuses a walk
in both directions at z=1509, the reported inside point stands on the
castle apron, and the authored gate at x=1130 still admits the walk.
================
*/
func TestHotanCastlePrecinctWall(t *testing.T) {
	root, err := clientdata.ClientPublicRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "assets", "world", "world-region-catalog.json")); err != nil {
		t.Skipf("real client assets not present (%v)", err)
	}
	validator := NewWaterValidator(root)
	const region = 0x5C87

	// The reported point stands on the castle apron (object owner), at
	// the apron's height - the player was inside the precinct.
	owner, height, ok := validator.ResolveNavOwner(
		simulation.Spawn{RegionID: region, X: 837.7, Y: 256.8, Z: 1532.9}, simulation.NavOwner{})
	if !ok || owner.Kind != simulation.NavOwnerObject || math.Abs(height-256.78) > 0.5 {
		t.Fatalf("reported point: owner %+v height %.2f ok %v, want the castle apron at ~256.8", owner, height, ok)
	}

	wall := validator.ClipMovementPath(spawnAt(region, 837.7, 1440), spawnAt(region, 837.7, 1560))
	if wall.Outcome == ClipArrived || wall.Rest.Z > 1510 {
		t.Fatalf("outside approach: outcome %s rest %+v, want the walk to never cross the wall (z 1509)", wall.Outcome, wall.Rest)
	}
	exit := validator.ClipMovementPath(spawnAt(region, 837.7, 1532.9), spawnAt(region, 837.7, 1440))
	if exit.Outcome != ClipBlocked || math.Abs(exit.Rest.Z-1509.09) > 0.5 {
		t.Fatalf("inside exit: outcome %s rest %+v, want blocked at the wall (z ~1509)", exit.Outcome, exit.Rest)
	}

	gate := validator.ClipMovementPath(spawnAt(region, 1130, 1460), spawnAt(region, 1130, 1560))
	if gate.Outcome != ClipArrived {
		t.Fatalf("authored gate: outcome %s rest %+v, want arrived through the gate", gate.Outcome, gate.Rest)
	}
}
