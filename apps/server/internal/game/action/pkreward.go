/*
===========================================================================

pkreward.go - what a player's kill of a player pays its killer

Two native owners pay the killer. The victim's death penalty
(CGObjPC_ApplyDeathPenalty 4E6980) pays a player killer its PvP EXP
(Formulae_CalculatePvPExperience 4105D0) for a murder or a guild-war kill,
doubled when the victim was itself a murderer. Then
CGObjPC_ProcessPvpKillRewards (4E1F60), reached from the killer's
CGObjPC_EnterBattleOnAttack, keeps the books by the kill's kind:

  - ordinary player kill (3): unless the victim is a legal world enemy,
    a living killer's total PK +1, penalty +(total+1)/2*1200, daily PK by
    the level-gap weight (pk.RecordMurder);
  - job (1): job EXP (Formulae_CalculateJobKillExp 4103E0), shared by the
    killer's party (CGObjPC_DistributeJobKillExp 5BD7F0), then EXP of
    leveldata +0x1c of the lower level, three times over;
  - guild war (5): the guild-war authority receives the fatal combat
    facts after the character door, then commits guild and member scores.

Everything but the party's job shares commits inside the fatal hit's
door; a share commits in its member's own door after it.

===========================================================================
*/

package action

import (
	"math"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// jobKillPartyBonus is 5BD8E9's 0.05 per member beyond the first.
	jobKillPartyBonus = 0.05
	// jobKillBasisScale is 410404's GoldMin * 10 * 0.125.
	jobKillBasisScale = 0.125
	// jobKillExpMultiplier is 4E20F3's leveldata +0x1c * 3.
	jobKillExpMultiplier = 3
)

/*
================
playerKill

A fatal player hit's kill, classified before the victim's death changes
its equipment (4E6EC0 stores the kind at victim +0x1CB4 first). legalEnemy
is 4EB590's target predicate before death relief; murderer
is 4E6980's "victim penalty > 0 and kind != 5", read before the death's
relief lowers the penalty.
================
*/
type playerKill struct {
	kind        pk.DeathKind
	murderer    bool
	legalEnemy  bool
	enemyRules  playerEnemyRules
	victimLevel int64
}

/*
================
jobKillShare

One party member's share of a job kill, paid after the hit's door.
================
*/
type jobKillShare struct {
	member *enterworld.Character
	exp    int64
}

/*
================
classifyPlayerKill

Capture faction/world facts before the character door; those authorities
can persist while holding their own mutex. withVictim refreshes local facts.
================
*/
func (rt *Runtime) classifyPlayerKill(division string, killer, victim *enterworld.Character) playerKill {
	kind := rt.deathKind(division, victim, rt.prepareDeathKiller(division, victim, deathKiller{player: killer}))
	kill := playerKill{kind: kind, enemyRules: rt.worldPlayerEnemyRules(division, killer, victim)}
	return kill.withVictim(killer, victim)
}

/*
================
withVictim

Refresh local criminal facts without acquiring external authority locks.
================
*/
func (kill playerKill) withVictim(killer, victim *enterworld.Character) playerKill {
	kill.murderer = murderer(victim) && kill.kind != pk.DeathGuildWar
	kill.legalEnemy = kill.enemyRules.enemy(killer, victim)
	kill.victimLevel = rewardLevel(victim)
	return kill
}

