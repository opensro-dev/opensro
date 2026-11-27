/*
===========================================================================

monsterdeferred.go - a monster's deferred vital instruction at swing end

A skill whose pmhp block carries the deferred mode queues the instruction
on its caster when it engages; the swing's end applies it (combat's
deferred executor does the arithmetic). The v1.150 data has one carrier,
the anti-bot bomb's attack: a hit on itself of its whole current HP, so
the caster dies by its own swing with nobody credited.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
monsterDeferredHost

The deferred executor's host for one monster. The projection fills only
the vital-hit slot (combat slot 0), so the parameter-write, motion and
equipment callbacks have no populated slot to serve; they stay inert
rather than inventing a write. The hit's amount is captured and
committed by the caller, never inside the callback.
================
*/
type monsterDeferredHost struct {
	rt       *Runtime
	division string
	gid      uint32
	hp       uint32
	nowMs    int64
	damage   int32
}

/*
================
monsterDeferredHost.NowMillis
================
*/
func (h *monsterDeferredHost) NowMillis() uint32 {
	return uint32(h.nowMs)
}

/*
================
monsterDeferredHost.CurrentHP
================
*/
func (h *monsterDeferredHost) CurrentHP() uint32 {
	if live, exists := h.rt.Monsters.Get(h.division, h.gid); exists {
		return live.CurrentHP
	}
	return h.hp
}

/*
================
monsterDeferredHost.CurrentMP

No MP slot is projected (the v1.150 data authors no pmmp block), so the
MP vital is never read through this host.
================
*/
func (h *monsterDeferredHost) CurrentMP() uint32 {
	return 0
}

/*
================
monsterDeferredHost.IsPlayer
================
*/
func (h *monsterDeferredHost) IsPlayer() bool {
	return false
}

/*
================
monsterDeferredHost.ApplyHit

Records the queued hit; applyMonsterDeferredSelfHit commits it.
================
*/
func (h *monsterDeferredHost) ApplyHit(damage int32) {
	h.damage = damage
}

/*
================
monsterDeferredHost.ConsumeResources

Slots 4 and 5 are never populated by the projection.
================
*/
func (h *monsterDeferredHost) ConsumeResources(hp, mp int32) {
}

/*
================
monsterDeferredHost.WriteParameter

Only a kind-1 slot writes a parameter; the projection pins kind 0 alone.
================
*/
func (h *monsterDeferredHost) WriteParameter(id, channel uint32, value float32) {
}

/*
================
monsterDeferredHost.SendParameterStats

A monster publishes no parameter stat frame.
================
*/
func (h *monsterDeferredHost) SendParameterStats() {
}

/*
================
monsterDeferredHost.CancelActionsAndRetireSkills

Slot 6 is never populated by the projection.
================
*/
func (h *monsterDeferredHost) CancelActionsAndRetireSkills() {
}

/*
================
monsterDeferredHost.SetMotion

Slot 6 is never populated by the projection.
================
*/
func (h *monsterDeferredHost) SetMotion(seconds float32) {
}

/*
================
monsterDeferredHost.DamageEquipment

Slot 7 serves players alone and is never populated here.
================
*/
func (h *monsterDeferredHost) DamageEquipment(kind, probability uint32) {
}

/*
================
applyMonsterDeferredSelfHit

Apply the caster's queued vital instruction now that its swing has
landed, and return the frames its death publishes. A kill by this hit
belongs to nobody: the settlement runs with no credited actor, exactly
as a monster killed by another monster settles (temptation.go).

The latch the executor clears dies with the only carrier the shipped
data has (its hit is its whole current HP); a retained per-actor latch
store and its recovery readers land with the monster recovery tick,
which reads the same latch. The caller holds the division door.
================
*/
func (rt *Runtime) applyMonsterDeferredSelfHit(division string, instance monster.Instance, skill enterworld.SkillRow, now int64) []wire.Frame {
	if !skill.DeferredVital.Present || rt.Monsters == nil {
		return nil
	}
	live, exists := rt.Monsters.Get(division, instance.Gid)
	if !exists || live.CurrentHP == 0 {
		return nil
	}
	mover, exists := rt.Monsters.Mover(division, instance.Gid)
	if !exists {
		return nil
	}
	block := [4]uint32{skill.DeferredVital.DurationMs, skill.DeferredVital.Base, skill.DeferredVital.Percent, 0}
	var descriptor combat.DeferredInstructions
	descriptor.Parameters[0] = &block
	descriptor.StartedAt = uint32(now)
	host := &monsterDeferredHost{rt: rt, division: division, gid: instance.Gid, hp: live.CurrentHP, nowMs: now}
	latches := combat.NewDeferredVitalLatches()
	descriptor.Advance(host, &latches)
	if host.damage <= 0 {
		return nil
	}
	roster := rt.monsterRewardRoster(division, nil, now)
	var settlement monsterSettlement
	var fatal bool
	commit := func() bool {
		plans := []simulation.MonsterDamagePlan{{GID: instance.Gid, ExpectedHP: live.CurrentHP, Damage: uint32(host.damage)}}
		results := rt.Monsters.ApplyDamageSequence(division, instance.Gid, live.CurrentHP, plans)
		if len(results) == 0 {
			return false
		}
		if last := results[len(results)-1]; last.Fatal {
			fatal = true
			settlement = rt.settleMonsterInsideDoor(division, nil, roster, last, mover.LivePoseAt(now, nil), now)
		}
		return true
	}
	if len(roster.characters) == 0 {
		if !commit() {
			return nil
		}
	} else if !rt.deps.UpdateManyDeferred(roster.characters, "monster-deferred-self-hit", commit) {
		return nil
	}
	if !fatal {
		return nil
	}
	rt.queueMonsterDefeat(division, instance.Gid, now+monsterDeathPresentationRetention.Milliseconds())
	rt.announceSealDrops(division, live.Ref.Name, settlement.drops, now)
	frames := rt.monsterKillBurst(instance.Gid, settlement.drops)
	frames = append(frames, settlement.public...)
	rt.queueMonsterLegRecipients(division, settlement.others)
	return frames
}
