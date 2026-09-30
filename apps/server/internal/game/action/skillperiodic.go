/*
===========================================================================

skillperiodic.go - release and damage authority for linked hostile effects

Cast admission and preparation belong to the ordinary action lane. Release
installs one pair per selected monster; the pulse owner only supplies clocks.
Health, abnormalities, hostility and kill rewards retain their existing owners.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/linkedpulse"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
periodicCast

All fields describe the same admitted release instant. This is not another
mutable cast owner; pendingProjectileCast retains preparation identity.
================
*/
type periodicCast struct {
	division            string
	character, snapshot *enterworld.Character
	skill               enterworld.SkillRow
	cast                wire.SkillAction
	target              monster.Instance
	attacker            combat.Stats
	now                 int64
	release             *pendingProjectileCast
}

/*
================
installPeriodicCast

58371C creates independent linked pairs for the selected recipients. Resolve
all packet layouts and admissions before the cost commit; never pay per pulse.
The division operation lock serializes installation with retirement.
================
*/
func (rt *Runtime) installPeriodicCast(p periodicCast) (OpResult, skillCastDecision) {
	d := p.skill.TimedEffect.Periodic
	targets := []monster.Instance{p.target}
	if d.Area.Radius != 0 {
		targets = rt.areaVictims(p.division, p.snapshot, p.target, d.Area, areaBaseRange(p.skill, p.attacker), p.now)
	}
	duration := d.DurationMs
	if d.Attack.Parameters.Has(enterworld.ParameterDotDuration) {
		duration = linkedpulse.Duration(duration, p.attacker.SkillParameters[enterworld.ParameterDotDuration])
	}
	var session uint64
	if owner, ok := rt.characterAdmissions.Load(simulation.WorldKey(p.division, p.snapshot.Name)); ok {
		session = owner.(populationAdmission).session
	}
	caster := enterworld.ObjectIDForCharacter(p.snapshot)
	var effects []linkedpulse.Effect
	var public, private []wire.Frame
	for _, target := range targets {
		effect := linkedpulse.Effect{Division: p.division, SourceName: p.snapshot.Name, SourceSession: session,
			SourceGID: caster, TargetGID: target.Gid, SkillID: p.skill.ID, LinkGroup: d.Link.Group,
			MaxPerTarget: d.Link.MaxOutgoing, StartedMs: p.now, DurationMs: duration, PeriodMs: d.PeriodMs,
			SourceToken: atomic.AddUint32(&rt.castTokenCounter, 1), TargetToken: atomic.AddUint32(&rt.castTokenCounter, 1)}
		if code := rt.periodicEffects.Refusal(effect); code != 0 {
			if target.Gid == p.target.Gid {
				return offensiveRefusal(code), skillCastRefused
			}
			continue
		}
		recipient, err := (wire.AttachedEffect{GID: target.Gid, SkillID: p.skill.ID, InstanceToken: effect.TargetToken,
			Phase: 2, Rider: duration - d.DurationMs}).Encode(wire.AttachedEffectLayout{Status: p.skill.SpawnStatus, Rider: p.skill.EffectRider})
		if err != nil {
			return OpResult{DiagnosticRefusal: "periodic-recipient-layout"}, skillCastRefused
		}
		source, err := (wire.SourceEffect{SkillID: p.skill.ID, InstanceToken: effect.SourceToken, SubjectGID: target.Gid}).Encode(p.skill.StealthDuration)
		if err != nil {
			return OpResult{DiagnosticRefusal: "periodic-source-layout"}, skillCastRefused
		}
		effects = append(effects, effect)
		public = append(public, wire.Frame{Opcode: wire.OpAttachedEffect, Payload: recipient})
		private = append(private, wire.Frame{Opcode: wire.OpSourceEffect, Payload: source})
	}
	if len(effects) == 0 {
		return OpResult{}, skillCastRefused
	}
	var refusal uint16
	if !rt.deps.Update(p.character, "periodic-skill-release", func() bool {
		cost, code := rt.offensivePhaseCost(p.division, p.character, p.skill, p.now, p.release)
		if refusal = code; code != 0 || !enterworld.CharacterAlive(p.character) {
			return false
		}
		// No external owner can install into this division while its action
		// lock is held. Preflight every pair before the non-refusing debit.
		for _, effect := range effects {
			if refusal = rt.periodicEffects.Refusal(effect); refusal != 0 {
				return false
			}
		}
		projections := make([]simulation.MonsterLinkedEffect, 0, len(effects))
		for _, effect := range effects {
			projections = append(projections, simulation.MonsterLinkedEffect{GID: effect.TargetGID,
				Effect: monster.AttachedSkill{SkillID: effect.SkillID, Token: effect.TargetToken}})
		}
		if !rt.Monsters.InstallMonsterLinkedEffects(p.division, projections) {
			return false
		}
		for _, effect := range effects {
			if code := rt.periodicEffects.Install(effect); code != 0 {
				panic("periodic effect lost serialized admission")
			}
		}
		if p.release == nil {
			rt.startSkillCast(p.division, p.character, p.now)
		}
		rt.commitOffensivePhaseCost(p.division, p.character, p.skill, cost, p.now, p.release != nil)
		return true
	}) {
		return offensiveRefusal(refusal), skillCastRefused
	}
	token := uint32(0)
	var start wire.Frame
	if p.release != nil {
		token = p.release.token
		start = wire.SkillCastReleaseFrame(token, p.target.Gid)
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
		start = wire.SkillCastAtTargetFrame(wire.SkillCastSuccess{SkillId: p.skill.ID, CasterGid: caster,
			InstanceToken: token, OwnerOrTargetGid: p.target.Gid})
		rt.queueSkillFinalize(p.division, p.snapshot.Name, caster, p.now, wire.SkillCastReleaseFrame(token, p.target.Gid))
	}
	rt.queueSkillCastClose(p.division, p.snapshot.Name, caster, token, p.skill, 0, p.now+int64(p.skill.ActionDurationMs))
	public = append([]wire.Frame{start}, public...)
	private = append(private, wire.Frame{Opcode: simulation.OpVitalsUpdate,
		Payload: simulation.VitalsRefreshPayload(caster, rt.publishedVitals(p.division, p.character))})
	frames := append(append([]wire.Frame(nil), public...), private...)
	return OpResult{Frames: frames, Broadcast: public, ActorPrivate: private}, skillCastAccepted
}

