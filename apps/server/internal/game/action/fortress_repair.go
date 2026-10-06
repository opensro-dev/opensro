/*
===========================================================================

fortress_repair.go - item-owned linked healing of fortress structures

49CC80 admits the target, then 59B840 enters the persistent skill handler.
The shared effect registry owns cancellation, linkedpulse owns pulse clocks,
and the existing fortress population owns HP, destruction and persistence.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"sync/atomic"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/linkedpulse"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	repairOutsideFortress  uint8  = 0xe1
	repairOutsideWar       uint8  = 0xe2
	repairWrongGuild       uint8  = 0xe5
	repairWrongRole        uint8  = 0xe6
	repairOutOfRange       uint8  = 0xea
	repairInvalidStructure uint8  = 0xed
	structureDestroyedMask uint16 = 1
)

/*
================
structureRepairTarget

Keep 49CC80's refusal order. Guild membership failure is an authority refusal
instead of reproducing the native missing-member assertion.
================
*/
func (rt *Runtime) structureRepairTarget(c *enterworld.Character, use skillItemUse, gid uint32) (monster.Instance, uint8) {
	var none monster.Instance
	if rt.Fortresses == nil || !rt.Fortresses.WarActive(use.division) {
		return none, repairOutsideWar
	}
	if rt.Monsters == nil {
		return none, 3
	}
	target, ok := rt.Monsters.Get(use.division, gid)
	if !ok {
		return none, 3
	}
	if !target.Ref.Structure || target.StructureState&structureDestroyedMask != 0 || target.CurrentHP == target.EffectiveMaxHP() {
		return none, repairInvalidStructure
	}
	definition, ok := instance.Lookup(instance.ID(domain.CharacterWorldInstance(c)).Definition())
	if !ok || !definition.Siege() {
		return none, repairOutsideFortress
	}
	fortressID, ok := rt.Fortresses.ForWorld(definition)
	if !ok {
		return none, 3
	}
	record, ok := rt.Fortresses.Get(use.division, fortressID)
	if !ok {
		return none, 3
	}
	guildID := int64(0)
	if c.GuildID != nil {
		guildID = *c.GuildID
	}
	if holder := record.Holder(); holder != 0 && holder != guildID {
		return none, repairWrongGuild
	}
	var role uint8
	if rt.Guilds != nil {
		_, members, found := rt.Guilds.Guild(use.division, guildID)
		if found {
			for _, member := range members {
				if member.CharID == c.ID {
					role = member.FortressRole
					break
				}
			}
		}
	}
	if role&uint8(use.ref.NativeFields.Get("itemParam5_2ac")) == 0 {
		return none, repairWrongRole
	}
	if _, sameWorld := rt.characterMonster(use.division, c, gid); !sameWorld {
		return none, repairInvalidStructure
	}
	from, nav := rt.liveNav(simulation.WorldKey(use.division, c.Name), c, use.nowMs)
	if rt.LineOfSight != nil && !rt.LineOfSight(from, nav, rt.monsterSpawn(use.division, target.Gid, use.nowMs)) {
		return none, repairInvalidStructure
	}
	distance := monster.NativeActorDistance(monster.Pose{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z},
		monster.Pose{RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z})
	if distance > float32(uint16(use.ref.NativeFields.Get("actionRange23c"))) {
		return none, repairOutOfRange
	}
	return target, 0
}

