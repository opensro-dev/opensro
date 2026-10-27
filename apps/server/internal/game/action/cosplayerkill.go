/*
===========================================================================

cosplayerkill.go - what a player earns for killing a COS

The reference server's killer credit runs for every character death,
a COS's included. Its only credit to a player killer is the murder-penalty
relief of the reference server's monster-kill PK relief, by the gap between
the victim's level (the character level getter, the same slot on every COS
class) and the killer's. Everything else a transport's death does, the
cargo scatter and the release, is killer-independent and settles in the
COS's own death (cosdeath.go). The player strike on a COS calls this once,
at the blow that kills it, inside the killer's door.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
)

/*
================
Runtime.creditPlayerCOSKill

The reference server's killer credit for a player who killed a COS of level cosLevel: a killer
with a murder penalty eases it by the level gap, as for a monster kill
(reward_group.go). Returns the killer's private frames and the public PVP
state frame when the state changed.
================
*/
func (rt *Runtime) creditPlayerCOSKill(division string, killer *enterworld.Character, cosLevel uint8) (actorFrames, public []wire.Frame) {
	if killer == nil || killer.PK == nil || killer.PK.Penalty <= 0 {
		return nil, nil
	}
	before := killer.PVPState()
	relief := pk.MonsterKillRelief(cosLevel, uint8(min(rewardLevel(killer), 0xff)))
	actorFrames = pkRecordFrames(killer, pk.AddPenalty(killer, relief, rt.Now()))
	rt.notePKRecord(division, killer)
	if before != killer.PVPState() {
		public = append(public, playerPVPStateFrame(killer))
	}
	return actorFrames, public
}
