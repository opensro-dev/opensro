/*
===========================================================================

agentstrike.go - a player's hits on a caravan agent (port-only, #3704)

The caravan agents are server-owned characters with no stored record,
so the player strike (pvpstrike.go) cannot hold their door. This file
is their arm of the same cast stage: admission, the job-PvP refusal and
the damage formula are the native-shaped ones, but the hit lands
through the caravan runtime's own damage door (RecordPlayerAttack),
which owns the agent's HP, death and robbery bookkeeping.

Port-only rules, stated once: an agent mitigates nothing (its defender
is its level alone; the HP pool is sized for it), no PK record is
booked for striking or killing one, and area, periodic, debuff and
taunt rows do not take agents as targets in this slice. Everything is
inert while Runtime.Agents is nil (SRO_AI_CARAVANS off).

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
AgentTarget

One caravan agent as a combat target: a detached snapshot, never a
stored character. HP is the snapshot's; the directory's damage door
stays the authority on life and death.
================
*/
type AgentTarget struct {
	// GID is the agent's object gid in the player band.
	GID          uint32
	Name         string
	JobType      uint8
	Pose         simulation.Spawn
	BodyRadius   float64
	Level        uint8
	HP           int
	MaxHP        int
	LastAttacker string
	// SkillID is the agent's strike skill; the attack powers are its
	// weapon's shipped ranges (slice 3e), the attacker side of the
	// same impact formula.
	SkillID                              uint32
	PhysicalAttackMin, PhysicalAttackMax float64
	MagicalAttackMin, MagicalAttackMax   float64
	// CombatTarget is the player the agent is currently fighting:
	// that player may always answer (agentAttackRefusal).
	CombatTarget string
}

/*
================
AgentDirectory

The caravan runtime as the combat lanes see it: resolve an agent by
its object gid, and apply real damage to it by name. Implemented by
the composition root over caravanai; nil disables every agent arm.
================
*/
type AgentDirectory interface {
	AgentByGID(division string, gid uint32) (AgentTarget, bool)
	// ApplyAgentDamage applies damage from attackerName and reports
	// whether the blow defeated the agent.
	ApplyAgentDamage(division, agentName, attackerName string, damage int, nowMs int64) (defeated bool)
}

/*
================
agentTarget

The agent a gid names, or false when no directory is wired or the gid
names no living agent.
================
*/
func (rt *Runtime) agentTarget(division string, gid uint32) (AgentTarget, bool) {
	if rt.Agents == nil {
		return AgentTarget{}, false
	}
	return rt.Agents.AgentByGID(division, gid)
}

/*
================
agentAttackRefusal

The job-PvP refusal for an agent victim (port-only rule): both regions
must permit battle as for a player victim, then the fight is legal
when the jobs are hostile (the native hostileJobs pair rule) or when
the agent is already fighting this caster. Anything else is the
generic 0x3006: agents carry no PK bookkeeping in this slice, so
there is no murder branch to open on one.
================
*/
func (rt *Runtime) agentAttackRefusal(division string, caster *enterworld.Character, agent AgentTarget, now int64) uint16 {
	casterAt := rt.liveSpawn(simulation.WorldKey(division, caster.Name), caster, now)
	for _, regionID := range []uint16{casterAt.RegionID, agent.Pose.RegionID} {
		allowed, known := worldgeom.RegionPlayerCombat(regionID)
		if !known {
			return 0x3009
		}
		if !allowed {
			return 0x3018
		}
	}
	if hostileJobs(enterworld.DressedJob(caster), agent.JobType) {
		return 0
	}
	if agent.LastAttacker == caster.Name {
		return 0
	}
	if agent.CombatTarget == caster.Name {
		return 0
	}
	return 0x3006
}

