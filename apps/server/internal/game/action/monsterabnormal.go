/*
===========================================================================

monsterabnormal.go - abnormal states on monsters

Resolve caster authority, advance periodic effects, and publish committed damage
and rewards. Loot uses the shared scene publisher so its later lifecycle remains
addressable to every session that received the spawn.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
monsterAbnormalContext

monsterAbnormalContext resolves what MonsterState cannot own for the
abnormal engine: casters (by durable name, as ObjMgr_FindByID does by GID),
per-actor probability streams and parameters that include self effects.
==================
*/
type monsterAbnormalContext struct{ rt *Runtime }

/*
================
monsterActionReach

Use the attacker's live keeper for both pursuit and final hit admission.
Monster skills author their reach; player equipment cannot contribute to it.
================
*/
func (rt *Runtime) monsterActionReach(instance monster.Instance, skill enterworld.SkillRow) simulation.ActionReach {
	cut := (monsterAbnormalContext{rt}).Param(instance, 0xb7)
	return reducedActionReach(float32(skill.ActionRange), cut)
}

/*
================
caster

Resolve a player source by durable name, then reject a stale runtime GID.
================
*/
func (c monsterAbnormalContext) caster(division string, gid uint32, name string) *enterworld.Character {
	if name == "" {
		return nil
	}
	character := c.rt.findCharacter(division, name)
	if character == nil || gid != 0 && enterworld.ObjectIDForCharacter(character) != gid {
		return nil
	}
	return character
}

/*
================
SourceExists

Abnormal effects can retain either a player caster or a live monster source.
================
*/
func (c monsterAbnormalContext) SourceExists(division string, gid uint32, name string) bool {
	if character := c.caster(division, gid, name); character != nil {
		return true
	}
	if name == "" && gid != 0 && c.rt.Monsters != nil {
		_, ok := c.rt.Monsters.Get(division, gid)
		return ok
	}
	return false
}

/*
================
SourceDead

Read source life state from its authority before assigning periodic damage credit.
================
*/
func (c monsterAbnormalContext) SourceDead(division string, gid uint32, name string) bool {
	if character := c.caster(division, gid, name); character != nil {
		snapshot := c.rt.characterSnapshot(division, character)
		return snapshot == nil || !enterworld.CharacterAlive(snapshot)
	}
	if name == "" && gid != 0 && c.rt.Monsters != nil {
		instance, ok := c.rt.Monsters.Get(division, gid)
		return ok && instance.CurrentHP == 0
	}
	return false
}

/*
================
Roll

Use the actor-owned probability stream so status checks share combat ordering.
================
*/
func (c monsterAbnormalContext) Roll(division string, owner, key uint32, chance int32) bool {
	if chance <= 0 {
		return false
	}
	proc, err := c.rt.effectOutcome(criticalActor{division: division, monster: owner}, key, uint32(chance))
	return err == nil && proc
}

/*
================
Param

Param reads the monster's keeper value through the same projection combat
uses (RefObjChar base, self effects, abnormal writes).
================
*/
func (c monsterAbnormalContext) Param(instance monster.Instance, id uint16) float32 {
	if id >= 5 && id <= 12 {
		stats, err := combat.MonsterInstanceStats(instance)
		if err != nil {
			return 0
		}
		return float32([...]float64{stats.PhysicalDefense, stats.MagicalDefense, stats.ParryRate, stats.MagicalParry,
			stats.EvasionRate, stats.BlockRate, stats.HitRate, stats.CriticalRate}[id-5])
	}
	var base float32
	switch {
	case id == 0x17:
		return float32(instance.WalkSpeed())
	case id == 0x18:
		return float32(instance.RunSpeed())
	case id == actionSpeedParameter:
		return float32(instance.ActionSpeed())
	case id >= 0x1b && id <= 0x20:
		// The reference loader already converts authored columns to keeper
		// order. Swapping again gives burn the shock resistance and vice versa.
		base = float32(instance.Ref.ElementResist[id-abnormalElementResistBase])
	}
	if instance.Abnormal == nil {
		return base
	}
	definition, ok := paramkeeper.NativeDefinition(id)
	if !ok {
		definition.Maximum = 9999999
	}
	value, err := instance.Abnormal.Evaluate(id, definition, base)
	if err != nil {
		return base
	}
	return value
}

