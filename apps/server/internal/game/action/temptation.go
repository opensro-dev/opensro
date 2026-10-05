/*
===========================================================================

temptation.go - monster hits on monsters in a Temptation fight

The Bard's Temptation (SKILL_EU_BARD_FORGETA_TARGET_A/B, ca) lands
Confusion on a regular monster, which then plans against the monsters
around it (world/simulation/monstertemptation.go). Its attack reaches the
action owner as an ordinary monster action whose target is a monster; this
module resolves that hit, and the struck monster's answer, through the
shared combat formula and the one monster HP door, and publishes it as the
same B245 bracket a monster's attack on a player uses.

Owner's rule: a tempted monster attacks other monsters nearby for the
duration instead of players, and only regular monsters and regular party
monsters are affected (monster.Instance.RegularMonster).

Inferred, recorded deliberately:
  - a monster fight rolls no abnormal status and resolves a casting
    skill at once; only the damage of the authored attack is ported.
  - a kill in a monster fight is credited to nobody: it settles like an
    uncredited damage-over-time tick, so players who damaged the victim
    are still rewarded from its contributions.

===========================================================================
*/

package action

import (
	"fmt"
	"sync/atomic"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
monsterFightAdmitted

A monster may strike another monster only inside a Temptation fight: the
attacker is tempted, or it answers the tempted monster that is its target.
Both must live in the same population.
================
*/
func (rt *Runtime) monsterFightAdmitted(division string, attacker monster.Instance, targetGID uint32) (monster.Instance, bool) {
	target, ok := rt.Monsters.Get(division, targetGID)
	if !ok || target.CurrentHP == 0 || target.Gid == attacker.Gid || !attacker.Tempted() && !target.Tempted() {
		return monster.Instance{}, false
	}
	lease, ok := rt.Monsters.ObjectPopulation(division, attacker.Gid)
	if other, same := rt.Monsters.ObjectPopulation(division, targetGID); !ok || !same || other != lease {
		return monster.Instance{}, false
	}
	return target, true
}

/*
================
monsterHitMonster

One monster action against a monster target. The refusals follow the
player path: out of reach keeps the target and the selected skill
(ApproachRequired), a summon or self effect is retried with another
selection (CommandRejected), anything else fails closed.
================
*/
func (rt *Runtime) monsterHitMonster(division string, instance, target monster.Instance, skillID uint32, nowMs int64) (result simulation.MonsterAttackResult) {
	skill, ok := rt.deps.SkillData().SkillByID(skillID)
	if !ok {
		return result
	}
	result.TargetAlive = true
	if skill.Summon.Present || skill.MonsterSelfEffect.Pinned {
		result.Refusal = simulation.MonsterAttackCommandRejected
		return result
	}
	lifecycle, pinned := skill.ActionLifecycleMs()
	if !skill.CombatPinned || !skill.Attack.Present || !skill.TargetRequired || !pinned || lifecycle == 0 ||
		!skill.ActionRangePinned || skill.ActionRange <= 0 {
		return simulation.MonsterAttackResult{}
	}
	actorMover, ok := rt.Monsters.Mover(division, instance.Gid)
	if !ok {
		return simulation.MonsterAttackResult{}
	}
	targetMover, ok := rt.Monsters.Mover(division, target.Gid)
	if !ok {
		return simulation.MonsterAttackResult{}
	}
	actorPose, targetPose := actorMover.LivePoseAt(nowMs, nil), targetMover.LivePoseAt(nowMs, nil)
	from := simulation.Spawn{RegionID: actorPose.RegionID, X: actorPose.X, Y: actorPose.Y, Z: actorPose.Z}
	to := simulation.Spawn{RegionID: targetPose.RegionID, X: targetPose.X, Y: targetPose.Y, Z: targetPose.Z}
	spacing := simulation.CombatSpacing{
		ActorBodyRadius:  simulation.BodyRadius(instance.BodyRadius()),
		TargetBodyRadius: simulation.BodyRadius(target.BodyRadius()),
		ActionReach:      rt.monsterActionReach(instance, skill),
	}
	if !spacing.Valid() || simulation.IsDungeonRegion(from.RegionID) != simulation.IsDungeonRegion(to.RegionID) {
		return simulation.MonsterAttackResult{}
	}
	if !spacing.Contains(from, to) {
		result.Refusal = simulation.MonsterAttackApproachRequired
		return result
	}
	attacker, err := combat.MonsterInstanceStats(instance)
	if err != nil {
		return simulation.MonsterAttackResult{}
	}
	defender, err := monsterDefenderStats(target, nowMs)
	if err != nil {
		return simulation.MonsterAttackResult{}
	}
	formulas := make([]combat.Result, 0, skill.Attack.ImpactCount)
	for range skill.Attack.ImpactCount {
		formula, err := rt.resolveCombat(criticalActor{division: division, monster: instance.Gid}, skill, attacker, defender)
		if err != nil {
			return simulation.MonsterAttackResult{}
		}
		formulas = append(formulas, formula)
	}
	if len(formulas) == 0 {
		return simulation.MonsterAttackResult{}
	}
	remaining := target.CurrentHP
	plans := make([]simulation.MonsterDamagePlan, 0, len(formulas))
	for _, formula := range formulas {
		plan := simulation.MonsterDamagePlan{GID: target.Gid, ExpectedHP: remaining, Damage: formula.Damage}
		if formula.Blocked {
			plan.Damage = 0 // 5905FB: a blocked impact deals no damage
		}
		plans = append(plans, plan)
		remaining -= min(remaining, plan.Damage)
		if remaining == 0 {
			break
		}
	}
	roster := rt.monsterRewardRoster(division, nil, nowMs)
	var committed []simulation.MonsterDamageResult
	var settlement monsterSettlement
	commit := func() bool {
		committed = rt.Monsters.ApplyDamageSequence(division, target.Gid, target.CurrentHP, plans)
		if len(committed) == 0 {
			return false
		}
		if last := committed[len(committed)-1]; last.Fatal {
			settlement = rt.settleMonsterInsideDoor(division, nil, roster, last, targetPose, nowMs)
		}
		return true
	}
	if len(roster.characters) == 0 {
		ok = commit()
	} else {
		ok = rt.deps.UpdateMany(roster.characters, "monster-fight", commit)
	}
	if !ok {
		return simulation.MonsterAttackResult{}
	}
	// The struck monster records its attacker like any hit, which arms its
	// retaliation against the tempted monster (skillhostility.go).
	rt.commitSkillHostility(division, instance.Gid, target.Gid, skill, committed, nowMs)

	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	impacts := make([]wire.SkillCastTargetImpact, 0, len(committed))
	for index, applied := range committed {
		impacts = append(impacts, committedSkillImpact(formulas[index], applied))
	}
	public := []wire.Frame{wire.SkillCastSingleTargetResultFrame(wire.NewStationarySkillCastSingleTargetResult(
		wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: instance.Gid, InstanceToken: token},
		target.Gid,
		impacts,
	))}
	bracket := fmt.Sprintf("@monster:%d", instance.Gid)
	closeAt := nowMs + max(int64(lifecycle), projectileFlightMs(from, to, skill.ProjectileSpeed)+1)
	rt.queueSkillFinalize(division, bracket, instance.Gid, nowMs, wire.SkillCastReleaseFrame(token, target.Gid))
	rt.queueSkillFinalize(division, bracket, instance.Gid, closeAt, wire.SkillCastFinalizeFrame(token))
	fatal := committed[len(committed)-1].Fatal
	if fatal {
		rt.queueMonsterDefeat(division, target.Gid, nowMs+monsterDeathPresentationRetention.Milliseconds())
		public = append(public, rt.monsterKillBurst(target.Gid, settlement.drops)...)
		public = append(public, settlement.public...)
		rt.queueMonsterLegRecipients(division, settlement.others)
	}
	result.Frames = simFrames(public)
	result.Accepted = true
	result.TargetAlive = !fatal
	return result
}

