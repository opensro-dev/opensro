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
	"opensro.online/server/internal/game/world/instance"
)

const guildWarMasterReach = 300

/*
================
GuildWarMasterInFortress

648220 finds the current fortress world. The later slot-1E refusal is
BattleArena-only (5ECFD0); that class is outside the v1.150 feature set.
================
*/
func (rt *Runtime) GuildWarMasterInFortress(_ string, c *enterworld.Character) bool {
	if c == nil || rt.Fortresses == nil {
		return false
	}
	world, ok := instance.Lookup(instance.ID(domain.CharacterWorldInstance(c)).Definition())
	if !ok {
		return false
	}
	_, ok = rt.Fortresses.ForWorld(world)
	return ok
}

/*
================
GuildWarMastersNear

5C6B60 compares the relative three-dimensional distance against 300.
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

4E1F60 calls 5C6FB0 only for kind 5; cape, job and siege kills take their
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
publishGuildWarCombat
================
*/
func (rt *Runtime) publishGuildWarCombat(division string, combat domain.GuildWarCombat, nowMs int64) {
	if combat.WarID != 0 && rt.GuildWarKill != nil {
		rt.GuildWarKill(division, combat, nowMs)
	}
}
