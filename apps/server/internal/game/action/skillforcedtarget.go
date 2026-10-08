/*
===========================================================================

skillforcedtarget.go - Scorn recipient installation and target constraints

hitm owns an ordinary recipient effect. The common admission path reads its
target; registry retirement is the only way the constraint ends.

===========================================================================
*/
package action

import (
	"sort"
	"sync/atomic"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
tauntPlayerCast

One admitted primary and the snapshot used for its resource transaction.
================
*/
type tauntPlayerCast struct {
	division                 string
	caster, snapshot, target *enterworld.Character
	skill                    enterworld.SkillRow
	now                      int64
}

/*
================
forcedTargetRecipients

The primary precedes its surrounding candidates. Each candidate must pass
the shared player-target predicate in the caster's world instance.
================
*/
func (rt *Runtime) forcedTargetRecipients(c tauntPlayerCast) []*enterworld.Character {
	out := []*enterworld.Character{c.target}
	area := c.skill.TimedEffect.Area
	if !area.Present || area.MaxTargets <= 1 {
		return out
	}
	casterRadius, ok := rt.deps.CharacterBodyRadius(c.snapshot)
	if !ok {
		return out
	}
	center := rt.liveSpawn(simulation.WorldKey(c.division, c.target.Name), c.target, c.now)
	candidates := append([]*enterworld.Character(nil), rt.deps.CharactersForDivision(c.division)...)
	sort.Slice(candidates, func(i, j int) bool {
		return enterworld.ObjectIDForCharacter(candidates[i]) < enterworld.ObjectIDForCharacter(candidates[j])
	})
	for _, candidate := range candidates {
		if candidate == nil || candidate.ID == c.target.ID || candidate.ID == c.caster.ID || candidate.DeletePending ||
			domain.CharacterWorldInstance(candidate) != domain.CharacterWorldInstance(c.snapshot) ||
			!enterworld.CharacterAlive(candidate) {
			continue
		}
		if rt.RewardActorPresent != nil && !rt.RewardActorPresent(c.division, candidate.Name) {
			continue
		}
		if rt.playerSkillTarget(c.division, c.snapshot, candidate, c.skill, c.now) != 0 {
			continue
		}
		if !rt.hostilePlayerRelation(c.division, c.snapshot, candidate) {
			continue
		}
		to := rt.liveSpawn(simulation.WorldKey(c.division, candidate.Name), candidate, c.now)
		radius, ok := rt.deps.CharacterBodyRadius(candidate)
		// Shape two expands the primary-centered sphere by the caster and
		// candidate radii, as the shared monster selector does (58AB6D).
		if !ok || !samePlaneAdjacent(center, to) || float32(distance3D(center, to)) > float32(float64(area.Radius)+casterRadius+radius) {
			continue
		}
		out = append(out, candidate)
		if len(out) >= int(area.MaxTargets) {
			break
		}
	}
	return out
}

/*
================
acceptForcedTarget

Admission and approach use the common targeted-effect path. Release installs
one noncancelable instance per recipient without manufacturing damage.
================
*/
func (rt *Runtime) acceptForcedTarget(c tauntPlayerCast) OpResult {
	recipients := rt.forcedTargetRecipients(c)
	var refusal uint16
	var battleFrames []wire.Frame
	if !rt.deps.Update(c.caster, "forced-target-cost", func() bool {
		if !enterworld.CharacterAlive(c.caster) || !enterworld.SkillLearned(c.caster, c.skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(c.division, c.caster, c.skill, c.now, nil)
		refusal = code
		if code != 0 {
			return false
		}
		rt.startSkillCast(c.division, c.caster, c.skill, c.now)
		rt.commitOffensivePhaseCost(c.division, c.caster, c.skill, cost, c.now, false)
		battleFrames = rt.enterBattleState(c.division, c.caster, c.now)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: "forced-target-cost-refused"}
	}
	casterGID := enterworld.ObjectIDForCharacter(c.snapshot)
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	open := wire.SkillCastAtTargetFrame(wire.SkillCastSuccess{SkillId: c.skill.ID, CasterGid: casterGID, InstanceToken: token, OwnerOrTargetGid: enterworld.ObjectIDForCharacter(c.target)})
	frames := []wire.Frame{open}
	var private []RecipientFrames
	for _, recipient := range recipients {
		if !rt.auraReplacementAllowed(c.division, recipient, c.skill, c.caster.ID == recipient.ID) {
			continue
		}
		var installed []wire.Frame
		owner := rt.newPlayerAbnormalOwner(c.division, recipient, c.now)
		if !rt.deps.Update(recipient, "forced-target-install", func() bool {
			if recipient.DeletePending || !enterworld.CharacterAlive(recipient) {
				return false
			}
			var ok bool
			installed, ok = rt.commitCharacterEffect(c.division, recipient, c.skill, atomic.AddUint32(&rt.castTokenCounter, 1),
				statuseffect.StateActive, false, EffectPresentation{Phase: 2, ForcedTargetGID: casterGID}, c.now)
			if ok {
				installed = append(installed, rt.enterBattleState(c.division, recipient, c.now)...)
				rt.endJobActivation(c.division, recipient)
				installed = append(installed, rt.registerPlayerAttacked(c.division, recipient, c.caster, c.now)...)
				owner.applyHit(abnormal.HitContext{Attack: true}, nil)
			}
			return ok
		}) {
			continue
		}
		// 58395E..5839A4 replaces selection record zero, exactly like 52B5B4
		// in HandleSelect. It does not cancel the command or a committed cast;
		// subsequent targeted releases encounter the shared hitm admission.
		rt.Selected.Set(c.division, recipient.Name, casterGID)
		frames = append(frames, installed...)
		rt.deps.Update(c.caster, "forced-target-hostility", func() bool {
			frames = append(frames, rt.registerPlayerAttack(c.division, c.caster, recipient, c.now)...)
			return true
		})
		publication := rt.playerAbnormalPublication(c.division, recipient, owner)
		frames = append(frames, publication.public...)
		if len(publication.actor) != 0 {
			private = append(private, RecipientFrames{CharacterID: recipient.ID, Frames: publication.actor})
		}
	}
	release := wire.SkillCastReleaseFrame(token, enterworld.ObjectIDForCharacter(c.target))
	frames = append(frames, release)
	frames = append(frames, battleFrames...)
	rt.queueDetachedCastClose(c.division, c.caster.Name, casterGID, token, c.now+int64(c.skill.ActionDurationMs))
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(casterGID, simulation.VitalsSourceCombatDamage, rt.publishedVitals(c.division, c.caster))}
	return OpResult{Frames: append(frames, vitals), Broadcast: frames, ActorPrivate: []wire.Frame{vitals}, Recipients: private}
}

/*
================
advanceForcedTargets

5847CF checks object presence, not HP or a distance tether. Death of the
recipient follows ordinary effect retirement; a dead source still exists.
================
*/
func (rt *Runtime) advanceForcedTargets() {
	for _, effect := range rt.effects.ForcedTargets() {
		unlock := rt.lockDivision(effect.DivisionID)
		source := rt.findCharacterByGid(effect.DivisionID, effect.ForcedTargetGID)
		if source == nil || source.DeletePending || rt.RewardActorPresent != nil && !rt.RewardActorPresent(effect.DivisionID, source.Name) {
			rt.effects.StopForcedTarget(effect)
		}
		unlock()
	}
}
