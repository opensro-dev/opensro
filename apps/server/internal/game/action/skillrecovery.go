/*
===========================================================================

skillrecovery.go - support casts: self, targeted and party heals, cures, resu

===========================================================================
*/

package action

import (
	"slices"
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

A party-area row (enterworld SkillRecovery) runs its action vector,
TargetSelection_Party (58BEF0, skillCureVector), at execution: a party heal
applies 5A0850 to every entry, a party resurrection runs the per-target
5946C5 arm (proposeResurrection) on every entry.
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
	partyHeal := skill.Recovery.PartyHealPinned
	partyResu := skill.Recovery.PartyResurrectPinned
	supported := skill.Recovery.SelfFlatPinned || cure || targeted || partyHeal || partyResu
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

	var party []uint32
	if partyHeal || partyResu {
		party = rt.skillCureVector(division, snapshot, skill, cast, now)
	}
	// The cure vector reads the store; resolve it before the caster's door.
	var cureTargets []cureTarget
	if cure {
		cureTargets = rt.resolveSkillCureTargets(division, character, snapshot, skill, cast, now)
	}
	casterGID := enterworld.ObjectIDForCharacter(snapshot)
	healCaster := skill.Recovery.SelfFlatPinned ||
		targeted && recipient == character ||
		partyHeal && slices.Contains(party, casterGID)

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
		if cure {
			cureActor, curePublic, cureRecipients = rt.applySkillCure(
				division,
				character,
				skill,
				cureTargets,
				now,
			)
		}

		if !healCaster {
			return true
		}

		hp, mp, ok := rt.skillHealAmounts(division, character, character, skill, healCast)
		if !ok {
			return false
		}

		vitals, ok = rt.applySkillRecovery(division, character, hp, mp)
		if ok && vitals.Opcode == 0 && cost.hp != 0 {
			// Rave Melody with a full MP gauge: the recovery moved nothing,
			// but the HP cost did. Publish the caster's gauges after the
			// charge, as the other cost-paying casts do (skillposition.go,
			// statuscastarea.go). Inferred from those owners, not from a
			// native address.
			vitals = wire.Frame{
				Opcode:  simulation.OpVitalsUpdate,
				Payload: simulation.VitalsRefreshWithSourcePayload(casterGID, simulation.VitalsSourceSkillRecovery, rt.publishedVitals(division, character)),
			}
		}
		return ok
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}

		return OpResult{DiagnosticRefusal: "recovery-commit-refused"}, skillCastRefused
	}

	switch {
	case partyResu:
		cureRecipients = append(cureRecipients, rt.proposePartyResurrection(division, snapshot, skill, party, now)...)
	case partyHeal:
		cureRecipients = append(cureRecipients, rt.applyPartyHeal(division, character, skill, party)...)
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
			skill.Recovery.SelfFlatPinned || vitals.Opcode != 0,
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
		skill.Recovery.SelfFlatPinned || vitals.Opcode != 0,
		cureActor,
		curePublic,
		cureRecipients,
	), skillCastAccepted
}

/*
==================
applyPartyHeal

The 5A0850 heal for every party entry but the caster, whose own heal ran
inside its door. Each member is healed inside its own door and receives its
0x33A6 frame alone. Inferred: a member whose door refuses (gone, or its
stats unavailable) is skipped, since the caster has already paid and the
others are still healed.
==================
*/
func (rt *Runtime) applyPartyHeal(division string, caster *enterworld.Character, skill enterworld.SkillRow, party []uint32) []RecipientFrames {
	casterGID := enterworld.ObjectIDForCharacter(caster)
	var out []RecipientFrames
	for _, gid := range party {
		if gid == casterGID {
			continue
		}
		member := rt.findCharacterByGid(division, gid)
		if member == nil {
			continue
		}

		var frame wire.Frame
		if !rt.deps.Update(member, "skill-party-heal", func() bool {
			hp, mp, ok := rt.skillHealAmounts(division, member, caster, skill, healCast)
			if !ok {
				return false
			}

			var applied bool
			frame, applied = rt.applySkillRecovery(division, member, hp, mp)
			return applied
		}) || frame.Opcode == 0 {
			continue
		}

		out = append(out, RecipientFrames{CharacterID: member.ID, Frames: []wire.Frame{frame}})
	}
	return out
}

/*
==================
proposePartyResurrection

The per-target 5946C5 arm for every party entry: proposeResurrection skips
the living, players above resu word 0 and players already answering a
proposal, so only dead members in range receive the 0x3393 prompt. As with
the targeted row, the heal block is the revival vitals and heals nobody now
(inferred from that shipped targeted rule, run once per vector entry).
==================
*/
func (rt *Runtime) proposePartyResurrection(division string, caster *enterworld.Character, skill enterworld.SkillRow, party []uint32, now int64) []RecipientFrames {
	var out []RecipientFrames
	for _, gid := range party {
		member := rt.characterSnapshot(division, rt.findCharacterByGid(division, gid))
		if member == nil || member.ID == caster.ID {
			continue
		}

		if prompt := rt.proposeResurrection(division, caster, member, skill, now); len(prompt) != 0 {
			out = append(out, RecipientFrames{CharacterID: member.ID, Frames: prompt})
		}
	}
	return out
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
