/*
===========================================================================

damagereturn.go - dmgr: a defender striking back at its attacker

CSkillManager_ProcessDamageEffects (5A0C2D) runs on the defender's skill
manager for every hit SkillCombat_CalculateHitOutcome resolves. The rule
is the learned passive's (+0x204) while the passive is enabled, else the
buff's (+0x208). This file owns the rule's arithmetic and the passive
projection; the runtime owns the roll, the range test and the commit.

===========================================================================
*/

package combat

import (
	"math"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// DamageReturnRollKey is the defender's probability key for the dmgr roll
// (CSkillManager_RollProbability 0x46000000).
const DamageReturnRollKey = 0x46000000

/*
================
DamageReturnApplies

5A0C2D's gate before the range test and the roll: the hit dealt damage
(+0x20) through at least one lane (+0x24 magical, +0x28 physical). A
blocked, missed or fixed-damage hit returns nothing.
================
*/
func DamageReturnApplies(rule enterworld.SkillDamageReturn, hit Result) bool {
	return rule.Present && hit.Damage != 0 && (hit.PhysicalDamage != 0 || hit.MagicalDamage != 0)
}

/*
================
DamageReturnInRange

5A0C2D: the rule's range word must exceed the 3D distance between the two
bodies (an equal distance fails, as the x87 compare's C0|C3 mask does).
================
*/
func DamageReturnInRange(rule enterworld.SkillDamageReturn, distance float64) bool {
	return float64(rule.Range) > distance
}

/*
================
ReturnedDamage

5A0C2D: trunc(magical% / 100 * magical lane) + trunc(physical% / 100 *
physical lane), each truncated under control word | 0xC00 and summed as
dwords.
================
*/
func ReturnedDamage(rule enterworld.SkillDamageReturn, hit Result) uint32 {
	magical := uint32(int64(math.Trunc(float64(rule.Magical) / 100 * float64(hit.MagicalDamage))))
	physical := uint32(int64(math.Trunc(float64(rule.Physical) / 100 * float64(hit.PhysicalDamage))))
	return magical + physical
}

/*
================
learnedDamageReturn

The highest learned rank's dmgr and its skill, when its reqi admits the
equipment: the 59F0E0 walk is what enables the passive instance (+0x10 ==
1) 5A0C2D tests.
================
*/
func learnedDamageReturn(c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource) (enterworld.SkillDamageReturn, uint32) {
	if skills == nil {
		return enterworld.SkillDamageReturn{}, 0
	}
	current := learnedGroupRanks(c, skills)
	for _, id := range c.Skills {
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		}
		if rank, exists := current[row.Group]; !exists || rank.id != id {
			continue
		}
		p := row.PassiveParameters
		if !p.Pinned || !p.DamageReturn.Present || row.ChainSub ||
			row.Reqi.Present && ReqiRefusal(c, items, row.Reqi) != 0 {
			continue
		}
		return p.DamageReturn, row.ID
	}
	return enterworld.SkillDamageReturn{}, 0
}