/*
================
strikeAgentTarget

acceptPlayerTargetStage for an agent target: the player strike's
admission, spacing and cast lifecycle, with the hit committed through
the agent directory inside the caster's own door (the agent has no
character door to hold). One door pays the cast's costs and books its
cooldown; the victim's side is the directory call alone.
================
*/
func (rt *Runtime) strikeAgentTarget(st offensiveStage, target combatTarget) (OpResult, skillCastDecision) {
	division, character, snapshot, skill, now := st.division, st.character, st.snapshot, st.skill, st.now
	agent := target.agent
	if agent.HP <= 0 {
		return OpResult{}, skillCastRefused
	}
	if code := rt.agentAttackRefusal(division, snapshot, *agent, now); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	// Rows whose victim handling is a player-record lane do not take
	// agents in this slice (file banner): no area, periodic, debuff
	// or taunt victims.
	if skill.Threat.Only || skill.HostileDebuff.Pinned || skill.TimedEffect.Periodic.Pinned ||
		skill.OffensiveArea.Radius != 0 {
		return OpResult{}, skillCastRefused
	}
	mask := admitExecution
	if st.rootID != 0 {
		mask &^= admitCooldown
	}
	struck := &admitTarget{at: target.at}
	if code := rt.skillAdmission(division, snapshot, skill, now, struck, st.release, mask); code != 0 {
		return offensiveRefusal(code), skillCastRefused
	}
	if st.advanced {
		if _, refusal := rt.stagePhaseCost(division, snapshot, skill, now, st.release, st.rootID); refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
	}
	casterAt := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	spacing, spacingOK := rt.combatTargetSpacing(snapshot, target, st.actionReach)
	if !spacingOK || !worldgeom.SamePlane(casterAt.RegionID, target.at.RegionID) {
		return OpResult{}, skillCastDeferred
	}
	if st.release == nil && !spacing.Contains(casterAt, target.at) {
		if st.rootID == 0 {
			return OpResult{}, skillCastDeferred
		}
		if code := linkedStageTargetRefusal(casterAt, target.at); code != 0 {
			return offensiveRefusal(code), skillCastRefused
		}
	}
	if st.release == nil && st.rootID == 0 {
		rt.noteHawkOwnerAttack(division, snapshot, target.gid, now)
	}
	if skill.ActionCastingTimeMs != 0 && st.release == nil {
		return rt.prepareOffensiveCast(division, character, snapshot, st.cast, skill, st.advanced, st.rootID, now, st.doors)
	}
	stealBase, stealOK := int64(0), true
	if skill.LifeSteal.Present {
		stealBase, stealOK = rt.lifeStealBase(division, snapshot, skill.LifeSteal, st.attacker)
	}
	if !stealOK {
		return OpResult{}, skillCastRefused
	}
	// The agent mitigates nothing in this slice: its defender is its
	// level alone (file banner), weighed by the same impact formula.
	defender := combat.Stats{Level: agent.Level}
	formulas := make([]combat.Result, 0, skill.Attack.ImpactCount)
	for range skill.Attack.ImpactCount {
		if skill.LifeSteal.Present {
			steal := lifeStealResult(stealBase, st.attacker, defender, uint32(max(agent.HP, 0)), fullAreaPercent)
			formulas = append(formulas, combat.FinishImpact(steal, combat.ImpactTail{PercentApplied: true, Attack: skill.Attack.Present}))
			continue
		}
		formula, resolveErr := rt.resolvePlayerImpact(division, snapshot.Name, skill, st.attacker, defender, now, false)
		if resolveErr != nil {
			return OpResult{}, skillCastRefused
		}
		formulas = append(formulas, combat.FinishImpact(formula, combat.ImpactTail{Attack: skill.Attack.Present}))
	}
	if len(formulas) == 0 {
		return OpResult{}, skillCastRefused
	}
	var travel skillTravelPlan
	if skill.PositionEffect.Charge {
		from, owner := rt.liveNav(simulation.WorldKey(division, snapshot.Name), snapshot, now)
		radius, valid := rt.deps.CharacterBodyRadius(snapshot)
		goal, admitted := chargeSkillGoal(from, target.at, skill.PositionEffect.Range, radius+agent.BodyRadius)
		if !valid || !admitted {
			return OpResult{}, skillCastRefused
		}
		var planned bool
		if travel, planned = rt.planSkillTravel(snapshot.Name, from, owner, goal); !planned {
			return OpResult{DiagnosticRefusal: "charge-navigation-refused"}, skillCastRefused
		}
	}
	committed := make([]simulation.MonsterDamageResult, 0, len(formulas))
	var ammo ammunitionResult
	var battle []wire.Frame
	var tuning, stolen wire.Frame
	var refusal uint16
	defeated := false
	if !st.doors.updateMany([]*enterworld.Character{character}, "player-strike-agent", func() bool {
		var cost skillCharge
		if st.advanced {
			if cost, refusal = rt.stagePhaseCost(division, character, skill, now, st.release, st.rootID); refusal != 0 {
				return false
			}
		}
		debit, admitted := ammunitionDebit{index: -1}, true
		if st.consumeAmmo {
			debit, admitted = rt.planEquippedAmmunition(character, st.loadout.WeaponKind, ammunitionSpent(skill, st.advanced))
		}
		if !admitted {
			refusal = 0x300e
			return false
		}
		committed, defeated = applyAgentImpacts(rt.Agents, division, agent, snapshot.Name, formulas, now)
		if len(committed) == 0 {
			return false
		}
		if st.release == nil {
			rt.startSkillCast(division, character, skill, now)
		}
		battle = rt.enterBattleState(division, character, now)
		if st.consumeAmmo {
			ammo = applyAmmunitionDebit(character, debit)
		}
		if st.advanced {
			rt.commitOffensivePhaseCost(division, character, skill, cost, now, st.release != nil)
		} else if st.release == nil {
			rt.registerPlayerSkillCooldown(division, character, skill, now)
		}
		if skill.FixedDamage.Present {
			tuning = rt.commitTuningMana(division, character, skill.FixedDamage, committed)
		}
		if skill.LifeSteal.Present {
			stolen = rt.commitLifeSteal(division, character, committed)
		}
		if skill.PositionEffect.Charge {
			rt.commitSkillTravel(simulation.WorldKey(division, character.Name), character, travel)
		}
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{}, skillCastRefused
	}
	if skill.PositionEffect.Charge {
		rt.bindResidentRegion(simulation.WorldKey(division, character.Name), now)
	}
	caster := enterworld.ObjectIDForCharacter(snapshot)
	var token uint32
	if st.release != nil {
		token = st.release.token
	} else {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	impacts := make([]wire.SkillCastTargetImpact, 0, len(committed))
	for index, applied := range committed {
		impacts = append(impacts, committedSkillImpact(formulas[index], applied))
	}
	result := wire.NewStationarySkillCastSingleTargetResult(
		wire.SkillCastSuccess{SkillId: st.cast.ActionId, CasterGid: caster, InstanceToken: token},
		target.gid, impacts)
	if skill.PositionEffect.Charge {
		result = wire.NewSkillCastSingleTargetResult(
			wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: caster, InstanceToken: token},
			target.gid, impacts, travel.point)
	}
	success := wire.SkillCastSingleTargetResultFrame(result)
	closeAt := now + int64(st.lifecycleMs) - int64(skill.ActionDurationMs) + rt.wolfHowlActionMs(division, snapshot, skill.ActionDurationMs)
	if st.release == nil {
		rt.queueSkillFinalize(division, snapshot.Name, caster, now+int64(skill.ActionCastingTimeMs), wire.SkillCastReleaseFrame(token, target.gid))
	} else {
		success = wire.SkillCastReleaseResultFrame(result)
		closeAt = now + int64(skill.ActionDurationMs) + rt.wolfHowlActionMs(division, snapshot, skill.ActionDurationMs)
	}
	if skill.ActionHandler == enterworld.SkillActionProjectile {
		closeAt = max(closeAt, now+projectileFlightMs(casterAt, target.at, skill.ProjectileSpeed)+1)
	}
	if !skill.PositionEffect.Charge {
		rt.queueSkillCastClose(division, snapshot.Name, caster, token, skill, st.rootID, closeAt)
	}
	actor := []wire.Frame{success}
	broadcast := []wire.Frame{success}
	if defeated {
		// The corpse's own death follows from the caravan tick: the
		// agent leaves the entity snapshot and the peer leg despawns
		// it (slice 3b). This frame is the fall itself.
		life := monsterLifeDeadFrame(target.gid)
		actor = append(actor, life)
		broadcast = append(broadcast, life)
	}
	var private []wire.Frame
	if st.consumeAmmo {
		ammoFrames := ammunitionFrames(ammo)
		actor = append(actor, ammoFrames...)
		private = append(private, ammoFrames...)
	}
	// The attacker's unblocked attempts wear its weapon, as on a
	// player victim.
	var tally wearTally
	for _, formula := range formulas {
		tally.note(formula.Blocked, true)
	}
	wear := rt.applyEquipmentWear(division, character, tally, st.doors)
	actor = append(actor, wear.actor...)
	private = append(private, wear.actor...)
	broadcast = append(broadcast, wear.public...)
	if tuning.Opcode != 0 {
		actor = append(actor, tuning)
		private = append(private, tuning)
	}
	if stolen.Opcode != 0 {
		actor = append(actor, stolen)
		broadcast = append(broadcast, stolen)
	}
	actor = append(actor, battle...)
	broadcast = append(broadcast, battle...)
	return OpResult{Frames: actor, Broadcast: broadcast, ActorPrivate: private}, skillCastAccepted
}

/*
================
AgentStrikeResult

One agent strike's publication: the room's frames (the cast result
under the agent's gid, the victim's struck frames), the victim's own
frames, and whether the victim fell.
================
*/
type AgentStrikeResult struct {
	Public    []wire.Frame
	Victim    RecipientFrames
	Committed bool
	Defeated  bool
}

// agentStrikeReach is the action side's re-judgement bound for an
// agent strike the caravan runtime already ranged (its own strike
// range, with grace for the poses having moved since).
const agentStrikeReach = 16.0

/*
================
StrikePlayerByAgent

One caravan agent's strike on one player (slice 3e): the agent is a
synthetic attacker - its weapon's shipped attack ranges and its own
skill row feed the same impact formula a monster's strike uses, and
the hit commits through the shared player-victim door with no killer
record, so the victim's death settles as a death to the world (no PK
bookkeeping on either side). Agents inflict no statuses, displace
nobody, and take no returned damage in this slice; linked shares off
the victim (a fence, Pain Quota) still split inside the victim's
commit but do not land on their takers.
================
*/
func (rt *Runtime) StrikePlayerByAgent(division string, agent AgentTarget, victimName string, now int64) AgentStrikeResult {
	var victim *enterworld.Character
	for _, candidate := range rt.deps.CharactersForDivision(division) {
		if candidate != nil && candidate.Name == victimName {
			victim = candidate
			break
		}
	}
	if victim == nil {
		return AgentStrikeResult{}
	}
	snapshot := rt.characterSnapshotCombat(division, victim)
	if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) {
		return AgentStrikeResult{}
	}
	skillSource := rt.deps.SkillData()
	if skillSource == nil {
		return AgentStrikeResult{}
	}
	skill, known := skillSource.SkillByID(agent.SkillID)
	if !known || !skill.Attack.Present {
		return AgentStrikeResult{}
	}
	victimAt := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	if !worldgeom.SamePlane(victimAt.RegionID, agent.Pose.RegionID) ||
		worldgeom.Distance(
			worldgeom.RegionXZ{RegionID: victimAt.RegionID, X: victimAt.X, Z: victimAt.Z},
			worldgeom.RegionXZ{RegionID: agent.Pose.RegionID, X: agent.Pose.X, Z: agent.Pose.Z},
		) > agentStrikeReach {
		return AgentStrikeResult{}
	}
	defender, _, err := rt.playerCombatStats(division, snapshot)
	if err != nil {
		return AgentStrikeResult{}
	}
	attacker := combat.Stats{
		Level:             agent.Level,
		PhysicalAttackMin: agent.PhysicalAttackMin,
		PhysicalAttackMax: agent.PhysicalAttackMax,
		MagicalAttackMin:  agent.MagicalAttackMin,
		MagicalAttackMax:  agent.MagicalAttackMax,
	}
	agentGID := agent.GID
	strike := playerStrike{division: division, victim: victim, killer: deathKiller{}, skill: skill, now: now}
	actor := criticalActor{division: division, monster: agentGID}
	if !rt.planPlayerStrike(&strike,
		func(wall *enterworld.SkillWall) (combat.WallOutcome, error) {
			outcome, outcomeErr := rt.resolveCombatBehindWall(actor, skill, attacker, defender, wall)
			outcome.Defender = combat.FinishImpact(outcome.Defender, combat.ImpactTail{Percent: fullAreaPercent,
				Attack: skill.Attack.Present, Covered: outcome.Covered})
			return outcome, outcomeErr
		},
		func(wall *enterworld.SkillWall, _ combat.Result) ([]abnormal.Record, error) {
			// Agents inflict no statuses in this slice.
			return nil, nil
		}) {
		return AgentStrikeResult{}
	}
	var struck playerStruck
	committed := rt.deps.UpdateDeferred(victim, "agent-strike-player", func() bool {
		if victim.DeletePending || !enterworld.CharacterAlive(victim) {
			return false
		}
		struck = rt.strikePlayerInDoor(strike)
		return len(struck.impacts) > 0
	})
	if !committed || len(struck.impacts) == 0 {
		return AgentStrikeResult{}
	}
	if struck.fatal {
		rt.bindResidentRegion(simulation.WorldKey(division, victim.Name), now)
	}
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	cast := wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: agentGID, InstanceToken: token}
	frame := wire.SkillCastSingleTargetResultFrame(
		wire.NewStationarySkillCastSingleTargetResult(cast, enterworld.ObjectIDForCharacter(snapshot), struck.impacts))
	rt.queueSkillFinalize(division, monsterCastOwner(agentGID), agentGID, now, wire.SkillCastReleaseFrame(token, enterworld.ObjectIDForCharacter(snapshot)))
	actionLifecycleMs, _ := skill.ActionLifecycleMs()
	rt.queueSkillFinalize(division, monsterCastOwner(agentGID), agentGID,
		now+max(int64(actionLifecycleMs), 1), wire.SkillCastFinalizeFrame(token))
	public, private := rt.playerStruckFrames(division, victim, struck, now)
	public = append([]wire.Frame{frame}, public...)
	return AgentStrikeResult{
		Public:    public,
		Victim:    RecipientFrames{CharacterID: victim.ID, Frames: private},
		Committed: true,
		Defeated:  struck.fatal,
	}
}

/*
================
applyAgentImpacts

Hands the strike's total damage to the directory (the HP authority)
and shapes the per-impact results the cast frame reports: each impact
debits what remains of the snapshot's HP, and the directory's verdict
marks the killing impact fatal.
================
*/
func applyAgentImpacts(dir AgentDirectory, division string, agent *AgentTarget, attackerName string, formulas []combat.Result, now int64) ([]simulation.MonsterDamageResult, bool) {
	total := 0
	for _, formula := range formulas {
		total += int(formula.Damage)
	}
	defeated := dir.ApplyAgentDamage(division, agent.Name, attackerName, total, now)
	out := make([]simulation.MonsterDamageResult, 0, len(formulas))
	remaining := uint32(max(agent.HP, 0))
	for index, formula := range formulas {
		before := remaining
		applied := min(remaining, formula.Damage)
		remaining -= applied
		result := simulation.MonsterDamageResult{BeforeHP: before, Damage: formula.Damage, Applied: applied}
		if defeated && index == len(formulas)-1 {
			result.Fatal = true
		}
		out = append(out, result)
	}
	return out, defeated
}
