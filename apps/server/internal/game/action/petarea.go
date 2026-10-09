/*
===========================================================================

petarea.go - one companion action publishes every admitted victim together

Companions use the shared creature geometry and cumulative area reduction.
Victim mutation remains with the monster, player or companion HP owner.

===========================================================================
*/
package action

import (
	"fmt"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
petAreaTargets

The candidate mask is the authored hostile/object mask (24). Guild soldiers
use the owner's world-context predicate; ordered attack pets use the owner's
ordinary attack permission. Each target retains its own body radius.
================
*/
func (rt *Runtime) petAreaTargets(step petCombatStep, primary petCombatTarget, skill enterworld.SkillRow) []petCombatTarget {
	if skill.ActionArea.Shape == 0 || skill.ActionArea.MaxTargets <= 1 {
		return nil
	}
	from := step.state.follower.Position(step.nowMs)
	center, reach := primary.at, float64(skill.ActionArea.Radius)
	nearest := skill.ActionArea.Shape == 6
	if skill.ActionArea.Shape == 1 {
		center = from
	}
	if skill.ActionArea.Shape == 3 || skill.ActionArea.Shape == 4 {
		reach = directionalSearchRadius
		if skill.ActionArea.Shape == 3 {
			center = from
		}
	} else if !nearest {
		reach += step.ref.Parameters.BodyRadius
	}
	var candidates []monsterAreaVictim
	targets := make(map[uint32]petCombatTarget)
	add := func(target petCombatTarget, radius float64) {
		if target.gid == primary.gid || targets[target.gid].gid != 0 {
			return
		}
		targets[target.gid] = target
		candidates = append(candidates, monsterAreaVictim{gid: target.gid, pose: target.at, radius: radius})
	}
	if lease, ok := rt.casterPopulation(step.key.division, step.snapshot); ok && rt.Monsters != nil {
		for _, m := range rt.Monsters.CombatCandidatesInPopulation(step.key.division, lease, center, reach, step.nowMs, nearest) {
			target, found := rt.resolvePetCombatTarget(step, m.Gid)
			if found {
				add(target, m.BodyRadius())
			}
		}
	}
	world := domain.CharacterWorldInstance(step.snapshot)
	for _, present := range rt.PopulationPlayers(step.key.division, step.nowMs) {
		if present.World != world || simulation.IsDungeonRegion(present.Spawn.RegionID) != simulation.IsDungeonRegion(from.RegionID) {
			continue
		}
		owner := rt.findCharacterByGid(step.key.division, present.GID)
		snapshot := rt.characterSnapshot(step.key.division, owner)
		if snapshot == nil || snapshot.DeletePending {
			continue
		}
		// 528F40 excludes same-team player targets before owner permission.
		// INFERENCE: apply that companion rule to secondary owners and their
		// companions too, so area selection cannot bypass team protection.
		if rt.companionTeamRefusal(step.snapshot, snapshot) != 0 {
			continue
		}
		if step.ref.TidWord>>11 == domain.MercenaryBand {
			if !rt.worldPlayerEnemy(step.key.division, step.snapshot, snapshot) {
				continue
			}
		} else if rt.playerAttackTargetRefusal(step.key.division, step.snapshot, snapshot, step.nowMs) != 0 {
			continue
		}
		if target, found := rt.resolvePetCombatTarget(step, present.GID); found {
			if radius, valid := rt.deps.CharacterBodyRadius(snapshot); valid {
				add(target, radius)
			}
		}
		for _, pet := range rt.companionTargets(step.key.division, present.GID, step.nowMs) {
			if target, found := rt.resolvePetCombatTarget(step, pet.Gid); found {
				add(target, float64(pet.BodyRadius))
			}
		}
	}
	var result []petCombatTarget
	for _, admitted := range selectCreatureArea(skill, from, primary.at, step.ref.Parameters.BodyRadius, candidates) {
		result = append(result, targets[admitted.gid])
	}
	return result
}

/*
================
strikePetTarget
================
*/
func (rt *Runtime) strikePetTarget(step petCombatStep, target petCombatTarget, skill enterworld.SkillRow) (petStrikeResult, bool) {
	if target.cos != nil {
		return rt.petStrikeCOS(step, target, skill)
	}
	if target.player != nil {
		return rt.petStrikePlayer(step, target.combatTarget, skill)
	}
	return rt.petStrike(step, *target.monster, skill, target.at)
}

/*
================
strikePetTargets

58E5F0 reduces the running share after each victim. B245/B505 names all
victims under one token, and one finalize closes the action.
================
*/
func (rt *Runtime) strikePetTargets(step petCombatStep, primary petCombatTarget, skill enterworld.SkillRow) (petStrikeResult, bool) {
	secondary := rt.petAreaTargets(step, primary, skill)
	step.percent = fullAreaPercent
	first, ok := rt.strikePetTarget(step, primary, skill)
	if !ok {
		return petStrikeResult{}, false
	}
	strikes := []monsterStrike{first.strike}
	actor := first.actor
	step.percent = step.percent * uint64(100-skill.ActionArea.ReductionPercent) / fullAreaPercent
	for _, target := range secondary {
		hit, committed := rt.strikePetTarget(step, target, skill)
		if !committed {
			continue
		}
		strikes = append(strikes, hit.strike)
		actor = append(actor, hit.actor...)
		step.percent = step.percent * uint64(100-skill.ActionArea.ReductionPercent) / fullAreaPercent
	}
	token := rt.petStrikeToken(step)
	cast := wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: step.pet.GID, InstanceToken: token}
	prepared := step.state.combat != nil && step.state.combat.castToken != 0
	var frame wire.Frame
	if skill.ActionArea.Shape == 0 {
		result := wire.NewStationarySkillCastSingleTargetResult(cast, first.strike.gid, first.strike.impacts)
		if first.strike.absorb != nil {
			result = result.WithAbsorb(first.strike.absorb)
		}
		frame = rt.petStrikeFrame(step, result)
	} else {
		targets := monsterAreaTargets(strikes, creatureImpactCount(skill))
		frame = wire.SkillCastAreaFrame(cast, first.strike.gid, targets)
		if prepared {
			frame = wire.SkillCastAreaReleaseFrame(cast, first.strike.gid, targets)
		}
	}
	lifecycle, _ := skill.ActionLifecycleMs()
	bracket := fmt.Sprintf("@pet:%d", step.pet.GID)
	if prepared {
		lifecycle = uint64(skill.ActionDurationMs)
	} else {
		rt.queueSkillFinalize(step.key.division, bracket, step.pet.GID, step.nowMs, wire.SkillCastReleaseFrame(token, first.strike.gid))
	}
	from := step.state.follower.Position(step.nowMs)
	closeAt := step.nowMs + max(int64(lifecycle), projectileFlightMs(from, primary.at, skill.ProjectileSpeed)+1)
	rt.queueSkillFinalize(step.key.division, bracket, step.pet.GID, closeAt, wire.SkillCastFinalizeFrame(token))
	public := []wire.Frame{frame}
	for _, hit := range strikes {
		public = append(public, hit.public...)
	}
	step.state.public = append(step.state.public, public...)
	return petStrikeResult{frames: simFrames(append(public, actor...)), fatal: first.fatal}, true
}
