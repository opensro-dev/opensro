/*
===========================================================================

petstrike_cos.go - companion attacks use the shared companion victim owner

A detached creature projection supplies only caster identity and authored
level to status rolls. HP, death, and status mutation stay with the existing
COS owner; this does not register a second monster actor.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
petStrikeCOS
================
*/
func (rt *Runtime) petStrikeCOS(step petCombatStep, target petCombatTarget, skill enterworld.SkillRow) (petStrikeResult, bool) {
	block := rt.cosAbnormal(step.key.division, step.snapshot.Name, step.pet.GID)
	attacker, err := cosCombatStats(step.ref, step.pet, block, rt.cosDebuffWords(step.key.division, step.snapshot.Name, step.pet.GID, step.nowMs))
	if err != nil {
		return petStrikeResult{}, false
	}
	var live *enterworld.CharacterCOS
	rt.deps.Read(step.key.division, func() { live = target.player.CompanionByGID(target.gid) })
	if live == nil || live.RefObjID != target.cos.RefObjID || live.SummonGeneration != target.cos.SummonGeneration {
		return petStrikeResult{}, false
	}
	ref := step.ref.Parameters
	ref.Level = step.pet.Level
	if ref.Level == 0 {
		ref.Level = step.ref.Level
	}
	from := step.state.follower.Position(step.nowMs)
	in := monsterStrikeInput{division: step.key.division, instance: monster.Instance{Gid: step.pet.GID, Ref: ref},
		skill: skill, attacker: attacker, from: monster.Pose{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z}, percent: step.percent, now: step.nowMs}
	outcome := rt.monsterStrikeCOS(in, target.player, live, target.cosRef, target.at)
	if !outcome.committed {
		return petStrikeResult{}, false
	}
	if len(outcome.strike.private) > 0 {
		step.state.others = append(step.state.others, RecipientFrames{CharacterID: target.player.ID, Frames: outcome.strike.private})
	}
	if !outcome.strike.fatal {
		return petStrikeResult{strike: outcome.strike, fatal: outcome.strike.fatal}, true
	}
	// Natively, for a pet's fatal hit, the pet's owner is the
	// killer, classified against the COS's owner, booked before its own
	// penalty relief for the COS. The hit already settled inside
	// the victim owner's door; the booking takes a door holding both.
	killer, victimOwner := step.state.character, target.player
	cosLevel := live.Level
	if cosLevel == 0 {
		cosLevel = target.cosRef.Level
	}
	kill := rt.classifyPlayerKill(step.key.division, killer, victimOwner)
	var rewardActor, rewardPublic []wire.Frame
	var after jobKillPayout
	var war domain.GuildWarCombat
	rt.deps.UpdateManyDeferred([]*enterworld.Character{killer, victimOwner}, "pet-cos-kill", func() bool {
		kill = kill.withVictim(killer, victimOwner)
		rewardActor, rewardPublic, after, war = rt.payCOSKillInDoor(step.key.division, killer, victimOwner, kill, cosLevel, step.nowMs)
		creditActor, creditPublic := rt.creditPlayerCOSKill(step.key.division, killer, cosLevel)
		rewardActor = append(rewardActor, creditActor...)
		rewardPublic = append(rewardPublic, creditPublic...)
		return true
	})
	rt.publishGuildWarCombat(step.key.division, war, step.nowMs)
	strike := outcome.strike
	strike.public = append(strike.public, rewardPublic...)
	step.state.others = append(step.state.others, rt.payJobKillShares(after)...)
	return petStrikeResult{strike: strike, actor: rewardActor, fatal: true}, true
}
