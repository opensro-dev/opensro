/*
===========================================================================

knockback_ridge_test.go - a push off a ridge stops at its walkable edge

The bug report position (region 0x5F94, 1829 / 1773) is a local high point
whose eastern slope is blocked tiles. An unclipped push onto them left a
monster where every monster path plan reports a blocked departure, so it
never moved or turned again. Through SpawnMoveTest (the native move query
CGObj_MoveTo runs) the same push stops on a tile a monster can plan from.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestKnockbackOffTheRidgeStopsAtTheWalkableEdge
================
*/
func TestKnockbackOffTheRidgeStopsAtTheWalkableEdge(t *testing.T) {
	licensed.RequireGameData(t)
	root, err := licensed.ClientPublicRoot()
	if err != nil {
		t.Fatal(err)
	}
	v := NewWaterValidator(root)
	const region = 0x5F94
	ground := func(x, z float64) simulation.Spawn {
		y, ok := v.TerrainHeightAt(region, x, z)
		if !ok {
			t.Fatalf("no terrain at %.0f/%.0f", x, z)
		}
		return simulation.Spawn{RegionID: region, X: x, Y: y, Z: z}
	}
	target := ground(1829, 1772.9)
	from := ground(1829, 1772.9)
	push := ground(1829+40, 1772.9)
	pose := func(s simulation.Spawn) monster.Pose {
		return monster.Pose{RegionID: s.RegionID, X: s.X, Y: s.Y, Z: s.Z}
	}
	// The unclipped landing point is the frozen state the report showed.
	if v.PlanMonsterPath(pose(push), pose(target)) != nil {
		t.Fatal("the fixture's push no longer lands on a blocked tile")
	}
	move := v.SpawnMoveTest(from, push)
	if move.Result&monster.NavResultClipped == 0 {
		t.Fatalf("the push was not clipped: result %#x rest %+v", move.Result, move.Rest)
	}
	if move.Rest.X >= push.X || v.PlanMonsterPath(pose(move.Rest), pose(target)) == nil {
		t.Fatalf("the clipped landing %+v cannot plan back to its target", move.Rest)
	}
}
