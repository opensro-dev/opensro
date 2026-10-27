/*
===========================================================================

gmmobkill.go - GM /MOBKILL: kill the monster the GM has selected

The client sends subcommand 0x14 with the selected target's object id and a
mode word of 0 or 1 (GmCommand_ProcessChatSlashCommand @0x0050b754). The
mode meaning below is INFERENCE from community GM guides for the V-Sro
client; no server binary was read:

	mode 0 - kill the monster as a normal kill: EXP/SP, loot and the other
	         kill rewards go through the ordinary settlement
	mode 1 - kill the monster without any reward

Both modes end the monster through the ordinary population door and the
ordinary defeat lifecycle, so the corpse, its presentation time and its nest
respawn behave exactly as for a monster killed in combat.

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
noteGMKillRefusal

One line per refused /MOBKILL: the dispatcher reports the verdict, this names
the reason, which the GM's ack cannot carry.
================
*/
func (rt *Runtime) noteGMKillRefusal(division, name string, gid uint32, reason string) {
	log.WithFields(log.Fields{"division": division, "character": name, "gid": gid, "reason": reason}).
		Info("gmcommand: /MOBKILL refused")
}

/*
================
KillGMMonster

Kills the live monster gid in the GM's own population. The GM is credited
with the HP it removed when rewards is true; with rewards false the hit is
unattributed and no settlement runs. A missing, dead or structure target
refuses the command: a fortress structure keeps standing in its destroyed
state under the fortress owner.
================
*/
func (rt *Runtime) KillGMMonster(division, name string, gid uint32, rewards bool) bool {
	if rt == nil || rt.deps == nil || rt.Monsters == nil {
		return false
	}
	unlockPublication := rt.LockPublication(division, "gmMobKill")
	defer unlockPublication()
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil || c.DeletePending || !c.GMPrivilege || !enterworld.CharacterAlive(c) {
		rt.noteGMKillRefusal(division, name, gid, "sender-not-a-live-gm")
		return false
	}
	lease, admitted := rt.EntryPopulationLease(division, c.Name)
	if !admitted {
		rt.noteGMKillRefusal(division, name, gid, "sender-not-in-the-world")
		return false
	}
	// A stale selection after a world transfer must not affect the old world.
	target, found := rt.Monsters.GetInPopulation(division, lease, gid)
	if !found {
		rt.noteGMKillRefusal(division, name, gid, "no-such-monster")
		return false
	}
	if target.CurrentHP == 0 {
		rt.noteGMKillRefusal(division, name, gid, "already-dead")
		return false
	}
	if target.Ref.Structure {
		rt.noteGMKillRefusal(division, name, gid, "structure")
		return false
	}
	now := rt.Now().UnixMilli()
	batches, killed := rt.killGMKillTarget(division, c, gmKillTarget{instance: target, rewards: rewards, dropSource: gid}, now)
	if !killed {
		rt.noteGMKillRefusal(division, name, gid, "kill-not-committed")
		return false
	}
	rt.queueMonsterLegFrames(batches)
	return true
}

/*
================
gmKillTarget

Existing victims publish drops around themselves. Newly created /ZOE victims
publish drops around the GM's colocated point, whose scope is already live.
================
*/
type gmKillTarget struct {
	instance   monster.Instance
	rewards    bool
	dropSource uint32
}

/*
================
killGMKillTarget

One /MOBKILL or /ZOE victim under the division lock: the ordinary HP door,
the ordinary reward settlement when rewards is true, and the ordinary defeat
lifecycle. Returns the frames to publish and whether the kill committed.
================
*/
func (rt *Runtime) killGMKillTarget(division string, c *enterworld.Character, request gmKillTarget, now int64) ([]simulation.DivisionFrames, bool) {
	target, rewards := request.instance, request.rewards
	gid := target.Gid
	plan := simulation.MonsterDamagePlan{GID: gid, ExpectedHP: target.CurrentHP, Damage: target.CurrentHP}
	var impacts []simulation.MonsterDamageResult
	var settlement monsterSettlement
	if rewards {
		plan.CreditGID = enterworld.ObjectIDForCharacter(c)
		roster := rt.monsterRewardRoster(division, c, now)
		commit := func() bool {
			impacts = rt.Monsters.ApplyDamageSequence(division, gid, target.CurrentHP, []simulation.MonsterDamagePlan{plan})
			if len(impacts) == 0 || !impacts[0].Fatal {
				return false
			}
			pose := monster.Pose{}
			if mover, exists := rt.Monsters.Mover(division, gid); exists {
				pose = mover.LivePoseAt(now, nil)
			}
			settlement = rt.settleMonsterInsideDoorCounted(division, c, roster, impacts[0], pose, now, false)
			return true
		}
		if !rt.deps.UpdateManyDeferred(roster.characters, "gm-mobkill", commit) {
			return nil, false
		}
	} else {
		impacts = rt.Monsters.ApplyDamageSequence(division, gid, target.CurrentHP, []simulation.MonsterDamagePlan{plan})
		if len(impacts) == 0 || !impacts[0].Fatal {
			return nil, false
		}
	}
	batches := []simulation.DivisionFrames{{DivisionID: division, SourceGID: gid, Frames: simFrames([]wire.Frame{monsterLifeDeadFrame(gid)})}}
	if rewards {
		drops := rt.groundReferences(settlement.drops)
		for _, drop := range settlement.drops {
			drops = append(drops, wire.DropBroadcastFrames(drop.SpawnRow(true))...)
		}
		if len(drops) > 0 {
			batches = append(batches, simulation.DivisionFrames{DivisionID: division, SourceGID: request.dropSource, Frames: simFrames(drops)})
		}
		rt.announceSealDrops(division, target.Ref.Name, settlement.drops, now)
	}
	rt.queueMonsterDefeat(division, gid, now+monsterDeathPresentationRetention.Milliseconds())
	// Death and drops belong to the victim's visibility scope, while level-up
	// and other progression visuals belong to the credited player's scope.
	if len(settlement.public) > 0 {
		batches = append(batches, simulation.DivisionFrames{DivisionID: division, SourceGID: enterworld.ObjectIDForCharacter(c), Frames: simFrames(settlement.public)})
	}
	if private := wire.ProgressionPrivateFrames(settlement.actorFrames); len(private) > 0 {
		batches = append(batches, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: c.ID, Frames: simFrames(private)})
	}
	batches = append(batches, recipientDivisionFrames(division, settlement.others)...)
	// Entry cannot start between settlement and capture: both entry points
	// retain the outer publication gate, before taking the division lock.
	return rt.sealTickPrivate(batches), true
}

/*
================
queueMonsterLegFrames

Frames a kill committed outside an action session waits for the action tick,
like queueMonsterLegRecipients.
================
*/
func (rt *Runtime) queueMonsterLegFrames(batches []simulation.DivisionFrames) {
	if len(batches) == 0 {
		return
	}
	rt.monsterLegRecipientsMu.Lock()
	rt.monsterLegRecipients = append(rt.monsterLegRecipients, batches...)
	rt.monsterLegRecipientsMu.Unlock()
}
