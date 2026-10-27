/*
===========================================================================

pkreward.go - what a player's kill of a player pays its killer

Two native owners pay the killer. The victim's death penalty
(the reference server's death penalty) pays a player killer its PvP EXP
(the PvP experience formula) for a murder or a guild-war kill,
doubled when the victim was itself a murderer. Then
the reference server's PvP kill rewards, reached from the killer's
battle entry on attack, keep the books by the kill's kind:

  - ordinary player kill (3): unless the victim is a legal world enemy,
    a living killer's total PK +1, penalty +(total+1)/2*1200, daily PK by
    the level-gap weight (pk.RecordMurder);
    - job (1): job EXP (the job kill formula), shared by the
        killer's party, then EXP of
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
	// jobKillPartyBonus is the reference server's 0.05 per member beyond the first.
	jobKillPartyBonus = 0.05
	// jobKillBasisScale is the reference server's GoldMin * 10 * 0.125.
	jobKillBasisScale = 0.125
	// jobKillExpMultiplier is the reference server's leveldata +0x1c * 3.
	jobKillExpMultiplier = 3
)

/*
================
playerKill

A fatal player hit's kill, classified before the victim's death changes
its equipment (the reference server stores the kind at victim +0x1CB4 first). legalEnemy
is the reference server's target predicate before death relief; murderer
is the death penalty's "victim penalty > 0 and kind != 5", read before the death's
relief lowers the penalty.
================
*/
type playerKill struct {
	kind        pk.DeathKind
	murderer    bool
	legalEnemy  bool
	enemyRules  playerEnemyRules
	victimLevel int64
	// Port-only, not native (mostwanted.go): whether killer and victim are
	// strangers (no shared account, party, guild or union), which both the
	// streak count and the bounty require, and the victim's wanted level
	// before its death cleared it.
	mostWantedStrangers bool
	victimWanted        int64
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
	kill.mostWantedStrangers = rt.mostWantedStrangers(division, killer, victim)
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
	kill.victimWanted = mostWantedLevel(victim)
	return kill
}

/*
================
payPlayerKillInDoor

Inside the fatal hit's door, after the victim's death settled: the PvP
EXP of the death penalty, then the kill rewards' bookkeeping. actor goes to the killer, public
to every observer; shares are the job EXP its party members still earn.
================
*/
func (rt *Runtime) payPlayerKillInDoor(division string, killer, victim *enterworld.Character, kill playerKill, now int64) (actor, public []wire.Frame, after jobKillPayout) {
	if killer == nil || victim == nil {
		return nil, nil, after
	}
	victimGid := enterworld.ObjectIDForCharacter(victim)
	party := rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(killer))
	if kill.kind == pk.DeathPlayer || kill.kind == pk.DeathGuildWar || kill.kind == pk.DeathSpecialWorld {
		// The reference server's death penalties award EXP before the victim can lose a level.
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
		// The reference server passes the victim in ESI to its enemy test. The world controller
		// tests that target (default: the normal combat enemy test), before its death relief.
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
		if notice, ok := jobKillNoticeOf(division, killer, victim, now); ok {
			after.notices = append(after.notices, notice)
		}
		if party == nil {
			if frames, ok := rt.addJobExperience(killer, jobKillExperience(rt.deps.LevelData(), killer, rewardLevel(victim))); ok {
				actor = append(actor, frames...)
			}
		} else {
			fallen := jobKillVictim{level: rewardLevel(victim), world: domain.CharacterWorldInstance(victim),
				at: rt.liveSpawn(simulation.WorldKey(division, victim.Name), victim, now)}
			for _, share := range rt.jobKillShares(division, killer, fallen, party, now) {
				if share.member != killer {
					after.shares = append(after.shares, share)
					continue
				}
				if frames, ok := rt.addJobExperience(killer, share.exp); ok {
					actor = append(actor, frames...)
				}
			}
		}
		// Reference server: a player striker also earns leveldata +0x1c of the lower
		// level, three times over.
		if basis, ok := rt.deps.LevelData().MonsterExpBasis(min(rewardLevel(killer), rewardLevel(victim))); ok && basis > 0 {
			frames := rt.grantKillExperience(killer, basis*jobKillExpMultiplier, victimGid)
			actor = append(actor, frames...)
			public = append(public, wire.ProgressionBroadcastFrames(frames)...)
		}
	}
	// Port-only, not native: the streak's count and the bounty (mostwanted.go).
	wantedActor, wantedPublic, owed, wanted := rt.mostWantedKillInDoor(division, killer, victim, kill, now)
	actor, public = append(actor, wantedActor...), append(public, wantedPublic...)
	after.bounties = append(after.bounties, owed...)
	after.wanted = append(after.wanted, wanted...)
	return actor, public, after
}

