/*
===========================================================================

gmteleport.go - the GM's town and player teleports

/GOTOWN, /TOTOWN, /MOVETOUSER and /RECALLUSER. The v1.188 per-player GM
handler CGObjPC_MsgHandler_SpecialCommand (51DE90) owns them: switch index
sub-1 through the byte table at 5202F0. Each one moves exactly one
character with the PC world teleport (vtable +0x378), which the port runs
through relocateCharacter, so a GM teleport carries the pet, rebuilds the
scene and rolls back like every other server-driven move.

The gmcommand dispatcher supplies decoded names and never writes character
state; every rule about who may move lives here.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
GoTownGM

/GOTOWN (sub 2, 51DE90 case 1): CGObjPC_TeleportToTown on the GM itself,
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

/TOTOWN name (sub 3, 51DE90 case 2): ObjMgr_FindByName, then
CGObjPC_TeleportToTown on the target, to the target's own appointed town.
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

/MOVETOUSER name (sub 8, 51DE90 case 7): the GM is teleported (vtable
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

/RECALLUSER name (sub 0x11, 51DE90 case 0xD): the request carries the name
and, when given, a position; otherwise the GM's own. 51DE90 always relays
it, and CGame_ProcessGMCommand (4154D0) finds the target and teleports it
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
objects (ObjMgr_FindByName) and, when it is not on this GameWorld, relays
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
