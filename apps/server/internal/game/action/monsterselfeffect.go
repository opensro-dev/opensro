/*
===========================================================================

monsterselfeffect.go - monster self buffs and their child effects

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"sync/atomic"
)

/*
================
releaseMonsterSelfEffect

Called under the division action door, after enemy contact admission or
owned self-cast preparation. The recipient is the caster, not the enemy.

The buff attaches under its own token, so its expiry (B6A0) never names the
cast. The cast closes with its own mode-2 B505 when its action ends, as a
player's timed self effect does (acceptTimedSelfEffect). The client keeps a
cast until it is cancelled (CIDecoSkill_Update 8DC440 returns 0 only after
+D0), so without the close every self-buff stayed in each observer's cast
table until the monster left view.
================
*/
func (rt *Runtime) releaseMonsterSelfEffect(division string, instance monster.Instance, skill enterworld.SkillRow, now int64, pending *pendingMonsterCast) simulation.MonsterAttackResult {
	result := simulation.MonsterAttackResult{TargetAlive: true}
	if !skill.MonsterSelfEffect.Pinned {
		return result
	}
	token := uint32(0)
	if pending != nil {
		token = pending.token
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	effectToken := atomic.AddUint32(&rt.castTokenCounter, 1)
	effect := monster.SelfEffect{SkillID: skill.ID, Token: effectToken, Tag: skill.MonsterSelfEffect.Tag, First: skill.MonsterSelfEffect.First, Second: skill.MonsterSelfEffect.Second, StartedAtMs: now, UntilMs: now + int64(skill.EffectDurationMs)}
	payload, err := (wire.AttachedEffect{GID: instance.Gid, SkillID: skill.ID, InstanceToken: effectToken, Phase: 2}).Encode(wire.AttachedEffectLayout{})
	if err != nil {
		panic(err)
	}
	if !rt.Monsters.InstallMonsterSelfEffect(division, instance.Gid, effect, now) {
		return result
	}
	appendFrame := func(f wire.Frame) {
		result.Frames = append(result.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
	}
	if pending == nil {
		appendFrame(wire.SkillCastSelfFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: instance.Gid, InstanceToken: token}))
	}
	appendFrame(wire.SkillCastReleaseFrame(token, 0))
	appendFrame(wire.Frame{Opcode: wire.OpAttachedEffect, Payload: payload})
	rt.queueSkillFinalize(division, monsterCastOwner(instance.Gid), instance.Gid, now+int64(skill.ActionDurationMs), wire.SkillCastFinalizeFrame(token))
	result.Accepted = true
	return result
}

func (rt *Runtime) retireMonsterSelfEffects(now int64) []simulation.DivisionFrames {
	if rt.Monsters == nil {
		return nil
	}
	var frames []simulation.DivisionFrames
	for _, row := range rt.Monsters.RetireMonsterSelfEffects(now) {
		payload, err := (wire.EndedEffectInstances{InstanceTokens: row.Tokens}).Encode()
		if err != nil {
			panic(err)
		}
		// The reference server sends the retired-buff
		// packet through its nearby-session broadcast. The
		// summon is absent from shownMonsters only on the before-hook that
		// creates it; RunMonsterLeg admits it before the next hook.
		frames = append(frames, simulation.DivisionFrames{DivisionID: row.DivisionID, SourceGID: row.GID, Frames: []simulation.Frame{{Opcode: wire.OpEndedEffectInstances, Payload: payload}}})
	}
	return frames
}
