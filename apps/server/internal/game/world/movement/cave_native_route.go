/*
===========================================================================

cave_native_route.go - the native cave region route, one waypoint a call

Where a dungeon's route file loaded and fit its meshes (ainavdata.go), a
blocked cave chord takes the native region route instead of the port's
portal search (cave_route.go). Read in the server binary at assembly
(#1698; the server's names and addresses stay in the research database):

	different blocks  fastest[dest*n + cur] names the current block's
	                  portal; standing in its cell steps to the landing
	                  point in the next block, otherwise the cell route
	                  heads for the portal cell.
	same block        the cell route toward the goal's cell.
	cell route        route[dest*m + cur] packs two point indices: the
	                  high half is tried first, and when the movement
	                  query toward it reports a clipped edge (result bit
	                  0) the low half is taken. A blocked-only result
	                  (bit 28) keeps the high half. A monster always
	                  queries in mode 0 (the mode is 1 only for players
	                  and COS).
	step              destination = position + planar unit direction x
	                  the distance clamped to [5, 2^32] (an unordered,
	                  NaN, distance takes the cap), each coordinate
	                  truncated toward zero; issued as an ordinary move,
	                  so the mover's own clip applies.

The step overshoots a point nearer than 5 units by design; the mover
replans from where the leg ends, as the native pursuit calls the route
again on its next tick. A dungeon without a valid table (0x8009, whose
file does not fit the v1.150 mesh) keeps the port's route.

Port-only, not native (SRO_CAVE_SEAM_STEP, owner 2026-10-26, on): a
doorway's landing point can sit a few ten-thousandths past a whole unit
on the far side, so the truncated step lands short of the seam, still in
the portal cell, and the next call steps along the seam again - 21 of
Donwhang's 300 doorways trap a chasing monster this way in the original.
With the switch on, only such a step - a landing step inside the 5-unit
minimum whose truncated destination stays in the current block - rounds
each coordinate toward its target instead of toward zero; every other
step is the native one.

===========================================================================
*/
package movement

import (
	"math"
	"os"
	"strings"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvCaveSeamStep turns the port-only cave seam step on ("on", "1",
// "true"); off is the native step.
const EnvCaveSeamStep = "SRO_CAVE_SEAM_STEP"

// The native step clamp: read from the binary's float constants.
const (
	caveStepMinUnits = 5.0
	caveStepMaxUnits = 4294967296.0
)

/*
================
nativeCaveWaypoint

The native region route's one waypoint from start toward target, both
already located on the floor (planCaveRoute ran the clear-chord and
same-cell heads). Returns false when the dungeon has no valid table, so
the caller keeps the port's route.
================
*/
func (v *WaterValidator) nativeCaveWaypoint(surface *dungeonSpawnSurface, from, goal monster.Pose, start, target caveCell) (*monster.NavigationRoute, bool) {
	table := surface.routeTable
	if table == nil {
		return nil, false
	}
	n := len(table.blocks)
	cur, dest := surface.blocks[start.block].ordinal, surface.blocks[target.block].ordinal
	block := &table.blocks[cur]

	var point [3]float32
	probes, work := 0, 0
	landing := false
	if cur == dest {
		point, probes, work = v.caveRoutePoint(block, target.cell, start.cell, from)
	} else {
		portal, ok := block.portal(uint32(table.fastest[dest*n+cur]))
		if !ok {
			// The loader refuses a table whose fastest entry names no
			// portal, so this is unreachable; the native route logs and
			// fails here.
			return monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked), true
		}
		if start.cell == int(portal.cell) {
			point = table.blocks[portal.landingBlock].points[portal.landingPoint]
			landing = true
		} else {
			point, probes, work = v.caveRoutePoint(block, int(portal.cell), start.cell, from)
		}
	}
	step := caveStep(from, point, false)
	if landing && v.caveSeamStep && caveStepShort(from, point) {
		if block, _, _, ok := surface.floorCellAt(step.X, from.Y, step.Z); !ok || block == start.block {
			step = caveStep(from, point, true)
		}
	}
	if y, ok := v.dungeonSpawnHeightAt(from.RegionID, step.X, from.Y, step.Z); ok {
		step.Y = y
	}
	step.Heading = goal.Heading
	return monster.NewNavigationRoute(goal, []monster.Pose{step}).WithProbes(probes).WithWork(work), true
}

/*
================
caveRoutePoint

The cell route's point from cur toward dest inside one block: the high
half's point, or the low half's when the movement query toward the high
one clips. Returns the probes and the clip work the query spent.
================
*/
func (v *WaterValidator) caveRoutePoint(block *caveRouteBlock, dest, cur int, from monster.Pose) ([3]float32, int, int) {
	word := block.route[dest*block.cells+cur]
	hi := block.points[word>>16]
	toward := from
	toward.X, toward.Y, toward.Z = float64(hi[0]), float64(hi[1]), float64(hi[2])
	path, work := v.planMonsterPathFrom(from, simulation.NavOwner{}, toward)
	// A query with no answer (no coverage under the line) is taken as
	// clipped: the native query always answers, and a line the port cannot
	// walk is one the high point cannot be reached along.
	if path == nil || path.Result()&monster.NavResultClipped != 0 {
		return block.points[word&0xFFFF], 1, work
	}
	return hi, 1, work
}

/*
================
caveStep

The native step toward a route point: along the planar direction, the
distance clamped to [5, 2^32] (NaN to the cap), each coordinate stored
as float32 and truncated toward zero. towardTarget (the port-only seam
step) rounds each coordinate away from the position along the direction
instead. The height is the caller's (the native step sends the
position's own; the port's mover reads the floor).
================
*/
func caveStep(from monster.Pose, point [3]float32, towardTarget bool) monster.Pose {
	dx, dz := float64(point[0])-from.X, float64(point[2])-from.Z
	distance := math.Hypot(dx, dz)
	ux, uz := 0.0, 0.0
	if distance > 0 {
		ux, uz = dx/distance, dz/distance
	}
	switch {
	case math.IsNaN(distance) || distance > caveStepMaxUnits:
		distance = caveStepMaxUnits
	case distance < caveStepMinUnits:
		distance = caveStepMinUnits
	}
	step := from
	step.X = caveStepCoordinate(float64(float32(from.X+ux*distance)), ux, towardTarget)
	step.Z = caveStepCoordinate(float64(float32(from.Z+uz*distance)), uz, towardTarget)
	return step
}

/*
================
caveStepShort

Whether a route point lies inside the step's 5-unit minimum: the regime
where the native step overshoots and a doorway's truncated landing can
pace along the seam.
================
*/
func caveStepShort(from monster.Pose, point [3]float32) bool {
	return math.Hypot(float64(point[0])-from.X, float64(point[2])-from.Z) < caveStepMinUnits
}

/*
================
caveStepCoordinate

One step coordinate made whole: truncated toward zero (native), or with
towardTarget rounded on along the step's direction.
================
*/
func caveStepCoordinate(at, direction float64, towardTarget bool) float64 {
	switch {
	case !towardTarget || direction == 0:
		return math.Trunc(at)
	case direction > 0:
		return math.Ceil(at)
	}
	return math.Floor(at)
}

/*
================
WaterValidator.SetCaveSeamStep

Turns the port-only cave seam step on or off (EnvCaveSeamStep). Set once
at boot, before the first move.
================
*/
func (v *WaterValidator) SetCaveSeamStep(on bool) {
	v.caveSeamStep = on
}

/*
================
CaveSeamStepFromEnv
================
*/
func CaveSeamStepFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvCaveSeamStep))) {
	case "on", "1", "true":
		return true
	}
	return false
}