/*
================
periodicRetirement

Both halves retire by instance identity. Peers may not have the private source
half, but native counted teardown ignores absent tokens.
================
*/
func (rt *Runtime) periodicRetirement(effect linkedpulse.Effect) []wire.Frame {
	if _, ok := rt.periodicEffects.Remove(effect.Division, effect.SourceToken); !ok {
		return nil
	}
	rt.Monsters.RemoveMonsterLinkedEffect(effect.Division, effect.TargetGID, effect.TargetToken)
	payload, err := (wire.EndedEffectInstances{InstanceTokens: []uint32{effect.SourceToken, effect.TargetToken}}).Encode()
	if err != nil {
		panic(err)
	}
	return []wire.Frame{{Opcode: wire.OpEndedEffectInstances, Payload: payload}}
}

/*
================
advancePeriodicEffects

Registry snapshots release their mutex before any division or character lock.
Retirement and a pulse share the same action transaction; expiry follows the
last permitted pulse, as 5830B0 does.
================
*/
func (rt *Runtime) advancePeriodicEffects(now int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, step := range rt.periodicEffects.Frame(now) {
		effect := step.Effect
		unlock := rt.lockDivision(effect.Division)
		c := rt.findCharacter(effect.Division, effect.SourceName)
		var snapshot *enterworld.Character
		if c != nil {
			snapshot = rt.characterSnapshot(effect.Division, c)
		}
		live := snapshot != nil && enterworld.CharacterAlive(snapshot) && !snapshot.DeletePending &&
			rt.periodicEffects.Active(effect.Division, effect.SourceToken)
		if effect.SourceSession != 0 {
			owner, ok := rt.characterAdmissions.Load(simulation.WorldKey(effect.Division, effect.SourceName))
			live = live && ok && owner.(populationAdmission).session == effect.SourceSession
		}
		target, exists := rt.characterMonster(effect.Division, snapshot, effect.TargetGID)
		live = live && exists && target.CurrentHP > 0
		var result OpResult
		if live && step.Pulse {
			result = rt.applyPeriodicPulse(effect, c, snapshot, target, now)
			if current, ok := rt.characterMonster(effect.Division, snapshot, effect.TargetGID); !ok || current.CurrentHP == 0 {
				live = false
			}
		}
		if !live || step.Expire {
			result.Broadcast = append(result.Broadcast, rt.periodicRetirement(effect)...)
		}
		out = append(out, periodicDivisionFrames(effect, c, result)...)
		unlock()
	}
	return out
}