/*
================
RetiresSkill

Reference data is immutable and can be read under the monster transaction.
Self effects are installed instances, not the pending current cast; that
cast is withdrawn by the action owner's cancellation publication.
================
*/
func (c monsterAbnormalContext) RetiresSkill(skillID uint32, all bool) bool {
	if c.rt.deps.SkillData() == nil {
		return false
	}
	row, exists := c.rt.deps.SkillData().SkillByID(skillID)
	return exists && row.RetiresForAbnormal(all, false)
}

/*
================
abnormalRandom

abnormalRandom is the caster's CZoeZoeRnd stream (keyed by ECX) and the
process rand() the time bomb draws.
================
*/
type abnormalRandom struct {
	rt    *Runtime
	actor criticalActor
	err   error
}

/*
================
Chance

Remember the first random-source error while evaluating one abnormal application.
================
*/
func (r *abnormalRandom) Chance(key uint32, chance int32) bool {
	if chance <= 0 || r.err != nil {
		return false
	}
	proc, err := r.rt.effectOutcome(r.actor, key, uint32(chance))
	if err != nil {
		r.err = err
	}
	return proc
}

/*
================
Rand

Provide the native fifteen-bit random draw used by time-bomb damage.
================
*/
func (r *abnormalRandom) Rand() int32 {
	if r.err != nil {
		return 0
	}
	value, err := r.rt.CombatRoll()
	if err != nil {
		r.err = err
		return 0
	}
	return int32(value & 0x7fff)
}

/*
================
rollPlayerOnMonster

rollPlayerOnMonster ports 590680 for a player's hit on a monster target.
Temptation's Confusion is then dropped on a monster it cannot affect
(untemptableConfusion, the owner's rule).
================
*/
func (rt *Runtime) rollPlayerOnMonster(division string, c *enterworld.Character, params *abnormal.SkillParams, target monster.Instance) ([]abnormal.Record, error) {
	if params == nil || !params.Present() {
		return nil, nil
	}
	ctx := monsterAbnormalContext{rt}
	stats, _, err := rt.playerCombatStats(division, c)
	if err != nil {
		return nil, err
	}
	values := stats.SkillParameters
	in := abnormal.RollInput{
		Params:      params,
		TargetLevel: target.Ref.Level,
		TargetBonus: ctx.Param(target, 0xa9),
		CasterLevel: stats.Level,
		// CSkillManager_GetSkillModifier: the caster's learned setv dictionary.
		CasterModifier: func(key uint32) (uint32, bool) {
			slot, known := enterworld.SkillParameterFromKey(key)
			if !known {
				return 0, false
			}
			return values[slot], values[slot] != 0
		},
		SourceGID:  enterworld.ObjectIDForCharacter(c),
		TargetGID:  target.Gid,
		SourceName: c.Name,
	}
	for i := range in.TargetResist {
		in.TargetResist[i] = float32(target.Ref.ElementResist[i])
	}
	random := &abnormalRandom{rt: rt, actor: criticalActor{division: division, character: c.Name}}
	records := untemptableConfusion(target, abnormal.Roll(in, random))
	return records, random.err
}