/*
================
temptedOpponentCandidate

A monster attacker resolves as an eligible opponent of the monster it
struck only while one of them is tempted; its distance is the live
centre-to-centre distance the player candidates use.
================
*/
func (rt *Runtime) temptedOpponentCandidate(division string, struck monster.Instance, gid uint32, from simulation.Spawn, now int64) (monster.OpponentCandidate, bool) {
	attacker, ok := rt.Monsters.Get(division, gid)
	if !ok || attacker.CurrentHP == 0 || !attacker.Tempted() && !struck.Tempted() {
		return monster.OpponentCandidate{}, false
	}
	mover, ok := rt.Monsters.Mover(division, gid)
	if !ok {
		return monster.OpponentCandidate{}, false
	}
	pose := mover.LivePoseAt(now, nil)
	to := simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
	return monster.OpponentCandidate{GID: gid, Eligible: true, Distance: simulation.WorldDistance2D(from, to), ActorDistance: monster.NativeActorDistance(monster.Pose{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z}, pose)}, true
}

/*
================
untemptableConfusion

Owner's rule: Temptation does not affect champions, giants, uniques or
event and quest monsters. Their rolled Confusion record is dropped, so the
status never lands (no icon, no AI event); the roll's random draws were
already consumed in native order.
================
*/
func untemptableConfusion(target monster.Instance, records []abnormal.Record) []abnormal.Record {
	if target.RegularMonster() {
		return records
	}
	kept := records[:0]
	for _, record := range records {
		if record.Status != abnormal.Confusion {
			kept = append(kept, record)
		}
	}
	return kept
}
