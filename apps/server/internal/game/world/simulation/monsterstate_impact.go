/*
===========================================================================

monsterstate_impact.go - knockdown plans against monsters

===========================================================================
*/

package simulation

import (
	"math"
	"opensro.online/server/internal/game/internal/vitals"
	"opensro.online/server/internal/game/world/monster"
)

// MonsterKnockdownPlan is the displacement and motion-timer consequence of
// one successful KO roll. HP, pose and timer commit under the same owner lock.
/*
================
MonsterKnockdownPlan
================
*/
type MonsterKnockdownPlan struct {
	Pose    monster.Pose
	UntilMs int64
}

/*
================
validKnockdownPlan
================
*/
func validKnockdownPlan(plan *MonsterKnockdownPlan) bool {
	if plan == nil {
		return true
	}
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	return plan.UntilMs > 0 && finite(plan.Pose.X) && finite(plan.Pose.Y) && finite(plan.Pose.Z)
}

// ApplyDamageSequence validates the original victim once, then commits every
// authored impact atomically. The first fatal impact ends the sequence.
/*
================
ApplyDamageSequence
================
*/
func (s *MonsterState) ApplyDamageSequence(division string, gid, expectedHP uint32, plans []MonsterDamagePlan) []MonsterDamageResult {
	if len(plans) == 0 || len(plans) > 255 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	instance, ok := state.instances.lookup(gid)
	if !ok || instance.CurrentHP == 0 || instance.CurrentHP != expectedHP {
		return nil
	}
	for _, p := range plans {
		if p.GID != gid || !validImpactDisplacement(p.Knockdown, p.Knockback) || !validAbnormalSources(p) {
			return nil
		}
	}
	out := make([]MonsterDamageResult, 0, len(plans))
	for _, p := range plans {
		result := s.applyDamageLocked(division, state, p)
		out = append(out, result)
		if result.Fatal {
			break
		}
	}
	return out
}

/*
================
applyDamageLocked
================
*/
func (s *MonsterState) applyDamageLocked(division string, state *divisionMonsterState, plan MonsterDamagePlan) MonsterDamageResult {
	gid, damage, creditGID := plan.GID, plan.Damage, plan.CreditGID
	knockdown, knockback := plan.Knockdown, plan.Knockback
	nowMs := s.nowMillis()
	instance := finishSummonAction(state.instances.get(gid), nowMs)
	before := instance.CurrentHP
	if before > 0 {
		state.recordContribution(gid, creditGID, damage)
	}
	// Native 585664 packs the full hit; 52A240 separately clamps the HP debit.
	// A corpse accepts no new hit even if a caller still holds its identity.
	if before == 0 {
		damage = 0
	}
	applied := vitals.HitDebit(before, damage)
	instance.CurrentHP -= applied
	instance.DamageSinceSummon += applied
	var committed *MonsterKnockdownPlan
	displacement := knockdown
	motion := uint8(8)
	if knockback != nil {
		displacement = knockback
		motion = 16
	}
	if instance.CurrentHP == 0 {
		if before > 0 {
			state.settleCorpseLocked(&instance, nowMs, s.ground)
		}
	} else if displacement != nil {
		delete(state.pendingSummons, gid)
		if instance.SummonActionUntilMs != 0 {
			instance.SummonActionUntilMs = 0
			// Command completion already cleared the selected skill. The
			// 562540 null-active branch does not consume subsequent damage
			// when a later impact cancels the pending summon execution.
		}
		mover, ok := state.movers.lookup(gid)
		if !ok {
			mover = monster.NewSpawnMover(instance, s.nowMillis())
		}
		live := mover.LivePoseAt(nowMs, nil)
		if err := mover.Transition(monster.MoverEventDisplaced, mover.TargetGID()); err != nil {
			panic(err)
		}
		// The displaced XZ carries the pre-impact height; the victim lands on
		// the surface under its new position (a push up a slope must not bury
		// it, and a later death would freeze it there).
		if s.ground != nil {
			pose := displacement.Pose
			if y, ok := s.ground(pose.RegionID, pose.X, pose.Y, pose.Z); ok {
				grounded := *displacement
				grounded.Pose.Y = y
				displacement = &grounded
			}
		}
		displacement = s.clipDisplacementLocked(live, displacement)
		mover.Pose = displacement.Pose
		mover.From, mover.To = displacement.Pose, displacement.Pose
		if state.movers == nil {
			state.movers = newMoverStorage(nil)
		}
		state.movers.set(gid, mover)
		state.behavior.set(gid, 0)
		instance.Motion = monster.MotionHold{State: motion, UntilMs: displacement.UntilMs}
		value := *displacement
		committed = &value
	}
	// Preserve the formula's magical lane and execution selector through
	// commit; neither can be recovered from the final HP debit (58F491/593BEF).
	effects := s.applyAbnormalLocked(monsterAbnormalInput{division: division, ctx: s.abnormalContext, state: state, ground: s.ground, instance: &instance, now: nowMs, sources: plan.AbnormalSources}, plan.StatusHit, plan.Abnormal)
	state.instances.set(gid, instance)
	result := MonsterDamageResult{Population: state.lease, Instance: instance, BeforeHP: before, CurrentHP: instance.CurrentHP, Damage: damage, Applied: applied, Fatal: before > 0 && instance.CurrentHP == 0, Knockdown: committed, Abnormal: effects}
	if knockback != nil {
		result.Knockback = committed
		result.Knockdown = nil
	}
	if result.Fatal {
		result.Contributions = state.contributionSnapshot(gid)
	}
	return result
}

/*
================
validImpactDisplacement
================
*/
func validImpactDisplacement(ko, kb *MonsterKnockdownPlan) bool {
	return !(ko != nil && kb != nil) && validKnockdownPlan(ko) && validKnockdownPlan(kb)
}

/*
================
clipDisplacementLocked

The displacement walks as a move (593D24 -> CGObjChar_MoveByStep 48B920 ->
CGObjMobile_MoveTo 48B660 -> CGObj_MoveTo 485740): the region manager's
move query runs from the live position, a blocked result (0x10000000)
leaves the victim where it stood, and any other result moves it to the
point the query wrote, short of a blocked edge. A knockback therefore never
lands a monster on a tile it could not have walked onto. Without an
installed move test (geometry-free fixtures) the push is kept as computed.
================
*/
func (s *MonsterState) clipDisplacementLocked(live monster.Pose, displacement *MonsterKnockdownPlan) *MonsterKnockdownPlan {
	if s.collide == nil {
		return displacement
	}
	clipped := *displacement
	move := s.collide(poseToSpawn(live), poseToSpawn(displacement.Pose))
	rest := move.Rest
	if move.Result&monster.NavResultBlocked != 0 {
		rest = poseToSpawn(live)
	}
	clipped.Pose = monster.Pose{RegionID: rest.RegionID, X: rest.X, Y: rest.Y, Z: rest.Z, Heading: displacement.Pose.Heading}
	return &clipped
}