/*
================
payCOSKillInDoor

The reference server's PvP kill rewards for a fatal hit on a COS: the
relation is classified against the COS's owner (kill, refreshed by
withVictim inside the door), while the actual victim is the COS. An
ordinary kill books the murder against the owner (RecordMurder on the
owner's level); a job kill pays job EXP on the owner's level, shared by
the killer's party, but not the +0x1c * 3 basis EXP, which the reference server gates
on the actual victim being a player, and not the port-only job-kill or
Most Wanted notices; a guild-war kill scores from the COS's own level
. The death penalty never runs on a COS death, so no PvP EXP is paid. The
killer's penalty relief for the COS itself stays with
creditPlayerCOSKill, after this. war is published by the caller after
its door, as a player kill's is.
================
*/
func (rt *Runtime) payCOSKillInDoor(division string, killer, owner *enterworld.Character, kill playerKill, cosLevel uint8, now int64) (actor, public []wire.Frame, after jobKillPayout, war domain.GuildWarCombat) {
	if killer == nil || owner == nil {
		return nil, nil, after, war
	}
	switch kill.kind {
	case pk.DeathPlayer:
		if !enterworld.CharacterAlive(killer) || kill.legalEnemy {
			break
		}
		before := killer.PVPState()
		changed := pk.RecordMurder(killer, levelByte(killer), levelByte(owner), rt.Now())
		actor = append(actor, pkRecordFrames(killer, changed)...)
		rt.notePKRecord(division, killer)
		if before != killer.PVPState() {
			public = append(public, playerPVPStateFrame(killer))
		}
	case pk.DeathJob:
		party := rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(killer))
		if party == nil {
			if frames, ok := rt.addJobExperience(killer, jobKillExperience(rt.deps.LevelData(), killer, rewardLevel(owner))); ok {
				actor = append(actor, frames...)
			}
			break
		}
		fallen := jobKillVictim{level: rewardLevel(owner), world: domain.CharacterWorldInstance(owner),
			at: rt.liveSpawn(simulation.WorldKey(division, owner.Name), owner, now)}
		for _, share := range rt.jobKillShares(division, killer, fallen, party, now) {
			if share.member != killer {
				after.shares = append(after.shares, share)
				continue
			}
			if frames, ok := rt.addJobExperience(killer, share.exp); ok {
				actor = append(actor, frames...)
			}
		}
	case pk.DeathGuildWar:
		war = rt.prepareCOSGuildWarCombat(division, killer, owner, cosLevel)
	}
	return actor, public, after, war
}

/*
================
jobKillPayout

What a committed kill still owes after its door: the party's job shares,
the port-only job-kill and Most Wanted notices (announce.go) and, with the
transfer on, the port-only bounty taken from the victim (mostwanted.go).
Nothing here runs when the kill's commit rolls back.
================
*/
type jobKillPayout struct {
	shares   []jobKillShare
	notices  []jobKillNotice
	wanted   []mostWantedNotice
	bounties []mostWantedBounty
}

/*
================
jobKillPayout.add

Gathers several kills' payouts (an area strike can kill several players).
================
*/
func (p *jobKillPayout) add(other jobKillPayout) {
	p.shares = append(p.shares, other.shares...)
	p.notices = append(p.notices, other.notices...)
	p.wanted = append(p.wanted, other.wanted...)
	p.bounties = append(p.bounties, other.bounties...)
}

/*
================
payJobKillShares

The job EXP distribution's party members other than the killer, each in its own door, then
the kill's notice and its Most Wanted notices, then each bounty owed from a victim (port-only, its
own door over both characters).
================
*/
func (rt *Runtime) payJobKillShares(after jobKillPayout) []RecipientFrames {
	for _, notice := range after.notices {
		rt.postJobKillNotice(notice)
	}
	for _, notice := range after.wanted {
		rt.postMostWantedNotice(notice)
	}
	var out []RecipientFrames
	for _, share := range after.shares {
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
	for _, owed := range after.bounties {
		out = append(out, rt.payMostWantedBounty(owed)...)
	}
	return out
}

/*
================
jobKillVictim

What the job kill formula and its distribution read of the fallen: its level, world and position.
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

Reference server: the killer's party members alive in the victim's world within
1000 units of it (a hunter killer shares only with hunters) split the job
EXP by level, raised 5% per member beyond the first. Each member's base is
its own job kill formula against the victim.
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

The reference server's PvP experience formula: leveldata +0x1c of the lower
level, times the killer-to-victim level-gap scale (the monster
reward's), the killer's mastery-gap EXP rate (+0x1CD8, written by
mastery stats recompute) and the relative-level bonus with its
party extras. The product spills to float32; zero becomes one.
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
	// Reference server: scale * basis * rate on the x87 stack, then the bonus.
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

The reference server's job kill formula: the victim's basis
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
	// Reference server: an unordered ratio (0 / 0) takes the 0.5 arm.
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

dg.txt column 1 (the drop gold minimum), the job formulas' basis.
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

The reference server's job EXP grant, through the progression owner.
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

The PvP kill rewards' monster branch for the killing blow's player: a thief monster
killed by a hunter or a trader, or a hunter monster killed by a thief,
pays job EXP (the job kill formula on the monster's level), shared by the killer's
party. Inside the reward roster's door, which holds every member.
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
