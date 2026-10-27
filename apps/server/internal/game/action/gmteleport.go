/*
===========================================================================

gmteleport.go - the GM's town and player teleports

/GOTOWN, /TOTOWN, /MOVETOUSER and /RECALLUSER. The reference server's
per-player GM special-command handler owns them: switch index
sub-1 through its byte table. Each one moves exactly one
character with the PC world teleport (vtable +0x378), which the port runs
through relocateCharacter, so a GM teleport carries the pet, rebuilds the
scene and rolls back like every other server-driven move.

The gmcommand dispatcher supplies decoded names and never writes character
state; every rule about who may move lives here.

===========================================================================
*/
package action

import (
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
GoTownGM

/GOTOWN (sub 2, case 1): the town teleport on the GM itself,
to the appointed town or recall point.
================
*/
func (rt *Runtime) GoTownGM(division, name string) bool {
	if rt == nil || rt.deps == nil {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gm := rt.findCharacter(division, name)
	if gm == nil {
		return false
	}
	return rt.relocateCharacter(division, gm, "gm-gotown", func() (travelPoint, bool) {
		if !gm.GMPrivilege || !gmTeleportMovable(gm) {
			return travelPoint{}, false
		}
		return rt.appointedRebirth(division, gm), true
	})
}

/*
================
SendToTownGM

/TOTOWN name (sub 3, case 2): a lookup by name, then
the town teleport on the target, to the target's own appointed town.
================
*/
func (rt *Runtime) SendToTownGM(division, name, targetName string) bool {
	if rt == nil || rt.deps == nil {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gm, target := rt.gmTeleportPair(division, name, targetName)
	if gm == nil {
		return false
	}
	return rt.relocateCharacter(division, target, "gm-totown", func() (travelPoint, bool) {
		if !gm.GMPrivilege || !gmTeleportMovable(target) {
			return travelPoint{}, false
		}
		return rt.appointedRebirth(division, target), true
	})
}

/*
================
MoveToUserGM

/MOVETOUSER name (sub 8, case 7): the GM is teleported (vtable
+0x378, type 2) to where the target stands now, in the target's world.
================
*/
func (rt *Runtime) MoveToUserGM(division, name, targetName string) bool {
	if rt == nil || rt.deps == nil {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gm, target := rt.gmTeleportPair(division, name, targetName)
	if gm == nil {
		return false
	}
	return rt.relocateCharacter(division, gm, "gm-movetouser", func() (travelPoint, bool) {
		if !gm.GMPrivilege || !gmTeleportMovable(gm) {
			return travelPoint{}, false
		}
		return rt.standingPoint(division, target), true
	})
}

/*
================
RecallUserGM

/RECALLUSER name (sub 0x11, case 0xD): the request carries the name
and, when given, a position; otherwise the GM's own. The handler always relays
it, and the game's GM command step finds the target and teleports it
there (vtable +0x378, type 2). The v1.150 client sends the name alone, so
the target comes to where the GM stands, in the GM's world.
================
*/
func (rt *Runtime) RecallUserGM(division, name, targetName string) bool {
	if rt == nil || rt.deps == nil {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gm, target := rt.gmTeleportPair(division, name, targetName)
	if gm == nil {
		return false
	}
	return rt.relocateCharacter(division, target, "gm-recalluser", func() (travelPoint, bool) {
		if !gm.GMPrivilege || !gmTeleportMovable(target) {
			return travelPoint{}, false
		}
		return rt.standingPoint(division, gm), true
	})
}

/*
================
gmTeleportPair

The GM and the named target. Native looks the target up among the live
objects (by name) and, when it is not on this GameWorld, relays
the command to the shard manager for another one. The port runs one
GameWorld per division, so a target that is not in the world has nowhere
else to be: the command fails (INFERENCE: the relay's answer for a player
no GameWorld holds is a failure). Returns nil, nil when either is missing.
================
*/
func (rt *Runtime) gmTeleportPair(division, name, targetName string) (gm, target *enterworld.Character) {
	gm = rt.findCharacter(division, name)
	target = rt.findCharacter(division, targetName)
	if gm == nil || target == nil {
		return nil, nil
	}
	if _, online := rt.EntryPopulationLease(division, target.Name); !online {
		return nil, nil
	}
	return gm, target
}

/*
================
standingPoint

Where a character stands this instant, in its world: the live pose
mid-move, not the saved one.
================
*/
func (rt *Runtime) standingPoint(division string, c *enterworld.Character) travelPoint {
	spawn := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, rt.Now().UnixMilli())
	return travelPoint{spawn: spawn, world: instance.ID(domain.CharacterWorldInstance(c))}
}

/*
================
gmTeleportMovable

The mover rules WarpGM keeps. INFERENCE: a corpse is not teleported (death
owns its revive flow), and a summoned COS needs a joint owner/follower
migration the port does not split.
================
*/
func gmTeleportMovable(c *enterworld.Character) bool {
	if !enterworld.CharacterAlive(c) {
		return false
	}
	return c.ActiveCOS == nil || !c.ActiveCOS.Summoned
}

/*
================
MoveToNpcGM

/movetonpc codename (sub 0x1F, case 0x18 -> the move-to-NPC command):
resolves the NPC or structure by codename (a live scan, then
_GetNPCPosByCodename nest fallback), refuses if the target's world is not a
permanent world (NativeType != 0), and teleports the GM to
its anchor in layer 1.
================
*/
func (rt *Runtime) MoveToNpcGM(division, name, codename string) bool {
	if rt == nil || rt.deps == nil || codename == "" {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gm := rt.findCharacter(division, name)
	if gm == nil {
		return false
	}
	target, ok := rt.npcTeleportPoint(division, codename)
	if !ok {
		return false
	}
	return rt.relocateCharacter(division, gm, "gm-movetonpc", func() (travelPoint, bool) {
		if !gm.GMPrivilege || !gmTeleportMovable(gm) {
			return travelPoint{}, false
		}
		return target, true
	})
}

/*
================
npcTeleportPoint

The native command's two lookup branches: first live field NPCs and standing fortress
structures, then the static nest catalog (_GetNPCPosByCodename), gated on a
permanent world (RefGameWorld +0x20 == 0).
================
*/
func (rt *Runtime) npcTeleportPoint(division, codename string) (travelPoint, bool) {
	for _, npc := range rt.NpcRoster {
		if strings.EqualFold(npc.Codename, codename) && npc.Spawn.RegionID != 0 {
			return travelPoint{spawn: npc.Spawn, world: instance.ID(domain.DefaultWorldInstance)}, true
		}
	}
	if rt.Monsters != nil && rt.Fortresses != nil {
		for _, definition := range instance.Shipped() {
			if definition.NativeType != 0 {
				continue
			}
			if _, ok := rt.Fortresses.ForWorld(definition); !ok {
				continue
			}
			world := instance.Pack(definition.ID, portalWorldLayer)
			for _, row := range rt.Monsters.WorldStructures(division, world) {
				if strings.EqualFold(row.Ref.Codename, codename) && row.Spawn.RegionID != 0 {
					spawn := simulation.Spawn{RegionID: row.Spawn.RegionID, X: row.Spawn.X, Y: row.Spawn.Y, Z: row.Spawn.Z}
					return travelPoint{spawn: spawn, world: world}, true
				}
			}
		}
	}
	if rt.Monsters != nil {
		worldCode, spawn, ok := rt.Monsters.FirstNestSpawnByCodename(codename)
		if !ok || spawn.RegionID == 0 {
			return travelPoint{}, false
		}
		definition, found := instance.Lookup(1)
		if worldCode != "" {
			found = false
			for _, row := range instance.Shipped() {
				if row.CodeName == worldCode {
					definition, found = row, true
					break
				}
			}
		}
		if !found || definition.NativeType != 0 {
			return travelPoint{}, false
		}
		return travelPoint{spawn: spawn, world: instance.Pack(definition.ID, portalWorldLayer)}, true
	}
	return travelPoint{}, false
}

/*
================
InstanceGM

/INSTANCE worldId layerId (sub 0x13, inline in the special-command handler):
layerId == 0 opens the first free layer slot on an instance world
(the world manager's layer open); layerId != 0 closes that layer
(its layer close), evacuating any residents back to
their saved return or appointed rebirth before releasing its population.
Permanent worlds (NativeType == 0) refuse both.
================
*/
func (rt *Runtime) InstanceGM(division string, worldID, layerID uint16) bool {
	if rt == nil || rt.Monsters == nil {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	defID := instance.DefinitionID(worldID)
	definition, ok := instance.Lookup(defID)
	if !ok || definition.NativeType == 0 {
		return false
	}
	if layerID == 0 {
		_, opened := rt.Monsters.OpenInstanceLayer(division, defID)
		return opened
	}
	target := instance.Pack(defID, layerID)
	lease, active := rt.Monsters.PopulationLease(division, target)
	if !active {
		return false
	}
	rt.Monsters.BeginPopulationRetirement(division, lease)
	for _, resident := range rt.fortressResidents(division, target) {
		rt.relocateCharacter(division, resident, "gm-instance-close", func() (travelPoint, bool) {
			return rt.instanceEvacuationPoint(division, resident), true
		})
	}
	return rt.Monsters.ReleasePopulation(division, lease)
}

/*
================
instanceEvacuationPoint

Where a resident returns when an instance layer closes: its saved permanent-world
return pose when recorded, or else its appointed rebirth point.
================
*/
func (rt *Runtime) instanceEvacuationPoint(division string, c *enterworld.Character) travelPoint {
	if c != nil && c.World != nil && c.World.SavedReturn != nil && c.World.SavedReturn.RegionID != 0 {
		saved := c.World.SavedReturn
		defID := instance.DefinitionID(saved.Definition)
		if defID == 0 {
			defID = 1
		}
		if definition, ok := instance.Lookup(defID); ok && definition.NativeType == 0 {
			return travelPoint{
				world: instance.Pack(defID, portalWorldLayer),
				spawn: simulation.Spawn{
					RegionID: saved.RegionID,
					X:        float64(saved.X),
					Y:        float64(saved.Y),
					Z:        float64(saved.Z),
				},
			}
		}
	}
	return rt.appointedRebirth(division, c)
}