/*
================
periodicDivisionFrames

Preserve result, public reward and private progression order on the tick path.
================
*/
func periodicDivisionFrames(effect linkedpulse.Effect, c *enterworld.Character, result OpResult) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	var actorID int64
	if c != nil {
		actorID = c.ID
	}
	for _, route := range []struct {
		frames []wire.Frame
		actor  int64
	}{{result.Broadcast, 0}, {result.ActorPrivate, actorID}} {
		if len(route.frames) == 0 {
			continue
		}
		batch := simulation.DivisionFrames{DivisionID: effect.Division, OnlyCharacterID: route.actor}
		if route.actor == 0 {
			batch.SourceGID = effect.SourceGID
		}
		for _, frame := range route.frames {
			batch.Frames = append(batch.Frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope})
		}
		out = append(out, batch)
	}
	return append(out, recipientDivisionFrames(effect.Division, result.Recipients)...)
}

/*
================
applyPeriodicPulse

Each pulse reads current combat stats and commits through the existing monster
HP door. It does not admit a new cast, charge resources or test cast range.
================
*/
func (rt *Runtime) applyPeriodicPulse(effect linkedpulse.Effect, c, snapshot *enterworld.Character, target monster.Instance, now int64) OpResult {
	skill, known := rt.deps.SkillData().SkillByID(effect.SkillID)
	if !known || !skill.TimedEffect.Periodic.Pinned {
		return OpResult{}
	}
	skill.Attack = skill.TimedEffect.Periodic.Attack
	attacker, _, err := rt.playerCombatStats(effect.Division, snapshot)
	if err != nil {
		return OpResult{}
	}
	defender, err := combat.MonsterInstanceStats(target)
	if err != nil {
		return OpResult{}
	}
	defender.MotionState = target.Motion.StateAt(now)
	formula, err := rt.resolvePlayerImpact(effect.Division, snapshot.Name, skill, attacker, defender, now, false)
	if err != nil {
		return OpResult{}
	}
	plans, ok := rt.planMonsterImpacts(effect.Division, snapshot, skill, target, []combat.Result{formula}, now)
	if !ok {
		return OpResult{}
	}
	roster := rt.monsterRewardRoster(effect.Division, c, now)
	var impacts []simulation.MonsterDamageResult
	var settlement monsterSettlement
	commit := func() bool {
		impacts = rt.Monsters.ApplyDamageSequence(effect.Division, target.Gid, target.CurrentHP, plans)
		if len(impacts) == 0 {
			return false
		}
		if impacts[0].Fatal {
			pose := monster.Pose{}
			if mover, exists := rt.Monsters.Mover(effect.Division, target.Gid); exists {
				pose = mover.LivePoseAt(now, nil)
			}
			settlement = rt.settleMonsterInsideDoor(effect.Division, c, roster, impacts[0], pose, now)
		}
		return true
	}
	// A surviving hit mutates only the monster. Its abnormal application
	// resolves the source through the character read door, so holding that
	// door's write lock here would recursively deadlock. Only a fatal hit
	// changes character rewards; dead victims skip abnormal application.
	committed := false
	if plans[0].Damage >= target.CurrentHP {
		committed = rt.deps.UpdateMany(roster.characters, "linked-skill-kill", commit)
	} else {
		committed = commit()
	}
	if !committed {
		return OpResult{}
	}
	rt.commitSkillHostility(effect.Division, effect.SourceGID, target.Gid, skill, impacts, now)
	public := []wire.Frame{wire.SkillPulseFrame(effect.SourceGID, skill.ID, []wire.SkillAreaTarget{
		{GID: target.Gid, Impacts: []wire.SkillCastTargetImpact{committedSkillImpact(formula, impacts[0])}},
	})}
	public = append(public, rt.monsterImpactAbnormalFrames(effect.Division, target.Gid, impacts)...)
	if impacts[0].Fatal {
		public = append(public, monsterLifeDeadFrame(target.Gid))
		public = append(public, rt.groundReferences(settlement.drops)...)
		for _, drop := range settlement.drops {
			public = append(public, wire.DropBroadcastFrames(drop.SpawnRow(true))...)
		}
		public = append(public, settlement.public...)
		rt.queueMonsterDefeat(effect.Division, target.Gid, now+monsterDeathPresentationRetention.Milliseconds())
	}
	return OpResult{Broadcast: public, ActorPrivate: wire.ProgressionPrivateFrames(settlement.actorFrames), Recipients: settlement.others}
}
