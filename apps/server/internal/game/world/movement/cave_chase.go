/*
===========================================================================

cave_chase.go - the cave chase's two port-only switches (#1742)

Port-only, not native (owner 2026-10-26: gameplay over the original where
the original is measurably worse; each switch's off value is native):

	SRO_CAVE_MODERN_CHASE      a monster chasing through a cave takes the
	                           port's planner (cave_route.go: A* over the
	                           cells, string-pulled, proved by the walk)
	                           instead of the original's region route
	                           (cave_native_route.go). The original's
	                           whole-unit steps pace forever where a cell
	                           route point lies a fraction off a unit line
	                           (Donwhang block 134); over 60 seeded
	                           Donwhang chases it arrives 53 times with
	                           its own 100 ms re-check, the port's planner
	                           59 (with the escape below).
	SRO_CAVE_OBSTACLE_ESCAPE   a leg that starts inside an obstacle circle
	                           walks out of it (dungeon_walk.go). The
	                           server clip is shared, but the browser
	                           client still runs the original circle test
	                           on a player's own walk (clipper.ts), so in
	                           effect it frees monster chases only.

The planner already replans whenever the chased target moves (the
simulation's goal-changed cancel), so the original's 100 ms re-check adds
no arrivals to it and is not ported.

===========================================================================
*/
package movement

import (
	"os"
	"strings"
)

// EnvCaveModernChase turns the port's cave chase planner on ("on", "1",
// "true"); off is the original's region route.
const EnvCaveModernChase = "SRO_CAVE_MODERN_CHASE"

// EnvCaveObstacleEscape lets a leg that starts inside an obstacle circle
// walk out ("on", "1", "true"); off is the original's circle test.
const EnvCaveObstacleEscape = "SRO_CAVE_OBSTACLE_ESCAPE"

/*
================
SetCaveModernChase
================
*/
func (v *WaterValidator) SetCaveModernChase(on bool) {
	v.caveModernChase = on
}

/*
================
SetCaveObstacleEscape
================
*/
func (v *WaterValidator) SetCaveObstacleEscape(on bool) {
	v.caveObstacleEscape = on
}

/*
================
CaveModernChaseFromEnv
================
*/
func CaveModernChaseFromEnv() bool {
	return switchOn(os.Getenv(EnvCaveModernChase))
}

/*
================
CaveObstacleEscapeFromEnv
================
*/
func CaveObstacleEscapeFromEnv() bool {
	return switchOn(os.Getenv(EnvCaveObstacleEscape))
}

/*
================
switchOn

The package's on/off switch parse: "on", "1" or "true", any case and
surrounding space; anything else is off (native).
================
*/
func switchOn(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "1", "true":
		return true
	}
	return false
}
