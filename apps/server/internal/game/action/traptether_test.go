/*
===========================================================================

traptether_test.go - native 3D planter distance, including sector transitions

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/skillobject"
	"testing"
)

/*
================
TestCombatTrapOwnerTether
================
*/
func TestCombatTrapOwnerTether(t *testing.T) {
	var object skillobject.Object
	object.Spawn.Region = 25000
	object.Spawn.X = 1910
	object.Program.OwnerDistance = 10
	for _, tc := range []struct {
		name  string
		owner simulation.Spawn
		want  bool
	}{
		{"vertical boundary", simulation.Spawn{RegionID: 25000, X: 1910, Y: 10}, true},
		{"vertical outside", simulation.Spawn{RegionID: 25000, X: 1910, Y: 11}, false},
		{"diagonal boundary", simulation.Spawn{RegionID: 25000, X: 1916, Y: 8}, true},
		{"diagonal outside", simulation.Spawn{RegionID: 25000, X: 1916, Y: 9}, false},
		{"sector boundary", simulation.Spawn{RegionID: 25001, X: 0}, true},
		{"sector with height", simulation.Spawn{RegionID: 25001, X: 0, Y: 1}, false},
		{"different plane", simulation.Spawn{RegionID: 25000 | 0x8000, X: 1910}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := combatTrapOwnerNear(object, tc.owner); got != tc.want {
				t.Fatalf("near = %v, want %v", got, tc.want)
			}
		})
	}
}
