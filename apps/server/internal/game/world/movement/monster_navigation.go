package movement

import (
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// PlanMonsterPath is the production AI/world-navigation boundary. Unlike the
// legacy player's uncovered-tile policy, AI must not publish an untested path.
// A nil path means geometry is unavailable, not that the requested goal is safe.
// Cell ownership is captured once and reused by every live-position consumer.
// Without a known source owner the walk starts by the native teleport rule
// (legitimate for a nest anchor or a freshly spawned monster).
func (v *WaterValidator) PlanMonsterPath(from, goal monster.Pose) *monster.NavigationPath {
	return v.PlanMonsterPathFrom(from, simulation.NavOwner{}, goal)
}

// PlanMonsterPathFrom plans from the monster's retained surface owner
// (monster.MoverState.LiveNavOwner): the native mover walks from its stored
// source cell (QueryMovement 0x98B300) and never re-guesses it from height.
// The returned path carries the walked owners so the next plan can do the same.
func (v *WaterValidator) PlanMonsterPathFrom(from monster.Pose, fromOwner simulation.NavOwner, goal monster.Pose) *monster.NavigationPath {
	spawn := func(p monster.Pose) simulation.Spawn {
		return simulation.Spawn{RegionID: p.RegionID, X: p.X, Y: p.Y, Z: p.Z, Angle: p.Heading}
	}
	report := v.ClipMovementPathFrom(spawn(from), fromOwner, spawn(goal))
	if report.Truncated || report.TilesUncovered != 0 || report.Outcome == ClipNoCoverage || report.Outcome == ClipDungeonExempt || report.Outcome == ClipStartBlocked {
		return nil
	}
	rest := monster.Pose{RegionID: report.Rest.RegionID, X: report.Rest.X, Y: report.Rest.Y, Z: report.Rest.Z, Heading: goal.Heading}
	walk := v.ownerWalk(spawn(from), fromOwner, spawn(rest))
	height := func(t float64, p monster.Pose) (float64, bool) {
		if simulation.IsDungeonRegion(p.RegionID) {
			return v.dungeonSpawnHeightAt(p.RegionID, p.X, p.Y, p.Z)
		}
		if y, ok := walk.heightAt(t); ok {
			return y, true
		}
		return v.TerrainHeightAt(p.RegionID, p.X, p.Z)
	}
	y, ok := height(1, rest)
	if !ok {
		return nil
	}
	rest.Y = y
	return monster.NewNavigationPath(from, goal, rest, report.NativeResult, height).WithOwners(walk.spans())
}
