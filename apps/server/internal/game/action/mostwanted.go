/*
===========================================================================

mostwanted.go - a suited player's job-kill streak, its bonuses and bounty

Port-only, not native (owner decision 2026-10-14, for the balance advisor).
With SRO_MOST_WANTED on:

Streak. Each counted job kill (pk.DeathJob) adds one to the killer's
streak; its wanted level is streak / 10, at most 10. A kill counts unless
the victim shares the killer's account, party, guild or guild union, and a
killer counts the same victim at most 3 times in any 60 minutes (kept in
memory: a restart forgets the window, never the streak). There is no level
window and no distinct-victims rule (the advisor's answers). The streak is
saved on the character (domain.MostWanted, store schema 24).

Reset. Death of any kind, or taking the job suit off, clears the streak.

Bonuses, per wanted level: +1 STR, +1 INT and +1% movement speed on foot;
damage dealt +1% and taken -1% from level 3, 2% from 5, 3% at 10, while
the suit is worn. They are
parameter writes in the same projection as every effect (statModifierWrites),
so stats, damage and the client's stat window agree.

Bounty. Killing a wanted player in PvP pays the killer 100,000,000 gold per
wanted level (1,000,000,000 at 10), under the same stranger test as a count. The gold is new: the owner accepted the
supply. Every count and every bounty writes an audit log line.

Off, the default here, nothing counts, no bonus applies and no bounty is
paid; a saved streak waits untouched. The GameWorld's Nomad job turns it on.

===========================================================================
*/
package action

import (
	"os"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/pk"
)

// EnvMostWanted turns the rule on ("on", "1", "true").
const EnvMostWanted = "SRO_MOST_WANTED"

const (
	// mostWantedKillsPerLevel counted kills make one wanted level.
	mostWantedKillsPerLevel = 10
	// mostWantedMaxLevel caps the wanted level.
	mostWantedMaxLevel = 10
	// mostWantedPairLimit and mostWantedPairWindowMs: one killer counts one
	// victim at most this often in the window.
	mostWantedPairLimit    = 3
	mostWantedPairWindowMs = 60 * 60_000
	// mostWantedPairPrune is the pair count past which stale pairs are dropped.
	mostWantedPairPrune = 4096
	// mostWantedBountyPerLevel is the gold a wanted level is worth.
	mostWantedBountyPerLevel = 100_000_000
	// mostWantedModifierSource keys the streak's writes apart from every
	// installed effect (whose sources count up from 0x80000000) and the
	// engaged skill's (0x7fffff00).
	mostWantedModifierSource uint32 = 0x7fffff01
	// The STR (1) and INT (2) keepers, and the damage-dealt rates
	// 0x80..0x83 (physical/magical, basic/skill) that dru also writes.
	mostWantedStrengthParameter  uint16 = 1
	mostWantedIntellectParameter uint16 = 2
	mostWantedDamageParameter    uint16 = 0x80
	// mostWantedReductionBits selects every incoming-damage keeper
	// (physical 4 and magical 8, basic 1 and skill 2) for odar's writes.
	mostWantedReductionBits = 4 | 8 | 1 | 2
)

/*
================
MostWantedFromEnv
================
*/
func MostWantedFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvMostWanted))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
mostWantedLevel

A streak's wanted level: streak / 10, at most 10.
================
*/
func mostWantedLevel(c *enterworld.Character) int64 {
	if c == nil || c.MostWanted == nil || c.MostWanted.Streak <= 0 {
		return 0
	}
	return min(c.MostWanted.Streak/mostWantedKillsPerLevel, mostWantedMaxLevel)
}

/*
================
mostWantedDamagePercent

The damage dealt and reduced at a wanted level: 1% from 3, 2% from 5, 3%
at 10.
================
*/
func mostWantedDamagePercent(level int64) float32 {
	switch {
	case level >= mostWantedMaxLevel:
		return 3
	case level >= 5:
		return 2
	case level >= 3:
		return 1
	}
	return 0
}

/*
================
mostWantedWrites

The streak's parameter writes, nil without the rule or a wanted level.
================
*/
func (rt *Runtime) mostWantedWrites(c *enterworld.Character) []paramkeeper.Write {
	level := mostWantedLevel(c)
	// Only a suited player is wanted: a streak saved while the rule was off
	// (when undressing does not clear it) never buffs an unsuited one.
	if !rt.MostWanted || level == 0 || enterworld.DressedJob(c) == 0 {
		return nil
	}
	writes := []paramkeeper.Write{
		{Parameter: mostWantedStrengthParameter, Channel: paramkeeper.Flat, Source: mostWantedModifierSource, Value: float32(level)},
		{Parameter: mostWantedIntellectParameter, Channel: paramkeeper.Flat, Source: mostWantedModifierSource, Value: float32(level)},
	}
	if percent := mostWantedDamagePercent(level); percent > 0 {
		for i := uint16(0); i < 4; i++ {
			writes = append(writes, paramkeeper.Write{Parameter: mostWantedDamageParameter + i, Channel: paramkeeper.Flat,
				Source: mostWantedModifierSource, Value: percent})
		}
		writes = append(writes, combat.IncomingReductionWrites(mostWantedReductionBits, uint32(percent), mostWantedModifierSource)...)
	}
	return writes
}

/*
================
statModifierWrites

Every parameter write a character's stat projection reads: its installed
effects, then the streak's (port-only).
================
*/
func (rt *Runtime) statModifierWrites(division string, c *enterworld.Character) []paramkeeper.Write {
	return append(rt.effects.ModifierWrites(division, c.Name), rt.mostWantedWrites(c)...)
}

