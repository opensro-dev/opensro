/*
===========================================================================

linkedlifecycle.go - retire live links when either endpoint or its tether fails

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// 58482E..584A67 checks the live pair, not the two original cast positions.
// Retirement itself stays in the existing character-effect update owner.
/*
================
advanceLinkedEffects
================
*/
func (rt *Runtime) advanceLinkedEffects(nowMs int64) {
	rt.advanceForcedTargets()
	for _, link := range rt.effects.Links() {
		unlock := rt.lockDivision(link.DivisionID)
		source := rt.characterSnapshot(link.DivisionID, rt.findCharacter(link.DivisionID, link.SourceName))
		target := rt.characterSnapshot(link.DivisionID, rt.findCharacter(link.DivisionID, link.TargetName))
		valid := source != nil && target != nil && !source.DeletePending && !target.DeletePending &&
			enterworld.ObjectIDForCharacter(source) == link.SourceGID && enterworld.ObjectIDForCharacter(target) == link.TargetGID &&
			enterworld.CharacterAlive(source) && enterworld.CharacterAlive(target)
		if valid && link.MaxDistance != 0 {
			from := rt.liveSpawn(simulation.WorldKey(link.DivisionID, source.Name), source, nowMs)
			to := rt.liveSpawn(simulation.WorldKey(link.DivisionID, target.Name), target, nowMs)
			// 5849F1 shares the trap tether's native 3D distance and float stores.
			a := monster.Pose{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z}
			b := monster.Pose{RegionID: to.RegionID, X: to.X, Y: to.Y, Z: to.Z}
			valid = monster.NativeActorDistance(a, b) <= float32(link.MaxDistance)
		}
		if !valid {
			rt.effects.StopLink(link.DivisionID, link.SourceToken)
		}
		unlock()
	}
}