/*
================
payPlayerKillInDoor

Inside the fatal hit's door, after the victim's death settled: the PvP
EXP of 4E6980, then 4E1F60's bookkeeping. actor goes to the killer, public
to every observer; shares are the job EXP its party members still earn.
================
*/
func (rt *Runtime) payPlayerKillInDoor(division string, killer, victim *enterworld.Character, kill playerKill, now int64) (actor, public []wire.Frame, shares []jobKillShare) {
	if killer == nil || victim == nil {
		return nil, nil, nil
	}
	victimGid := enterworld.ObjectIDForCharacter(victim)
	party := rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(killer))
	if kill.kind == pk.DeathPlayer || kill.kind == pk.DeathGuildWar || kill.kind == pk.DeathSpecialWorld {
		// 4E6980/4E6D60 award EXP before the victim can lose a level.
		victimBefore := *victim
		victimBefore.Level = &kill.victimLevel
		exp := pvpExperience(rt.deps.LevelData(), killer, &victimBefore, party)
		if kill.murderer && kill.kind != pk.DeathSpecialWorld {
			exp *= 2
		}
		frames := rt.grantKillExperience(killer, exp, victimGid)
		actor = append(actor, frames...)
		public = append(public, wire.ProgressionBroadcastFrames(frames)...)
		petFrames, petArea := rt.awardAttackPetPvPExperience(killer, victim)
		actor = append(actor, petFrames...)
		public = append(public, petArea...)
	}
	switch kill.kind {
	case pk.DeathPlayer:
		// 4E2004 passes the victim in ESI to 4EB590. The world controller
		// tests that target (default: 52B6D0), before its death relief.
		if !enterworld.CharacterAlive(killer) || kill.legalEnemy {
			break
		}
		before := killer.PVPState()
		changed := pk.RecordMurder(killer, levelByte(killer), levelByte(victim), rt.Now())
		actor = append(actor, pkRecordFrames(killer, changed)...)
		rt.notePKRecord(division, killer)
		if before != killer.PVPState() {
			public = append(public, playerPVPStateFrame(killer))
		}
	case pk.DeathJob:
		if party == nil {
			if frames, ok := rt.addJobExperience(killer, jobKillExperience(rt.deps.LevelData(), killer, rewardLevel(victim))); ok {
				actor = append(actor, frames...)
			}
		} else {
			fallen := jobKillVictim{level: rewardLevel(victim), world: domain.CharacterWorldInstance(victim),
				at: rt.liveSpawn(simulation.WorldKey(division, victim.Name), victim, now)}
			for _, share := range rt.jobKillShares(division, killer, fallen, party, now) {
				if share.member != killer {
					shares = append(shares, share)
					continue
				}
				if frames, ok := rt.addJobExperience(killer, share.exp); ok {
					actor = append(actor, frames...)
				}
			}
		}
		// 4E20B9: a player striker also earns leveldata +0x1c of the lower
		// level, three times over.
		if basis, ok := rt.deps.LevelData().MonsterExpBasis(min(rewardLevel(killer), rewardLevel(victim))); ok && basis > 0 {
			frames := rt.grantKillExperience(killer, basis*jobKillExpMultiplier, victimGid)
			actor = append(actor, frames...)
			public = append(public, wire.ProgressionBroadcastFrames(frames)...)
		}
	}
	return actor, public, shares
}

/*
================
payJobKillShares

5BD7F0's party members other than the killer, each in its own door.
================
*/
func (rt *Runtime) payJobKillShares(shares []jobKillShare) []RecipientFrames {
	var out []RecipientFrames
	for _, share := range shares {
		var frames []wire.Frame
		rt.deps.Update(share.member, "job-kill-share", func() bool {
			var ok bool
			frames, ok = rt.addJobExperience(share.member, share.exp)
			return ok
		})
		if len(frames) > 0 {
			out = append(out, RecipientFrames{CharacterID: share.member.ID, Frames: frames})
		}
	}
	return out
}

/*
================
jobKillVictim

What 4103E0 and 5BD7F0 read of the fallen: its level, world and position.
================
*/
type jobKillVictim struct {
	level int64
	world uint32
	at    simulation.Spawn
}

