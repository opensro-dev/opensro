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
cast until it is cancelled (client CIDecoSkill_Update 8DC440 returns 0 only after
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
	if !rt.Monsters.InstallMonsterSelfEffect(division, instance.Gid, effect, now) {
		return result
	}
	var frames []wire.Frame
	if pending == nil {
		frames = append(frames, wire.SkillCastSelfFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: instance.Gid, InstanceToken: token}))
	}
	frames = append(frames, rt.landMonsterSelfCast(division, instance.Gid, skill, token, effectToken, now)...)
	for _, f := range frames {
		result.Frames = append(result.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
	}
	result.Accepted = true
	return result
}

/*
================
landMonsterSelfCast

The wire side of a monster's self cast landing, once its buff is
installed: the cast's release, the buff attached under its own token,
and the cast's close queued for when its action ends.
================
*/
func (rt *Runtime) landMonsterSelfCast(division string, gid uint32, skill enterworld.SkillRow, token, effectToken uint32, now int64) []wire.Frame {
	payload, err := (wire.AttachedEffect{GID: gid, SkillID: skill.ID, InstanceToken: effectToken, Phase: 2}).Encode(wire.AttachedEffectLayout{})
	if err != nil {
		panic(err)
	}
	rt.queueSkillFinalize(division, monsterCastOwner(gid), gid, now+int64(skill.ActionDurationMs), wire.SkillCastFinalizeFrame(token))
	return []wire.Frame{wire.SkillCastReleaseFrame(token, 0), {Opcode: wire.OpAttachedEffect, Payload: payload}}
}

/*
================
monsterEventCast

A self cast an event begins on a monster (the Roc's hour mark), not its
AI: begun at once, landing when its casting time has passed.
================
*/
type monsterEventCast struct {
	gid         uint32
	skill       enterworld.SkillRow
	token       uint32
	releaseAtMs int64
}

/*
================
beginMonsterStackingCast

Starts an event's stacking self buff (the v1.188 server's indirect
skill begin, no target): the observers see the cast begin, and it lands
after the row's casting time (landMonsterStackingCast). Only a row the
mom parse pinned is cast. The caller holds the division lock.
================
*/
func (rt *Runtime) beginMonsterStackingCast(gid uint32, codename string, now int64) (monsterEventCast, wire.Frame, bool) {
	skill, found := rt.skillByCodename(codename)
	if !found || !skill.MonsterStackingEffect.Pinned {
		return monsterEventCast{}, wire.Frame{}, false
	}
	cast := monsterEventCast{gid: gid, skill: skill, token: atomic.AddUint32(&rt.castTokenCounter, 1),
		releaseAtMs: now + int64(skill.ActionCastingTimeMs)}
	return cast, wire.SkillCastSelfFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: gid, InstanceToken: cast.token}), true
}

/*
================
landMonsterStackingCast

Installs a begun stacking cast's buff on its still-living caster and
answers the frames its observers see. False when the caster died or
already carries the buff. The caller holds the division lock.
================
*/
func (rt *Runtime) landMonsterStackingCast(division string, cast monsterEventCast, now int64) ([]wire.Frame, bool) {
	effectToken := atomic.AddUint32(&rt.castTokenCounter, 1)
	stacking := cast.skill.MonsterStackingEffect
	effect := monster.SelfEffect{SkillID: cast.skill.ID, Token: effectToken, Tag: enterworld.TagMonsterStacking,
		First: stacking.PeriodMs, Second: stacking.StepValue, StartedAtMs: now}
	if rt.Monsters == nil || !rt.Monsters.InstallMonsterStackingEffect(division, cast.gid, effect, now) {
		return nil, false
	}
	return rt.landMonsterSelfCast(division, cast.gid, cast.skill, cast.token, effectToken, now), true
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
