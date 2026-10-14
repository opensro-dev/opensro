/*
===========================================================================

placementguard_test.go - tests for placementguard.go

===========================================================================
*/

package action

import (
	"testing"

	logtest "github.com/sirupsen/logrus/hooks/test"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPlacementInsideAClosedObjectIsReportedByItsPath

A rescue that lands inside a closed object counts under its path and warns
once per quiet interval; the position it committed is left alone, and a
rescue elsewhere reports nothing.
================
*/
func TestPlacementInsideAClosedObjectIsReportedByItsPath(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	c := rebirthTestCharacter(20, 100)
	rt, _ := newTestRuntime(c, testItems())
	trunk := simulation.Spawn{RegionID: 26265, X: 957, Y: -80, Z: 1508}
	open := simulation.Spawn{RegionID: 26265, X: 100, Y: -80, Z: 100}
	rt.InsideClosedObject = func(pose simulation.Spawn) (int, bool) {
		return 1160, pose.X == trunk.X && pose.Z == trunk.Z
	}
	rt.portals = &portalCatalog{destinations: map[uint32]portalDestination{
		2: {id: 2, ref: 2095, code: "GATE_WC", building: true, recall: true, spawn: trunk},
		3: {id: 3, ref: 2096, code: "GATE_OPEN", building: true, recall: true, spawn: open},
	}}
	warnings := func() int {
		n := 0
		for _, entry := range hook.AllEntries() {
			if entry.Message == "action: player placed inside a closed object" {
				n++
			}
		}
		return n
	}
	if err := rt.OperatorRescue(testDivision, c.Name, 3); err != nil {
		t.Fatal(err)
	}
	if counts := rt.PlacementCounters(); len(counts) != 0 || warnings() != 0 {
		t.Fatalf("an open landing was reported: %v", counts)
	}
	for range 2 {
		if err := rt.OperatorRescue(testDivision, c.Name, 2); err != nil {
			t.Fatal(err)
		}
	}
	if got := rt.PlacementCounters()["operator-rescue"]; got != 2 {
		t.Fatalf("operator-rescue counted %d, want 2", got)
	}
	if warnings() != 1 {
		t.Fatalf("%d warnings, want one per quiet interval", warnings())
	}
	if got := simulation.SeedWorldState(c).Spawn; got != trunk {
		t.Fatalf("the guard moved the player: %+v", got)
	}
}
