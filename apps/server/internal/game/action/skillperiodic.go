/*
===========================================================================

skillperiodic.go - release and damage authority for linked hostile effects

Cast admission and preparation belong to the ordinary action lane. Release
installs one pair per selected recipient (a monster or a player); the pulse owner only supplies clocks.
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
	target              combatTarget
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
	targets := []combatTarget{p.target}
	if d.Area.Radius != 0 {
		targets = rt.areaVictims(p.division, p.snapshot, p.skill, p.target, d.Area, areaBaseRange(p.skill, p.attacker), p.now)
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
	var monsters []bool
	var public, private []wire.Frame
	for _, target := range targets {
		effect := linkedpulse.Effect{Division: p.division, SourceName: p.snapshot.Name, SourceSession: session,
			SourceGID: caster, TargetGID: target.gid, SkillID: p.skill.ID, LinkGroup: d.Link.Group,
			MaxPerTarget: d.Link.MaxOutgoing, StartedMs: p.now, DurationMs: duration, PeriodMs: d.PeriodMs,
			SourceToken: atomic.AddUint32(&rt.castTokenCounter, 1), TargetToken: atomic.AddUint32(&rt.castTokenCounter, 1)}
		if code := rt.periodicEffects.Refusal(effect); code != 0 {
			if target.gid == p.target.gid {
				return offensiveRefusal(code), skillCastRefused
			}
			continue
		}
		recipient, err := (wire.AttachedEffect{GID: target.gid, SkillID: p.skill.ID, InstanceToken: effect.TargetToken,
			Phase: 2, Rider: duration - d.DurationMs}).Encode(wire.AttachedEffectLayout{Status: p.skill.SpawnStatus, Rider: p.skill.EffectRider})
		if err != nil {
			return OpResult{DiagnosticRefusal: "periodic-recipient-layout"}, skillCastRefused
		}
		source, err := (wire.SourceEffect{SkillID: p.skill.ID, InstanceToken: effect.SourceToken, SubjectGID: target.gid}).Encode(p.skill.StealthDuration)
		if err != nil {
			return OpResult{DiagnosticRefusal: "periodic-source-layout"}, skillCastRefused
		}
		effects = append(effects, effect)
		monsters = append(monsters, target.monster != nil)
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
		// A monster recipient also carries the attachment in its own state;
		// a player recipient's is the published 0x3015 alone.
		projections := make([]simulation.MonsterLinkedEffect, 0, len(effects))
		for i, effect := range effects {
			if monsters[i] {
				projections = append(projections, simulation.MonsterLinkedEffect{GID: effect.TargetGID,
					Effect: monster.AttachedSkill{SkillID: effect.SkillID, Token: effect.TargetToken}})
			}
		}
		if len(projections) > 0 && !rt.Monsters.InstallMonsterLinkedEffects(p.division, projections) {
			return false
		}
		for _, effect := range effects {
			if code := rt.periodicEffects.Install(effect); code != 0 {
				panic("periodic effect lost serialized admission")
			}
		}
		if p.release == nil {
			rt.startSkillCast(p.division, p.character, p.skill, p.now)
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
		start = wire.SkillCastReleaseFrame(token, p.target.gid)
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
		start = wire.SkillCastAtTargetFrame(wire.SkillCastSuccess{SkillId: p.skill.ID, CasterGid: caster,
			InstanceToken: token, OwnerOrTargetGid: p.target.gid})
		rt.queueSkillFinalize(p.division, p.snapshot.Name, caster, p.now, wire.SkillCastReleaseFrame(token, p.target.gid))
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
	if rt.Monsters != nil {
		rt.Monsters.RemoveMonsterLinkedEffect(effect.Division, effect.TargetGID, effect.TargetToken)
	}
	tokens := []uint32{effect.SourceToken, effect.TargetToken}
	if effect.StructureRepair {
		// Damage cancellation may already have published the source retirement.
		ended := rt.effects.RetireInstances(effect.Division, effect.SourceName, []uint32{effect.SourceToken})
		if len(ended) == 0 {
			tokens = []uint32{effect.TargetToken}
		}
	}
	payload, err := (wire.EndedEffectInstances{InstanceTokens: tokens}).Encode()
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
		if effect.StructureRepair {
			live = live && rt.structureRepairSourceActive(effect)
		}
		if effect.SourceSession != 0 {
			owner, ok := rt.characterAdmissions.Load(simulation.WorldKey(effect.Division, effect.SourceName))
			live = live && ok && owner.(populationAdmission).session == effect.SourceSession
		}
		target, exists := rt.resolveCombatTarget(effect.Division, snapshot, effect.TargetGID, now)
		live = live && exists
		var result OpResult
		if live && step.Pulse {
			if target.monster != nil {
				result = rt.applyPeriodicPulse(effect, c, snapshot, *target.monster, now)
			} else {
				result = rt.applyPeriodicPlayerPulse(effect, c, snapshot, target, now)
			}
			if _, ok := rt.resolveCombatTarget(effect.Division, snapshot, effect.TargetGID, now); !ok {
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
	if known && effect.StructureRepair && skill.StructureRepair.Pinned {
		return rt.pulseStructureRepair(effect, skill, target, now)
	}
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
	hit, ok := rt.commitCreditedMonsterHit(effect.Division, c, snapshot, skill, target, formula, "linked-skill-kill", now)
	if !ok {
		return OpResult{}
	}
	public := []wire.Frame{wire.SkillPulseFrame(effect.SourceGID, skill.ID, []wire.SkillAreaTarget{
		{GID: target.Gid, Impacts: []wire.SkillCastTargetImpact{committedSkillImpact(formula, hit.impacts[0])}},
	})}
	public = append(public, rt.monsterImpactAbnormalFrames(effect.Division, target.Gid, hit.impacts)...)
	return rt.creditedHitResult(effect.Division, target, hit, public, now)
}

/*
================
applyPeriodicPlayerPulse

A pulse on a player recipient: the periodic attack's record as the
caster's credited hit (pvpstrike.go), published as the monster pulse is.
================
*/
func (rt *Runtime) applyPeriodicPlayerPulse(effect linkedpulse.Effect, c, snapshot *enterworld.Character, target combatTarget, now int64) OpResult {
	skill, known := rt.deps.SkillData().SkillByID(effect.SkillID)
	if !known || !skill.TimedEffect.Periodic.Pinned || c == nil {
		return OpResult{}
	}
	skill.Attack = skill.TimedEffect.Periodic.Attack
	attacker, _, err := rt.playerCombatStats(effect.Division, snapshot)
	if err != nil {
		return OpResult{}
	}
	hit, result, landed := rt.creditPlayerHit(playerHitInput{division: effect.Division, caster: c, snapshot: snapshot,
		attacker: attacker, skill: skill, target: target, impacts: 1, now: now})
	if !landed {
		return OpResult{}
	}
	pulse := wire.SkillPulseFrame(effect.SourceGID, skill.ID, []wire.SkillAreaTarget{
		{GID: target.gid, Impacts: hit.struck.impacts},
	})
	result.Broadcast = append([]wire.Frame{pulse}, result.Broadcast...)
	return result
}
