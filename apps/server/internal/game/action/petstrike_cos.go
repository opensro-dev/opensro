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
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
petStrikeCOS
================
*/
func (rt *Runtime) petStrikeCOS(step petCombatStep, target petCombatTarget, skill enterworld.SkillRow) (petStrikeResult, bool) {
	block := rt.cosAbnormal(step.key.division, step.snapshot.Name, step.pet.GID)
	attacker, err := cosCombatStats(step.ref, step.pet, block)
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
	return petStrikeResult{strike: outcome.strike, fatal: outcome.strike.fatal}, true
}
