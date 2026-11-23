/*
===========================================================================

skilltravel_exit_test.go - a position skill never lands where no walk leaves

#2807: Aerial Teleport seated a character on a tree's standable object cell,
which every walk leg clips at its edge. The fake world here holds one such
tree: a walk may enter it, no leg may leave it. A landing on it is pulled
back along the chord to a seat with an exit; with no such seat the cast is
refused.

===========================================================================
*/

package action

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

// treeX, treeZ and treeRadius place the trap disc of the fake world.
const treeX, treeZ, treeRadius = 600.0, 300.0, 20.0

func insideTree(p simulation.Spawn) bool {
	return math.Hypot(p.X-treeX, p.Z-treeZ) < treeRadius
}

/*
================
treeWalk

The fake world's walk: a leg that starts inside the tree goes nowhere (its
edge clips it); any other leg reaches its goal. trapped makes every leg go
nowhere, the world with no exit anywhere.
================
*/
func treeWalk(trapped bool) func(string, simulation.Spawn, simulation.NavOwner, simulation.Spawn) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
	return func(_ string, from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
		if trapped || insideTree(from) {
			return from, simulation.NavWalk{Rest: owner}, nil
		}
		return to, simulation.NavWalk{Rest: owner}, nil
	}
}

/*
================
TestPositionSkillLandingKeepsAWalkableExit
================
*/
func TestPositionSkillLandingKeepsAWalkableExit(t *testing.T) {
	from := simulation.Spawn{RegionID: 26247, X: 400, Y: 0, Z: 300}
	onTree := simulation.Spawn{RegionID: 26247, X: treeX, Y: 0, Z: treeZ}
	for _, teleport := range []bool{true, false} {
		rt, _, _, _ := newCombatTestRuntime(t, 100)
		rt.ConstrainMovement = nil
		rt.ResolveNavOwner = nil
		rt.ConstrainWalk = treeWalk(false)
		plan, ok := rt.planSkillMove("c", from, simulation.NavOwner{}, onTree, teleport)
		if !ok {
			t.Fatalf("teleport=%v: a landing with an exit short of the tree was refused", teleport)
		}
		if insideTree(plan.to) {
			t.Fatalf("teleport=%v: landed inside the tree at (%v, %v)", teleport, plan.to.X, plan.to.Z)
		}
		// Pulled back along the chord, and only as far as the tree's edge needs.
		if plan.to.Z != from.Z || plan.to.X <= from.X || plan.to.X < treeX-treeRadius-(treeX-from.X)/landingExitSteps {
			t.Fatalf("teleport=%v: landed at (%v, %v), want the furthest seat on the chord before the tree", teleport, plan.to.X, plan.to.Z)
		}

		rt.ConstrainWalk = treeWalk(true)
		if _, ok := rt.planSkillMove("c", from, simulation.NavOwner{}, onTree, teleport); ok {
			t.Fatalf("teleport=%v: a cast with no seat to walk away from was accepted", teleport)
		}

		rt.ConstrainWalk = treeWalk(false)
		open := simulation.Spawn{RegionID: 26247, X: 500, Y: 0, Z: 400}
		plan, ok = rt.planSkillMove("c", from, simulation.NavOwner{}, open, teleport)
		if !ok || plan.to.X != open.X || plan.to.Z != open.Z {
			t.Fatalf("teleport=%v: an open landing moved to (%v, %v) ok=%v", teleport, plan.to.X, plan.to.Z, ok)
		}
	}
}