/*
==================
monsterAbnormalFrames

monsterAbnormalFrames publishes the consequences of a committed impact or
update: withdrawn casts (570), the B2F5 stop (4A9430), the 376F speed pair
(4AA410) and the vitals mask (4A5C60 dirty bit 0x100).
==================
*/
func (rt *Runtime) monsterAbnormalFrames(division string, instance monster.Instance, effects simulation.MonsterAbnormalEffects) []wire.Frame {
	var frames []wire.Frame
	if effects.CancelActions {
		frames = append(frames, rt.interruptMonsterCast(division, instance.Gid)...)
	}
	if len(effects.EndedSkills) != 0 {
		payload, err := (wire.EndedEffectInstances{InstanceTokens: effects.EndedSkills}).Encode()
		if err != nil {
			panic(err)
		}
		frames = append(frames, wire.Frame{Opcode: wire.OpEndedEffectInstances, Payload: payload})
	}
	if effects.Halted != nil {
		frames = append(frames, wire.Frame{Opcode: 0xB2F5, Payload: simulation.MonsterCorrectionPayload(instance.Gid, *effects.Halted)})
	}
	if effects.SpeedChanged {
		frames = append(frames, wire.Frame{Opcode: 0x376F, Payload: simulation.MonsterSpeedPayload(instance)},
			wire.ActionSpeedFrame(instance.Gid, float32(instance.ActionSpeed())))
	}
	if effects.MaskChanged {
		frames = append(frames, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.MonsterAbnormalPayload(instance)})
	}
	return frames
}

/*
================
advanceMonsterAbnormals

advanceMonsterAbnormals runs 4A4390 for every monster with an active block:
expiry, damage-over-time ticks, time-bomb detonation and mask publication.
================
*/
func (rt *Runtime) advanceMonsterAbnormals(nowMs int64) []simulation.DivisionFrames {
	if rt.Monsters == nil {
		return nil
	}
	var out []simulation.DivisionFrames
	for _, candidate := range rt.Monsters.AbnormalCandidates() {
		out = append(out, rt.advanceMonsterAbnormal(candidate.DivisionID, candidate.Instance.Gid, nowMs)...)
	}
	return out
}

/*
================
advanceMonsterAbnormal

Commit periodic damage and rewards together, then publish the resulting life, loot and status changes.
================
*/
func (rt *Runtime) advanceMonsterAbnormal(division string, gid uint32, nowMs int64) []simulation.DivisionFrames {
	unlock := rt.lockDivision(division)
	defer unlock()
	plan, ok := rt.Monsters.PlanAbnormalUpdate(division, gid, nowMs)
	if !ok {
		return nil
	}
	ctx := monsterAbnormalContext{rt}
	// The first credited hit's caster owns the reward roster, as the burn
	// and bleeding owners did; uncredited ticks settle without a killer.
	var character *enterworld.Character
	for _, hit := range plan.Effects.Hits {
		if hit.Credited {
			if character = ctx.caster(division, hit.SourceGID, hit.SourceName); character != nil {
				break
			}
		}
	}
	roster := rt.monsterRewardRoster(division, character, nowMs)
	var impact simulation.MonsterDamageResult
	var settlement monsterSettlement
	commit := func() bool {
		var ok bool
		impact, ok = rt.Monsters.CommitAbnormalUpdate(plan, nowMs)
		if !ok {
			return false
		}
		if impact.Fatal {
			pose := monster.Pose{}
			if mover, found := rt.Monsters.Mover(division, gid); found {
				pose = mover.LivePoseAt(nowMs, nil)
			}
			settlement = rt.settleMonsterInsideDoor(division, character, roster, impact, pose, nowMs)
		}
		return true
	}
	var committed bool
	if len(plan.Effects.Hits) == 0 || len(roster.characters) == 0 {
		committed = commit()
	} else {
		committed = rt.deps.UpdateMany(roster.characters, "monster-abnormal", commit)
	}
	if !committed {
		return nil
	}
	var public []wire.Frame
	var private []privateFrames
	send := func(id int64, frames ...wire.Frame) {
		for i := range private {
			if private[i].id == id {
				private[i].frames = append(private[i].frames, frames...)
				return
			}
		}
		private = append(private, privateFrames{id, frames})
	}
	for _, hit := range plan.Effects.Hits {
		// 52A33D emits 3058 privately to the credited source; v1.150's
		// handler is 3128 -> 74FE80 (gid, raw damage). The detonation has its
		// own public presentation below.
		if source := ctx.caster(division, hit.SourceGID, hit.SourceName); source != nil && hit.Credited && hit.Reason == abnormalDamageOverTimeReason {
			send(source.ID, abnormalDamageFrame(gid, hit.Damage))
		}
	}
	for _, slot := range plan.Effects.Detonations {
		public = append(public, timeBombFrame(gid, slot.Damage1C, impact.Fatal))
	}
	if len(plan.Effects.Hits) > 0 {
		public = append(public, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(gid, simulation.VitalsSourceFlags(2), impact.CurrentHP)})
	}
	var recipients []RecipientFrames
	if impact.Fatal {
		public = append(public, monsterLifeDeadFrame(gid))
		public = append(public, rt.groundReferences(settlement.drops)...)
		for _, drop := range settlement.drops {
			public = append(public, wire.DropBroadcastFrames(drop.SpawnRow(true))...)
		}
		public = append(public, settlement.public...)
		if character != nil {
			send(character.ID, wire.ProgressionPrivateFrames(settlement.actorFrames)...)
		}
		recipients = settlement.others
		public = append(public, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.MonsterAbnormalPayload(impact.Instance)})
		rt.queueMonsterDefeat(division, gid, nowMs+monsterDeathPresentationRetention.Milliseconds())
	} else {
		public = append(public, rt.monsterAbnormalFrames(division, impact.Instance, plan.Effects)...)
	}
	var out []simulation.DivisionFrames
	if len(public) > 0 {
		batch := simulation.DivisionFrames{DivisionID: division}
		for _, f := range public {
			batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
		}
		out = append(out, batch)
	}
	for _, p := range private {
		batch := simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: p.id}
		for _, f := range p.frames {
			batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
		}
		out = append(out, batch)
	}
	return append(out, recipientDivisionFrames(division, recipients)...)
}