/*
================
mostWantedSpeedFactor

The streak's movement factor on foot: +1% per wanted level.
================
*/
func (rt *Runtime) mostWantedSpeedFactor(c *enterworld.Character) float32 {
	if !rt.MostWanted || enterworld.DressedJob(c) == 0 {
		return 1
	}
	return 1 + float32(mostWantedLevel(c))/100
}

/*
================
mostWantedStrangers

Whether a kill may count toward the killer's streak or pay a bounty: the
victim shares neither the killer's account, party, guild nor guild union,
so an alt or a guildmate can never cash a streak. Called before any
character door (the union authority takes its own lock).
================
*/
func (rt *Runtime) mostWantedStrangers(division string, killer, victim *enterworld.Character) bool {
	if !rt.MostWanted || killer == nil || victim == nil {
		return false
	}
	if killer.AccountID != "" && killer.AccountID == victim.AccountID {
		return false
	}
	if party := rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(killer)); party != nil {
		victimGid := enterworld.ObjectIDForCharacter(victim)
		for _, member := range party.Members {
			if member == victimGid {
				return false
			}
		}
	}
	if killer.GuildID != nil && victim.GuildID != nil {
		if *killer.GuildID == *victim.GuildID || rt.Unions != nil && rt.Unions.Allied(division, *killer.GuildID, *victim.GuildID) {
			return false
		}
	}
	return true
}

/*
================
mostWantedPairs

How often each killer counted each victim inside the window.
================
*/
type mostWantedPairs struct {
	mu     sync.Mutex
	counts map[string][]int64
}

/*
================
Runtime.countMostWantedPair

Whether killer may count victim now, recording it when so.
================
*/
func (rt *Runtime) countMostWantedPair(division, killer, victim string, nowMs int64) bool {
	p := &rt.mostWantedPairs
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.counts == nil {
		p.counts = map[string][]int64{}
	}
	key := division + "\x00" + strings.ToLower(killer) + "\x00" + strings.ToLower(victim)
	recent := p.counts[key][:0]
	for _, at := range p.counts[key] {
		if nowMs-at < mostWantedPairWindowMs {
			recent = append(recent, at)
		}
	}
	if len(recent) >= mostWantedPairLimit {
		p.counts[key] = recent
		return false
	}
	if len(p.counts) >= mostWantedPairPrune {
		for pair, times := range p.counts {
			if len(times) == 0 || nowMs-times[len(times)-1] >= mostWantedPairWindowMs {
				delete(p.counts, pair)
			}
		}
	}
	p.counts[key] = append(recent, nowMs)
	return true
}

/*
================
Runtime.mostWantedKillInDoor

A job kill's streak count and every PvP kill's bounty, in the killer's
door after the victim's death settled. victimLevel is the wanted level the
victim held before its death cleared it. actor goes to the killer.
================
*/
func (rt *Runtime) mostWantedKillInDoor(division string, killer, victim *enterworld.Character, kill playerKill, now int64) (actor, public []wire.Frame) {
	if !rt.MostWanted || killer == nil || victim == nil {
		return nil, nil
	}
	if kill.victimWanted > 0 && kill.mostWantedStrangers {
		bounty := uint64(kill.victimWanted) * mostWantedBountyPerLevel
		setGold(killer, goldOf(killer)+bounty)
		actor = append(actor, goldFrame(killer))
		log.WithFields(log.Fields{"division": division, "killer": killer.Name, "victim": victim.Name,
			"wanted": kill.victimWanted, "gold": bounty}).Info("most wanted: bounty paid")
	}
	if kill.kind != pk.DeathJob || !kill.mostWantedStrangers || !enterworld.CharacterAlive(killer) ||
		!rt.countMostWantedPair(division, killer.Name, victim.Name, now) {
		return actor, nil
	}
	before := mostWantedLevel(killer)
	if killer.MostWanted == nil {
		killer.MostWanted = &domain.MostWanted{}
	}
	killer.MostWanted.Streak++
	log.WithFields(log.Fields{"division": division, "killer": killer.Name, "victim": victim.Name,
		"streak": killer.MostWanted.Streak}).Info("most wanted: kill counted")
	if mostWantedLevel(killer) != before {
		stats, speed := rt.mostWantedRefresh(division, killer, now)
		actor, public = append(actor, stats...), speed
	}
	return actor, public
}

/*
================
Runtime.clearMostWantedInDoor

Death or taking the suit off clears the streak; the caller holds c's door.
private goes to c, public to its observers.
================
*/
func (rt *Runtime) clearMostWantedInDoor(division string, c *enterworld.Character, reason string, now int64) (private, public []wire.Frame) {
	if !rt.MostWanted || c == nil || c.MostWanted == nil {
		return nil, nil
	}
	level := mostWantedLevel(c)
	log.WithFields(log.Fields{"division": division, "character": c.Name, "streak": c.MostWanted.Streak, "reason": reason}).
		Info("most wanted: streak cleared")
	c.MostWanted = nil
	if level == 0 {
		return nil, nil
	}
	return rt.mostWantedRefresh(division, c, now)
}

/*
================
Runtime.mostWantedRefresh

The stat block and movement speeds after a wanted level changed.
================
*/
func (rt *Runtime) mostWantedRefresh(division string, c *enterworld.Character, now int64) (private, public []wire.Frame) {
	if stats, err := rt.PlayerBaseStats(division, c); err == nil {
		private = append(private, wire.Frame{Opcode: wire.OpBaseStats, Payload: stats.Encode()})
	} else {
		log.WithError(err).WithField("character", c.Name).Error("most wanted: stat projection failed")
	}
	return private, rt.refreshMovementEffects(division, c, now)
}
