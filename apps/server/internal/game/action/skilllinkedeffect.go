/*
===========================================================================

skilllinkedeffect.go - timed effects paired between caster and recipient

The Cleric's Force and Mental Blessings (BLESSA_STR / BLESSA_INT) carry
lnks. 5830B0 installs a source half on the caster (context mode 1) and a
recipient half on the target (mode 2), joined by a descriptor that
59DC80 admits and 5829D0 tears down as a pair. Only the recipient
receives the stri / inti writes: 594F53 skips them for a linked source.
The caster sees the source half on its own board through the private
B5ED, which names the subject; everyone sees the recipient through B419.
The Warrior's Protect (GUARDA_AGGRO) is the same pair with lkag and no
writes: the link carries the threat share commitAggression hands to the
source (594EAC, 5A03A0). The Bard's Mana Switch (BATTLAA_MPSTEAL) is the
pair with lkdh: the link hands the Bard MP from the member's dealt damage
(linkedmana.go).

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

/*
==================
acceptLinkedTargetEffect

Runs after acceptTimedTargetEffect has admitted the target: builds the
recipient's writes from the caster's HLFS / HLMI (583A65) and the target's
current parameters, checks the link against 59DC80 before the caster pays,
then installs both halves.
==================
*/
func (rt *Runtime) acceptLinkedTargetEffect(division string, c, snapshot, target, view *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64) OpResult {
	d := skill.TimedEffect
	if skill.EffectDurationMs == 0 {
		return OpResult{DiagnosticRefusal: "linked-effect-without-duration"}
	}
	casterStats, _, err := rt.playerCombatStats(division, snapshot)
	if err != nil {
		return OpResult{DiagnosticRefusal: "linked-effect-stats-unavailable"}
	}
	targetStats, _, err := rt.playerCombatStats(division, view)
	if err != nil {
		return OpResult{DiagnosticRefusal: "linked-effect-stats-unavailable"}
	}
	boost := func(b enterworld.SkillStatBoost, addend bool, key enterworld.SkillParameter, parameter uint16) combat.StatBoost {
		current, _ := targetStats.Param(parameter)
		out := combat.StatBoost{Present: b.Present, Value: b.Value, CapPercent: b.CapPercent, Current: current}
		if addend {
			out.Addend = casterStats.SkillParameters[key]
		}
		return out
	}
	writes, err := combat.StatBoostWrites(
		boost(d.Strength, d.StrengthAddend, enterworld.ParameterBlessStrength, 1),
		boost(d.Intellect, d.IntellectAddend, enterworld.ParameterBlessIntellect, 2),
	)
	if err != nil {
		return OpResult{DiagnosticRefusal: "linked-effect-writes-invalid"}
	}
	modifiers, err := statuseffect.NewModifiers(writes)
	if err != nil {
		return OpResult{DiagnosticRefusal: "linked-effect-writes-invalid"}
	}

	casterGID := enterworld.ObjectIDForCharacter(snapshot)
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	link := statuseffect.Link{
		DivisionID: division, SourceName: snapshot.Name, TargetName: view.Name,
		SourceGID: casterGID, TargetGID: cast.TargetGid,
		SourceToken: atomic.AddUint32(&rt.castTokenCounter, 1), TargetToken: atomic.AddUint32(&rt.castTokenCounter, 1),
		SkillID: skill.ID, SkillGroup: skill.Group,
		Group: d.Link.Group, MaxDistance: d.Link.MaxDistance, MaxOutgoing: d.Link.MaxOutgoing, ThreatPercent: d.Link.ThreatPercent,
		ManaHPPercent: d.Link.ManaHPPercent, ManaPercent: d.Link.ManaPercent, ManaCap: d.Link.ManaCap,
		Hunt:        d.Link.Hunt,
		StartedAtMs: now, ExpiresAtMs: now + int64(skill.EffectDurationMs),
		ClientCancelable: !skill.VoluntaryCancelBlocked, TargetModifiers: modifiers,
	}
	if code := rt.effects.LinkRefusal(link); code != 0 {
		return offensiveRefusal(code)
	}
	recipient, err := (wire.AttachedEffect{GID: link.TargetGID, SkillID: skill.ID, InstanceToken: link.TargetToken, Phase: 2}).
		Encode(wire.AttachedEffectLayout{Status: skill.SpawnStatus, Rider: skill.EffectRider})
	if err != nil {
		return OpResult{DiagnosticRefusal: "linked-effect-encode-failed"}
	}
	source, err := (wire.SourceEffect{SkillID: skill.ID, InstanceToken: link.SourceToken, SubjectGID: link.TargetGID, SubjectName: []byte(view.Name)}).
		Encode(skill.StealthDuration)
	if err != nil {
		return OpResult{DiagnosticRefusal: "linked-effect-encode-failed"}
	}

	var refusal uint16
	if !rt.deps.Update(c, "linked-effect-cost", func() bool {
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
		return OpResult{DiagnosticRefusal: "linked-effect-commit-refused"}
	}
	installed := rt.deps.Update(target, "linked-effect-install", func() bool {
		if target.DeletePending || !enterworld.CharacterAlive(target) {
			return false
		}
		return rt.effects.ApplyLink(link) == 0
	})
	var frames, private []wire.Frame
	lifetime, _ := skill.ActionLifecycleMs()
	rt.queueSkillFinalize(division, snapshot.Name, casterGID, now+int64(lifetime), wire.SkillCastFinalizeFrame(token))
	frames = append(frames, wire.SkillCastAtTargetFrame(wire.SkillCastSuccess{
		SkillId: skill.ID, CasterGid: casterGID, InstanceToken: token, OwnerOrTargetGid: cast.TargetGid,
	}))
	broadcast := append([]wire.Frame(nil), frames...)
	if installed {
		frames = append(frames, wire.Frame{Opcode: wire.OpAttachedEffect, Payload: recipient})
		broadcast = append(broadcast, wire.Frame{Opcode: wire.OpAttachedEffect, Payload: recipient})
		private = []wire.Frame{{Opcode: wire.OpSourceEffect, Payload: source}}
		frames = append(frames, private...)
		if rt.PushCharacterFrames != nil {
			if stats, err := rt.PlayerBaseStats(division, target); err == nil {
				rt.PushCharacterFrames(division, target.Name, []wire.Frame{{Opcode: wire.OpBaseStats, Payload: stats.Encode()}})
			}
		}
	}
	return OpResult{Frames: frames, Broadcast: broadcast, ActorPrivate: private}
}