/*
================
jobKillShares

5BD7F0: the killer's party members alive in the victim's world within
1000 units of it (a hunter killer shares only with hunters) split the job
EXP by level, raised 5% per member beyond the first. Each member's base is
its own 4103E0 against the victim.
================
*/
func (rt *Runtime) jobKillShares(division string, killer *enterworld.Character, victim jobKillVictim, party *RewardParty, now int64) []jobKillShare {
	world, origin := victim.world, victim.at
	hunters := enterworld.DressedJob(killer) == domain.JobHunter
	var members []*enterworld.Character
	var levels int64
	for _, gid := range party.Members {
		member := killer
		if gid != enterworld.ObjectIDForCharacter(killer) {
			member = rt.findCharacterByGid(division, gid)
		}
		if member == nil || member.DeletePending || !enterworld.CharacterAlive(member) ||
			domain.CharacterWorldInstance(member) != world {
			continue
		}
		if !withinPartyRewardRange(origin, rt.liveSpawn(simulation.WorldKey(division, member.Name), member, now)) {
			continue
		}
		if hunters && enterworld.DressedJob(member) != domain.JobHunter {
			continue
		}
		members = append(members, member)
		levels += rewardLevel(member)
	}
	if len(members) == 0 || levels == 0 {
		return nil
	}
	bonus := float32(float64(float32(len(members)-1))*float64(float32(jobKillPartyBonus)) + 1)
	shares := make([]jobKillShare, 0, len(members))
	for _, member := range members {
		portion := float32(float64(float32(rewardLevel(member))) / float64(float32(levels)))
		base := jobKillExperience(rt.deps.LevelData(), member, victim.level)
		exp := int64(float64(float32(base)) * float64(portion) * float64(bonus))
		shares = append(shares, jobKillShare{member: member, exp: exp})
	}
	return shares
}

/*
================
pvpExperience

Formulae_CalculatePvPExperience (4105D0): leveldata +0x1c of the lower
level, times the killer-to-victim level-gap scale (40FED0, the monster
reward's), the killer's mastery-gap EXP rate (+0x1CD8, written by
CGObjChar_RecomputeMasteryStats) and the relative-level bonus with its
party extras (40FFE0). The product spills to float32; zero becomes one.
================
*/
func pvpExperience(levels enterworld.LevelDataSource, killer, victim *enterworld.Character, party *RewardParty) int64 {
	if levels == nil {
		return 0
	}
	killerLevel, victimLevel := rewardLevel(killer), rewardLevel(victim)
	basis, ok := levels.MonsterExpBasis(min(killerLevel, victimLevel))
	if !ok {
		return 0
	}
	maxMastery := int64(0)
	for _, mastery := range killer.Masteries {
		maxMastery = max(maxMastery, mastery.Level)
	}
	expRate, _ := masteryGapProgressionRates(killerLevel, maxMastery)
	relative := monsterRelativeLevelBonus(killerLevel, victimLevel)
	if party != nil && party.Options&1 != 0 {
		relative = monsterPartyRelativeLevelBonus(killerLevel, victimLevel, enterworld.NativeCountryByte9C(killer) == 1)
	}
	// 410673..41067F: scale * basis * rate on the x87 stack, then the bonus.
	exp := float32(float64(monsterLevelGapRewardScale(killerLevel, victimLevel)) *
		float64(float32(uint32(basis))) * float64(expRate) * float64(relative))
	if exp == 0 {
		return 1
	}
	return int64(exp)
}

