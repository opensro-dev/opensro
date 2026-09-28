/*
===========================================================================

skillknockdown.go - knockdown and knockback consequences (58FF7A..590157)

===========================================================================
*/

package action

import (
	"math"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
knockdownConsequence

58FF7A..590157: normalize the planar caster->victim vector, displace
twenty units, and hold motion 8 for KORecover + action duration + 0.5s.
Preserve the explicit float32 spills, including the unsigned duration sum.
==================
*/
func knockdownConsequence(from simulation.Spawn, to monster.Pose, recovery, duration uint32, now int64) simulation.MonsterKnockdownPlan {
	to = displaceImpactPose(from, to, 20)
	seconds := float32(float64(recovery+duration)/1000 + 0.5)
	return simulation.MonsterKnockdownPlan{Pose: to, UntilMs: now + int64(float64(seconds)*1000)}
}

/*
================
displaceImpactPose
================
*/
func displaceImpactPose(from simulation.Spawn, to monster.Pose, distance uint32) monster.Pose {
	dx, dz := worldgeom.Delta(worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z}, worldgeom.RegionXZ{RegionID: to.RegionID, X: to.X, Z: to.Z})
	x, z := float32(dx), float32(dz)
	length := float32(math.Sqrt(float64(float32(float64(x)*float64(x) + float64(z)*float64(z)))))
	inverse := float32(0)
	if length > 0 {
		inverse = float32(1 / float64(length))
	}
	x, z = float32(x*inverse), float32(z*inverse)
	to.X = float64(float32(float32(to.X) + float32(x*float32(distance))))
	to.Z = float64(float32(float32(to.Z) + float32(z*float32(distance))))
	normalized := simulation.NormalizeSpawnFrame(simulation.Spawn{RegionID: to.RegionID, X: to.X, Y: to.Y, Z: to.Z})
	to.RegionID, to.X, to.Y, to.Z = normalized.RegionID, normalized.X, normalized.Y, normalized.Z
	return to
}

/*
================
planMonsterImpacts
================
*/
func (rt *Runtime) planMonsterImpacts(division string, c *enterworld.Character, skill enterworld.SkillRow, target monster.Instance, formulas []combat.Result, now int64) ([]simulation.MonsterDamagePlan, bool) {
	mover, ok := rt.Monsters.Mover(division, target.Gid)
	if !ok {
		return nil, false
	}
	pose := mover.LivePoseAt(now, nil)
	from := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	remaining := target.CurrentHP
	plans := make([]simulation.MonsterDamagePlan, 0, len(formulas))
	for _, formula := range formulas {
		plan := simulation.MonsterDamagePlan{GID: target.Gid, ExpectedHP: remaining, Damage: formula.Damage, CreditGID: enterworld.ObjectIDForCharacter(c)}
		if formula.Blocked {
			// 5905FB: a blocked impact skips damage, knockdown and statuses.
			plan.Damage = 0
			plans = append(plans, plan)
			continue
		}
		remaining -= min(remaining, formula.Damage)
		if remaining > 0 && skill.Knockdown.Present && target.Ref.Knockdown&1 != 0 && target.Motion.StateAt(now) != 8 {
			chance := combat.KnockdownChance(skill.Knockdown.Rank, skill.Knockdown.Chance, target.Ref.Level)
			proc, err := rt.effectOutcome(criticalActor{division: division, character: c.Name}, 0x44000000|(skill.ID&0xffffff), uint32(chance))
			if err != nil {
				return nil, false
			}
			if proc {
				effect := knockdownConsequence(from, pose, target.Ref.KORecoverMs, skill.ActionDurationMs, now)
				if _, valid := wire.NewSkillCastFacingPoint(effect.Pose.RegionID, effect.Pose.X, effect.Pose.Y, effect.Pose.Z); !valid {
					return nil, false
				}
				plan.Knockdown = &effect
				pose = effect.Pose
			}
		}
		if remaining > 0 && plan.Knockdown == nil && skill.Knockback.Present && int32(skill.Knockback.Chance) > 0 && target.Ref.Knockdown&2 != 0 {
			proc, err := rt.effectOutcome(criticalActor{division: division, character: c.Name}, 0x45000000|(skill.ID&0xffffff), skill.Knockback.Chance)
			if err != nil {
				return nil, false
			}
			if proc {
				point := displaceImpactPose(from, pose, skill.Knockback.Distance)
				if _, valid := wire.NewSkillCastFacingPoint(point.RegionID, point.X, point.Y, point.Z); !valid {
					return nil, false
				}
				plan.Knockback = &simulation.MonsterKnockdownPlan{Pose: point, UntilMs: now + 2000}
				pose = point
			}
		}
		// 590680 rolls on every hit; 593F0C applies the records only to a
		// surviving victim (MonsterState.applyDamageLocked).
		blocked := formula.ResultFlags&8 != 0
		records, err := rt.rollPlayerOnMonster(division, c, &skill.Abnormal, target, blocked)
		if err != nil {
			return nil, false
		}
		imbueRecords, err := rt.rollPlayerOnMonster(division, c, &formula.Imbue, target, blocked)
		if err != nil {
			return nil, false
		}
		plan.Abnormal = append(records, imbueRecords...)
		plan.AbnormalSources = rt.Monsters.PrepareAbnormalSources(division, plan.Abnormal)
		plans = append(plans, plan)
		if remaining == 0 {
			break
		}
	}
	return plans, true
}

/*
================
committedSkillImpact
================
*/
func committedSkillImpact(formula combat.Result, result simulation.MonsterDamageResult) wire.SkillCastTargetImpact {
	impact := wire.SkillCastTargetImpact{ResultFlags: formula.ResultFlags, Damage: result.Applied, Fatal: result.Fatal, Blocked: formula.Blocked}
	if result.Knockdown != nil {
		pose := result.Knockdown.Pose
		point, ok := wire.NewSkillCastFacingPoint(pose.RegionID, pose.X, pose.Y, pose.Z)
		if !ok {
			panic("committed knockdown lost its admitted wire pose")
		}
		impact.Knockdown = point
	}
	if result.Knockback != nil {
		pose := result.Knockback.Pose
		point, ok := wire.NewSkillCastFacingPoint(pose.RegionID, pose.X, pose.Y, pose.Z)
		if !ok {
			panic("committed knockback lost its admitted wire pose")
		}
		impact.Knockback = point
	}
	return impact
}

// The division operation lock owns this cancellation together with the HP/
// motion commit. Remove all releases and publish one close per owned token.
/*
================
interruptMonsterCast
================
*/
func (rt *Runtime) interruptMonsterCast(division string, gid uint32) []wire.Frame {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	var frames []wire.Frame
	preparing := rt.pendingMonsterCasts[:0]
	for _, p := range rt.pendingMonsterCasts {
		if p.division == division && p.instance.Gid == gid {
			frames = append(frames, wire.SkillCastFinalizeFrame(p.token))
		} else {
			preparing = append(preparing, p)
		}
	}
	rt.pendingMonsterCasts = preparing
	owner := monsterCastOwner(gid)
	kept := rt.pendingSkillFinalizes[:0]
	for _, p := range rt.pendingSkillFinalizes {
		if p.divisionID == division && p.characterName == owner {
			if len(p.frame.Payload) > 0 && p.frame.Payload[0] == 2 {
				frames = append(frames, p.frame)
			}
		} else {
			kept = append(kept, p)
		}
	}
	rt.pendingSkillFinalizes = kept
	return frames
}
