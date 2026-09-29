/*
===========================================================================

concealment.go - hiding, detection, and the effects that end them

A hide is an ordinary installed effect whose row carries a hide block:
commitCharacterEffect sets body status 6 or 7 and the speed cut. What
this file adds is who else receives a cast (efr kind 1 recipients) and
the two ways effects end early:

	event   CSkillManager_RetireEffectsForEventMask (5A16C0): a skill
	        cast (bit 2, from InitiateSkillCast 59B745) ends every
	        effect whose skc event mask holds the bit
	damage  CSkillManager_ProcessDamageEffects (5A0B80): a landed or
	        blocked hit whose att flags share a bit with the effect's
	        skc damage mask ends it, unless the keep roll holds

Whether a hidden character is seen is the observer client's decision
(CICharactor_UpdateStatesAndOwnedDecorations 85D890); the server only
installs the hide, dtt and dttp effects it reads.

===========================================================================
*/

package action

import (
	"sort"
	"sync/atomic"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// Event bits of the skc event mask (the second word).
const (
	effectEventSkillCast uint8 = 2 // InitiateSkillCast, and a pet's attack command (4D2592)
	effectEventBerserk   uint8 = 4 // berserk request (515C43)
)

// damageCancelRollKey is the probability stream 5A166A rolls on the victim.
const damageCancelRollKey = 0x4b000000

/*
===============================================================================

RECIPIENTS

===============================================================================
*/

/*
==================
concealmentRecipients

The other characters an efr kind 1 program lands on. Select 4/5 dispatches
to TargetSelection_Party (58CB70 -> 58BEF0): center distance, no body-radius
expansion and no target-cap read. This includes March, Heal Shield and party
invisibility. Other selections retain the around-source walk (58A020).
==================
*/
func (rt *Runtime) concealmentRecipients(division string, caster *enterworld.Character, area enterworld.SkillRecipientArea, now int64) []*enterworld.Character {
	if !area.Present || rt.deps == nil {
		return nil
	}
	party := rt.auraParty(division, caster)
	partyOnly := area.Select == enterworld.SelectParty || area.Select == enterworld.SelectParty|enterworld.SelectCaster
	world := domain.CharacterWorldInstance(caster)
	from := rt.liveSpawn(simulation.WorldKey(division, caster.Name), caster, now)
	casterGID := enterworld.ObjectIDForCharacter(caster)

	var out []*enterworld.Character
	for _, c := range rt.deps.CharactersForDivision(division) {
		gid := enterworld.ObjectIDForCharacter(c)
		if c == nil || gid == casterGID || c.DeletePending || !enterworld.CharacterAlive(c) ||
			domain.CharacterWorldInstance(c) != world {
			continue
		}
		if rt.RewardActorPresent != nil && !rt.RewardActorPresent(division, c.Name) {
			continue
		}
		member := party[gid]
		switch {
		case member && area.Select&enterworld.SelectParty == 0:
			continue
		case !member && area.Select&enterworld.SelectCharacter == 0:
			continue
		}
		to := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
		if partyOnly {
			// 58C078 compares the unsigned radius with the x87 length;
			// equality is included, and no body-radius getter is called.
			if !partyAreaReach(from, to, area.Radius) {
				continue
			}
		} else {
			if !samePlaneAdjacent(from, to) {
				continue
			}
			radius, ok := rt.deps.CharacterBodyRadius(c)
			if !ok || float32(distance3D(from, to)) > float32(float64(area.Radius)+radius) {
				continue
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return enterworld.ObjectIDForCharacter(out[i]) < enterworld.ObjectIDForCharacter(out[j])
	})
	if !partyOnly && area.MaxTargets != 0 {
		limit := int(area.MaxTargets)
		if area.Select&enterworld.SelectCaster != 0 {
			limit-- // the caster was pushed first
		}
		if len(out) > limit {
			out = out[:max(limit, 0)]
		}
	}
	return out
}

/*
==================
installRecipientEffects

The recipient loop of 5830B0 (583DFE..583FE2): every recipient gets its
own instance, in execution context mode 2, if buff replacement admits it.
Runs after the caster's own door; each recipient is published to itself
and its observers.
==================
*/
func (rt *Runtime) installRecipientEffects(division string, recipients []*enterworld.Character, skill enterworld.SkillRow, rider uint32, now int64) {
	for _, c := range recipients {
		if !rt.auraReplacementAllowed(division, c, skill) {
			continue
		}
		token := atomic.AddUint32(&rt.castTokenCounter, 1)
		var frames []wire.Frame
		if !rt.deps.Update(c, "concealment-recipient", func() bool {
			if c.DeletePending || !enterworld.CharacterAlive(c) {
				return false
			}
			var ok bool
			frames, ok = rt.commitCharacterEffect(division, c, skill, token, statuseffect.StateActive, false, EffectPresentation{Phase: 2, Rider: rider}, now)
			return ok
		}) {
			continue
		}
		rt.publishBodyStatus(division, c.Name, frames)
		// A recipient whose defense changed (Heal Shield) sees its new stats.
		if skill.TimedEffect.Pinned && rt.PushCharacterFrames != nil {
			if stats, err := rt.PlayerBaseStats(division, c); err == nil {
				rt.PushCharacterFrames(division, c.Name, []wire.Frame{{Opcode: wire.OpBaseStats, Payload: stats.Encode()}})
			}
		}
	}
}

/*
================
concealmentRider

STDU milliseconds the caster's passives add to a hide (5833C8). Other timed
effects carry no duration rider.
================
*/
func (rt *Runtime) concealmentRider(division string, caster *enterworld.Character, skill enterworld.SkillRow) (uint32, bool) {
	if !skill.Concealment.Pinned || !skill.Concealment.DurationBonus {
		return 0, true
	}
	stats, _, err := rt.playerCombatStats(division, caster)
	if err != nil {
		return 0, false
	}
	return stats.SkillParameters[enterworld.ParameterStealthDuration], true
}

/*
================
startSkillCast

InitiateSkillCast's event retirement (59B745), before a fresh cast installs or
strikes. Basic attacks share this event; the caller holds the character door.
================
*/
func (rt *Runtime) startSkillCast(division string, c *enterworld.Character, now int64) {
	rt.retireEffectsOnEvent(division, c, effectEventSkillCast, now)
}

/*
===============================================================================

EARLY RETIREMENT

===============================================================================
*/

/*
==================
retireEffectsOnEvent

5A16C0 for one event bit. The caller holds c's door; the ended effects
are published at once, ahead of whatever the event itself sends, as the
native retires them before it broadcasts the cast.
==================
*/
func (rt *Runtime) retireEffectsOnEvent(division string, c *enterworld.Character, event uint8, now int64) {
	if rt.effects == nil || c == nil {
		return
	}
	rt.publishEndedEffects(division, c, rt.effects.RetireEvent(division, c.Name, event), now)
}

/*
==================
cancelEffectsOnDamage

5A15E0..5A1696 for one landed or blocked hit on c. flags are the
attack's att word 0. The keep roll holds with KeepPercent (plus the
victim's modifiers, none of which a shipped hide carries). The caller
holds c's door.
==================
*/
func (rt *Runtime) cancelEffectsOnDamage(division string, c *enterworld.Character, flags uint32, now int64) {
	if rt.effects == nil || c == nil || flags == 0 {
		return
	}
	skills := rt.deps.SkillData()
	if skills == nil {
		return
	}
	var tokens []uint32
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		row, ok := skills.SkillByID(effect.SkillID)
		if !ok || !row.DamageCancel.Present || row.DamageCancel.Mask&flags == 0 {
			continue
		}
		breaks, err := rt.effectOutcome(criticalActor{division: division, character: c.Name}, damageCancelRollKey, 100-row.DamageCancel.KeepPercent)
		if err != nil || !breaks {
			continue
		}
		tokens = append(tokens, effect.InstanceToken)
	}
	rt.publishEndedEffects(division, c, rt.effects.RetireInstances(division, c.Name, tokens), now)
}

/*
==================
retireHide

A direct RequestRetirement of the character's hide instance (char+0xC1C):
an NPC function request (510262) and a scroll's indirect skill (493D5E).
The caller holds c's door; the result reports whether a hide ended.
==================
*/
func (rt *Runtime) retireHide(division string, c *enterworld.Character, now int64) bool {
	if rt.effects == nil || c == nil {
		return false
	}
	skills := rt.deps.SkillData()
	if skills == nil {
		return false
	}
	var tokens []uint32
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		if row, ok := skills.SkillByID(effect.SkillID); ok && row.Concealment.Pinned && row.Concealment.Hide {
			tokens = append(tokens, effect.InstanceToken)
		}
	}
	ended := rt.effects.RetireInstances(division, c.Name, tokens)
	rt.publishEndedEffects(division, c, ended, now)
	return len(ended) != 0
}

