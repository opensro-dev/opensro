/*
===========================================================================

spawnregion.go - region residency for native generated-spawn admission

The port serves one complete authored map and loads its immutable surfaces
on demand. A resolvable region is the equivalent of native resident state 1;
cell restrictions and object decks remain with the subsequent movement test.

===========================================================================
*/
package movement

import (
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
SpawnRegionAvailable
================
*/
func (v *WaterValidator) SpawnRegionAvailable(regionID uint16) bool {
	return v.validatorPlanes().spawnRegionAvailable(regionID)
}

/*
================
ConstrainCompanionSpawn

The companion-creation path supplies no clipped-result output to the
monster spawn. A clipped move therefore keeps the navigation endpoint;
a blocked move returns to the owner.
Player-request endpoint refusal is a different contract and must not replace
this creation policy. Missing port assets fail closed at the admitted owner.
================
*/
func (v *WaterValidator) ConstrainCompanionSpawn(origin, candidate simulation.Spawn) simulation.Spawn {
	report := v.ClipMovementPath(origin, candidate)
	if report.NativeResult&monster.NavResultBlocked != 0 || report.Outcome == ClipNoCoverage ||
		report.Outcome == ClipDungeonExempt || report.TilesUncovered != 0 || report.Truncated {
		return origin
	}
	position := report.Rest
	height, available := v.WalkableSpawnHeightAt(position.RegionID, position.X, position.Y, position.Z)
	if !available {
		return origin
	}
	position.Y = height
	return position
}