/*
================
privateFrames

Keep each character's progression packets separate from the public monster update.
================
*/
type privateFrames struct {
	id     int64
	frames []wire.Frame
}

/*
================
timeBombFrame

timeBombFrame is 59B300's detonation broadcast (research B0BC), v1.150
B3C6 type 4 (7756D0): flags (80 fatal), victim gid, u16 damage.
================
*/
func timeBombFrame(gid, damage uint32, fatal bool) wire.Frame {
	flags := uint8(0)
	if fatal {
		flags = 0x80
	}
	return wire.Frame{Opcode: 0xB3C6, Payload: wire.NewWriter(8).U8(4).U8(flags).U32(gid).U16(uint16(damage)).Payload()}
}

/*
==================
monsterImpactAbnormalFrames

monsterImpactAbnormalFrames follows committed direct impacts: withdrawn
casts, the stop, the speed pair, and the HP+mask baseline (flags 5) while
the survivor carries or has just changed a mask.
==================
*/
func (rt *Runtime) monsterImpactAbnormalFrames(division string, gid uint32, impacts []simulation.MonsterDamageResult) []wire.Frame {
	var effects simulation.MonsterAbnormalEffects
	for _, impact := range impacts {
		effects.Merge(impact.Abnormal)
	}
	instance, ok := rt.Monsters.Get(division, gid)
	if !ok || instance.CurrentHP == 0 {
		return nil
	}
	mask := effects.MaskChanged
	effects.MaskChanged = false
	frames := rt.monsterAbnormalFrames(division, instance, effects)
	if mask || instance.AbnormalMask() != 0 {
		abnormalPayload := simulation.MonsterAbnormalPayload(instance)
		payload := wire.NewWriter(16).U32(gid).U16(0x100).U8(5).U32(instance.CurrentHP).Payload()
		frames = append(frames, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: append(payload, abnormalPayload[7:]...)})
	}
	return frames
}
