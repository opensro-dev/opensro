/*
===========================================================================

skilltimedtarget.go - timed effects cast on an allied player

The Warrior's guards (GUARDA_INTERCEPT, ALL_BLOCK, MAGIC_BLOCK) are the
timed defense program of skilltimedeffect.go aimed at a player within
column 21's range. The caster pays; the target receives an independent
instance in context mode 2 (5830B0), which 59F0E0 never re-checks
against anyone's equipment.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
acceptTimedTargetEffect

A target out of reach defers the cast behind a support intent; within
reach admission runs the execution mask with the player target, the
caster pays and the target receives the instance.
==================
*/
func (rt *Runtime) acceptTimedTargetEffect(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64) OpResult {
	// A row that admits Self (the blessings) is cast on the caster when no
	// target is selected.
	if !cast.HasTarget && !cast.HasGroundTarget && skill.Targets.Self {
		cast.HasTarget, cast.TargetGid = true, enterworld.ObjectIDForCharacter(snapshot)
	}
	if !skill.TimedEffect.Pinned || !skill.TimedEffect.Targeted || skill.TimedEffect.Persistent ||
		!cast.HasTarget || cast.HasGroundTarget || cast.TargetGid == 0 ||
		!enterworld.CharacterAlive(snapshot) || !enterworld.SkillLearned(snapshot, skill.ID) {
		return OpResult{DiagnosticRefusal: "timed-target-admission-refused"}
	}
	target := rt.findCharacterByGid(division, cast.TargetGid)
	view := rt.characterSnapshot(division, target)
	if view == nil {
		return offensiveRefusal(0x3006)
	}
	to := rt.liveSpawn(simulation.WorldKey(division, view.Name), view, now)
	spacing, pinned, ok := rt.supportTargetSpacing(snapshot, view, skill)
	if !ok {
		return OpResult{DiagnosticRefusal: "timed-target-spacing-unavailable"}
	}
	from := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	if pinned && !spacing.Contains(from, to) {
		return rt.beginSupportApproach(division, c, snapshot, cast, spacing, from, to, now)
	}
	if rt.skillCastPostureBlocked(division, snapshot, now) || rt.hasOpenSkillCast(division, snapshot.Name) {
		return OpResult{DiagnosticRefusal: "timed-target-action-busy"}
	}
	if code := rt.skillAdmission(division, snapshot, skill, now, &admitTarget{at: to, player: view}, nil, admitExecution); code != 0 {
		return offensiveRefusal(code)
	}
	if !rt.auraReplacementAllowed(division, target, skill) {
		return offensiveRefusal(0x300c)
	}
	if skill.TimedEffect.ForcedTarget {
		return rt.acceptForcedTarget(tauntPlayerCast{division: division, caster: c, snapshot: snapshot, target: target, skill: skill, now: now})
	}
	if skill.TimedEffect.Link.Present {
		return rt.acceptLinkedTargetEffect(division, c, snapshot, target, view, cast, skill, now)
	}

	// 58381F: the caster's HLBP, else HLSM, rides the recipient's context.
	presentation := EffectPresentation{Phase: 2}
	if d := skill.TimedEffect; d.PhysicalAddend || d.MagicalAddend {
		stats, _, err := rt.playerCombatStats(division, snapshot)
		if err != nil {
			return OpResult{DiagnosticRefusal: "timed-target-stats-unavailable"}
		}
		if d.PhysicalAddend {
			presentation.DefenseAddend[0] = stats.SkillParameters[enterworld.ParameterBlessPhysical]
		} else {
			presentation.DefenseAddend[1] = stats.SkillParameters[enterworld.ParameterBlessMagical]
		}
	}
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	effectToken := atomic.AddUint32(&rt.castTokenCounter, 1)
	var refusal uint16
	if !rt.deps.Update(c, "timed-target-cost", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, nil)
		if refusal = code; code != 0 {
			return false
		}
		rt.startSkillCast(division, c, skill, now)
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, false)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: "timed-target-commit-refused"}
	}
	var installed []wire.Frame
	rt.deps.Update(target, "timed-target-install", func() bool {
		if target.DeletePending || !enterworld.CharacterAlive(target) {
			return false
		}
		var ok bool
		installed, ok = rt.commitCharacterEffect(division, target, skill, effectToken, statuseffect.StateActive, false, presentation, now)
		return ok
	})
	if len(installed) != 0 && rt.PushCharacterFrames != nil {
		if stats, err := rt.PlayerBaseStats(division, target); err == nil {
			rt.PushCharacterFrames(division, target.Name, []wire.Frame{{Opcode: wire.OpBaseStats, Payload: stats.Encode()}})
		}
	}

	casterGID := enterworld.ObjectIDForCharacter(snapshot)
	lifetime, _ := skill.ActionLifecycleMs()
	rt.queueSkillFinalize(division, snapshot.Name, casterGID, now+int64(lifetime), wire.SkillCastFinalizeFrame(token))
	frames := append([]wire.Frame{wire.SkillCastAtTargetFrame(wire.SkillCastSuccess{
		SkillId: skill.ID, CasterGid: casterGID, InstanceToken: token, OwnerOrTargetGid: cast.TargetGid,
	})}, installed...)
	return OpResult{Frames: frames, Broadcast: frames}
}
