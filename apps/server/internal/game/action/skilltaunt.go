/*
===========================================================================

skilltaunt.go - damage-free Warrior aggression actions

Selection and resource admission precede a single cast commit. The hostility
owner changes monster targets without inventing damage or reward credit.

===========================================================================
*/

package action

import (
	"strconv"
	"sync/atomic"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
tauntCast

The targeted variant enters through ordinary offensive approach admission.
The caster-centered variant deliberately carries no fabricated primary.
================
*/
type tauntCast struct {
	division            string
	character, snapshot *enterworld.Character
	skill               enterworld.SkillRow
	primary             monster.Instance
	now                 int64
}

/*
================
acceptUntargetedTaunt

The same execution mask checks costs, cooldown, equipment and caster state
as the targeted attack owner. No selection is inferred from a stale UI target.
================
*/
func (rt *Runtime) acceptUntargetedTaunt(c tauntCast, cast wire.SkillAction) OpResult {
	c.now = rt.Now().UnixMilli()
	if cast.HasTarget || cast.HasGroundTarget || !c.skill.Threat.Only || c.skill.TargetRequired ||
		!enterworld.CharacterAlive(c.snapshot) || !enterworld.SkillLearned(c.snapshot, c.skill.ID) || rt.Monsters == nil {
		return offensiveRefusal(0x3011)
	}
	if rt.skillCastPostureBlocked(c.division, c.snapshot, c.now) || rt.hasOpenSkillCast(c.division, c.snapshot.Name) {
		return offensiveRefusal(0x3009)
	}
	if code := rt.skillAdmission(c.division, c.snapshot, c.skill, c.now, nil, nil, admitExecution); code != 0 {
		return offensiveRefusal(code)
	}
	out, _ := rt.releaseTaunt(c)
	return out
}

/*
================
tauntVictims

EFR shape one samples the caster's population and body-expanded sphere.
Shape two uses the shared primary-centered selector and target ordering.
================
*/
func (rt *Runtime) tauntVictims(c tauntCast) []monster.Instance {
	area := c.skill.Threat.Area
	if c.skill.TargetRequired {
		return rt.areaVictims(c.division, c.snapshot, c.primary, area, float32(c.skill.ActionRange), c.now)
	}
	radius, ok := rt.deps.CharacterBodyRadius(c.snapshot)
	if !ok {
		return nil
	}
	from := rt.liveSpawn(simulation.WorldKey(c.division, c.snapshot.Name), c.snapshot, c.now)
	world := instance.ID(domain.CharacterWorldInstance(c.snapshot))
	candidates := rt.Monsters.CombatCandidatesInWorld(c.division, world, from, float64(area.Radius)+radius, c.now, false)
	var out []monster.Instance
	for _, candidate := range candidates {
		if _, allowed := rt.characterMonster(c.division, c.snapshot, candidate.Gid); !allowed {
			continue
		}
		out = append(out, candidate)
		if len(out) == int(area.MaxTargets) {
			break
		}
	}
	return out
}

/*
================
tauntAggression

Read the equipped weapon's unrounded physical pair. An empty slot contributes
zero; malformed equipment cannot turn a failed calculation into free threat.
================
*/
func (rt *Runtime) tauntAggression(c tauntCast) (uint32, bool) {
	stats, _, err := rt.playerCombatStats(c.division, c.snapshot)
	if err != nil {
		return 0, false
	}
	var low, high float32
	for _, item := range c.snapshot.MissionInventory {
		if item.Slot != 6 {
			continue
		}
		ref, found := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
		if !found || ref.Combat == nil {
			return 0, false
		}
		bits, err := strconv.ParseUint(item.VarianceBits, 10, 64)
		if err != nil {
			return 0, false
		}
		low, high = combat.WeaponPhysicalAttack(ref, bits, uint8(max(0, min(item.Plus, 255))))
		break
	}
	return combat.TauntAggression(stats, low, high, c.skill.Threat), true
}

/*
================
releaseTaunt

58E5F0 emits a normal successful result with zero HP damage. Aggression is
committed through the shared link-aware hostility owner after resource debit.
================
*/
func (rt *Runtime) releaseTaunt(c tauntCast) (OpResult, skillCastDecision) {
	victims := rt.tauntVictims(c)
	aggression, valid := rt.tauntAggression(c)
	if !valid {
		return OpResult{}, skillCastRefused
	}
	var refusal uint16
	if !rt.deps.Update(c.character, "skill-taunt", func() bool {
		if !enterworld.CharacterAlive(c.character) || !enterworld.SkillLearned(c.character, c.skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(c.division, c.character, c.skill, c.now, nil)
		refusal = code
		if code != 0 {
			return false
		}
		rt.startSkillCast(c.division, c.character, c.now)
		rt.commitOffensivePhaseCost(c.division, c.character, c.skill, cost, c.now, false)
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal), skillCastRefused
		}
		return OpResult{}, skillCastRefused
	}
	gid := enterworld.ObjectIDForCharacter(c.snapshot)
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	cast := wire.SkillCastSuccess{SkillId: c.skill.ID, CasterGid: gid, InstanceToken: token}
	var targets []wire.SkillAreaTarget
	for _, target := range victims {
		rt.commitAggression(c.division, target.Gid, simulation.HostilityEvent{Attacker: gid, Aggression: aggression}, c.now)
		targets = append(targets, wire.SkillAreaTarget{GID: target.Gid, Impacts: []wire.SkillCastTargetImpact{{ResultFlags: 1}}})
	}
	open := wire.SkillCastUntargetedFrame(cast)
	if len(targets) != 0 {
		if c.skill.TargetRequired {
			open = wire.SkillCastAreaFrame(cast, targets[0].GID, targets)
		} else {
			open = wire.SkillCastUntargetedAreaFrame(cast, targets)
		}
	}
	rt.queueSkillFinalize(c.division, c.snapshot.Name, gid, c.now, wire.SkillCastReleaseFrame(token, c.primary.Gid))
	rt.queueSkillCastClose(c.division, c.snapshot.Name, gid, token, c.skill, 0, c.now+int64(c.skill.ActionDurationMs))
	vitals := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceSkillRecovery, rt.publishedVitals(c.division, c.character))}
	return OpResult{Frames: []wire.Frame{open, vitals}, Broadcast: []wire.Frame{open}, ActorPrivate: []wire.Frame{vitals}}, skillCastAccepted
}
