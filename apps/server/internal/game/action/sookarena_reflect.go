/*
===========================================================================
sookarena_reflect.go - port-only Mirror Shell, paid damage reflected once.

The victim's door records actual HP debits; reflection commits afterwards.
Only the same generation-pinned solo population may receive reflected hits.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
sookReflectedHit
================
*/
type sookReflectedHit struct {
	source, damage uint32
}

/*
================
noteSookReflection - absorbed, blocked, self and zero-damage hits reflect nothing.
================
*/
func (o *playerAbnormalOwner) noteSookReflection(source, paid uint32) {
	if paid == 0 || source == 0 || source == enterworld.ObjectIDForCharacter(o.c) || !o.rt.sookPowerActive(o.division, o.c, sookReflect, o.now) {
		return
	}
	o.sookReflected = append(o.sookReflected, sookReflectedHit{source: source, damage: paid})
}

/*
================
reflectSookDamage - bypass Damage Surge and settle monster damage normally.
================
*/
func (rt *Runtime) reflectSookDamage(division string, c *enterworld.Character, source, damage uint32, now int64) {
	run := rt.sookRun(division, c)
	if run == nil || damage == 0 {
		return
	}
	snapshot := rt.characterSnapshotCombat(division, c)
	if snapshot == nil {
		return
	}
	for _, target := range rt.Monsters.PopulationInstances(division, run.lease, []uint16{run.center.RegionID}) {
		if target.Gid != source || target.CurrentHP == 0 {
			continue
		}
		hit, ok := rt.commitCreditedMonsterHit(division, c, snapshot, enterworld.SkillRow{}, target, combat.Result{Damage: uint32(min(uint64(wire.MaxSkillActionDamage), uint64(damage)*5)), ResultFlags: 1}, "sook-reflection", now)
		if !ok {
			return
		}
		impact := hit.impacts[0]
		out := rt.creditedHitResult(division, target, hit, []wire.Frame{wire.SkillDamageTickFrame(enterworld.ObjectIDForCharacter(c), impact.Fatal, target.Gid, []uint32{impact.Damage})}, now)
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: division, OnlyCharacterID: c.ID, Frames: simFrames(out.Broadcast)}, {DivisionID: division, OnlyCharacterID: c.ID, Frames: simFrames(out.ActorPrivate)}})
		rt.queueMonsterLegFrames(recipientDivisionFrames(division, out.Recipients))
		return
	}
}
