/*
===========================================================================

concealment.go - hiding, detection, and the effects that end them

A hide is an ordinary installed effect whose row carries a hide block:
commitCharacterEffect sets body status 6 or 7 and the speed cut. What
this file adds is who else receives a cast (efr kind 1 recipients) and
the two ways effects end early:

	event   CSkillManager_RetireEffectsForEventMask (5A16C0): an
	        accepted move (bit 1, CGObjChar_HandleMoveCommand 4B0EA0)
	        or a skill cast (bit 2, from InitiateSkillCast 59B745) ends
	        every effect whose skc event mask holds the bit
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
	effectEventMove      uint8 = 1 // an accepted move command (4B0EA0)
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
		if !rt.auraReplacementAllowed(division, c, skill, false) {
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
skillDurationRider

RPBU milliseconds extend a poison coating (5833BE); otherwise STDU extends
a hide (5833C8). The same rider owns the server lifetime and client timer.
================
*/
func (rt *Runtime) skillDurationRider(division string, caster *enterworld.Character, skill enterworld.SkillRow) (uint32, bool) {
	parameter := enterworld.ParameterPoisonCoatingDuration
	if !skill.Imbue.Poison {
		parameter = enterworld.ParameterStealthDuration
		if !skill.Concealment.Pinned || !skill.Concealment.DurationBonus {
			return 0, true
		}
	}
	if !skill.EffectRider {
		return 0, true
	}
	stats, _, err := rt.playerCombatStats(division, caster)
	if err != nil {
		return 0, false
	}
	return stats.SkillParameters[parameter], true
}

/*
================
startSkillCast

InitiateSkillCast (59B480) before a fresh cast installs or strikes: the
caster's walk stops (haltCasterWalk, 59B5F6), then the event retirement
(59B745). Basic attacks share this event; the caller holds the character
door.
================
*/
func (rt *Runtime) startSkillCast(division string, c *enterworld.Character, skill enterworld.SkillRow, now int64) {
	rt.haltCasterWalk(division, c, skill, now)
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
RetireMoveEffects

The movement event of CGObjChar_HandleMoveCommand (4B0EA0): once a player's
move is accepted, every effect whose skc event mask holds bit 1 ends. The
movement runtime calls it after releasing the character lock.
==================
*/
func (rt *Runtime) RetireMoveEffects(division, name string, now int64) {
	character := rt.findCharacter(division, name)
	if character == nil {
		return
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	rt.deps.Update(character, "move-event", func() bool {
		rt.retireEffectsOnEvent(division, character, effectEventMove, now)
		return true
	})
}

/*
==================
cancelEffectsOnDamage

5A15E0..5A1696 for one landed or blocked hit on c. flags are the
attack's att word 0. Each effect whose skc damage mask the hit matches
ends with 100 - damageKeepPercent, rolled on the victim's probability
stream.

A party aura's child is never cut by a hit on the member holding it.
Owner's rule: a tambour, instrument march or dance is cut when its Bard
receives an attack; the Bard's own instance carries the roll, and its end
retires every child at the aura's next update. Inferred: a hit on a member
leaves that member's copy alone, since the rule names the Bard only.

The caller holds c's door.
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
		if !ok || !row.DamageCancel.Present || row.DamageCancel.Mask&flags == 0 || effect.AuraParentToken != 0 {
			continue
		}
		keep, ok := rt.damageKeepPercent(division, c, row)
		if !ok {
			continue
		}
		breaks, err := rt.effectOutcome(criticalActor{division: division, character: c.Name}, damageCancelRollKey, enterworld.FullKeepPercent-keep)
		if err != nil || !breaks {
			continue
		}
		tokens = append(tokens, effect.InstanceToken)
	}
	rt.publishEndedEffects(division, c, rt.effects.RetireInstances(division, c.Name, tokens), now)
}

/*
==================
damageKeepPercent

The chance one masked hit leaves an effect of row on c running, as
CSkillManager_ProcessDamageEffects (5A160A..5A1691) forms it: skc word 2,
plus c's learned MUCR when the row reads getv MUCR (+0x548), plus c's
learned DSER when it reads getv DSER (+0x54C), held at 100. The roll then
ends the effect with 100 - keep. DSCR (+0x550) has no reader and adds
nothing.

ok is false when c's stats cannot be read; the hit then ends nothing.
==================
*/
func (rt *Runtime) damageKeepPercent(division string, c *enterworld.Character, row enterworld.SkillRow) (uint32, bool) {
	keep := row.DamageCancel.KeepPercent
	addends := [...]enterworld.SkillParameter{enterworld.ParameterMusicCutResist, enterworld.ParameterDanceRange}
	if !row.Attack.Parameters.Has(addends[0]) && !row.Attack.Parameters.Has(addends[1]) {
		return keep, true
	}
	stats, _, err := rt.playerCombatStats(division, c)
	if err != nil {
		return 0, false
	}
	for _, key := range addends {
		if row.Attack.Parameters.Has(key) {
			keep += stats.SkillParameters[key]
		}
	}
	return min(keep, enterworld.FullKeepPercent), true
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
	public, actor := rt.finishEndedEffects(division, c, ended, now)
	rt.publishBodyStatus(division, c.Name, public)
	if len(actor) != 0 && rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, c.Name, actor)
	}
}

/*
================
finishEndedEffects

Commit teardown through existing lifecycle owners and collect its packets.
Status callbacks call this under the character door, then publish after the
door closes; event-driven callers retain their existing publication wrapper.
================
*/
func (rt *Runtime) finishEndedEffects(division string, c *enterworld.Character, ended []statuseffect.Effect, now int64) (public, actor []wire.Frame) {
	if len(ended) == 0 {
		return nil, nil
	}
	rt.retireSkillJobs(c, ended)
	gid := enterworld.ObjectIDForCharacter(c)
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
	if stats {
		hp, mp := rt.clampStoredGaugeToKeeper(division, c)
		actor = rt.gaugeDropFrames(division, c, hp, mp, true)
	}
	return public, actor
}
