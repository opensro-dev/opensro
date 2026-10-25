/*
===========================================================================

tactics_plane_test.go - each plane's actors run their native pursuit routine

===========================================================================
*/
package simulation

import "testing"

/*
================
TestPursuitRoutinePerPlane

The server installs the direction-gate pursuit outdoors and the
region-route pursuit in a cave; only the outdoor routine carries its goal
on toward a moving target.
================
*/
func TestPursuitRoutinePerPlane(t *testing.T) {
	for _, test := range []struct {
		region  uint16
		routine PursuitRoutine
		extends bool
		indoors bool
	}{
		{0x61A7, PursuitDirectionGate, true, false}, // Jangan
		{0x8001, PursuitRegionRoute, false, true},   // the Donwhang cave
	} {
		routine := pursuitAt(test.region)
		if routine != test.routine || routine.ExtendsTowardMovingTarget() != test.extends ||
			routine.TracesIndoors() != test.indoors {
			t.Errorf("region %#x: routine %d extends %t indoors %t, want %d extends %t indoors %t",
				test.region, routine, routine.ExtendsTowardMovingTarget(), routine.TracesIndoors(),
				test.routine, test.extends, test.indoors)
		}
	}
}