/*
================
jobKillExperience

Formulae_CalculateJobKillExp (4103E0): the victim's basis
trunc(GoldMin * 10 * 0.125) times its ratio to the killer's, held to
0.5..1.5; a trader killer earns half; nothing positive becomes one.
================
*/
func jobKillExperience(levels enterworld.LevelDataSource, killer *enterworld.Character, victimLevel int64) int64 {
	gold, ok := levels.(goldBasisSource)
	if !ok {
		return 0
	}
	victimGold, okVictim := gold.WithdrawalGoldBasis(victimLevel)
	killerGold, okKiller := gold.WithdrawalGoldBasis(rewardLevel(killer))
	if !okVictim || !okKiller {
		return 0
	}
	victimBasis := math.Trunc(float64(victimGold*10) * jobKillBasisScale)
	killerBasis := math.Trunc(float64(killerGold*10) * jobKillBasisScale)
	ratio := float32(float64(float32(victimBasis)) / killerBasis)
	// 410479..4104A6: an unordered ratio (0 / 0) takes the 0.5 arm.
	if !(ratio >= 0.5) {
		ratio = 0.5
	} else if ratio > 1.5 {
		ratio = 1.5
	}
	exp := float32(float64(float32(victimBasis)) * float64(ratio))
	if enterworld.DressedJob(killer) == domain.JobTrader {
		exp = float32(float64(exp) * 0.5)
	}
	if !(exp > 0) {
		exp = 1
	}
	return int64(exp)
}

/*
================
goldBasisSource

dg.txt column 1 (CRefData_GetDropGoldMinimum), the job formulas' basis.
================
*/
type goldBasisSource interface {
	WithdrawalGoldBasis(level int64) (int64, bool)
}

/*
================
rewardPartyOf

The party gid belongs to, or nil.
================
*/
func (rt *Runtime) rewardPartyOf(division string, gid uint32) *RewardParty {
	if rt.RewardParties == nil {
		return nil
	}
	parties := rt.RewardParties(division)
	for i := range parties {
		for _, member := range parties[i].Members {
			if member == gid {
				return &parties[i]
			}
		}
	}
	return nil
}

/*
================
grantKillExperience

The killer's EXP gain (vtable +0x694) through the progression owner.
================
*/
func (rt *Runtime) grantKillExperience(c *enterworld.Character, exp int64, source uint32) []wire.Frame {
	if rt.UpdateExperience == nil || exp <= 0 {
		return nil
	}
	frames, _ := rt.UpdateExperience(c, exp, 0, source)
	return frames
}

/*
================
addJobExperience

CGObjPC_AddJobExp (4E2830) through the progression owner.
================
*/
func (rt *Runtime) addJobExperience(c *enterworld.Character, exp int64) ([]wire.Frame, bool) {
	if rt.UpdateJobExperience == nil || exp == 0 {
		return nil, false
	}
	return rt.UpdateJobExperience(c, exp)
}

/*
================
levelByte
================
*/
func levelByte(c *enterworld.Character) uint8 {
	return uint8(min(rewardLevel(c), 0xff))
}

/*
================
payMonsterJobKillInDoor

4E1F60's monster branch for the killing blow's player: a thief monster
killed by a hunter or a trader, or a hunter monster killed by a thief,
pays job EXP (4103E0 on the monster's level), shared by the killer's
party (5BD7F0). Inside the reward roster's door, which holds every member.
================
*/
func (rt *Runtime) payMonsterJobKillInDoor(division string, killer *enterworld.Character, victim monster.Instance, pose monster.Pose, now int64) (actor []wire.Frame, others []RecipientFrames) {
	job := enterworld.DressedJob(killer)
	thiefKill := victim.ThiefMonster() && (job == domain.JobHunter || job == domain.JobTrader)
	hunterKill := victim.HunterMonster() && job == domain.JobThief
	if !thiefKill && !hunterKill {
		return nil, nil
	}
	level := int64(victim.Ref.Level)
	party := rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(killer))
	if party == nil {
		frames, _ := rt.addJobExperience(killer, jobKillExperience(rt.deps.LevelData(), killer, level))
		return frames, nil
	}
	fallen := jobKillVictim{level: level, world: domain.CharacterWorldInstance(killer),
		at: simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}}
	for _, share := range rt.jobKillShares(division, killer, fallen, party, now) {
		frames, ok := rt.addJobExperience(share.member, share.exp)
		if !ok {
			continue
		}
		if share.member == killer {
			actor = append(actor, frames...)
		} else {
			others = append(others, RecipientFrames{CharacterID: share.member.ID, Frames: frames})
		}
	}
	return actor, others
}
