/*
===========================================================================

monster_navigation.go - monster world-surface path planning and ownership

Bridges the movement clip and surface-owner walk to monster.NavigationPath so
AI, combat, and interest queries share one authoritative surface cursor.

===========================================================================
*/
package movement

import (
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
PlanMonsterPath

PlanMonsterPath is the production AI/world-navigation boundary. Unlike the
legacy player's uncovered-tile policy, AI must not publish an untested path.
A nil path means geometry is unavailable, not that the requested goal is safe.
Cell ownership is captured once and reused by every live-position consumer.
Without a known source owner the walk starts by the native teleport rule
(legitimate for a nest anchor or a freshly spawned monster).
================
*/
func (v *WaterValidator) PlanMonsterPath(from, goal monster.Pose) *monster.NavigationPath {
	return v.PlanMonsterPathFrom(from, simulation.NavOwner{}, goal)
}

/*
================
monsterPathHeightAt
================
*/
func (v *WaterValidator) monsterPathHeightAt(t float64, p monster.Pose, walk *navWalk) (float64, bool) {
	if simulation.IsDungeonRegion(p.RegionID) {
		return v.dungeonSpawnHeightAt(p.RegionID, p.X, p.Y, p.Z)
	}
	if y, ok := walk.heightAt(t); ok {
		return y, true
	}
	return v.TerrainHeightAt(p.RegionID, p.X, p.Z)
}

/*
================
PlanMonsterPathFrom

PlanMonsterPathFrom plans from the monster's retained surface owner
(monster.MoverState.LiveNavOwner): the native mover walks from its stored
source cell (QueryMovement 0x98B300) and never re-guesses it from height.
The returned path carries the walked owners so the next plan can do the same.
================
*/
func (v *WaterValidator) PlanMonsterPathFrom(from monster.Pose, fromOwner simulation.NavOwner, goal monster.Pose) *monster.NavigationPath {
	fromSpawn := simulation.Spawn{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z, Angle: from.Heading}
	goalSpawn := simulation.Spawn{RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z, Angle: goal.Heading}
	report, walk := v.clipMovementPathFromWithWalk(fromSpawn, fromOwner, goalSpawn)
	if report.Truncated || report.TilesUncovered != 0 || report.Outcome == ClipNoCoverage || report.Outcome == ClipDungeonExempt || report.Outcome == ClipStartBlocked {
		return nil
	}
	rest := monster.Pose{RegionID: report.Rest.RegionID, X: report.Rest.X, Y: report.Rest.Y, Z: report.Rest.Z, Heading: goal.Heading}
	if walk == nil || rest.RegionID != goal.RegionID || rest.X != goal.X || rest.Z != goal.Z {
		walk = v.ownerWalk(fromSpawn, fromOwner, simulation.Spawn{RegionID: rest.RegionID, X: rest.X, Y: rest.Y, Z: rest.Z, Angle: rest.Heading})
	}
	y, ok := v.monsterPathHeightAt(1, rest, walk)
	if !ok {
		return nil
	}
	rest.Y = y
	height := func(t float64, p monster.Pose) (float64, bool) {
		return v.monsterPathHeightAt(t, p, walk)
	}
	return monster.NewNavigationPath(from, goal, rest, report.NativeResult, height).WithOwners(walk.spans())
}

/*
================
probeMonsterRouteStep

Evaluates one candidate chord for PlanMonsterRoute without allocating a
NavigationPath, height closure, or owner-span slice, and skips the second
ownerWalk on clipped/blocked intermediate search probes.
================
*/
func (v *WaterValidator) probeMonsterRouteStep(from, goal monster.Pose, needAvailable bool) (float64, bool, bool) {
	fromSpawn := simulation.Spawn{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z, Angle: from.Heading}
	goalSpawn := simulation.Spawn{RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z, Angle: goal.Heading}
	report, walk := v.clipMovementPathFromWithWalk(fromSpawn, simulation.NavOwner{}, goalSpawn)
	if report.Truncated || report.TilesUncovered != 0 || report.Outcome == ClipNoCoverage || report.Outcome == ClipDungeonExempt || report.Outcome == ClipStartBlocked {
		return 0, false, false
	}
	rest := monster.Pose{RegionID: report.Rest.RegionID, X: report.Rest.X, Y: report.Rest.Y, Z: report.Rest.Z, Heading: goal.Heading}
	clear := report.NativeResult&(monster.NavResultBlocked|monster.NavResultClipped) == 0 && routeDistance(rest, goal) < .01
	if !clear && !needAvailable {
		return 0, false, true
	}
	if walk == nil || rest.RegionID != goal.RegionID || rest.X != goal.X || rest.Z != goal.Z {
		walk = v.ownerWalk(fromSpawn, simulation.NavOwner{}, simulation.Spawn{RegionID: rest.RegionID, X: rest.X, Y: rest.Y, Z: rest.Z, Angle: rest.Heading})
	}
	y, ok := v.monsterPathHeightAt(1, rest, walk)
	if !ok {
		return 0, false, false
	}
	return y, clear, true
}
