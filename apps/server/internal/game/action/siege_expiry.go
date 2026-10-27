/*
===========================================================================

siege_expiry.go - the siege corpse clock runs out

The reference server's player tick counts a siege corpse's clock (+0x1CD4) down
while a fortress war runs; at zero the corpse is sent to town
(the town teleport), asked or not. The port stamps the
death instant instead of ticking every corpse (domain.SiegeDeathAtMs):
this sweep, riding the simulation tick beside the fortress phases, is
the tick that notices the count ran out, and it revives through the
same rebirth door as the player's own town choice.

===========================================================================
*/

package action

import (
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// siegeCorpseExpiryMs is the +0x1CD4 count itself: 180 ticks from the
// death, at the recorded one second a tick (domain.SiegeDeathAtMs).
const siegeCorpseExpiryMs = 180 * 1000

/*
================
advanceSiegeCorpses

One simulation tick of the corpse clock. Every resident corpse whose
180 ticks ran out inside an active siege war is teleported to town
(handleRebirthInDoor with the town forced: OnTick calls TeleportToTown
whatever the death kind). The re-entry corpus is the corpse's own;
the correction, LIFE-alive and companion despawns go to its observers
at the spot it left.

Measuring from the death instant is exact for deaths during the war.
A corpse stamped before the war started expires at the war's first
tick instead of 180 war-ticks later - the recorded approximation on
#1158: the fortress Authority keeps no period-start instant to clamp
against, and town is the destination either way.
================
*/
func (rt *Runtime) advanceSiegeCorpses(nowMs int64) []simulation.DivisionFrames {
	if rt.Fortresses == nil {
		return nil
	}
	type corpseKey struct {
		division, name string
	}
	var keys []corpseKey
	rt.characterAdmissions.Range(func(key, _ any) bool {
		if value, exists := rt.characterAdmissions.Load(key); exists {
			owner := value.(populationAdmission)
			keys = append(keys, corpseKey{owner.division, owner.name})
		}
		return true
	})
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].division != keys[j].division {
			return keys[i].division < keys[j].division
		}
		return keys[i].name < keys[j].name
	})

	var out []simulation.DivisionFrames
	lastDivision, warActive := "", false
	for _, key := range keys {
		if key.division != lastDivision {
			lastDivision, warActive = key.division, rt.Fortresses.WarActive(key.division)
		}
		if !warActive {
			continue
		}
		out = append(out, rt.expireSiegeCorpseInDivision(key.division, key.name, nowMs)...)
	}
	return out
}

/*
================
expireSiegeCorpseInDivision
================
*/
func (rt *Runtime) expireSiegeCorpseInDivision(division, name string, nowMs int64) []simulation.DivisionFrames {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if !siegeCorpseDue(c, nowMs) {
		return nil
	}
	result := rt.handleRebirthInDoor(division, c, wire.RebirthAtSpecifiedPoint, true)
	var out []simulation.DivisionFrames
	if len(result.Frames) > 0 {
		out = append(out, simulation.DivisionFrames{
			DivisionID: division, OnlyCharacterID: c.ID, Frames: simFrames(result.Frames),
		})
	}
	if len(result.Broadcast) > 0 {
		out = append(out, simulation.DivisionFrames{
			DivisionID: division, SourceGID: enterworld.ObjectIDForCharacter(c),
			ExceptCharacterID: c.ID, Frames: simFrames(result.Broadcast),
		})
	}
	return out
}

/*
================
siegeCorpseDue

The OnTick conjunction for one corpse: dead, stamped, the count run
out, and lying in a siege world. The active war is the caller's check -
the native siege-war check gates the native tick itself.
================
*/
func siegeCorpseDue(c *enterworld.Character, nowMs int64) bool {
	if c == nil || enterworld.CharacterAlive(c) || c.SiegeDeathAtMs == 0 {
		return false
	}
	return nowMs-c.SiegeDeathAtMs >= siegeCorpseExpiryMs && inSiegeWorld(c)
}
