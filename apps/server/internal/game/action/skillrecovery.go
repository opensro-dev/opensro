/*
===========================================================================

skillrecovery.go - support casts: self and targeted heals, cures, resu

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
acceptSupportSkill

The action owner holds the division lock. Positive-time recovery publishes
only its opening bracket at acceptance. Healing and cost commit together at
release; a refused release closes that bracket without publishing vitals.
==================
*/
func (rt *Runtime) acceptSupportSkill(
	division string,
	character, snapshot *enterworld.Character,
	cast wire.SkillAction,
	skill enterworld.SkillRow,
) OpResult {
	result, _ := rt.acceptSupportSkillPhase(
		division,
		character,
		snapshot,
		cast,
		skill,
		rt.Now().UnixMilli(),
		nil,
	)
	return result
}

/*
==================
acceptSupportSkillPhase

A target out of reach defers the cast behind a support intent: the command
actor walks first, and target validation and the clear line (phases 0x08
and 0x40) belong to execution. A resu row proposes a revival to a dead
target (resurrection.go) and heals nobody.
==================
*/
func (rt *Runtime) acceptSupportSkillPhase(
	division string,
	character, snapshot *enterworld.Character,
	cast wire.SkillAction,
	skill enterworld.SkillRow,
	now int64,
	release *pendingProjectileCast,
) (OpResult, skillCastDecision) {
	cure := skill.Abnormal.CurePresent()
	resu := skill.Abnormal.AdmitDeadParty
	targeted := ((skill.Heal.Present && !skill.Aura.Eshp) || resu) && skill.TargetRequired
	supported := skill.Recovery.SelfFlatPinned || cure || targeted
	selfHealOnly := skill.Recovery.SelfFlatPinned && !cure && !targeted
	casterReady := enterworld.SkillLearned(snapshot, skill.ID) &&
		enterworld.CharacterAlive(snapshot)

	if !supported || skill.ChainSub || cast.HasGroundTarget || !casterReady ||
		selfHealOnly && cast.HasTarget {
		return OpResult{DiagnosticRefusal: "recovery-admission-refused"}, skillCastRefused
	}

	recipient, recipientView := character, snapshot
	var target *admitTarget

	if targeted {
		if !cast.HasTarget || cast.TargetGid == 0 {
			return OpResult{DiagnosticRefusal: "recovery-admission-refused"}, skillCastRefused
		}

		recipient = rt.findCharacterByGid(division, cast.TargetGid)
		recipientView = rt.characterSnapshot(division, recipient)
		if recipientView == nil {
			return offensiveRefusal(0x3006), skillCastRefused
		}

		to := rt.liveSpawn(simulation.WorldKey(division, recipientView.Name), recipientView, now)
		if release == nil {
			spacing, pinned, ok := rt.supportTargetSpacing(snapshot, recipientView, skill)
			if !ok {
				return OpResult{DiagnosticRefusal: "recovery-spacing-unavailable"}, skillCastRefused
			}

			from := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
			if pinned && !spacing.Contains(from, to) {
				return rt.beginSupportApproach(division, character, snapshot, cast, spacing, from, to, now), skillCastDeferred
			}
		}

		target = &admitTarget{at: to, player: recipientView}
	}

	if rt.skillCastPostureBlocked(division, snapshot, now) ||
		release == nil && rt.hasOpenSkillCast(division, snapshot.Name) {
		return OpResult{DiagnosticRefusal: "recovery-action-busy"}, skillCastRefused
	}

	if code := rt.skillAdmission(division, snapshot, skill, now, target, release, admitExecution); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}

	costForPhase := func(c *enterworld.Character) (skillCharge, uint16) {
		return rt.offensivePhaseCost(division, c, skill, now, release)
	}

	if _, code := costForPhase(snapshot); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}

	if release == nil && skill.ActionCastingTimeMs > 0 {
		var refusal uint16
		if !rt.deps.Update(character, "prepare-self-recovery", func() bool {
			if !enterworld.CharacterAlive(character) ||
				!enterworld.SkillLearned(character, skill.ID) {
				return false
			}

			_, refusal = rt.offensiveCost(division, character, skill, now)
			if refusal != 0 {
				return false
			}

			rt.startSkillCast(division, character, now)
			rt.registerPlayerSkillCooldown(division, character, skill, now)
			return true
		}) {
			if refusal != 0 {
				return offensiveRefusal(refusal), skillCastRefused
			}

			return OpResult{DiagnosticRefusal: "recovery-prepare-commit-refused"}, skillCastRefused
		}

		return rt.prepareSupportCast(division, snapshot, cast, skill, now), skillCastAccepted
	}

	var refusal uint16
	var vitals wire.Frame
	var cureActor, curePublic []wire.Frame
	var cureRecipients []RecipientFrames

	if !rt.deps.Update(character, "skill-self-recovery", func() bool {
		if !enterworld.CharacterAlive(character) || !enterworld.SkillLearned(character, skill.ID) {
			return false
		}

		cost, code := costForPhase(character)
		refusal = code
		if code != 0 {
			return false
		}

		if release == nil {
			rt.startSkillCast(division, character, now)
		}
		rt.commitOffensivePhaseCost(division, character, skill, cost, now, release != nil)
		if skill.Abnormal.CurePresent() {
			cureActor, curePublic, cureRecipients = rt.applySkillCure(
				division,
				character,
				skill,
				cast,
				now,
			)
		}

		if !skill.Recovery.SelfFlatPinned && !targeted {
			return true
		}

		if recipient != character {
			return true
		}

		hp, mp, ok := rt.skillHealAmounts(division, character, character, skill, healCast)
		if !ok {
			return false
		}

		vitals, ok = rt.applySkillRecovery(division, character, hp, mp)
		return ok
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}

		return OpResult{DiagnosticRefusal: "recovery-commit-refused"}, skillCastRefused
	}

	switch {
	case resu:
		if prompt := rt.proposeResurrection(division, snapshot, recipientView, skill, now); len(prompt) != 0 {
			cureRecipients = append(cureRecipients, RecipientFrames{
				CharacterID: recipientView.ID,
				Frames:      prompt,
			})
		}
	case targeted && recipient != character:
		var frame wire.Frame
		if !rt.deps.Update(recipient, "skill-target-heal", func() bool {
			hp, mp, ok := rt.skillHealAmounts(division, recipient, character, skill, healCast)
			if !ok {
				return false
			}

			var applied bool
			frame, applied = rt.applySkillRecovery(division, recipient, hp, mp)
			return applied
		}) {
			return OpResult{DiagnosticRefusal: "recovery-commit-refused"}, skillCastRefused
		}

		if frame.Opcode != 0 {
			cureRecipients = append(cureRecipients, RecipientFrames{
				CharacterID: recipient.ID,
				Frames:      []wire.Frame{frame},
			})
		}
	}

	var token uint32
	if release != nil {
		token = release.token
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}

	open := wire.SkillCastSelfFrame(wire.SkillCastSuccess{
		SkillId:       skill.ID,
		CasterGid:     enterworld.ObjectIDForCharacter(snapshot),
		InstanceToken: token,
	})

	lifetime, _ := skill.ActionLifecycleMs()
	if release != nil {
		started := release.releaseAtMs - int64(skill.ActionCastingTimeMs) - 1
		rt.queueSkillFinalize(
			division,
			snapshot.Name,
			enterworld.ObjectIDForCharacter(snapshot),
			max(now+1, started+int64(lifetime)),
			wire.SkillCastFinalizeFrame(token),
		)

		control := wire.SkillCastReleaseFrame(token, enterworld.ObjectIDForCharacter(snapshot))
		return supportCastResult(
			control,
			vitals,
			skill.Recovery.SelfFlatPinned,
			cureActor,
			curePublic,
			cureRecipients,
		), skillCastAccepted
	}

	rt.queueSkillFinalize(
		division,
		snapshot.Name,
		enterworld.ObjectIDForCharacter(snapshot),
		now+int64(skill.ActionCastingTimeMs),
		wire.SkillCastReleaseFrame(token, enterworld.ObjectIDForCharacter(snapshot)),
	)
	rt.queueSkillFinalize(
		division,
		snapshot.Name,
		enterworld.ObjectIDForCharacter(snapshot),
		now+int64(lifetime),
		wire.SkillCastFinalizeFrame(token),
	)

	return supportCastResult(
		open,
		vitals,
		skill.Recovery.SelfFlatPinned,
		cureActor,
		curePublic,
		cureRecipients,
	), skillCastAccepted
}

/*
==================
supportCastResult
==================
*/
func supportCastResult(
	control, vitals wire.Frame,
	heal bool,
	actor, public []wire.Frame,
	recipients []RecipientFrames,
) OpResult {
	frames := []wire.Frame{control}
	if heal {
		frames = append(frames, vitals)
	}

	frames = append(frames, actor...)
	frames = append(frames, public...)

	return OpResult{
		Frames:       frames,
		Broadcast:    append([]wire.Frame{control}, public...),
		ActorPrivate: frames[1:],
		Recipients:   recipients,
	}
}
