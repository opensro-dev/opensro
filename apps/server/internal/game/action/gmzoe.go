/*
===========================================================================

gmzoe.go - GM /ZOE: spawn monsters at the GM and kill them immediately

The client sends subcommand 0x0C with the monster reference id and a count
clamped to 1..255 (client GmCommand_ProcessChatSlashCommand @0x0050a6dd, three
builder calls of 1, 4 and 1 bytes). The effect is INFERENCE from community GM
guides for the V-Sro client, not from a server binary:

	/zoe <MONSTER> <count> - load the monsters, then kill them, awarding
	EXP/SP and drops as a normal kill would.

That is exactly what this owner does: the ordinary GM spawn path (so the
monsters are real, in the GM's own world and population) followed by the
ordinary reward kill for each one, with the GM credited.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// The ordinary GM spawn owner caps one request at 250; /ZOE's native u8
// count permits 255, so the tail gets its own ordinary spawn request.
const gmZoeSpawnBatchLimit = 250

/*
================
KillGMZoe

Loads count monsters of refObjID at the GM's position and kills each with the
ordinary reward settlement. Returns whether any kill committed; an unknown
reference or missing admission refuses the command before spawning.
================
*/
func (rt *Runtime) KillGMZoe(division, name string, refObjID uint32, count uint8) bool {
	if rt == nil || rt.deps == nil || rt.Monsters == nil {
		return false
	}
	unlockPublication := rt.LockPublication(division, "gmZoe")
	defer unlockPublication()
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil || c.DeletePending || !c.GMPrivilege || !enterworld.CharacterAlive(c) {
		rt.noteGMKillRefusal(division, name, 0, "zoe-sender-not-a-live-gm")
		return false
	}
	lease, admitted := rt.EntryPopulationLease(division, c.Name)
	if !admitted {
		rt.noteGMKillRefusal(division, name, 0, "zoe-sender-not-in-the-world")
		return false
	}
	if count < 1 {
		rt.noteGMKillRefusal(division, name, refObjID, "zoe-zero-count")
		return false
	}
	if ref, found := rt.Monsters.Reference(refObjID); !found || ref.MaxHP == 0 || ref.Structure {
		rt.noteGMKillRefusal(division, name, refObjID, "zoe-invalid-monster-reference")
		return false
	}
	now := rt.Now().UnixMilli()
	request := simulation.GMMonsterSpawn{
		Division: division, Population: lease, RefObjID: refObjID, Count: count, Type: 0, NowMs: now,
		Position: rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now),
	}
	var gids []uint32
	for remaining := int(count); remaining > 0; {
		request.Count = uint8(min(remaining, gmZoeSpawnBatchLimit))
		created := rt.Monsters.SpawnGMMonstersTracked(request)
		gids = append(gids, created...)
		remaining -= int(request.Count)
		if len(created) != int(request.Count) {
			break
		}
	}
	if len(gids) == 0 {
		rt.noteGMKillRefusal(division, name, refObjID, "zoe-spawn-refused")
		return false
	}
	var batches []simulation.DivisionFrames
	for _, gid := range gids {
		target, found := rt.Monsters.Get(division, gid)
		if !found || target.CurrentHP == 0 {
			continue
		}
		frames, killed := rt.killGMKillTarget(division, c, gmKillTarget{
			instance: target, rewards: true, dropSource: enterworld.ObjectIDForCharacter(c),
		}, now)
		if killed {
			batches = append(batches, frames...)
		}
	}
	if len(batches) == 0 {
		rt.noteGMKillRefusal(division, name, refObjID, "zoe-kill-not-committed")
		return false
	}
	rt.queueMonsterLegFrames(batches)
	return true
}
