/*
===========================================================================

hostiledebuff.go - Vital Spot: a timed buff instance cast on an enemy

The caster pays the offensive cost and the target receives the instance
(skillhostiledebuff.go). A monster holds it in its target-effect slots,
whose terd/thrd word the stat owner subtracts (monster_self_effect.go); a
player holds it in the status-effect registry, whose parameter writes do
the same (activeeffect.go). The cast deals no damage: tant is the
aggression it leaves on a monster, and on a player it is a hostile
execution that registers the attack as Scorn does.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
debuffCast

One admitted Vital Spot release. The compiler pins zero preparation, so
every release is immediate and owns its cast token.
================
*/
type debuffCast struct {
	division            string
	character, snapshot *enterworld.Character
	skill               enterworld.SkillRow
	target              uint32
	player              *enterworld.Character
	now                 int64
}

/*
================
chargeDebuff

The caster's half of the transaction, shared by both recipients.
================
*/
func (rt *Runtime) chargeDebuff(c debuffCast) (OpResult, bool) {
	var refusal uint16
	if !rt.deps.Update(c.character, "skill-hostile-debuff", func() bool {
		if !enterworld.CharacterAlive(c.character) || !enterworld.SkillLearned(c.character, c.skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(c.division, c.character, c.skill, c.now, nil)
		refusal = code
		if code != 0 {
			return false
		}
		rt.startSkillCast(c.division, c.character, c.skill, c.now)
		rt.commitOffensivePhaseCost(c.division, c.character, c.skill, cost, c.now, false)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), false
		}
		return OpResult{}, false
	}
	return OpResult{}, true
}

/*
================
debuffOpen

The cast frame names the target as a single non-damaging impact, as the
taunt release does, and the release closes the action.
================
*/
func (rt *Runtime) debuffOpen(c debuffCast) (wire.Frame, uint32, uint32) {
	gid := enterworld.ObjectIDForCharacter(c.snapshot)
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	cast := wire.SkillCastSuccess{SkillId: c.skill.ID, CasterGid: gid, InstanceToken: token}
	open := wire.SkillCastAreaFrame(cast, c.target, []wire.SkillAreaTarget{{GID: c.target,
		Impacts: []wire.SkillCastTargetImpact{{ResultFlags: 1}}}})
	rt.queueSkillFinalize(c.division, c.snapshot.Name, gid, c.now, wire.SkillCastReleaseFrame(token, c.target))
	rt.queueSkillCastClose(c.division, c.snapshot.Name, gid, token, c.skill, 0, c.now+int64(c.skill.ActionDurationMs))
	return open, gid, token
}

/*
================
debuffMonster

The monster keeps the word for the authored duration; a recast of the same
word replaces the earlier instance, whose token retires on the wire.
================
*/
func (rt *Runtime) debuffMonster(c debuffCast) (OpResult, skillCastDecision) {
	if out, ok := rt.chargeDebuff(c); !ok {
		return out, skillCastRefused
	}
	open, gid, _ := rt.debuffOpen(c)
	frames := []wire.Frame{open}
	debuff := c.skill.HostileDebuff
	tag, value := debuff.Word()
	effectToken := atomic.AddUint32(&rt.castTokenCounter, 1)
	effect := monster.SelfEffect{SkillID: c.skill.ID, Token: effectToken, Tag: tag, First: value,
		StartedAtMs: c.now, UntilMs: c.now + int64(debuff.DurationMs)}
	if replaced, ok := rt.Monsters.InstallMonsterTargetEffect(c.division, c.target, effect, c.now); ok {
		if replaced != 0 {
			ended, err := (wire.EndedEffectInstances{InstanceTokens: []uint32{replaced}}).Encode()
			if err != nil {
				panic(err)
			}
			frames = append(frames, wire.Frame{Opcode: wire.OpEndedEffectInstances, Payload: ended})
		}
		payload, err := (wire.AttachedEffect{GID: c.target, SkillID: c.skill.ID, InstanceToken: effectToken, Phase: 2}).Encode(
			wire.AttachedEffectLayout{Status: c.skill.SpawnStatus, Rider: c.skill.EffectRider})
		if err != nil {
			panic(err)
		}
		frames = append(frames, wire.Frame{Opcode: wire.OpAttachedEffect, Payload: payload})
	}
	rt.commitAggression(c.division, c.target, simulation.HostilityEvent{Attacker: gid, Aggression: int32(debuff.ThreatFlat)}, c.now)
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceSkillRecovery, rt.publishedVitals(c.division, c.character))}
	return OpResult{Frames: append(frames, vitals), Broadcast: frames, ActorPrivate: []wire.Frame{vitals}}, skillCastAccepted
}

/*
================
debuffPlayer

The registry installs the instance like any buff of the skill's group, so
a recast replaces it there. The recipient enters battle and both sides
register the attack, as acceptForcedTarget does for Scorn.
================
*/
func (rt *Runtime) debuffPlayer(c debuffCast) (OpResult, skillCastDecision) {
	if c.player == nil {
		return OpResult{}, skillCastRefused
	}
	if out, ok := rt.chargeDebuff(c); !ok {
		return out, skillCastRefused
	}
	open, gid, _ := rt.debuffOpen(c)
	frames := []wire.Frame{open}
	var private []RecipientFrames
	var installed []wire.Frame
	owner := rt.newPlayerAbnormalOwner(c.division, c.player, c.now)
	if rt.deps.Update(c.player, "hostile-debuff-install", func() bool {
		if c.player.DeletePending || !enterworld.CharacterAlive(c.player) {
			return false
		}
		var ok bool
		installed, ok = rt.commitCharacterEffect(c.division, c.player, c.skill, atomic.AddUint32(&rt.castTokenCounter, 1),
			statuseffect.StateActive, false, EffectPresentation{Phase: 2}, c.now)
		if ok {
			installed = append(installed, rt.enterBattleState(c.division, c.player, c.now)...)
			rt.endJobActivation(c.division, c.player)
			installed = append(installed, rt.registerPlayerAttacked(c.division, c.player, c.character, c.now)...)
			owner.applyHit(abnormal.HitContext{Attack: true}, nil)
		}
		return ok
	}) {
		frames = append(frames, installed...)
		rt.deps.Update(c.character, "hostile-debuff-hostility", func() bool {
			frames = append(frames, rt.registerPlayerAttack(c.division, c.character, c.player, c.now)...)
			return true
		})
		publication := rt.playerAbnormalPublication(c.division, c.player, owner)
		frames = append(frames, publication.public...)
		if len(publication.actor) != 0 {
			private = append(private, RecipientFrames{CharacterID: c.player.ID, Frames: publication.actor})
		}
	}
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceSkillRecovery, rt.publishedVitals(c.division, c.character))}
	return OpResult{Frames: append(frames, vitals), Broadcast: frames, ActorPrivate: []wire.Frame{vitals}, Recipients: private}, skillCastAccepted
}
