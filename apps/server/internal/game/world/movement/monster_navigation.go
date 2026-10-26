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

The height of a monster path's sample at chord parameter t, by the
plane the sample is on (pathHeightOnPlane, plane_height.go).
================
*/
func (v *WaterValidator) monsterPathHeightAt(t float64, p monster.Pose, walk *navWalk) (float64, bool) {
	return pathHeightOnPlane(v, t, p, walk)
}

/*
================
PlanMonsterPathFrom

PlanMonsterPathFrom plans from the monster's retained surface owner
(monster.MoverState.LiveNavOwner): the native mover walks from its stored
source cell (the native movement query) and never re-guesses it from
height.
The returned path carries the walked owners so the next plan can do the same.
================
*/
func (v *WaterValidator) PlanMonsterPathFrom(from monster.Pose, fromOwner simulation.NavOwner, goal monster.Pose) *monster.NavigationPath {
	path, _ := v.planMonsterPathFrom(from, fromOwner, goal)
	return path
}

/*
================
planMonsterPathFrom

PlanMonsterPathFrom, plus the clip's deterministic work (ClipReport.Work),
for the cave planner, which prices each probe by its scan (#1678).
================
*/
func (v *WaterValidator) planMonsterPathFrom(from monster.Pose, fromOwner simulation.NavOwner, goal monster.Pose) (*monster.NavigationPath, int) {
	fromSpawn := simulation.Spawn{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z, Angle: from.Heading}
	goalSpawn := simulation.Spawn{RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z, Angle: goal.Heading}
	report, walk := v.clipMovementPathFromWithWalk(fromSpawn, fromOwner, goalSpawn)
	if report.Truncated || report.TilesUncovered != 0 || report.Outcome == ClipNoCoverage || report.Outcome == ClipDungeonExempt || report.Outcome == ClipStartBlocked {
		return nil, report.Work
	}
	rest := monster.Pose{RegionID: report.Rest.RegionID, X: report.Rest.X, Y: report.Rest.Y, Z: report.Rest.Z, Heading: goal.Heading}
	if walk == nil || rest.RegionID != goal.RegionID || rest.X != goal.X || rest.Z != goal.Z {
		walk = v.ownerWalk(fromSpawn, fromOwner, simulation.Spawn{RegionID: rest.RegionID, X: rest.X, Y: rest.Y, Z: rest.Z, Angle: rest.Heading})
	}
	y, ok := v.monsterPathHeightAt(1, rest, walk)
	if !ok {
		return nil, report.Work
	}
	rest.Y = y
	height := func(t float64, p monster.Pose) (float64, bool) {
		if sampleY, sampleOK := v.monsterPathHeightAt(t, p, walk); sampleOK {
			return sampleY, true
		}
		return p.Y, true
	}
	return monster.NewNavigationPath(from, goal, rest, report.NativeResult, height).WithOwners(pathOwnerSpans(walk, report)), report.Work
}

/*
================
pathOwnerSpans

The owner spans a monster path carries: the owner walk's, and on the cave
plane, which walks no surface owners, the rest's dungeon owner over the
whole chord (#1727). The cell-by-cell spans arrive with the native
dungeon walk (#1728).
================
*/
func pathOwnerSpans(walk *navWalk, report ClipReport) []simulation.NavOwnerSpan {
	if spans := walk.spans(); len(spans) > 0 || report.RestOwner.Kind != simulation.NavOwnerDungeon {
		return spans
	}
	return []simulation.NavOwnerSpan{{From: 0, To: 1, Owner: report.RestOwner}}
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
