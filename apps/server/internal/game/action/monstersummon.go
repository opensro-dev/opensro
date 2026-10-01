/*
===========================================================================

monstersummon.go - native skill admission and lifecycle ownership

Share the existing authority and publication owners across skill families.

===========================================================================
*/

package action

import (
	"fmt"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"sync/atomic"
)

/*
================
monsterSummonPlan

================
*/
func (rt *Runtime) monsterSummonPlan(instance monster.Instance, sample float64) (simulation.MonsterAttackPlan, bool) {
	if !monster.SummonDue(instance) {
		return simulation.MonsterAttackPlan{}, false
	}
	var rows []enterworld.SkillRow
	var specs []monster.SummonSkill
	for _, id := range instance.Ref.DefaultSkillIDs {
		row, ok := rt.deps.SkillData().SkillByID(id)
		if ok && row.Summon.Present {
			rows = append(rows, row)
			specs = append(specs, row.Summon)
		}
	}
	index, ok := monster.SelectSummon(instance, specs, sample)
	if !ok {
		return simulation.MonsterAttackPlan{}, false
	}
	row := rows[index]
	duration, valid := row.ActionLifecycleMs()
	if !valid || !row.TimingPinned || row.CoolTimeMs == 0 {
		return simulation.MonsterAttackPlan{}, false
	}
	return simulation.MonsterAttackPlan{SkillID: row.ID, Summon: true, CooldownMs: int64(row.CooldownDurationMs((monsterAbnormalContext{rt}).Param(instance, actionSpeedParameter))), ActionLifecycleMs: int64(duration)}, true
}

/*
================
monsterSummon

Called inside the existing division action transaction. The population
owner commits children; the visibility owner sends their reference/create
rows on its next pass before any child movement or combat can be emitted.
================
*/
func (rt *Runtime) monsterSummon(divisionID string, instance monster.Instance, skill enterworld.SkillRow, nowMs int64) simulation.MonsterAttackResult {
	result := simulation.MonsterAttackResult{TargetAlive: true}
	duration, valid := skill.ActionLifecycleMs()
	if !valid || !skill.TimingPinned || skill.CoolTimeMs == 0 {
		return result
	}
	ranges := make(map[uint32]float64)
	ids := []uint32{instance.Ref.RefObjID}
	for _, id := range ids {
		ref, ok := rt.Monsters.Reference(id)
		if !ok {
			return result
		}
		maximum := 0.0
		for _, skillID := range ref.DefaultSkillIDs {
			if skillID == 0 {
				continue
			}
			row, ok := rt.deps.SkillData().SkillByID(skillID)
			if !ok || !row.ActionRangePinned {
				return result
			}
			maximum = max(maximum, row.ActionRange)
		}
		ranges[id] = maximum
	}
	if _, ok := rt.Monsters.BeginSummon(divisionID, instance, skill.Summon, nowMs, nowMs+int64(skill.ActionCastingTimeMs), nowMs+int64(duration), ranges); !ok {
		return result
	}
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	result.Accepted = true
	start := wire.SkillCastUntargetedFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: instance.Gid, InstanceToken: token})
	result.Frames = append(result.Frames, simulation.Frame{Opcode: start.Opcode, Payload: start.Payload, Current: start.Current})
	key := fmt.Sprintf("@monster:%d", instance.Gid)
	if skill.ActionCastingTimeMs == 0 {
		release := wire.SkillCastReleaseFrame(token, 0)
		result.Frames = append(result.Frames, simulation.Frame{Opcode: release.Opcode, Payload: release.Payload, Current: release.Current})
	} else {
		rt.queueSkillFinalize(divisionID, key, instance.Gid, nowMs+int64(skill.ActionCastingTimeMs), wire.SkillCastReleaseFrame(token, 0))
	}
	if duration == 0 {
		finalize := wire.SkillCastFinalizeFrame(token)
		result.Frames = append(result.Frames, simulation.Frame{Opcode: finalize.Opcode, Payload: finalize.Payload, Current: finalize.Current})
	} else {
		rt.queueSkillFinalize(divisionID, key, instance.Gid, nowMs+int64(duration), wire.SkillCastFinalizeFrame(token))
	}
	return result
}