/*
================
useStructureRepair

49CC80 consumes after 59B840 accepts the invocation. 59B480 returns one
even when the persistent handler refuses its prerequisites, so a skill
refusal still consumes the kit and publishes an item success afterward.
================
*/
func (rt *Runtime) useStructureRepair(c *enterworld.Character, use skillItemUse, tail []byte, result *OpResult) bool {
	if len(tail) != 4 {
		return false
	}
	target, code := rt.structureRepairTarget(c, use, binary.LittleEndian.Uint32(tail))
	if code != 0 {
		*result = itemUseFailure(code)
		return false
	}
	source, ok := rt.deps.SkillData().(interface {
		SkillByCodename(string) (enterworld.SkillRow, bool)
	})
	if !ok {
		return false
	}
	skill, ok := source.SkillByCodename(use.ref.AssociatedSkillCodename)
	if !ok || !skill.StructureRepair.Pinned {
		return false
	}
	if rt.playerMotionState(use.division, c, use.nowMs) == simulation.MotionPostureNow || rt.repairPreparingAction(use.division, c.Name) {
		return false
	}
	if code := rt.skillAdmission(use.division, c, skill, use.nowMs, &admitTarget{at: rt.monsterSpawn(use.division, target.Gid, use.nowMs)}, nil, 0x1f); code != 0 {
		return rt.consumeRefusedRepair(c, use, code, result)
	}
	cost, code16 := rt.offensivePhaseCost(use.division, c, skill, use.nowMs, nil)
	if code16 != 0 {
		return rt.consumeRefusedRepair(c, use, code16, result)
	}
	d := skill.StructureRepair
	effect := linkedpulse.Effect{StructureRepair: true, Division: use.division, SourceName: c.Name,
		SourceGID: enterworld.ObjectIDForCharacter(c), TargetGID: target.Gid, SkillID: skill.ID, MaxPerTarget: 1,
		StartedMs: use.nowMs, DurationMs: d.DurationMs, PeriodMs: d.PeriodMs,
		SourceToken: atomic.AddUint32(&rt.castTokenCounter, 1), TargetToken: atomic.AddUint32(&rt.castTokenCounter, 1)}
	if owner, present := rt.characterAdmissions.Load(simulation.WorldKey(use.division, c.Name)); present {
		effect.SourceSession = owner.(populationAdmission).session
	}
	if rt.periodicEffects.Refusal(effect) != 0 {
		return false
	}
	recipient, err := (wire.AttachedEffect{GID: target.Gid, SkillID: skill.ID, InstanceToken: effect.TargetToken, Phase: 2}).Encode(
		wire.AttachedEffectLayout{Status: skill.SpawnStatus, Rider: skill.EffectRider})
	if err != nil {
		return false
	}
	own, err := (wire.SourceEffect{SkillID: skill.ID, InstanceToken: effect.SourceToken, SubjectGID: target.Gid}).Encode(skill.StealthDuration)
	if err != nil {
		return false
	}
	if !rt.Monsters.InstallMonsterLinkedEffects(use.division, []simulation.MonsterLinkedEffect{{GID: target.Gid,
		Effect: monster.AttachedSkill{SkillID: skill.ID, Token: effect.TargetToken}}}) {
		return false
	}
	installed := rt.effects.Apply(statuseffect.Effect{DivisionID: use.division, CharacterName: c.Name, OwnerGID: effect.SourceGID,
		SourceTargetGID: target.Gid, SkillID: skill.ID, SkillGroup: skill.Group, InstanceToken: effect.SourceToken,
		State: statuseffect.StateActive, Phase: 1, StartedAtMs: use.nowMs, ExpiresAtMs: use.nowMs + int64(d.DurationMs),
		DurationPresent: true, ClientCancelable: !skill.VoluntaryCancelBlocked, EventCancelMask: skill.Replacement.EventCancelMask})
	if !installed {
		rt.Monsters.RemoveMonsterLinkedEffect(use.division, target.Gid, effect.TargetToken)
		return false
	}
	if rt.periodicEffects.Install(effect) != 0 {
		panic("serialized repair installation lost admission")
	}
	rt.startSkillCast(use.division, c, skill, use.nowMs)
	rt.commitOffensivePhaseCost(use.division, c, skill, cost, use.nowMs, false)
	remaining := rt.consumeItemUseRow(c, use.row)
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	start := wire.SkillCastAtTargetFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: effect.SourceGID, InstanceToken: token, OwnerOrTargetGid: target.Gid})
	rt.queueSkillFinalize(use.division, c.Name, effect.SourceGID, use.nowMs, wire.SkillCastFinalizeFrame(token))
	public := []wire.Frame{start, {Opcode: wire.OpAttachedEffect, Payload: recipient}}
	frames := append([]wire.Frame(nil), public...)
	frames = append(frames, wire.Frame{Opcode: wire.OpSourceEffect, Payload: own}, wire.Frame{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)})
	frames = append(frames, rt.updateQuestInventory(c)...)
	*result = OpResult{Frames: frames, Broadcast: public}
	return true
}

/*
================
structureRepairSourceActive

Voluntary stop, damage cancellation, death and logout all use the existing
character effect owner. A stopped source cannot heal once more on this tick.
================
*/
func (rt *Runtime) structureRepairSourceActive(effect linkedpulse.Effect) bool {
	for _, row := range rt.effects.Snapshot(effect.Division, effect.SourceName) {
		if row.InstanceToken == effect.SourceToken {
			return !row.StopRequested
		}
	}
	return false
}

/*
================
pulseStructureRepair

5A09F0 forms a percentage of recipient maximum HP. Native structures in
this feature set have no heal amplification or recovery-reduction modifiers.
================
*/
func (rt *Runtime) pulseStructureRepair(effect linkedpulse.Effect, skill enterworld.SkillRow, target monster.Instance, now int64) OpResult {
	amount := uint32(float64(int32(target.EffectiveMaxHP())) * float64(skill.StructureRepair.HPPercent) / 100)
	healed, ok := rt.Monsters.HealStructure(effect.Division, target.Gid, amount)
	if !ok {
		return OpResult{}
	}
	rt.publishSkillHealingThreat(skillHealingThreat{division: effect.Division, caster: effect.SourceGID,
		recipient: target.Gid, category: skill.Category, amount: int64(amount)}, now)
	if healed.CurrentHP == target.CurrentHP {
		return OpResult{}
	}
	frame := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(target.Gid, 0x40, healed.CurrentHP)}
	return OpResult{Broadcast: []wire.Frame{frame}}
}

/*
================
consumeRefusedRepair

59B7A8 returns success after the handler's skill refusal. Preserve both
responses and the item debit; this is distinct from target admission.
================
*/
func (rt *Runtime) consumeRefusedRepair(c *enterworld.Character, use skillItemUse, code uint16, result *OpResult) bool {
	remaining := rt.consumeItemUseRow(c, use.row)
	*result = offensiveRefusal(code)
	result.Frames = append(result.Frames, wire.Frame{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)})
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}

/*
================
repairPreparingAction

59B840 refuses an item-started targeted skill while the prepared command
owns an ao action. Read the common preparation owner under its mutex.
================
*/
func (rt *Runtime) repairPreparingAction(division, name string) bool {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	for _, pending := range rt.pendingProjectileCasts {
		if pending.divisionID == division && pending.characterName == name {
			if row, ok := rt.deps.SkillData().SkillByID(pending.cast.ActionId); ok && row.CastGate.Ao {
				return true
			}
		}
	}
	return false
}
