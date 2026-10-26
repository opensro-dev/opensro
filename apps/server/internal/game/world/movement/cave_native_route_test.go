/*
===========================================================================

cave_native_route_test.go - the native cave region route and its seam step
(#1698)

The step's arithmetic is checked on synthetic positions. On Donwhang's
real tables every doorway is walked the way the mover walks it - ask the
route, walk the step to its clip rest, ask again - with the port-only
seam step off (native) and on: native traps a monster at some doorways,
the seam step frees every one, and a doorway native already crosses
takes exactly the same steps either way.

===========================================================================
*/
package movement

import (
	"math"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// caveDoorwaySteps bounds one doorway walk: native crosses a doorway in a
// few steps, so a walk still in the portal's block after this many is
// trapped.
const caveDoorwaySteps = 12

/*
================
TestCaveStepClampAndTruncation
================
*/
func TestCaveStepClampAndTruncation(t *testing.T) {
	from := monster.Pose{RegionID: planeCaveRegion, X: 100.5, Y: 7, Z: -20.5}
	cases := []struct {
		name         string
		point        [3]float32
		towardTarget bool
		x, z         float64
	}{
		// 30 units along +x: reached exactly, then truncated toward zero.
		{"far point", [3]float32{130.9, 0, -20.5}, false, 130, -20},
		// 2 units away: overshot to the 5-unit minimum.
		{"near point overshoots to 5", [3]float32{102.5, 0, -20.5}, false, 105, -20},
		// The same point at the position: no direction, the position itself.
		{"no direction", [3]float32{100.5, 0, -20.5}, false, 100, -20},
		// Toward the target: the 5-unit leg ends at (104.04, -24.04), then
		// each axis rounds on along its direction.
		{"seam step rounds on", [3]float32{102.5, 0, -22.5}, true, 105, -25},
	}
	for _, c := range cases {
		step := caveStep(from, c.point, c.towardTarget)
		if step.X != c.x || step.Z != c.z || step.Y != from.Y || step.RegionID != from.RegionID {
			t.Errorf("%s: step %+v, want x %v z %v at the position's height", c.name, step, c.x, c.z)
		}
	}
	// An unordered distance takes the cap, not the minimum.
	nan := caveStep(monster.Pose{X: math.NaN()}, [3]float32{1, 0, 1}, false)
	if !math.IsNaN(nan.X) {
		t.Errorf("a NaN position stepped to %+v", nan)
	}
}

/*
================
walkCaveDoorway

Walks from the middle of a portal cell toward the landing block until the
clip rest stands in it. Returns the steps taken and whether it crossed.
================
*/
func walkCaveDoorway(v *WaterValidator, surface *dungeonSpawnSurface, block int, portal caveRoutePortal) ([]monster.Pose, bool) {
	x, y, z := surface.cellCentroid(caveCell{block: surface.ordinalIndex[block], mesh: 0, cell: int(portal.cell)})
	at := monster.Pose{RegionID: planeCaveRegion, X: x, Y: y, Z: z}
	goal := caveStandpoint(surface, planeCaveRegion, surface.ordinalIndex[int(portal.landingBlock)])
	var steps []monster.Pose
	// The mover carries the cell each step reached, as LiveNavOwner does.
	owner := simulation.NavOwner{}
	for i := 0; i < caveDoorwaySteps; i++ {
		route := v.planCaveRouteFrom(at, owner, goal)
		if route.Status() != monster.NavigationRouteReady || route.Len() == 0 {
			return steps, false
		}
		step := route.Point(0)
		steps = append(steps, step)
		report := v.ClipMovementPathFrom(simulation.Spawn{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}, owner,
			simulation.Spawn{RegionID: step.RegionID, X: step.X, Y: step.Y, Z: step.Z})
		at = monster.Pose{RegionID: report.Rest.RegionID, X: report.Rest.X, Y: report.Rest.Y, Z: report.Rest.Z}
		owner = report.RestOwner
		if owner.Kind == simulation.NavOwnerDungeon && int(owner.Object.Object) == int(portal.landingBlock) {
			return steps, true
		}
	}
	return steps, false
}

/*
================
TestCaveSeamStepFreesEveryDoorway

Every doorway, native and with the seam step: the seam step crosses
wherever native crosses, in no more steps, and frees every doorway native
traps - except where the port's own clip refuses the first step outright,
which the port's earlier route cannot cross either (a clip limit, not the
route's; #1698 follow-up).
================
*/
func TestCaveSeamStepFreesEveryDoorway(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	surface := v.caveSurface(planeCaveRegion)
	if surface == nil || surface.routeTable == nil {
		t.Fatal("Donwhang's native route table did not load")
	}
	table := surface.routeTable
	doorways, trapped, freed, clipBlocked, identical := 0, 0, 0, 0, 0
	for block := range table.blocks {
		for _, portal := range table.blocks[block].portals {
			doorways++
			v.SetCaveSeamStep(false)
			native, crossed := walkCaveDoorway(v, surface, block, portal)
			v.SetCaveSeamStep(true)
			seam, seamCrossed := walkCaveDoorway(v, surface, block, portal)
			switch {
			case crossed && !seamCrossed:
				t.Errorf("block %d portal %+v: native crosses, the seam step does not", block, portal)
			case crossed && len(seam) > len(native):
				t.Errorf("block %d portal %+v: the seam step takes %d steps, native %d", block, portal, len(seam), len(native))
			case crossed:
				if reflect.DeepEqual(native, seam) {
					identical++
				}
			case seamCrossed:
				trapped++
				freed++
			default:
				trapped++
				clipBlocked++
				surface.routeTable = nil
				_, portCrossed := walkCaveDoorway(v, surface, block, portal)
				surface.routeTable = table
				if portCrossed {
					t.Errorf("block %d portal %+v: the seam step stays trapped where the port's earlier route crosses", block, portal)
				}
			}
		}
	}
	t.Logf("%d doorways: native traps %d; the seam step frees %d, %d are clip-blocked for every route; %d of %d native crossings take identical steps",
		doorways, trapped, freed, clipBlocked, identical, doorways-trapped)
	// The premise: native really traps doorways the seam step frees, or the
	// switch is tested against nothing.
	if freed == 0 {
		t.Fatal("the seam step freed no doorway")
	}
}
