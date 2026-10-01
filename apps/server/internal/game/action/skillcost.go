/*
===========================================================================

skillcost.go - skill MP costs and cooldowns

Checks run from Skill_ValidatePrerequisitesAndCost (58D8F0); the amount
actually charged is the prepared snapshot (58312C / 5867DC), which later
regeneration or drain must not change.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
===============================================================================

CHECKS

===============================================================================
*/

/*
==================
resourceCostAt

The admission MP check: flat plus a percent of maximum MP against the
current gauge. 58E2B1 compares strictly, so exact MP is enough. A row
without parsed consumption is refused 0x3003.
==================
*/
func resourceCostAt(currentMP, maxMP int64, skill enterworld.SkillRow) (int64, uint16) {
	if !skill.Consumption.Pinned || skill.Group == 0 {
		return 0, 0x3003
	}
	cost := int64(skill.Consumption.MP) + maxMP*int64(skill.Consumption.MPPercent)/100
	if cost > currentMP {
		return 0, 0x3004
	}
	return cost, 0
}

/*
==================
hpCostRefusal

58E1B6..58E292: the HP cost is the flat word plus the percent of maximum
HP (truncated); current HP below it refuses 0x3013. The HP check runs
before the MP check.
==================
*/
func hpCostRefusal(currentHP, maxHP int64, skill enterworld.SkillRow) uint16 {
	cost := int64(skill.Consumption.HP)
	if skill.Consumption.HPPercent != 0 {
		cost += int64(crtFtol(float64(int32(maxHP)) * float64(skill.Consumption.HPPercent) / 100))
	}
	if cost > 0 && currentHP < cost {
		return 0x3013
	}
	return 0
}

/*
================
offensiveResourceCost

offensiveResourceCost is the HP check, then resourceCostAt on the
caster's keeper maximum (param 4) and stored current MP.
================
*/
func (rt *Runtime) offensiveResourceCost(division string, c *enterworld.Character, skill enterworld.SkillRow) (int64, uint16) {
	maxHP, maxMP, currentHP, currentMP := rt.playerKeeperVitals(division, c)
	if refusal := hpCostRefusal(currentHP, maxHP, skill); refusal != 0 {
		return 0, refusal
	}
	return resourceCostAt(currentMP, maxMP, skill)
}

/*
================
offensiveCost

offensiveCost is the full check for a new cast: data, cooldown, then MP.
================
*/
func (rt *Runtime) offensiveCost(division string, c *enterworld.Character, skill enterworld.SkillRow, nowMs int64) (int64, uint16) {
	if !skill.Consumption.Pinned || skill.Group == 0 || nowMs < 0 {
		return 0, 0x3003
	}
	if skillCoolingDown(c, skill, nowMs) {
		return 0, 0x3005
	}
	return rt.offensiveResourceCost(division, c, skill)
}

/*
==================
skillCoolingDown

CCooltimeManager_IsCooldownAvailable (64C1A0), negated. A nonzero shared
group (ref+8F) reads the group map, otherwise the skill's own entry. The
deadline itself is already available.
==================
*/
func skillCoolingDown(c *enterworld.Character, skill enterworld.SkillRow, nowMs int64) bool {
	return skillCooldownDeadline(c, skill) > nowMs
}

/*
==================
skillCooldownDeadline

When the skill's cooldown entry expires (0 when none applies). 64C1CD
bypasses both maps for an authored zero duration; a shared group selects
its own map and does not also consult the skill map.
==================
*/
func skillCooldownDeadline(c *enterworld.Character, skill enterworld.SkillRow) int64 {
	if skill.CoolTimeMs == 0 {
		return 0
	}
	if skill.CoolTimeGroup != 0 {
		return c.SharedSkillCooldowns[skill.CoolTimeGroup]
	}
	return c.OffensiveSkillCooldowns[skill.Group]
}

/*
==================
offensivePhaseCost

The check and the amount for one phase of a cast. A released cast
(prepared != nil) rechecks MP but not the cooldown its own preparation
installed, and is charged its prepared snapshot. A fresh cast is checked
in full and charged a new snapshot.
==================
*/
func (rt *Runtime) offensivePhaseCost(division string, c *enterworld.Character, skill enterworld.SkillRow, now int64, prepared *pendingProjectileCast) (skillCharge, uint16) {
	if prepared != nil {
		if _, refusal := rt.offensiveResourceCost(division, c, skill); refusal != 0 {
			return skillCharge{}, refusal
		}
		return prepared.executionCost, 0
	}

	if _, refusal := rt.offensiveCost(division, c, skill, now); refusal != 0 {
		return skillCharge{}, refusal
	}
	cost, err := rt.preparedExecutionMPCost(division, c, skill)
	if err != nil {
		return skillCharge{}, 0x3003
	}
	return skillCharge{mp: cost, hp: rt.preparedExecutionHPCost(division, c, skill)}, 0
}

/*
================
skillCharge

skillCharge is the prepared snapshot 58312C stores on the context: HP at
+0x10, MP at +0x14. Release charges it as prepared.
================
*/
type skillCharge struct{ mp, hp int64 }

