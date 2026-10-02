/*
===========================================================================

skilltimedeffect.go - timed self effects (5830B0 mode zero)

===========================================================================
*/

package action

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"sync/atomic"
)

/*
==================
acceptTimedSelfEffect

5830B0 mode zero releases the cast and creates independent recipient
instances. 59BB80 releases the root without adding a second cancel packet;
the recipient's later retirement is B6A0, using its own identity.

Hiding and detection programs (concealment.go) take the same path. Their
efr kind 1 recipients are installed after the caster's own door; the
caster itself receives an instance only when its select word names it.
==================
*/
func (rt *Runtime) acceptTimedSelfEffect(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64, pending *pendingProjectileCast) (OpResult, skillCastDecision) {
	timed := skill.TimedEffect.Pinned && !skill.TimedEffect.Persistent && !skill.TimedEffect.Targeted
	concealment := skill.Concealment.Pinned && !skill.Concealment.Persistent
	if !timed && !concealment || skill.ChainSub || cast.HasTarget || cast.HasGroundTarget ||
		!enterworld.CharacterAlive(snapshot) || !enterworld.SkillLearned(snapshot, skill.ID) {
		return OpResult{DiagnosticRefusal: "timed-effect-admission-refused"}, skillCastRefused
	}
	if rt.skillCastPostureBlocked(division, snapshot, now) || pending == nil && rt.hasOpenSkillCast(division, snapshot.Name) {
		return OpResult{DiagnosticRefusal: "timed-effect-action-busy"}, skillCastRefused
	}
	if code := rt.skillAdmission(division, snapshot, skill, now, nil, pending, admitExecution); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	if _, code := rt.offensivePhaseCost(division, snapshot, skill, now, pending); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	if pending == nil && skill.ActionCastingTimeMs > 0 {
		var refusal uint16
		if !rt.deps.Update(c, "prepare-timed-effect", func() bool {
			if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
				return false
			}
			_, refusal = rt.offensiveCost(division, c, skill, now)
			if refusal != 0 {
				return false
			}
			rt.startSkillCast(division, c, now)
			rt.registerPlayerSkillCooldown(division, c, skill, now)
			return true
		}) {
			if refusal != 0 {
				return offensiveRefusal(refusal), skillCastRefused
			}
			return OpResult{DiagnosticRefusal: "timed-effect-prepare-commit-refused"}, skillCastRefused
		}
		return rt.prepareSelfEffectCast(division, snapshot, cast, skill, now), skillCastAccepted
	}
	var token uint32
	if pending != nil {
		token = pending.token
		// Native 5835EF clears the current preparation before target/effect
		// validation. Its own packed states must not reject its recipient.
		rt.clearCurrentSkillCommand(division, c.Name)
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	effectToken := atomic.AddUint32(&rt.castTokenCounter, 1)
	rider, ok := rt.concealmentRider(division, snapshot, skill)
	if !ok {
		return OpResult{DiagnosticRefusal: "timed-effect-stats-unavailable"}, skillCastRefused
	}
	area := skill.Concealment.Area
	if skill.TimedEffect.Area.Present {
		area = skill.TimedEffect.Area
	}
	self := !area.Present || area.Select&enterworld.SelectCaster != 0
	var effects []wire.Frame
	var refusal uint16
	if !rt.deps.Update(c, "release-timed-effect", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, pending)
		refusal = code
		if code != 0 {
			return false
		}
		if pending == nil {
			rt.startSkillCast(division, c, now)
		}
		if self {
			if !rt.requestSelfEffectReplacement(division, c, skill) {
				refusal = 0x300c
				return false
			}
			var ok bool
			effects, ok = rt.commitCharacterEffect(division, c, skill, effectToken, statuseffect.StateActive, false, EffectPresentation{Phase: 1, Rider: rider}, now)
			if !ok {
				return false
			}
		}
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, pending != nil)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{DiagnosticRefusal: "timed-effect-release-commit-refused"}, skillCastRefused
	}
	if area.Present {
		rt.installRecipientEffects(division, rt.concealmentRecipients(division, c, area, now), skill, rider, now)
	}
	gid := enterworld.ObjectIDForCharacter(c)
	var frames, broadcast []wire.Frame
	if pending == nil {
		open := wire.SkillCastSelfFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: gid, InstanceToken: token})
		frames = append(frames, open)
		broadcast = append(broadcast, open)
	}
	private := []wire.Frame{{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceCombatDamage, rt.publishedVitals(division, c))}}
	if stats, err := rt.PlayerBaseStats(division, c); err == nil {
		private = append(private, wire.Frame{Opcode: wire.OpBaseStats, Payload: stats.Encode()})
	} else {
		log.WithError(err).WithFields(log.Fields{"division": division, "character": c.Name, "skill": skill.ID}).Error("effect installation stat projection failed")
	}
	released := append([]wire.Frame{wire.SkillCastReleaseFrame(token, 0)}, effects...)
	// The release B505 is not a close: the cast bracket ends with its own
	// closing B505 after the recovery phase, or the client keeps the
	// casting aura on the ground. The release already ended action
	// ownership, so the close must not hold the caster busy.
	rt.queueDetachedCastClose(division, c.Name, gid, token, now+int64(skill.ActionDurationMs))
	frames = append(frames, private...)
	frames = append(frames, released...)
	broadcast = append(broadcast, released...)
	return OpResult{Frames: frames, Broadcast: broadcast, ActorPrivate: private}, skillCastAccepted
}
