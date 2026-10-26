/*
===========================================================================

plane_groundwalk_test.go - ground-walk admission keeps the old answers
on the planes (#1674)

AdmitGroundWalk used to test the dungeon region bit itself. #1674 moved
each arm into its plane. These tests keep the old condition here, as
the parity reference, and require the plane dispatch and the entry to
answer every region pair exactly as it did.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
legacyGroundWalkAdmitted

Ground-walk admission as groundwalk.go had it before #1674.
================
*/
func legacyGroundWalkAdmitted(from, to simulation.Spawn) bool {
	if simulation.IsDungeonRegion(from.RegionID) != simulation.IsDungeonRegion(to.RegionID) ||
		simulation.IsDungeonRegion(from.RegionID) && from.RegionID != to.RegionID {
		return false
	}
	return true
}

/*
================
TestGroundWalkAdmissionPlaneDispatchKeepsTheOldAnswers

Every pair over the region set - outdoor regions, cave regions, and
the bit's edges - through the dispatcher and through AdmitGroundWalk
itself.
================
*/
func TestGroundWalkAdmissionPlaneDispatchKeepsTheOldAnswers(t *testing.T) {
	regions := []uint16{planeOutdoorRegion, 0x6B4F, 0x0000, 0x7FFF, planeCaveRegion, planeOtherCaveRegion, 0x8002, 0x8000, 0xFFFF}
	rt := &Runtime{groundEnabled: true}
	outdoorAdmitted, caveAdmitted, refused := 0, 0, 0
	for _, a := range regions {
		for _, b := range regions {
			from := simulation.Spawn{RegionID: a, X: 10, Y: 2, Z: 20}
			to := simulation.Spawn{RegionID: b, X: 100, Y: 2, Z: 20}
			want := legacyGroundWalkAdmitted(from, to)
			if got := groundWalkAdmittedOnPlane(from, to); got != want {
				t.Fatalf("%#04x -> %#04x: admitted %t, legacy %t", a, b, got, want)
			}
			accepted, _, refusal := rt.AdmitGroundWalk("Walker", from, simulation.NavOwner{}, to)
			if want {
				if refusal != nil || accepted != simulation.NormalizeSpawnFrame(to) {
					t.Fatalf("%#04x -> %#04x: entry (%+v, %v)", a, b, accepted, refusal)
				}
				if simulation.PlaneKindOf(a) == simulation.PlaneCave {
					caveAdmitted++
				} else {
					outdoorAdmitted++
				}
			} else {
				if refusal == nil || accepted != from {
					t.Fatalf("%#04x -> %#04x: entry admitted (%+v, %v)", a, b, accepted, refusal)
				}
				refused++
			}
		}
	}
	// Both planes must admit for real, and refusals must happen, or the
	// pairs no longer reach both arms.
	if outdoorAdmitted == 0 || caveAdmitted == 0 || refused == 0 {
		t.Fatalf("outdoor admitted %d, cave admitted %d, refused %d: the pairs no longer reach both planes",
			outdoorAdmitted, caveAdmitted, refused)
	}
}