/*
================
preparedExecutionHPCost

preparedExecutionHPCost is 58312C..5831A9: the flat word plus the percent
of CURRENT HP (the admission used maximum HP), truncated.
================
*/
func (rt *Runtime) preparedExecutionHPCost(division string, c *enterworld.Character, skill enterworld.SkillRow) int64 {
	cost := int64(skill.Consumption.HP)
	if skill.Consumption.HPPercent != 0 {
		_, _, currentHP, _ := rt.playerKeeperVitals(division, c)
		cost += int64(crtFtol(float64(int32(currentHP)) * float64(skill.Consumption.HPPercent) / 100))
	}
	return cost
}

/*
===============================================================================

PREPARED SNAPSHOT

===============================================================================
*/

/*
==================
preparedExecutionMPCost

The charged amount (combat.PreparedCost), then the caster's MP Decrease
cuts. Instant and persistent casts take the cuts; SkillAction_Projectile,
picked by RefSkill+0x168 in SkillActionHandler (589B50), does not.
==================
*/
func (rt *Runtime) preparedExecutionMPCost(division string, c *enterworld.Character, skill enterworld.SkillRow) (int64, error) {
	_, _, _, currentMP := rt.playerKeeperVitals(division, c)
	cost := combat.PreparedCost(uint32(currentMP), skill.Consumption.MP,
		skill.Consumption.MPPercent, skill.TimedEffect.Pinned, true, 100)
	if skill.ActionHandler == enterworld.SkillActionProjectile {
		return int64(cost), nil
	}

	stats, _, err := rt.playerCombatStats(division, c)
	if err != nil {
		return 0, err
	}
	return int64(combat.ApplyMPDecrease(cost, skill.Attack.Parameters, stats.SkillParameters)), nil
}

/*
===============================================================================

CHARGING

===============================================================================
*/

/*
================
commitOffensivePhaseCost

commitOffensivePhaseCost charges a phase: a released cast pays MP only,
a fresh cast also starts its cooldown.
================
*/
func (rt *Runtime) commitOffensivePhaseCost(division string, c *enterworld.Character, skill enterworld.SkillRow, cost skillCharge, now int64, prepared bool) {
	if prepared {
		rt.commitOffensiveResources(division, c, cost)
		return
	}
	rt.commitOffensiveCost(division, c, skill, cost, now)
}

/*
================
commitOffensiveCost

================
*/
func (rt *Runtime) commitOffensiveCost(division string, c *enterworld.Character, skill enterworld.SkillRow, cost skillCharge, nowMs int64) {
	rt.commitOffensiveResources(division, c, cost)
	rt.registerPlayerSkillCooldown(division, c, skill, nowMs)
}

/*
================
commitOffensiveResources

commitOffensiveResources is 593558 -> 4A8770: MP through ConsumeMana, HP
clamped to leave 1 (a skill's HP cost never kills).
================
*/
func (rt *Runtime) commitOffensiveResources(division string, c *enterworld.Character, cost skillCharge) {
	_, maxMP, currentHP, currentMP := rt.playerKeeperVitals(division, c)
	mp := int64(combat.ConsumeMana(uint32(currentMP), uint32(maxMP), int32(cost.mp)))
	c.CurrentMP = &mp
	if hp := max(0, cost.hp); hp != 0 {
		if hp >= currentHP {
			hp = max(0, currentHP-1)
		}
		left := currentHP - hp
		c.CurrentHP = &left
	}
}

/*
==================
registerOffensiveCooldown

Drops expired entries, then starts the skill's cooldown. A nonzero ref+8F
also starts the shared group's (64C862 / 64CC20).
==================
*/
func registerOffensiveCooldown(c *enterworld.Character, skill enterworld.SkillRow, nowMs int64) {
	if c.OffensiveSkillCooldowns == nil {
		c.OffensiveSkillCooldowns = map[uint32]int64{}
	}
	for group, until := range c.OffensiveSkillCooldowns {
		if until <= nowMs {
			delete(c.OffensiveSkillCooldowns, group)
		}
	}
	for group, until := range c.SharedSkillCooldowns {
		if until <= nowMs {
			delete(c.SharedSkillCooldowns, group)
		}
	}
	if skill.CoolTimeMs == 0 {
		return
	}

	until := nowMs + int64(skill.CoolTimeMs)
	c.OffensiveSkillCooldowns[skill.Group] = until
	if skill.CoolTimeGroup != 0 {
		if c.SharedSkillCooldowns == nil {
			c.SharedSkillCooldowns = map[uint8]int64{}
		}
		c.SharedSkillCooldowns[skill.CoolTimeGroup] = until
	}
}

/*
================
offensiveRefusal

offensiveRefusal is the cast result (wire.OpSkillCastResult) {2, code}.
================
*/
func offensiveRefusal(code uint16) OpResult {
	payload := wire.NewWriter(2).U8(2).U8(uint8(code)).Payload()
	return OpResult{Frames: []wire.Frame{{Opcode: wire.OpSkillCastResult, Payload: payload}}}
}
