/*
===========================================================================

guildwar.go - movement admission and fatal combat handoff to guild wars

The guild-war lane owns records and payment. Action supplies native live
range and immutable pre-death scoring facts, then publishes after its door.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/social/guildwar"
)

const guildWarMasterReach = 300

/*
================
GuildWarMastersNear

The reference server compares the relative three-dimensional distance against 300.
================
*/
func (rt *Runtime) GuildWarMastersNear(division string, first, second *enterworld.Character) bool {
	if first == nil || second == nil || domain.CharacterWorldInstance(first) != domain.CharacterWorldInstance(second) {
		return false
	}
	a, b := rt.LiveSpawnFor(division, first), rt.LiveSpawnFor(division, second)
	return samePlaneAdjacent(a, b) && distance3D(a, b) <= guildWarMasterReach
}

/*
================
prepareGuildWarCombat

The reference server's PvP kill rewards score the guild war only for kind 5; cape, job and siege kills take their
own rewards even when the same guilds are also fighting a declared war.
================
*/
func (rt *Runtime) prepareGuildWarCombat(division string, victim *enterworld.Character, killer deathKiller) domain.GuildWarCombat {
	if victim == nil || killer.player == nil || victim.ID == killer.player.ID || rt.deathKind(division, victim, killer) != pk.DeathGuildWar {
		return domain.GuildWarCombat{}
	}
	war, ok := rt.GuildWars.Find(division, *killer.player.GuildID, *victim.GuildID)
	if !ok {
		return domain.GuildWarCombat{}
	}
	level := killer.strikerLevel
	if level == 0 {
		level = rewardLevel(killer.player)
	}
	return domain.GuildWarCombat{WarID: war.ID, KillerID: killer.player.ID, VictimID: victim.ID, Score: guildwar.KillScore(rewardLevel(victim), level)}
}

/*
================
prepareCOSGuildWarCombat

prepareGuildWarCombat for a COS victim: the reference server checks the actual victim
and, because it is a COS, scores from the COS's own level against
the killer's, not from its owner's level.
================
*/
func (rt *Runtime) prepareCOSGuildWarCombat(division string, killer, owner *enterworld.Character, cosLevel uint8) domain.GuildWarCombat {
	if owner == nil || killer == nil || owner.ID == killer.ID || rt.deathKind(division, owner, deathKiller{player: killer}) != pk.DeathGuildWar {
		return domain.GuildWarCombat{}
	}
	war, ok := rt.GuildWars.Find(division, *killer.GuildID, *owner.GuildID)
	if !ok {
		return domain.GuildWarCombat{}
	}
	return domain.GuildWarCombat{WarID: war.ID, KillerID: killer.ID, VictimID: owner.ID, Score: guildwar.KillScore(int64(cosLevel), rewardLevel(killer))}
}

/*
================
publishGuildWarCombat
================
*/
func (rt *Runtime) publishGuildWarCombat(division string, combat domain.GuildWarCombat, nowMs int64) {
	if combat.WarID != 0 && rt.GuildWarKill != nil {
		rt.GuildWarKill(division, combat, nowMs)
	}
}
