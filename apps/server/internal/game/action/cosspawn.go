/*
===========================================================================

cosspawn.go - native companion admission around the owner's location

Creation, restored sessions and travel use the same random-radius placement
as native monster creation. A blocked pet spawn falls back to the owner.
Vehicles retain the rider anchor and never consume a pet-position draw.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	companionSpawnRadius    float32 = 20
	companionSpawnRandomMax uint32  = 32767
)

/*
================
EntryCompanionSpawn

Bootstrap reads the same movement owner as peer visibility. In particular,
a resumed connection must not move an existing follower back onto its owner.
The entry character is detached; only its identity selects the live state.
================
*/
func (rt *Runtime) EntryCompanionSpawn(division string, character *enterworld.Character, pet *enterworld.CharacterCOS) simulation.Spawn {
	return rt.companionLiveSpawn(division, character, pet, rt.Now().UnixMilli())
}

/*
================
companionAdmissionSpawn

4FBAE0 supplies radius 20 to 5F6EB0 for attack/pickup pets. The latter uses
531240's shared sampler and falls back to the centre on blocked COS moves.
The caller holds the division lock, never petMu, across geometry admission.
================
*/
func (rt *Runtime) companionAdmissionSpawn(pet *enterworld.CharacterCOS, centre simulation.Spawn) simulation.Spawn {
	ref, found := rt.cosReference(pet)
	if !found || ref.TidWord>>11 < 3 || ref.TidWord>>11 > 5 {
		return centre
	}
	roll := rt.CompanionRoll
	if roll == nil {
		roll = combat.SecureRoll32767
	}
	valid := true
	draw := func() uint32 {
		value, err := roll()
		if err != nil || value > companionSpawnRandomMax {
			valid = false
			return 0
		}
		return value
	}
	x, z, _ := monster.NativeSpawnPosition(float32(centre.X), float32(centre.Z), companionSpawnRadius, draw)
	if !valid {
		// Native rand cannot fail. On an OS entropy failure the admitted owner
		// remains a valid fallback; never manufacture out-of-range coordinates.
		return centre
	}
	candidate := centre
	candidate.X, candidate.Z = float64(x), float64(z)
	candidate = simulation.NormalizeSpawnFrame(candidate)
	candidate = simulation.ClampGeneratedSpawnRegion(centre, candidate, rt.SpawnRegionAvailable)
	if rt.ConstrainCompanionSpawn != nil {
		candidate = rt.ConstrainCompanionSpawn(centre, candidate)
	}
	return candidate
}

/*
================
EntryCompanionActionSpeed

Same-session world re-entry retains the companion's abnormal keeper.
================
*/
func (rt *Runtime) EntryCompanionActionSpeed(division string, character *enterworld.Character, pet *enterworld.CharacterCOS) float32 {
	return cosParameter(nil, pet, rt.cosAbnormal(division, character.Name, pet.GID), actionSpeedParameter)
}
