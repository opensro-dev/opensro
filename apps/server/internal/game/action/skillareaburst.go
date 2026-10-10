/*
===========================================================================

skillareaburst.go - untargeted caster-centred attacks (Booming Chord/Wave)

The Bard's Booming Chord and Booming Wave strike without a target: the
hostile monsters and attackable players around the caster are the victims. Selection follows the
untargeted taunt and status area (efr shape 1 around the caster, 58A088);
each victim takes every authored mc impact with the area's reduction per
victim in selection order, as the cast-owned area does (58E5F0). The rows
author zero casting time, so the cast resolves at command time like the
untargeted taunt: one commit, results on the opening record.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
acceptAreaBurst

Admission is shared with the prepared untargeted casts. The cost, cooldown
and cast start commit even when nothing stands in range, as the untargeted
status area and the untargeted taunt already do: the cast is the
caster's, not its victims'.
================
*/
func (rt *Runtime) acceptAreaBurst(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64) OpResult {
	if !skill.AreaBurst || skill.TargetRequired || skill.Attack.ImpactCount == 0 || rt.Monsters == nil {
		return OpResult{DiagnosticRefusal: "area-burst-admission-refused"}
	}
	if refusal, refused := rt.admitUntargetedCast(division, snapshot, cast, skill, now, nil); refused {
		return refusal
	}
	victims := rt.casterAreaVictims(division, snapshot, skill, skill.OffensiveArea, now)
	attacker, _, err := rt.playerCombatStats(division, snapshot)
	if err != nil {
		return OpResult{DiagnosticRefusal: "area-burst-stats-unavailable"}
	}
	plan, planned := rt.planAreaVictims(areaPlanInput{
		division: division, caster: c, snapshot: snapshot, skill: skill, attacker: attacker, victims: victims,
		reduction: skill.OffensiveArea.ReductionPercent, impacts: int(skill.Attack.ImpactCount), now: now,
	})
	if !planned {
		return OpResult{DiagnosticRefusal: "area-burst-plan"}
	}
	var commit areaCommit
	var battleFrames []wire.Frame
	var refusal uint16
	roster := rt.monsterRewardRoster(division, c, now)
	if !rt.deps.UpdateMany(roster.characters, "player-area-burst", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, nil)
		refusal = code
		if code != 0 {
			return false
		}
		if len(plan.victims) > 0 {
			var ok bool
			if commit, ok = rt.commitAreaInDoor(division, c, roster, &plan, now); !ok {
				return false
			}
			battleFrames = rt.enterBattleState(division, c, now)
		}
		rt.startSkillCast(division, c, skill, now)
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, false)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: "area-burst-commit-refused"}
	}
	gid := enterworld.ObjectIDForCharacter(snapshot)
	published := rt.publishArea(division, snapshot, skill, int(skill.Attack.ImpactCount), plan, commit, now)
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	success := wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: gid, InstanceToken: token}
	open := wire.SkillCastUntargetedFrame(success)
	if len(published.targets) > 0 {
		open = wire.SkillCastUntargetedAreaFrame(success, published.targets)
	}
	lifetime, _ := skill.ActionLifecycleMs()
	rt.queueSkillFinalize(division, snapshot.Name, gid, now, wire.SkillCastReleaseFrame(token, 0))
	rt.queueSkillCastClose(division, snapshot.Name, gid, token, skill, 0, now+int64(lifetime))
	public := append([]wire.Frame{open}, published.after...)
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(gid, rt.publishedVitals(division, c))}
	actor := append(append([]wire.Frame{}, public...), vitals)
	actor = append(actor, commit.progression...)
	actor = append(actor, commit.playerActor...)
	actor = append(actor, commit.settlements.otherPublic...)
	private := append(wire.ProgressionPrivateFrames(commit.progression), vitals)
	private = append(private, wire.ProgressionPrivateFrames(commit.playerActor)...)
	public = append(public, commit.settlements.public...)
	// Entering battle (4E27C0) follows the hit's burst.
	actor = append(actor, battleFrames...)
	public = append(public, battleFrames...)
	out := OpResult{Frames: actor, Broadcast: public, ActorPrivate: private,
		Recipients: append(commit.settlements.others, published.recipients...)}
	return mergeOpResults(out, published.returned)
}

/*
================
casterAreaVictims

efr shape 1 around the caster (58A088): the caster's population's
monsters and the attackable players within the radius plus both body
radii, in selection order, at most the area's most-targets.
================
*/
func (rt *Runtime) casterAreaVictims(division string, c *enterworld.Character, skill enterworld.SkillRow, area enterworld.SkillOffensiveArea, now int64) []combatTarget {
	if area.MaxTargets == 0 {
		return nil
	}
	lease, present := rt.casterPopulation(division, c)
	if !present {
		return nil
	}
	radius, ok := rt.deps.CharacterBodyRadius(c)
	if !ok {
		return nil
	}
	q := areaQuery{selects: area.Select, division: division, caster: c, skill: skill, lease: lease,
		center: rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now), reach: float64(area.Radius) + radius, now: now}
	var out []combatTarget
	for _, candidate := range rt.areaCandidates(q) {
		out = append(out, candidate.target)
		if len(out) == int(area.MaxTargets) {
			break
		}
	}
	return out
}
