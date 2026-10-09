/*
===========================================================================

pkrelation.go - what kind of kill a player's kill of a player is

The player branch of CGObjPC_ResolvePvpKillRelation (4E6590), in its
order: free-battle capes in hostile groups (CGObjPC_IsHostileTeamOrParty
4EB320, called with force 1) are a team kill (2, no penalty); a killer in
a job suit whose job opposes the victim's (CGObjPC_IsHostileJobType
4EB620) is a job kill (1); a killer whose guild alliance is at war with
the victim's guild (CGObjPC_IsHostileGuildAlliance 4EB390) is a guild-war
kill (5); anything else is murder (3). A COS killer stands for its owner.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
playerKillKind
================
*/
func (rt *Runtime) playerKillKind(division string, victim, killer *enterworld.Character) pk.DeathKind {
	_, killerCape := rt.playerRelationEquipment(killer)
	_, victimCape := rt.playerRelationEquipment(victim)
	if killerCape != 0 && victimCape != 0 &&
		(killerCape != victimCape || killerCape == freeBattleAllOpponents || victimCape == freeBattleAllOpponents) {
		return pk.DeathTeam
	}
	if killerJob := enterworld.DressedJob(killer); killerJob != 0 && hostileJobs(killerJob, enterworld.DressedJob(victim)) {
		return pk.DeathJob
	}
	if rt.guildsAtWar(division, killer, victim) {
		return pk.DeathGuildWar
	}
	return pk.DeathPlayer
}

/*
================
hostileJobs

4EB620 (killer, victim): a hunter victim of a thief, a thief victim of a
trader or hunter, a trader victim of a thief.
================
*/
func hostileJobs(killer, victim uint8) bool {
	switch victim {
	case domain.JobHunter, domain.JobTrader:
		return killer == domain.JobThief
	case domain.JobThief:
		return killer == domain.JobHunter || killer == domain.JobTrader
	}
	return false
}

/*
================
guildsAtWar

4EB390 asks the killer guild's enemy map. The shared guild-war authority
publishes immutable relation snapshots safe to read inside character doors.
================
*/
func (rt *Runtime) guildsAtWar(division string, killer, victim *enterworld.Character) bool {
	if killer == nil || victim == nil || killer.GuildID == nil || victim.GuildID == nil {
		return false
	}
	_, hostile := rt.GuildWars.Find(division, *killer.GuildID, *victim.GuildID)
	return hostile
}

// A murderer's refusals, v1.188 error low bytes the v1.150 client names
// (category*256 + code in its notice table): a riding horse's summon
// (49BF24, 1:0x76) and a building gate (4F30E1, 13:0x16). The return scroll's
// (1:0x75) is returnCastAdmission's. The stall's (10:0x39) waits for the
// stall owner.
const (
	errCodeMurdererTransport uint8 = 0x76
	errCodeMurdererGate      byte  = 0x16
)

/*
================
murderer

A player with PK penalty points: PvP state 2 (CGObjPC_AddPKPenaltyPoints
4EAE70 sets it; every native check reads actor core +0x0C == 2).
================
*/
func murderer(c *enterworld.Character) bool {
	return c != nil && c.PK != nil && c.PK.Penalty > 0
}

/*
================
playerEnemyRules

External relation facts captured before the character-store write. Fortress
and union persistence take their own locks before the character store.
================
*/
type playerEnemyRules struct {
	minimumLevel    int64
	hostileRelation bool
}

/*
================
enemy

Only character-local facts are refreshed inside the fatal transaction.
================
*/
func (rules playerEnemyRules) enemy(owner, target *enterworld.Character) bool {
	if owner == nil || target == nil || owner.ID == target.ID {
		return false
	}
	if rules.minimumLevel != 0 && (owner.Level == nil || target.Level == nil ||
		*owner.Level < rules.minimumLevel || *target.Level < rules.minimumLevel) {
		return false
	}
	return len(target.Aggressions) != 0 || target.PVPState() == 2 || rules.hostileRelation
}

/*
================
normalPlayerEnemyRules

52B6D0 precedes the job/guild checks with both level floors. Cape colors
are absent here; deliberate player attack permission is a different query.
================
*/
func (rt *Runtime) normalPlayerEnemyRules(division string, owner, target *enterworld.Character) playerEnemyRules {
	rules := playerEnemyRules{minimumLevel: playerCombatMinimumLevel}
	if owner != nil && target != nil {
		rules.hostileRelation = hostileJobs(enterworld.DressedJob(owner), enterworld.DressedJob(target)) || rt.guildsAtWar(division, owner, target)
	}
	return rules
}

/*
================
worldPlayerEnemy

52DA50 uses guild/union identity in fortress worlds, without the normal-world
level floor. The same guild and union authorities own these identities.
================
*/
func (rt *Runtime) worldPlayerEnemy(division string, owner, target *enterworld.Character) bool {
	return rt.worldPlayerEnemyRules(division, owner, target).enemy(owner, target)
}

/*
================
worldPlayerEnemyRules

Read the world controller's faction rules before entering a character door.
================
*/
func (rt *Runtime) worldPlayerEnemyRules(division string, owner, target *enterworld.Character) playerEnemyRules {
	if owner == nil || target == nil || owner.ID == target.ID {
		return playerEnemyRules{}
	}
	world, found := instance.Lookup(instance.ID(domain.CharacterWorldInstance(owner)).Definition())
	if !found || !world.Siege() {
		return rt.normalPlayerEnemyRules(division, owner, target)
	}
	var a, b int64
	if owner.GuildID != nil {
		a = *owner.GuildID
	}
	if target.GuildID != nil {
		b = *target.GuildID
	}
	if a == b || rt.Unions.Allied(division, a, b) {
		return playerEnemyRules{}
	}
	return playerEnemyRules{hostileRelation: rt.Fortresses != nil && rt.Fortresses.WarActive(division)}
}
