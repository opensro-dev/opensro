/*
===========================================================================

jobdresstown.go - a finished dress reloads the wearer at the town spawn

Port-only, not native (owner request 2026-11-11). The suit's reload
(reloadJobDress) re-enters the wearer where they stand, so a rival job
player who watched the dress bar sees the same character come back under
its alias and can follow it. With SRO_JOB_DRESS_TOWN_SPAWN on, the reload
lands on the spawn of the nearest town instead: the recall point (the
gate a return scroll designates) closest to the wearer in the same world.
Off, the default, keeps the in-place reload.

===========================================================================
*/
package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvJobDressTownSpawn turns the rule on ("on", "1", "true").
const EnvJobDressTownSpawn = "SRO_JOB_DRESS_TOWN_SPAWN"

// dungeonRegionBit marks a region of a dungeon world, which has no town.
const dungeonRegionBit uint16 = 0x8000

/*
================
JobDressTownSpawnFromEnv
================
*/
func JobDressTownSpawnFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJobDressTownSpawn))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
jobDressTownPoint

The town spawn a finished dress reloads the wearer at: the recall point
nearest to here in the same world. In a dungeon, or where the world has no
recall point, the wearer's appointed town (the return scroll's) stands in.
Ties go to the lower teleport id so the choice never depends on map order.
================
*/
func (rt *Runtime) jobDressTownPoint(division string, c *enterworld.Character, here travelPoint) travelPoint {
	if rt.portals == nil || here.spawn.RegionID&dungeonRegionBit != 0 {
		return rt.appointedRebirth(division, c)
	}
	var best portalDestination
	bestDistance := -1.0
	for _, destination := range rt.portals.destinations {
		if !destination.recall || destination.spawn.RegionID == 0 || destination.spawn.RegionID&dungeonRegionBit != 0 {
			continue
		}
		if portalWorld(destination) != here.world {
			continue
		}
		distance := simulation.WorldDistance2D(here.spawn, destination.spawn)
		if bestDistance < 0 || distance < bestDistance || distance == bestDistance && destination.id < best.id {
			best, bestDistance = destination, distance
		}
	}
	if bestDistance < 0 {
		return rt.appointedRebirth(division, c)
	}
	return travelPoint{spawn: best.spawn, world: portalWorld(best)}
}