/*
==================
publishEndedEffects

The character-effect update's teardown for effects already removed from
the registry: durable jobs, body status, speed, stats, then the one 0xB6A0
naming every ended instance. The caller holds c's door.
==================
*/
func (rt *Runtime) publishEndedEffects(division string, c *enterworld.Character, ended []statuseffect.Effect, now int64) {
	if len(ended) == 0 {
		return
	}
	rt.retireSkillJobs(c, ended)
	gid := enterworld.ObjectIDForCharacter(c)
	var public []wire.Frame
	stats := false
	tokens := make([]uint32, 0, len(ended))
	for _, e := range ended {
		if e.BodyStatusOwner != 0 && c.TransitionBodyStatus(domain.BodyStatusTransition{RetireOwner: e.BodyStatusOwner}) {
			public = append(public, bodyStatusFrame(gid, 0))
		}
		stats = stats || e.Modifiers.HasWrites()
		tokens = append(tokens, e.InstanceToken)
	}
	clearTransform(c, ended)
	public = append(public, rt.refreshMovementEffects(division, c, now)...)
	if payload, err := (wire.EndedEffectInstances{InstanceTokens: tokens}).Encode(); err == nil {
		public = append(public, wire.Frame{Opcode: wire.OpEndedEffectInstances, Payload: payload})
	}
	rt.publishBodyStatus(division, c.Name, public)
	if stats && rt.PushCharacterFrames != nil {
		hp, mp := rt.clampStoredGaugeToKeeper(division, c)
		rt.PushCharacterFrames(division, c.Name, rt.gaugeDropFrames(division, c, hp, mp, true))
	}
}
