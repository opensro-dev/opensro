/*
===========================================================================

concealment.go - hiding, detection, and the effects that end them

A hide is an ordinary installed effect whose row carries a hide block:
commitCharacterEffect sets body status 6 or 7 and the speed cut. What
this file adds is who else receives a cast (efr kind 1 recipients) and
the two ways effects end early:

	event   the reference server's event-mask retirement: an
	        accepted move (bit 1, the move command handler)
	        or a skill cast (bit 2, from the cast start) ends
	        every effect whose skc event mask holds the bit
	damage  the reference server's damage-effect pass: a landed or
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
	effectEventMove      uint8 = 1 // an accepted move command
	effectEventSkillCast uint8 = 2 // InitiateSkillCast, and a pet's attack command
	effectEventBerserk   uint8 = 4 // berserk request
)

// damageCancelRollKey is the probability stream the reference server rolls on the victim.
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
to the party selector: center distance, no body-radius
expansion and no target-cap read. This includes March, Heal Shield and party
invisibility. Other selections retain the around-source walk.
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
			// The reference server compares the unsigned radius with the x87 length;
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

The cast release's recipient installs, through the immediate door: a
handler's answer follows the commit. See installRecipientEffectsThrough.
==================
*/
func (rt *Runtime) installRecipientEffects(division string, recipients []*enterworld.Character, skill enterworld.SkillRow, rider uint32, now int64) []*enterworld.Character {
	return rt.installRecipientEffectsThrough(rt.deps.Update, division, recipients, skill, rider, now)
}

/*
==================
installTickRecipientEffects

The skill-object tick's recipient installs, through the deferred door.
The rule (#2294): no immediate commit while the tick holds the
publication gates - each one would pay its own store transaction
inside the pass, beside the retirements' deferred door. The tick's
batch commits every recipient in its one transaction instead.
==================
*/
func (rt *Runtime) installTickRecipientEffects(division string, recipients []*enterworld.Character, skill enterworld.SkillRow, rider uint32, now int64) []*enterworld.Character {
	return rt.installRecipientEffectsThrough(rt.deps.UpdateDeferred, division, recipients, skill, rider, now)
}

/*
==================
installRecipientEffectsThrough

The recipient loop of the reference server's persistent cast: every recipient gets its
own instance, in execution context mode 2, if buff replacement admits it.
Runs after the caster's own door; each recipient is published to itself
and its observers. Returns the recipients that received an instance.
The door is the caller's lane: immediate from a handler, deferred from
the tick.
==================
*/
func (rt *Runtime) installRecipientEffectsThrough(door func(*enterworld.Character, string, func() bool) bool, division string, recipients []*enterworld.Character, skill enterworld.SkillRow, rider uint32, now int64) []*enterworld.Character {
	var installed []*enterworld.Character
	for _, c := range recipients {
		if !rt.auraReplacementAllowed(division, c, skill, false) {
			continue
		}
		token := atomic.AddUint32(&rt.castTokenCounter, 1)
		var frames []wire.Frame
		if !door(c, "concealment-recipient", func() bool {
			if c.DeletePending || !enterworld.CharacterAlive(c) {
				return false
			}
			var ok bool
			frames, ok = rt.commitCharacterEffect(division, c, skill, token, statuseffect.StateActive, false, EffectPresentation{Phase: 2, Rider: rider}, now)
			return ok
		}) {
			continue
		}
		installed = append(installed, c)
		rt.publishBodyStatus(division, c.Name, frames)
		// A recipient whose defense changed (Heal Shield) sees its new stats.
		if skill.TimedEffect.Pinned && rt.PushCharacterFrames != nil {
			if stats, err := rt.PlayerBaseStats(division, c); err == nil {
				rt.PushCharacterFrames(division, c.Name, []wire.Frame{{Opcode: wire.OpBaseStats, Payload: stats.Encode()}})
			}
		}
	}
	return installed
}

/*
================
skillDurationRider

RPBU milliseconds extend a poison coating; otherwise STDU extends
a hide. The same rider owns the server lifetime and client timer.
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

The reference server's cast start, before a fresh cast installs or strikes: the
caster's walk stops (haltCasterWalk), then the event retirement
runs. Basic attacks share this event; the caller holds the character
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

The reference server's retirement for one event bit. The caller holds c's door; the ended effects
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

The movement event of the reference server's move command handler: once a player's
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

The reference server's damage-effect pass for one landed or blocked hit on c. flags are the
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
the reference server's damage-effect pass forms it: skc word 2,
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
an NPC function request and a scroll's indirect skill.
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
==================
endEffectsInTickDoor

publishEndedEffects for a tick sweep that holds only the division lock (a
field's leaver, a retired trap's planter). The teardown writes the saved
record, so it runs in c's tick-batched door (#1836); publication runs after
the door closes.
==================
*/
func (rt *Runtime) endEffectsInTickDoor(division string, c *enterworld.Character, ended []statuseffect.Effect, label string, now int64) {
	if len(ended) == 0 {
		return
	}
	var public, actor []wire.Frame
	rt.deps.UpdateDeferred(c, label, func() bool {
		public, actor = rt.finishEndedEffects(division, c, ended, now)
		return true
	})
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
